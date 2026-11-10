//! QuickJS 宿主：加载编译产物、注入最小全局、把每个 HTTP 请求转成一次 JS dispatch。
//!
//! 设计要点：
//!  - 绝不注入 `process`。`isNode = typeof process !== "undefined"` 是 Sub-Store 的总开关，
//!    一旦为 true 就会回落到真 express + node:http，QuickJS 里必然失败。
//!  - 伪装成 `isGUIforCores`（注入 `$Plugins`）：这是唯一用 Promise、参数结构化、
//!    且不带 Surge 遗留副作用的宿主分支。
//!  - 与 JS 之间只交换 JSON 字符串，避免大量值转换。

use anyhow::{anyhow, Context as _, Result};
use rquickjs::{
    function::Async, AsyncContext, AsyncRuntime, CatchResultExt, Ctx, Function, Module, Promise,
};
use serde::{Deserialize, Serialize};
use std::{
    collections::HashMap,
    sync::{
        atomic::{AtomicU32, Ordering},
        Arc,
    },
    time::Duration,
};

use crate::{
    plugins::{Cache, Http},
    rust_tasks::{RustDns, RustGeoIp},
};

#[derive(Debug, Serialize)]
pub struct JsRequest {
    pub method: String,
    pub url: String,
    pub headers: HashMap<String, String>,
    pub body: String,
}

#[derive(Debug, Deserialize, Serialize)]
pub struct JsResponse {
    #[serde(default)]
    pub status: u16,
    #[serde(default)]
    pub body: String,
    #[serde(default)]
    pub headers: HashMap<String, String>,
}

pub struct JsHost {
    // Runtime 必须保活；Context 只是它的一个视图。
    runtime: AsyncRuntime,
    context: AsyncContext,
    /// 每处理若干请求触发一次 GC，压住循环引用造成的长期堆增长。
    gc_counter: AtomicU32,
}

/// 每 64 次 dispatch 做一次完整 GC。间隔太小会拖慢，太大则堆会缓慢累积。
const GC_EVERY: u32 = 64;

/// QuickJS 的 Runtime/Context 不是 Send/Sync，必须固定在一个专用线程里。
/// axum 与定时任务只持有这个通道的发送端，通过 Job 把请求投递过去。
pub struct Job {
    pub req: JsRequest,
    pub reply: tokio::sync::oneshot::Sender<Result<JsResponse>>,
}

pub type JobTx = tokio::sync::mpsc::Sender<Job>;


const GLUE_JS: &str = r#"
globalThis.console = {
  log:   (...a) => __rustLog("log",   a.map(String).join(" ")),
  info:  (...a) => __rustLog("info",  a.map(String).join(" ")),
  warn:  (...a) => __rustLog("warn",  a.map(String).join(" ")),
  error: (...a) => __rustLog("error", a.map(String).join(" ")),
  debug: (...a) => __rustLog("debug", a.map(String).join(" "))
};

globalThis.__timers = new Map();
globalThis.__timerSeq = 0;
globalThis.setTimeout = (fn, ms) => {
  const id = ++globalThis.__timerSeq;
  globalThis.__timers.set(id, true);
  __rustSleep(Number(ms) || 0).then(() => {
    if (globalThis.__timers.has(id)) {
      globalThis.__timers.delete(id);
      try { fn(); } catch (e) { __rustLog("error", String(e)); }
    }
  });
  return id;
};
globalThis.clearTimeout = (id) => globalThis.__timers.delete(id);
globalThis.setInterval = (fn, ms) => {
  const tick = () => { setTimeout(tick, ms); fn(); };
  return setTimeout(tick, ms);
};
globalThis.clearInterval = (id) => globalThis.__timers.delete(id);

// GUI.for.Cores 兼容分支需要的全部宿主能力
globalThis.$Plugins = {
  SubStoreCache: {
    get:    (k)   => __rustCacheGet(k) || "{}",
    set:    (k,v) => __rustCacheSet(k, v),
    remove: (k)   => __rustCacheRemove(k)
  },
  Requests: async (spec) => JSON.parse(await __rustRequest(JSON.stringify(spec))),
  Notify:   (t, m) => __rustNotify(t, m)
};

// Rust 侧重写的 DNS / GeoIP，以宿主函数形式暴露给 JS 侧后续使用
globalThis.__substoreDns = {
  resolve: async (name) => JSON.parse(await __rustResolveDns(name))
};
globalThis.__substoreGeo = {
  lookup: (ip) => JSON.parse(__rustGeoIp(ip))
};

globalThis.__rustDone = () => {};
globalThis.$done = (resp) => globalThis.__rustDone(JSON.stringify(resp));
globalThis.$request = null;

// 关键：不能让 process 存在，否则 isNode=true
globalThis.process = undefined;
"#;

fn q(e: rquickjs::Error) -> anyhow::Error {
    anyhow!("QuickJS: {e}")
}

fn install_globals<'js>(
    ctx: &Ctx<'js>,
    cache: Cache,
    http: Arc<Http>,
    dns: Arc<RustDns>,
    geoip: Arc<RustGeoIp>,
    cors_origins: Option<String>,
) -> Result<()> {
    let globals = ctx.globals();

    // 上游 utils/cors.js 在非 Node 分支从全局 $argument 读 `cors=`。
    // 只有显式配置跨域白名单时才注入；内嵌同源部署不注入，
    // 由 Rust 边界剥离 Origin（见 main.rs），避免开放 cors=* 带来的跨站读取风险。
    if let Some(origins) = cors_origins {
        globals
            .set("$argument", format!("cors={origins}"))
            .map_err(q)?;
        tracing::info!("[cors] 跨域白名单: {origins}");
    }

    globals
        .set(
            "__rustLog",
            Function::new(ctx.clone(), |level: String, text: String| {
                match level.as_str() {
                    "error" => tracing::error!("[js] {text}"),
                    "warn" => tracing::warn!("[js] {text}"),
                    "debug" => tracing::debug!("[js] {text}"),
                    _ => tracing::info!("[js] {text}"),
                }
            })
            .map_err(q)?,
        )
        .map_err(q)?;

    globals
        .set(
            "__rustSleep",
            Function::new(
                ctx.clone(),
                Async(|ms: f64| async move {
                    tokio::time::sleep(Duration::from_millis(ms.max(0.0) as u64)).await;
                    0i32
                }),
            )
            .map_err(q)?,
        )
        .map_err(q)?;

    let c = cache.clone();
    globals
        .set(
            "__rustCacheGet",
            Function::new(ctx.clone(), move |key: String| -> Option<String> { c.get(&key) })
                .map_err(q)?,
        )
        .map_err(q)?;
    let c = cache.clone();
    globals
        .set(
            "__rustCacheSet",
            Function::new(ctx.clone(), move |key: String, val: String| {
                c.set(&key, &val)
            })
            .map_err(q)?,
        )
        .map_err(q)?;
    let c = cache.clone();
    globals
        .set(
            "__rustCacheRemove",
            Function::new(ctx.clone(), move |key: String| c.remove(&key)).map_err(q)?,
        )
        .map_err(q)?;

    globals
        .set(
            "__rustRequest",
            Function::new(
                ctx.clone(),
                Async(move |spec: String| {
                    let http = http.clone();
                    async move {
                        match http.request(&spec).await {
                            Ok(s) => s,
                            Err(e) => format!(
                                "{{\"status\":502,\"headers\":{{}},\"body\":{}}}",
                                serde_json::to_string(&format!("rust http error: {e}"))
                                    .unwrap_or_default()
                            ),
                        }
                    }
                }),
            )
            .map_err(q)?,
        )
        .map_err(q)?;

    globals
        .set(
            "__rustNotify",
            Function::new(ctx.clone(), |title: String, msg: String| {
                tracing::info!("[notify] {title}: {msg}");
            })
            .map_err(q)?,
        )
        .map_err(q)?;

    // Rust 侧 DNS：手写 UDP 报文查询，不走 Node 的 dgram/net/tls
    globals
        .set(
            "__rustResolveDns",
            Function::new(
                ctx.clone(),
                Async(move |name: String| {
                    let dns = dns.clone();
                    async move {
                        match dns.resolve_a(&name).await {
                            Ok(ips) => serde_json::to_string(
                                &ips.iter().map(|i| i.to_string()).collect::<Vec<_>>(),
                            )
                            .unwrap_or_else(|_| "[]".into()),
                            Err(_) => "[]".to_string(),
                        }
                    }
                }),
            )
            .map_err(q)?,
        )
        .map_err(q)?;

    // Rust 侧 GeoIP：maxminddb 直接读 mmdb，不走 fs.readFileSync
    globals
        .set(
            "__rustGeoIp",
            Function::new(ctx.clone(), move |ip: String| {
                geoip.lookup(ip.parse().unwrap_or(std::net::IpAddr::V4(
                    std::net::Ipv4Addr::UNSPECIFIED,
                )))
                .to_string()
            })
            .map_err(q)?,
        )
        .map_err(q)?;

    ctx.eval::<(), _>(GLUE_JS).map_err(q)?;
    Ok(())
}

impl JsHost {
    pub async fn new(
        bundle_source: Option<&str>,
        data_dir: &std::path::Path,
        dns: Arc<RustDns>,
        geoip: Arc<RustGeoIp>,
        cors_origins: Option<String>,
    ) -> Result<Self> {
        let runtime = AsyncRuntime::new().map_err(q).context("创建 QuickJS Runtime")?;
        // 硬上限防止用户自定义脚本把盒子内存吃光；0 表示不限制。
        let limit = std::env::var("SUB_STORE_JS_MEMORY_LIMIT")
            .ok()
            .and_then(|v| v.parse::<usize>().ok())
            .unwrap_or(256 * 1024 * 1024);
        runtime.set_memory_limit(limit).await;
        runtime.set_gc_threshold(512 * 1024).await;
        if limit > 0 {
            tracing::info!("[quickjs] JS 堆上限 {} MB，GC 阈值 512 KB", limit / 1024 / 1024);
        }
        let context = AsyncContext::full(&runtime)
            .await
            .map_err(q)
            .context("创建 QuickJS Context")?;

        let cache = Cache::open(data_dir.join("cache.json"))?;
        let http = Arc::new(Http::new()?);

        context
            .with(|ctx| install_globals(&ctx, cache, http, dns, geoip, cors_origins))
            .await
            .context("注入宿主全局失败")?;

        // 默认走预编译字节码；只有外部覆盖 bundle 时才退回源码编译。
        let has_dispatch: bool = match bundle_source {
            Some(src) => boot_from_source(&context, src).await?,
            None => boot_from_bytecode(&context, crate::assets::BUNDLE_BYTECODE).await?,
        };
        anyhow::ensure!(has_dispatch, "bundle 未注册 __substoreDispatch，补丁未生效");
        runtime.run_gc().await; // 回收启动期临时对象

        Ok(Self {
            runtime,
            context,
            gc_counter: AtomicU32::new(0),
        })
    }

    /// 一次 HTTP 请求 → 一次 JS dispatch → $done 回吐响应。
    /// 调用方必须保证串行（专用线程里的 host loop 已保证）。
    pub async fn dispatch(&self, req: JsRequest) -> Result<JsResponse> {
        let n = self.gc_counter.fetch_add(1, Ordering::Relaxed);
        let req_json = serde_json::to_string(&req)?;
        let context = &self.context;

        let fut = async {
            // async_with 会在 runtime 锁内驱动这个 async 闭包，
            // 这样 JS Promise 与 Rust future 才能正确互相推进。
            context
                .async_with(async move |ctx| {
                    let globals = ctx.globals();
                    let (promise, resolve, _reject) = Promise::new(&ctx)?;
                    globals.set("__rustDone", resolve)?;
                    let f: Function = globals.get("__substoreDispatch")?;
                    // Function::call<A, R>：第一个泛型是参数元组，第二个是返回值类型
                    f.call::<_, ()>((req_json,))?;
                    promise.into_future::<String>().await
                })
                .await
        };

        let inner: Result<String, rquickjs::Error> =
            tokio::time::timeout(Duration::from_secs(30), fut)
                .await
                .context("JS dispatch 超时（30s）")?;
        let raw = inner.map_err(q)?;
        let resp = serde_json::from_str::<JsResponse>(&raw).with_context(|| {
            format!("解析 $done 响应失败，原始内容前 500 字节: {}", &raw[..raw.len().min(500)])
        });

        // 在 async_with 之外调用，避免与 runtime 内部锁重叠。
        if n % GC_EVERY == GC_EVERY - 1 {
            self.runtime.run_gc().await;
        }
        resp
    }
}

const DISPATCH_CHECK: &str = "typeof globalThis.__substoreDispatch === 'function'";

/// 源码路径：仅开发时外部覆盖 bundle 使用。
/// 不再用字符串拼 try/catch（会再复制一份 3.3MB 源码），直接借 rquickjs 的 catch 取异常。
async fn boot_from_source(context: &AsyncContext, src: &str) -> Result<bool> {
    let patched = crate::bundle::prepare(src)?;
    let ok = context
        .with(|ctx| -> Result<bool> {
            let boot: rquickjs::Result<()> = ctx.eval(patched.as_str());
            boot.catch(&ctx)
                .map_err(|e| anyhow!("bundle 启动异常: {e}"))?;
            let ok: bool = ctx.eval(DISPATCH_CHECK).map_err(q)?;
            Ok(ok)
        })
        .await?;
    drop(patched); // 源码字符串立刻归还
    Ok(ok)
}

/// 字节码路径：反序列化后直接执行，跳过 3.3MB 源码解析，也不把源码文本搬进 JS 堆。
async fn boot_from_bytecode(context: &AsyncContext, bytes: &'static [u8]) -> Result<bool> {
    context
        .async_with(async move |ctx| -> Result<bool> {
            let module = unsafe { Module::load(ctx.clone(), bytes) }.map_err(q)?;
            let (_module, promise) = module.eval().map_err(q)?;
            promise.into_future::<()>().await.map_err(q)?;
            let ok: bool = ctx.eval(DISPATCH_CHECK).map_err(q)?;
            Ok(ok)
        })
        .await
}
