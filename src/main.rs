mod assets;
mod bundle;
mod host;
mod plugins;
mod rust_tasks;
mod store;

use anyhow::{Context as _, Result};
use axum::{
    body::{to_bytes, Body},
    extract::{Request, State},
    http::{header, HeaderName, HeaderValue, StatusCode},
    response::Response,
    routing::get,
    Router,
};
use host::{Job, JobTx, JsRequest};
use std::{collections::HashMap, env, path::PathBuf, sync::OnceLock, time::Duration};
use tokio::{
    net::TcpListener,
    sync::{mpsc, oneshot},
};
use tracing_subscriber::EnvFilter;

// 实测 mimalloc 在本场景反而增加约 20MB 私有脏页，默认用系统分配器。
// 需要在别的负载上对比时可 `cargo build --release --features mimalloc`。
#[cfg(feature = "mimalloc")]
#[global_allocator]
static ALLOC: mimalloc::MiMalloc = mimalloc::MiMalloc;

fn main() -> Result<()> {
    // 单用户盒子上 1 个 worker 足够（JS 本来就是单线程串行，网络 I/O 都是异步的）；
    // 线程栈与 per-thread 分配器缓存都是实打实的内存开销。需要更高并发再用环境变量调大。
    let workers = env::var("SUB_STORE_TOKIO_WORKERS")
        .ok()
        .and_then(|v| v.parse::<usize>().ok())
        .filter(|n| (1..=16).contains(n))
        .unwrap_or(1);
    tokio::runtime::Builder::new_multi_thread()
        .worker_threads(workers)
        .enable_all()
        .build()?
        .block_on(run())
}

async fn run() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .init();

    let data_dir = PathBuf::from(
        env::var("SUB_STORE_DATA_DIR").unwrap_or_else(|_| "./data".into()),
    );
    std::fs::create_dir_all(&data_dir)?;

    // 内嵌路径直接用预编译字节码（跳过 3.3MB 源码解析，也不把源码搬进堆）；
    // 只有开发时外部覆盖 bundle 才需要退回源码编译。
    let source: Option<&'static str> = match env::var("SUB_STORE_BUNDLE") {
        Ok(p) => {
            let s = std::fs::read_to_string(&p)
                .with_context(|| format!("读取外部 bundle 失败: {p}"))?;
            tracing::info!("使用外部 bundle {p} ({} 字节)，走源码编译路径", s.len());
            Some(Box::leak(s.into_boxed_str()))
        }
        Err(_) if env::var_os("SUB_STORE_FORCE_SOURCE").is_some() => {
            // 换 CPU 架构或升级 rquickjs 后字节码可能不兼容，用这个开关临时降级。
            // 重新生成：cargo run --bin gen_bytecode
            tracing::warn!(
                "强制源码编译路径（{} 字节），冷启动会变慢",
                assets::BUNDLE_JS.len()
            );
            Some(assets::BUNDLE_JS)
        }
        Err(_) => {
            tracing::info!(
                "使用内嵌预编译字节码 ({} 字节)",
                assets::BUNDLE_BYTECODE.len()
            );
            None
        }
    };
    assets::self_check()?;

    // ---- Rust 侧重写的三块能力（不依赖 Node，不透传代理）----
    let dns = std::sync::Arc::new(rust_tasks::RustDns::from_env());
    tracing::info!("[dns] Rust 侧解析器: {:?}", dns.servers());
    let geoip = std::sync::Arc::new(rust_tasks::RustGeoIp::open(
        env::var("SUB_STORE_MMDB_COUNTRY_PATH").ok().map(PathBuf::from),
        env::var("SUB_STORE_MMDB_ASN_PATH").ok().map(PathBuf::from),
    )?);
    tracing::info!("[geoip] mmdb 已按需加载");

    // QuickJS 的 Runtime/Context 不是 Send/Sync，必须固定在一个专用线程里。
    // axum / 定时任务只持有 mpsc 发送端，请求通过 Job 投递过去，天然串行。
    let (tx, rx) = mpsc::channel::<Job>(64);
    let (ready_tx, ready_rx) = oneshot::channel::<Result<()>>();
    let thread_source = source;
    let thread_data_dir = data_dir.clone();
    let thread_dns = dns.clone();
    let thread_geoip = geoip.clone();
    let thread_cors = cors_config().map(str::to_string);

    std::thread::Builder::new()
        .name("quickjs-host".into())
        .spawn(move || {
            let rt = tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .expect("创建 QuickJS 线程 runtime 失败");
            rt.block_on(async move {
                match host::JsHost::new(
                    thread_source,
                    &thread_data_dir,
                    thread_dns,
                    thread_geoip,
                    thread_cors,
                )
                .await
                {
                    Ok(js_host) => {
                        let _ = ready_tx.send(Ok(()));
                        let mut rx = rx;
                        while let Some(job) = rx.recv().await {
                            let resp = js_host.dispatch(job.req).await;
                            let _ = job.reply.send(resp);
                        }
                    }
                    Err(e) => {
                        let _ = ready_tx.send(Err(e));
                    }
                }
            });
        })
        .context("启动 QuickJS 专用线程失败")?;

    ready_rx.await.context("QuickJS 宿主线程无响应")??;
    tracing::info!("QuickJS 宿主就绪：未加载 Node.js，未启用任何代理");
    tracing::info!(
        "[cors] {}",
        if cors_config().is_some() {
            "跨域白名单模式（保留 Origin）"
        } else {
            "内嵌同源模式（剥离 Origin）"
        }
    );

    let cron_path =
        env::var("SUB_STORE_SYNC_PATH").unwrap_or_else(|_| "/api/utils/env".into());
    let cron_every = Duration::from_secs(
        env::var("SUB_STORE_SYNC_INTERVAL")
            .ok()
            .and_then(|v| v.parse().ok())
            .unwrap_or(300),
    );
    let cron = rust_tasks::spawn_sync_scheduler(tx.clone(), cron_path.clone(), cron_every);
    tracing::info!("[cron] Rust 调度器已启动: 每 {cron_every:?} 触发 {cron_path}");

    // 真实服务里这两个能力通过 host function 暴露给 JS；此处先完成初始化自检。
    let _ = (dns, geoip);

    let app = Router::new()
        .route("/health", get(|| async { "ok" }))
        .fallback(dispatch)
        .with_state(tx);

    match backend_prefix() {
        Some(p) => {
            tracing::info!("[auth] 后端仅在密钥路径 {p} 下可达（/api、/download、/share 均需带此前缀）");
            if p.len() < 12 {
                tracing::warn!("[auth] 密钥路径只有 {} 个字符，容易被猜到；建议 16 位以上随机串", p.len() - 1);
            }
        }
        None => tracing::warn!(
            "[auth] 未设置 SUB_STORE_FRONTEND_BACKEND_PATH：后端挂在根路径，任何能访问该端口的人都能读写订阅"
        ),
    }

    let addr = env::var("SUB_STORE_BIND").unwrap_or_else(|_| "127.0.0.1:3000".into());
    let listener = TcpListener::bind(&addr).await?;
    tracing::info!("单端口就绪：网页与 API 共用 http://{addr}");
    axum::serve(listener, app).await?;
    cron.abort();
    Ok(())
}

/// 后端只占这三个前缀（与前端 sw.js 的 NavigationRoute denylist 一致）。
/// 其余路径全部由内嵌前端处理，`/` 不再走上游那个 302 跳 Vercel 的分支。
fn is_backend_path(p: &str) -> bool {
    ["/api/", "/download/", "/share/"]
        .iter()
        .any(|prefix| p.starts_with(prefix))
        || matches!(p, "/api" | "/download" | "/share")
}

/// 后端路径前缀（上游同名环境变量 SUB_STORE_FRONTEND_BACKEND_PATH）。
///
/// 设置为 `/你的密钥路径` 后，/api、/download、/share 只在该前缀下可达
/// （如 `/你的密钥路径/api/subs`），根路径下的后端接口一律 404；前端静态页仍在 `/`。
/// 这个路径就是访问密码：首次打开前端会弹出“后端连接配置”，填入该路径即可，
/// 订阅链接也是 `https://host/你的密钥路径/download/名称`。
/// 未设置或设为 `/` 时保持旧行为（后端挂在根路径，无任何保护）。
pub fn backend_prefix() -> Option<&'static str> {
    static PREFIX: OnceLock<Option<String>> = OnceLock::new();
    PREFIX
        .get_or_init(|| {
            let raw = env::var("SUB_STORE_FRONTEND_BACKEND_PATH").ok()?;
            let p = raw.trim().trim_end_matches('/');
            if p.is_empty() {
                return None; // "" 或 "/"
            }
            if !p.starts_with('/') {
                // 与上游一致：必须以 / 开头。这里直接退出，避免静默失去保护。
                eprintln!("SUB_STORE_FRONTEND_BACKEND_PATH 必须以 \"/\" 开头，当前: {raw:?}");
                std::process::exit(2);
            }
            Some(p.to_string())
        })
        .as_deref()
}

/// `path` 以 `prefix` 作为完整路径段开头时，返回去掉前缀后的剩余部分（以 / 开头）。
fn strip_prefix_segment<'a>(path: &'a str, prefix: &str) -> Option<&'a str> {
    let rest = path.strip_prefix(prefix)?;
    (rest.is_empty() || rest.starts_with('/')).then_some(rest)
}

/// 可选跨域白名单（上游 utils/cors.js 的 `cors=` 参数）。
/// 未设置时走内嵌同源模式：前端与 API 同端口，浏览器不做 CORS 校验，
/// Rust 边界直接剥离 Origin，避免上游默认白名单误伤同源的 PATCH/POST。
fn cors_config() -> Option<&'static str> {
    static CORS: OnceLock<Option<String>> = OnceLock::new();
    CORS.get_or_init(|| env::var("SUB_STORE_CORS_ORIGINS").ok())
        .as_deref()
}

/// 单端口统一入口：API → QuickJS；其它 → 内嵌静态前端（含 SPA fallback）。
async fn dispatch(State(tx): State<JobTx>, mut req: Request) -> Response {
    let backend = match backend_prefix() {
        // 密钥路径模式：只有带前缀的后端请求才会进 JS，JS 看到的是去掉前缀后的路径。
        Some(prefix) => {
            if let Some(rest) = strip_prefix_segment(req.uri().path(), prefix) {
                if !is_backend_path(rest) {
                    return text(StatusCode::NOT_FOUND, "not found".into());
                }
                let new_uri = match req.uri().query() {
                    Some(q) => format!("{rest}?{q}"),
                    None => rest.to_string(),
                };
                match new_uri.parse() {
                    Ok(u) => *req.uri_mut() = u,
                    Err(_) => return text(StatusCode::BAD_REQUEST, "bad uri".into()),
                }
                true
            } else if is_backend_path(req.uri().path()) {
                // 不带前缀直接访问 /api 等：和不存在一样，不泄露任何信息。
                return text(StatusCode::NOT_FOUND, "not found".into());
            } else {
                false
            }
        }
        None => is_backend_path(req.uri().path()),
    };
    if backend {
        if cors_config().is_none() {
            req.headers_mut().remove(header::ORIGIN);
        }
        return proxy(State(tx), req).await;
    }
    // 静态分支不消费 req，直接借用即可，避免整份 HeaderMap 的克隆。
    assets::serve(req.uri().path(), req.headers(), req.method().as_str())
}

async fn proxy(State(tx): State<JobTx>, req: Request) -> Response {
    let (parts, body) = req.into_parts();
    let bytes = match to_bytes(body, 16 * 1024 * 1024).await {
        Ok(b) => b,
        Err(e) => return text(StatusCode::BAD_REQUEST, format!("读取请求体失败: {e}")),
    };

    let mut headers = HashMap::with_capacity(parts.headers.len());
    for (k, v) in parts.headers.iter() {
        headers.insert(k.to_string(), v.to_str().unwrap_or("").to_string());
    }

    // 非 Node 分支的 extractURL() 用 /https?:\/\/[^/]+(\/[^?]*)/ 提取 path，
    // 只认绝对 URL（QX/Surge 宿主传的就是完整 URL）。传相对路径会把所有请求都当成 "/"，
    // 命中上游那个 302 跳 Vercel 前端的重定向。
    let authority = parts
        .headers
        .get("host")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("127.0.0.1:3000");
    let url = format!("http://{authority}{}", parts.uri);

    let js_req = JsRequest {
        method: parts.method.to_string(),
        url,
        headers,
        body: String::from_utf8_lossy(&bytes).to_string(),
    };

    let (reply, reply_rx) = oneshot::channel();
    if tx
        .send(Job {
            req: js_req,
            reply,
        })
        .await
        .is_err()
    {
        return text(StatusCode::BAD_GATEWAY, "QuickJS 宿主已退出".into());
    }

    let result = match tokio::time::timeout(Duration::from_secs(35), reply_rx).await {
        Ok(Ok(r)) => r,
        Ok(Err(_)) => return text(StatusCode::BAD_GATEWAY, "QuickJS 响应通道关闭".into()),
        Err(_) => return text(StatusCode::GATEWAY_TIMEOUT, "QuickJS 处理超时".into()),
    };

    match result {
        Ok(resp) => {
            let mut builder = Response::builder()
                .status(StatusCode::from_u16(resp.status).unwrap_or(StatusCode::OK));
            for (k, v) in &resp.headers {
                if let (Ok(name), Ok(val)) = (
                    HeaderName::from_bytes(k.as_bytes()),
                    HeaderValue::from_str(v),
                ) {
                    builder = builder.header(name, val);
                }
            }
            builder
                .body(Body::from(resp.body))
                .unwrap_or_else(|_| text(StatusCode::INTERNAL_SERVER_ERROR, "响应构造失败".into()))
        }
        Err(e) => {
            tracing::error!("dispatch 失败: {e:#}");
            text(StatusCode::BAD_GATEWAY, format!("QuickJS dispatch 失败: {e}"))
        }
    }
}

fn text(status: StatusCode, body: String) -> Response {
    Response::builder()
        .status(status)
        .header("content-type", "text/plain; charset=utf-8")
        .body(Body::from(body))
        .unwrap()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn prefix_must_match_whole_segment() {
        assert_eq!(strip_prefix_segment("/sec/api/subs", "/sec"), Some("/api/subs"));
        assert_eq!(strip_prefix_segment("/sec", "/sec"), Some(""));
        assert_eq!(strip_prefix_segment("/secret/api/subs", "/sec"), None);
        assert_eq!(strip_prefix_segment("/api/subs", "/sec"), None);
    }

    #[test]
    fn backend_paths() {
        assert!(is_backend_path("/api/subs"));
        assert!(is_backend_path("/download/x"));
        assert!(!is_backend_path("/index.js"));
        assert!(!is_backend_path(""));
    }
}
