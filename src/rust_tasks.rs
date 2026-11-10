//! 按需求，把 Sub-Store 原本依赖 Node 的三块能力搬到 Rust 侧重写：
//!   1. DNS（原 dgram / net / tls 的 UDP、TCP、DoT 查询）
//!   2. GeoIP（原 @maxmind/geoip2-node + fs.readFileSync）
//!   3. 定时同步（原 cron / startArtifactCronJobs，被 isNode 分支包住，非 Node 下不会执行）
//! 三者都不做任何代理透传。

use anyhow::{Context as _, Result};
use maxminddb::{geoip2, Reader};
use std::{
    net::{IpAddr, Ipv4Addr, SocketAddr},
    path::PathBuf,
    time::Duration,
};
use tokio::{net::UdpSocket, sync::oneshot};

use crate::host::{Job, JobTx, JsRequest};

// ---------------------------------------------------------------------------
// 1. DNS —— 手写 DNS 报文的 UDP A 查询，零 Node 依赖
// ---------------------------------------------------------------------------

pub struct RustDns {
    servers: Vec<SocketAddr>,
}

impl RustDns {
    pub fn from_env() -> Self {
        let raw = std::env::var("SUB_STORE_DNS_SERVERS")
            .unwrap_or_else(|_| "223.5.5.5:53,119.29.29.29:53".to_string());
        let servers: Vec<SocketAddr> = raw
            .split(',')
            .filter_map(|s| s.trim().parse().ok())
            .collect();
        Self {
            servers: if servers.is_empty() {
                vec!["223.5.5.5:53".parse().unwrap()]
            } else {
                servers
            },
        }
    }

    pub fn servers(&self) -> &[SocketAddr] {
        &self.servers
    }

    /// UDP A 查询；超时或截断时返回空，TCP/DoT 是后续扩展点（见文件末注释）。
    pub async fn resolve_a(&self, name: &str) -> Result<Vec<Ipv4Addr>> {
        let query = build_query(name);
        let sock = UdpSocket::bind("0.0.0.0:0").await.context("绑定 UDP 套接字")?;
        for server in &self.servers {
            sock.send_to(&query, server).await?;
            let mut buf = [0u8; 512];
            let got = match tokio::time::timeout(
                Duration::from_secs(3),
                sock.recv_from(&mut buf),
            )
            .await
            {
                Ok(Ok((n, _))) => n,
                _ => continue,
            };
            let addrs = parse_a(&buf[..got]);
            if !addrs.is_empty() {
                return Ok(addrs);
            }
        }
        Ok(vec![])
    }
}

fn build_query(name: &str) -> Vec<u8> {
    let id = (std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .subsec_millis()
        & 0xffff) as u16;
    // header: ID, RD, QDCOUNT=1
    let mut p = vec![
        (id >> 8) as u8,
        id as u8,
        0x01,
        0x00,
        0x00,
        0x01,
        0x00,
        0x00,
        0x00,
        0x00,
        0x00,
        0x00,
    ];
    for label in name.split('.').filter(|s| !s.is_empty()) {
        p.push(label.len().min(63) as u8);
        p.extend_from_slice(&label.as_bytes()[..label.len().min(63)]);
    }
    p.push(0);
    p.extend_from_slice(&[0x00, 0x01, 0x00, 0x01]); // QTYPE=A, QCLASS=IN
    p
}

fn skip_name(p: &[u8], mut off: usize) -> usize {
    loop {
        if off >= p.len() {
            return off;
        }
        let len = p[off];
        if len & 0xc0 == 0xc0 {
            return off + 2; // 压缩指针
        }
        off += 1;
        if len == 0 {
            return off;
        }
        off += len as usize;
    }
}

fn parse_a(resp: &[u8]) -> Vec<Ipv4Addr> {
    if resp.len() < 12 {
        return vec![];
    }
    let qd = u16::from_be_bytes([resp[4], resp[5]]);
    let an = u16::from_be_bytes([resp[6], resp[7]]);
    let mut off = 12usize;
    for _ in 0..qd {
        off = skip_name(resp, off);
        off += 4;
    }
    let mut out = Vec::new();
    for _ in 0..an {
        off = skip_name(resp, off);
        if off + 10 > resp.len() {
            break;
        }
        let rtype = u16::from_be_bytes([resp[off], resp[off + 1]]);
        let rclass = u16::from_be_bytes([resp[off + 2], resp[off + 3]]);
        let rdlen = u16::from_be_bytes([resp[off + 8], resp[off + 9]]) as usize;
        off += 10;
        if rtype == 1 && rclass == 1 && rdlen == 4 && off + 4 <= resp.len() {
            out.push(Ipv4Addr::new(resp[off], resp[off + 1], resp[off + 2], resp[off + 3]));
        }
        off += rdlen;
    }
    out
}

// ---------------------------------------------------------------------------
// 2. GeoIP —— Rust 侧读 mmdb，替代 fs.readFileSync + @maxmind/geoip2-node
// ---------------------------------------------------------------------------

pub struct RustGeoIp {
    country: Option<Reader<Vec<u8>>>,
    asn: Option<Reader<Vec<u8>>>,
}

impl RustGeoIp {
    pub fn open(country: Option<PathBuf>, asn: Option<PathBuf>) -> Result<Self> {
        let load = |p: Option<PathBuf>| -> Option<Reader<Vec<u8>>> {
            p.and_then(|path| match Reader::open_readfile(&path) {
                Ok(r) => {
                    tracing::info!("[geoip] 已加载 {}", path.display());
                    Some(r)
                }
                Err(e) => {
                    tracing::warn!("[geoip] 无法加载 {}: {e}", path.display());
                    None
                }
            })
        };
        Ok(Self {
            country: load(country),
            asn: load(asn),
        })
    }

    pub fn lookup(&self, ip: IpAddr) -> serde_json::Value {
        let mut out = serde_json::Map::new();
        if let Some(reader) = &self.country {
            if let Ok(c) = reader.lookup::<geoip2::Country>(ip) {
                if let Some(code) = c.country.and_then(|x| x.iso_code) {
                    out.insert("country".into(), code.into());
                }
            }
        }
        if let Some(reader) = &self.asn {
            if let Ok(a) = reader.lookup::<geoip2::Asn>(ip) {
                if let Some(n) = a.autonomous_system_number {
                    out.insert("asn".into(), n.into());
                }
            }
        }
        serde_json::Value::Object(out)
    }
}

// ---------------------------------------------------------------------------
// 3. 定时同步 —— 取代 Node 的 cron / startArtifactCronJobs
// ---------------------------------------------------------------------------

/// 周期性向 JS 后端投递一个内部请求。生产环境把 path 换成 `/api/sync/artifacts`
/// 之类即可，完全由 Rust 掌握调度，不依赖 JS 侧的 cron 包。
pub fn spawn_sync_scheduler(
    tx: JobTx,
    path: String,
    every: Duration,
) -> tokio::task::JoinHandle<()> {
    tokio::spawn(async move {
        let mut ticker = tokio::time::interval(every);
        ticker.tick().await; // 首次立即触发，跳过
        loop {
            ticker.tick().await;
            let req = JsRequest {
                method: "GET".into(),
                // 必须绝对 URL，理由见 main.rs 的构造处
                url: format!("http://127.0.0.1:3000{path}"),
                headers: Default::default(),
                body: String::new(),
            };
            let (reply, rx) = oneshot::channel();
            if tx.send(Job { req, reply }).await.is_err() {
                tracing::warn!("[cron] QuickJS 宿主已退出，调度器停止");
                break;
            }
            match rx.await {
                Ok(Ok(r)) => tracing::info!("[cron] {} -> HTTP {}", path, r.status),
                Ok(Err(e)) => tracing::warn!("[cron] {} 失败: {e}", path),
                Err(_) => tracing::warn!("[cron] {} 响应通道关闭", path),
            }
        }
    })
}

// TODO 后续扩展点（均不需要 Node）：
//  - TCP DNS：报文加 2 字节长度前缀，走 tokio::net::TcpStream
//  - DoT：TCP + rustls 连接 853 端口
//  - 把 resolve_a / lookup 注册成 QuickJS host function（__rustResolveDns / __rustGeoIp），
//    供 JS 侧 utils/dns.js、utils/geo.js 在需要时直接调用。
