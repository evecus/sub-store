//! QuickJS 宿主：加载编译产物、注入最小全局、把每个 HTTP 请求转成一次 JS dispatch。
//!
//! 设计要点：
//!  - 绝不注入 `process`。`isNode = typeof process !== "undefined"` 是 Sub-Store 的总开关，
//!    一旦为 true 就会回落到真 express + node:http，QuickJS 里必然失败。
//!  - 伪装成 `isGUIforCores`（注入 `$Plugins`）：这是唯一用 Promise、参数结构化、
//!    且不带 Surge 遗留副作用的宿主分支。
//!  - 与 JS 之间只交换 JSON 字符串，避免大量值转换。

use anyhow::{anyhow, Context as _, Result};
use rquickjs::{function::Async, AsyncContext, AsyncRuntime, Ctx, Function, Promise};
use serde::{Deserialize, Serialize};
use std::{collections::HashMap, sync::Arc, time::Duration};

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
    #[allow(dead_code)]
    runtime: AsyncRuntime,
    context: AsyncContext,
}

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
        bundle_source: &str,
        data_dir: &std::path::Path,
        dns: Arc<RustDns>,
        geoip: Arc<RustGeoIp>,
        cors_origins: Option<String>,
    ) -> Result<Self> {
        let runtime = AsyncRuntime::new().map_err(q).context("创建 QuickJS Runtime")?;
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

        let patched = crate::bundle::prepare(bundle_source)?;
        // 包一层 try/catch，才能把 QuickJS 内部异常拿到 Rust 侧看到
        let boot = format!(
            "try{{\n{patched}\n}}catch(e){{globalThis.__bootError=String((e&&e.stack)||e)}}"
        );

        let (boot_err, has_dispatch): (Option<String>, bool) = context
            .with(|ctx| -> Result<_, anyhow::Error> {
                ctx.eval::<(), _>(boot).map_err(q)?;
                let err: Option<String> = ctx.globals().get("__bootError").map_err(q)?;
                let ok: bool = ctx
                    .eval("typeof globalThis.__substoreDispatch === 'function'")
                    .map_err(q)?;
                Ok((err, ok))
            })
            .await?;

        if let Some(e) = boot_err {
            return Err(anyhow!("bundle 启动异常:\n{e}"));
        }
        anyhow::ensure!(has_dispatch, "bundle 未注册 __substoreDispatch，补丁未生效");

        Ok(Self { runtime, context })
    }

    /// 一次 HTTP 请求 → 一次 JS dispatch → $done 回吐响应。
    /// 调用方必须保证串行（专用线程里的 host loop 已保证）。
    pub async fn dispatch(&self, req: JsRequest) -> Result<JsResponse> {
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

        serde_json::from_str::<JsResponse>(&raw).with_context(|| {
            format!("解析 $done 响应失败，原始内容前 500 字节: {}", &raw[..raw.len().min(500)])
        })
    }
}
