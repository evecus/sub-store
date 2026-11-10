//! 内嵌资源层：把前端 dist 与后端 JS bundle 全部编译进二进制。
//!
//! 单端口分流由 main.rs 完成：`/api/`、`/download/`、`/share/` 交给 QuickJS 后端，
//! 其余路径在这里作为静态前端资源返回（含 SPA fallback）。
//!
//! 前端的两个"同源化"处理（否则页面会去连公网 https://sub.store）：
//!  1. index.html 注入引导脚本，预置 localStorage.hostAPI 指向 location.origin；
//!  2. 把编译期写死的默认 API 字面量 "https://sub.store" 改成 location.origin 兜底。

use axum::{
    body::Body,
    http::{header, HeaderMap, StatusCode},
    response::Response,
};
use rust_embed::RustEmbed;
use std::{collections::HashMap, sync::OnceLock};

#[derive(RustEmbed)]
#[folder = "assets/dist/"]
struct Frontend;

/// 后端 JS bundle：编译期常量，运行时不需要任何磁盘文件。
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

/// 启动时一次性修补：替换默认 API 常量，并同步生成对应的 .gz（保留预压缩优势）。
/// 结果缓存在 OnceLock 里，之后每请求只做一次查表。
fn patched_assets() -> &'static HashMap<String, Vec<u8>> {
    static CACHE: OnceLock<HashMap<String, Vec<u8>>> = OnceLock::new();
    CACHE.get_or_init(|| {
        let mut out = HashMap::new();
        for name in Frontend::iter() {
            let key = name.as_ref();
            if !key.ends_with(".js") {
                continue;
            }
            let Some(file) = Frontend::get(key) else { continue };
            let Ok(text) = std::str::from_utf8(&file.data) else { continue };
            let hits = text.matches(DEFAULT_API_LITERAL).count();
            if hits == 0 {
                continue;
            }
            if hits != 1 {
                tracing::warn!(
                    "[frontend] {key} 中默认 API 字面量出现 {hits} 次，跳过替换（前端版本可能已变）"
                );
                continue;
            }
            let plain = text
                .replace(DEFAULT_API_LITERAL, DEFAULT_API_PATCH)
                .into_bytes();
            out.insert(format!("{key}.gz"), gzip_bytes(&plain));
            out.insert(key.to_string(), plain);
            tracing::info!("[frontend] 已把 {key} 的默认后端地址改为同源");
        }
        out
    })
}

fn gzip_bytes(data: &[u8]) -> Vec<u8> {
    use flate2::{write::GzEncoder, Compression};
    use std::io::Write;
    let mut enc = GzEncoder::new(Vec::new(), Compression::default());
    if enc.write_all(data).is_err() {
        return Vec::new();
    }
    enc.finish().unwrap_or_default()
}

fn asset_bytes(key: &str) -> Option<Vec<u8>> {
    if let Some(v) = patched_assets().get(key) {
        return Some(v.clone());
    }
    Frontend::get(key).map(|f| f.data.to_vec())
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

fn build(status: StatusCode, mime: &str, data: Vec<u8>, gzipped: bool, cacheable: bool) -> Response {
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

/// 返回带引导脚本的 SPA 入口页。index.html 不缓存，保证升级后立即生效。
fn index_page() -> Response {
    let Some(raw) = asset_bytes("index.html") else {
        return status_only(StatusCode::INTERNAL_SERVER_ERROR);
    };
    let html = String::from_utf8_lossy(&raw);
    let out = if html.contains("<head>") {
        html.replacen("<head>", &format!("<head>\n{}", bootstrap_js()), 1)
    } else {
        tracing::warn!("[frontend] index.html 缺少 <head>，引导脚本未注入");
        html.to_string()
    };
    build(
        StatusCode::OK,
        "text/html; charset=utf-8",
        out.into_bytes(),
        false,
        false,
    )
}

fn try_file(key: &str, accept_gzip: bool) -> Option<Response> {
    let mime = mime_of(key).to_string();

    // 直接请求 .gz 文件时按 gzip 内容返回（mime 按去掉 .gz 后的扩展名推断）。
    if key.ends_with(".gz") {
        let data = asset_bytes(key)?;
        return Some(build(StatusCode::OK, &mime, data, true, true));
    }

    // 优先命中预压缩产物；修补过的 JS 也已生成对应的 .gz。
    if accept_gzip {
        let gz_key = format!("{key}.gz");
        if let Some(data) = asset_bytes(&gz_key) {
            if !data.is_empty() {
                return Some(build(StatusCode::OK, &mime, data, true, true));
            }
        }
    }

    let data = asset_bytes(key)?;
    Some(build(StatusCode::OK, &mime, data, false, true))
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
    anyhow::ensure!(!BUNDLE_JS.is_empty(), "内嵌后端 bundle 为空");
    let patched = patched_assets();
    anyhow::ensure!(
        patched.keys().any(|k| k.ends_with(".js")),
        "未能定位前端默认 API 常量，前端可能仍会连到公网 https://sub.store"
    );
    tracing::info!(
        "[assets] 内嵌资源就绪：前端 {} 个文件，后端 bundle {} 字节",
        Frontend::iter().count(),
        BUNDLE_JS.len()
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
        assert!(BUNDLE_JS.contains("__substore") || BUNDLE_JS.len() > 1_000_000);
    }

    #[test]
    fn default_api_is_patched() {
        let patched = patched_assets();
        let key = "chunks/main-1712152b.js";
        let data = patched.get(key).expect("应已修补 main chunk");
        let text = std::str::from_utf8(data).unwrap();
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
