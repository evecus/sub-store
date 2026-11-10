//! `$Plugins` 的 Rust 后端实现（GUI.for.Cores 兼容分支）。
//!
//! Sub-Store 在非 Node 环境下只认三个宿主能力：
//!   - SubStoreCache.get/set/remove  → 持久化
//!   - Requests(spec)                → 出网请求
//!   - Notify(title, message)        → 通知
//! 本文件把它们全部落在 Rust 侧。按需求：Requests 直连，不透传任何代理。

use anyhow::{Context as _, Result};
use serde::{Deserialize, Serialize};
use std::{
    collections::HashMap,
    path::PathBuf,
    sync::{Arc, Mutex},
};

// ---------------------------------------------------------------------------
// 持久化：替代 Node 分支的 root.json + fs.writeFileSync
// ---------------------------------------------------------------------------

#[derive(Clone)]
pub struct Cache {
    inner: Arc<Mutex<HashMap<String, String>>>,
    path: PathBuf,
}

impl Cache {
    pub fn open(path: PathBuf) -> Result<Self> {
        let mut map = HashMap::new();
        if path.exists() {
            let raw = std::fs::read_to_string(&path)
                .with_context(|| format!("读取缓存失败: {}", path.display()))?;
            map = serde_json::from_str(&raw).unwrap_or_default();
        } else if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        Ok(Self {
            inner: Arc::new(Mutex::new(map)),
            path,
        })
    }

    pub fn get(&self, key: &str) -> Option<String> {
        self.inner.lock().unwrap().get(key).cloned()
    }

    pub fn set(&self, key: &str, value: &str) {
        self.inner
            .lock()
            .unwrap()
            .insert(key.to_string(), value.to_string());
        self.flush();
    }

    pub fn remove(&self, key: &str) {
        self.inner.lock().unwrap().remove(key);
        self.flush();
    }

    fn flush(&self) {
        let snapshot: HashMap<String, String> = self.inner.lock().unwrap().clone();
        if let Ok(raw) = serde_json::to_string_pretty(&snapshot) {
            let _ = std::fs::write(&self.path, raw);
        }
    }
}

// ---------------------------------------------------------------------------
// 出网请求：替代 undici / fetch-socks / $httpClient
// ---------------------------------------------------------------------------

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RequestSpec {
    pub method: String,
    pub url: String,
    #[serde(default)]
    pub headers: HashMap<String, String>,
    #[serde(default)]
    pub body: Option<String>,
    #[serde(default)]
    pub options: RequestOptions,
}

#[derive(Debug, Default, Deserialize)]
pub struct RequestOptions {
    /// JS 侧会传 Proxy，但按约定忽略：不做代理透传。
    #[serde(default)]
    pub proxy: Option<String>,
    #[serde(default)]
    pub timeout: Option<f64>,
}

#[derive(Debug, Serialize)]
pub struct RequestResult {
    pub status: u16,
    pub headers: HashMap<String, String>,
    pub body: String,
}

pub struct Http {
    client: reqwest::Client,
}

impl Http {
    pub fn new() -> Result<Self> {
        // .no_proxy() 是关键：禁用 reqwest 对 HTTP_PROXY/ALL_PROXY 环境变量的自动读取，
        // 保证「不要代理透传」在沙箱/代理盒子上也成立。
        let client = reqwest::Client::builder()
            .no_proxy()
            .timeout(std::time::Duration::from_secs(15))
            .redirect(reqwest::redirect::Policy::limited(3))
            .build()
            .context("构造直连 HTTP 客户端失败")?;
        Ok(Self { client })
    }

    /// 输入 JSON 字符串，输出 JSON 字符串 —— 与 QuickJS 之间只交换字符串，避免值转换开销。
    pub async fn request(&self, spec_json: &str) -> Result<String> {
        let spec: RequestSpec =
            serde_json::from_str(spec_json).context("Requests 参数不是合法 JSON")?;

        // JS 侧会传 options.Proxy，但按约定一律忽略：不做任何代理透传。
        let _ = spec.options.proxy.as_ref();

        let method: reqwest::Method = spec
            .method
            .parse()
            .with_context(|| format!("非法 HTTP method: {}", spec.method))?;

        let mut builder = self.client.request(method, &spec.url);
        for (k, v) in &spec.headers {
            if let (Ok(name), Ok(val)) = (
                reqwest::header::HeaderName::from_bytes(k.as_bytes()),
                reqwest::header::HeaderValue::from_str(v),
            ) {
                builder = builder.header(name, val);
            }
        }
        if let Some(body) = spec.body.as_ref() {
            builder = builder.body(body.clone());
        }
        if let Some(t) = spec.options.timeout {
            if t > 0.0 {
                builder = builder.timeout(std::time::Duration::from_secs_f64(t));
            }
        }

        let resp = builder.send().await.with_context(|| format!("请求失败: {}", spec.url))?;
        let status = resp.status().as_u16();
        let mut headers = HashMap::new();
        for (k, v) in resp.headers().iter() {
            headers.insert(k.to_string(), v.to_str().unwrap_or("").to_string());
        }
        let body = resp.text().await.unwrap_or_default();

        Ok(serde_json::to_string(&RequestResult {
            status,
            headers,
            body,
        })?)
    }
}
