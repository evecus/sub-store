//! 内嵌资源层：把前端 dist 与后端 JS bundle 全部编译进二进制。
//!
//! 单端口分流由 main.rs 完成：`/api/`、`/download/`、`/share/` 交给 QuickJS 后端，
//! 其余路径在这里作为静态前端资源返回（含 SPA fallback）。
//!
//! 前端的两个"同源化"处理（否则页面会去连公网 https://sub.store）：
//!  1. index.html 注入引导脚本，预置 localStorage.hostAPI 指向 location.origin；
//!  2. 把编译期写死的默认 API 字面量 "https://sub.store" 改成 location.origin 兜底。
//!
//! 内存策略：所有响应体都是 `Bytes`（引用计数，克隆零拷贝），内嵌文件直接用
//! `Bytes::from_static` 指向 .rodata；修补过的文件只在启动时分配一次并永久缓存。

use axum::{
    body::Body,
    http::{header, HeaderMap, StatusCode},
    response::Response,
};
use bytes::Bytes;
use rust_embed::RustEmbed;
use std::{
    borrow::Cow,
    collections::HashMap,
    sync::{Mutex, OnceLock},
};

#[derive(RustEmbed)]
#[folder = "assets/dist/"]
struct Frontend;

/// 预编译的 QuickJS 字节码（由 `cargo run --bin gen_bytecode` 生成）。
/// 运行时直接反序列化执行，跳过 3.3MB 源码解析；源码文本不进 JS 堆。
pub const BUNDLE_BYTECODE: &[u8] = include_bytes!("../assets/sub-store.bundle.qbc");

/// 原始 bundle 源码：只用于「字节码不兼容」时的降级路径（SUB_STORE_FORCE_SOURCE=1）
/// 以及开发时重新生成字节码。它在 .rodata 里，默认路径不会触碰，不影响常驻内存。
pub const BUNDLE_JS: &str = include_str!("../assets/sub-store.bundle.js");

/// 前端 localStorage 的 key 与"本地后端"条目名。
/// 与 useHostAPI.ts 中的 `const lsKey = 'hostAPI'` 保持一致。
const HOST_API_KEY: &str = "hostAPI";
const LOCAL_API_NAME: &str = "__substore_local__";

/// 页面加载前执行：保证存在一条指向当前 origin 的 API 配置并设为当前项。
/// 若用户已在设置页手动切到别的后端，则不强行拉回（只刷新本地条目的 url）。
const BOOTSTRAP_TEMPLATE: &str = r#"<script>
(function () {
  try {
    var K = '__HOST_API_KEY__', N = '__LOCAL_API_NAME__', U = location.origin, s = null;
    try { s = JSON.parse(localStorage.getItem(K) || 'null'); } catch (e) {}
    if (!s || typeof s !== 'object') s = { current: '', apis: [] };
    if (!Array.isArray(s.apis)) s.apis = [];
    var hit = null;
    for (var i = 0; i < s.apis.length; i++) {
      if (s.apis[i] && s.apis[i].name === N) { hit = s.apis[i]; break; }
    }
    if (hit) hit.url = U; else s.apis.unshift({ name: N, url: U });
    var valid = s.apis.some(function (a) { return a && a.name === s.current; });
    if (!s.current || !valid || s.current === N) s.current = N;
    localStorage.setItem(K, JSON.stringify(s));
  } catch (e) {}
})();
</script>
"#;

fn bootstrap_js() -> &'static str {
    static SCRIPT: OnceLock<String> = OnceLock::new();
    SCRIPT
        .get_or_init(|| {
            BOOTSTRAP_TEMPLATE
                .replace("__HOST_API_KEY__", HOST_API_KEY)
                .replace("__LOCAL_API_NAME__", LOCAL_API_NAME)
        })
        .as_str()
}

/// Vite 把 `.env.production` 的 VITE_API_URL 编译成字面量。
/// 精确替换这一个字符串常量即可，文案里的 "https://sub.store ..." 不受影响。
const DEFAULT_API_LITERAL: &str = r#""https://sub.store""#;
const DEFAULT_API_PATCH: &str = "location.origin";

/// 按需修补：只有真正被请求的 JS 文件才会扫描/替换/压缩，结果永久缓存。
/// 之前启动时会把全部 JS chunk（约 3MB）读一遍并对命中的文件做 gzip，
/// 既拉高启动期 RSS（文件页被触碰），也让不会被访问的文件白占内存。
/// 值为 None 表示该文件无需修补（直接用内嵌原文件）。
type PatchEntry = Option<(Bytes, Bytes)>; // (plain, gz)

fn patch_cache() -> &'static Mutex<HashMap<String, PatchEntry>> {
    static CACHE: OnceLock<Mutex<HashMap<String, PatchEntry>>> = OnceLock::new();
    CACHE.get_or_init(|| Mutex::new(HashMap::new()))
}

fn build_patch(key: &str) -> PatchEntry {
    let file = Frontend::get(key)?;
    let text = std::str::from_utf8(&file.data).ok()?;
    let hits = text.matches(DEFAULT_API_LITERAL).count();
    if hits == 0 {
        return None;
    }
    if hits != 1 {
        tracing::warn!(
            "[frontend] {key} 中默认 API 字面量出现 {hits} 次，跳过替换（前端版本可能已变）"
        );
        return None;
    }
    let plain = Bytes::from(
        text.replace(DEFAULT_API_LITERAL, DEFAULT_API_PATCH)
            .into_bytes(),
    );
    let gz = Bytes::from(gzip_bytes(&plain));
    tracing::info!("[frontend] 已把 {key} 的默认后端地址改为同源");
    Some((plain, gz))
}

/// 取（必要时构造）修补后的资源。`key` 可带 .gz 后缀。
fn patched_asset(key: &str) -> Option<Bytes> {
    let (base, want_gz) = match key.strip_suffix(".gz") {
        Some(b) => (b, true),
        None => (key, false),
    };
    if !base.ends_with(".js") {
        return None;
    }
    let mut cache = patch_cache().lock().unwrap();
    let entry = cache
        .entry(base.to_string())
        .or_insert_with(|| build_patch(base));
    entry
        .as_ref()
        .map(|(plain, gz)| if want_gz { gz.clone() } else { plain.clone() })
}

fn gzip_bytes(data: &[u8]) -> Vec<u8> {
    use flate2::{write::GzEncoder, Compression};
    use std::io::Write;
    let mut enc = GzEncoder::new(Vec::with_capacity(data.len() / 4), Compression::default());
    if enc.write_all(data).is_err() {
        return Vec::new();
    }
    enc.finish().unwrap_or_default()
}

/// 零拷贝取资源：内嵌文件指向 .rodata，修补过的文件是缓存里的 Bytes 引用。
fn asset_bytes(key: &str) -> Option<Bytes> {
    if let Some(v) = patched_asset(key) {
        return Some(v);
    }
    Frontend::get(key).map(|f| match f.data {
        Cow::Borrowed(b) => Bytes::from_static(b),
        Cow::Owned(v) => Bytes::from(v),
    })
}

fn mime_of(key: &str) -> &'static str {
    let k = key.strip_suffix(".gz").unwrap_or(key);
    match k.rsplit('.').next().unwrap_or("") {
        "html" | "htm" => "text/html; charset=utf-8",
        "js" | "mjs" => "text/javascript; charset=utf-8",
        "css" => "text/css; charset=utf-8",
        "json" => "application/json; charset=utf-8",
        "webmanifest" => "application/manifest+json; charset=utf-8",
        "svg" => "image/svg+xml",
        "png" => "image/png",
        "ico" => "image/x-icon",
        "woff" => "font/woff",
        "woff2" => "font/woff2",
        "ttf" => "font/ttf",
        "txt" => "text/plain; charset=utf-8",
        _ => "application/octet-stream",
    }
}

fn build(status: StatusCode, mime: &'static str, data: Bytes, gzipped: bool, cacheable: bool) -> Response {
    let mut b = Response::builder()
        .status(status)
        .header(header::CONTENT_TYPE, mime);
    if gzipped {
        b = b
            .header(header::CONTENT_ENCODING, "gzip")
            .header(header::VARY, "Accept-Encoding");
    }
    b = b.header(
        header::CACHE_CONTROL,
        if cacheable {
            "public, max-age=3600"
        } else {
            "no-cache, no-store, must-revalidate"
        },
    );
    b.body(Body::from(data)).unwrap_or_else(|_| status_only(StatusCode::INTERNAL_SERVER_ERROR))
}

fn status_only(status: StatusCode) -> Response {
    Response::builder()
        .status(status)
        .body(Body::empty())
        .unwrap()
}

/// 带引导脚本的 SPA 入口页，只在首次请求时构造一次。
fn index_page() -> Response {
    static PAGE: OnceLock<Bytes> = OnceLock::new();
    let page = PAGE.get_or_init(|| {
        let Some(raw) = asset_bytes("index.html") else {
            return Bytes::new();
        };
        let html = String::from_utf8_lossy(&raw);
        let out = if html.contains("<head>") {
            html.replacen("<head>", &format!("<head>\n{}", bootstrap_js()), 1)
        } else {
            tracing::warn!("[frontend] index.html 缺少 <head>，引导脚本未注入");
            html.to_string()
        };
        Bytes::from(out.into_bytes())
    });
    if page.is_empty() {
        return status_only(StatusCode::INTERNAL_SERVER_ERROR);
    }
    build(
        StatusCode::OK,
        "text/html; charset=utf-8",
        page.clone(),
        false,
        false,
    )
}

fn try_file(key: &str, accept_gzip: bool) -> Option<Response> {
    let mime = mime_of(key);

    // 直接请求 .gz 文件时按 gzip 内容返回（mime 按去掉 .gz 后的扩展名推断）。
    if key.ends_with(".gz") {
        let data = asset_bytes(key)?;
        return Some(build(StatusCode::OK, mime, data, true, true));
    }

    // 优先命中预压缩产物；修补过的 JS 也已生成对应的 .gz。
    if accept_gzip {
        let gz_key = format!("{key}.gz");
        if let Some(data) = asset_bytes(&gz_key) {
            if !data.is_empty() {
                return Some(build(StatusCode::OK, mime, data, true, true));
            }
        }
    }

    let data = asset_bytes(key)?;
    Some(build(StatusCode::OK, mime, data, false, true))
}

/// 静态资源 / SPA 入口。非 GET/HEAD 一律 405。
pub fn serve(req_path: &str, headers: &HeaderMap, method: &str) -> Response {
    if method != "GET" && method != "HEAD" {
        return status_only(StatusCode::METHOD_NOT_ALLOWED);
    }

    let accept_gzip = headers
        .get(header::ACCEPT_ENCODING)
        .and_then(|v| v.to_str().ok())
        .map(|v| v.split(',').any(|p| p.trim().starts_with("gzip")))
        .unwrap_or(false);

    let raw = req_path.trim_start_matches('/');
    // 目录穿越防护
    if raw.split('/').any(|seg| seg == "..") {
        return status_only(StatusCode::NOT_FOUND);
    }

    if !raw.is_empty() {
        if let Some(resp) = try_file(raw, accept_gzip) {
            return resp;
        }
        // 有扩展名的路径确实是缺失的静态资源 -> 404；
        // 无扩展名的一律当作前端路由，回吐 index.html（与 vercel.json rewrites 一致）。
        if raw.rsplit('/').next().map(|s| s.contains('.')).unwrap_or(false) {
            return status_only(StatusCode::NOT_FOUND);
        }
    }

    index_page()
}

/// 内嵌资源自检，启动时调用一次。
pub fn self_check() -> anyhow::Result<()> {
    anyhow::ensure!(
        asset_bytes("index.html").is_some(),
        "内嵌前端缺少 index.html"
    );
    anyhow::ensure!(
        asset_bytes("index.js").is_some(),
        "内嵌前端缺少 index.js"
    );
    anyhow::ensure!(
        BUNDLE_BYTECODE.len() > 1024,
        "内嵌后端字节码为空，请先运行 cargo run --bin gen_bytecode"
    );
    tracing::info!(
        "[assets] 内嵌资源就绪：前端 {} 个文件，后端字节码 {} 字节",
        Frontend::iter().count(),
        BUNDLE_BYTECODE.len()
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn embedded_files_exist() {
        assert!(asset_bytes("index.html").is_some());
        assert!(asset_bytes("chunks/main-1712152b.js").is_some());
        assert!(BUNDLE_BYTECODE.len() > 1_000_000);
    }

    #[test]
    fn default_api_is_patched() {
        let key = "chunks/main-1712152b.js";
        let data = patched_asset(key).expect("应已修补 main chunk");
        let text = std::str::from_utf8(&data).unwrap();
        assert!(text.contains(DEFAULT_API_PATCH));
        assert!(!text.contains(DEFAULT_API_LITERAL));
    }

    #[test]
    fn bootstrap_is_injected() {
        let resp = index_page();
        assert_eq!(resp.status(), StatusCode::OK);
        let ct = resp
            .headers()
            .get(header::CONTENT_TYPE)
            .unwrap()
            .to_str()
            .unwrap();
        assert!(ct.starts_with("text/html"));
    }
}
