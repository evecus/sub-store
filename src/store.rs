//! SQLite 持久化：替代原来的单个 cache.json。
//!
//! 上游 Sub-Store 把全部状态序列化成一个键 `sub-store`（subs / collections / files /
//! settings …），每次写入都整块重写。这里改为：
//!
//! * `app_items`：订阅、组合订阅、文件、模块、artifact、token、归档 **一条一行**，
//!   保存时只对有变化的行做 INSERT/DELETE（行级 diff）；
//! * `app_kv`：settings、schemaVersion、rules 等标量/对象键；
//! * `raw_kv`：资源缓存、响应头缓存、脚本缓存、日志等 `#` 键。
//!
//! JS 侧仍把整份状态放在内存里读（`$.read` 不碰数据库），数据库只在启动时加载一次、
//! 写入时落盘。虚拟键 `sub-store` 兼容上游的备份导出 / 还原：get 返回整份状态 JSON，
//! set 则整体替换。
//!
//! 内存：SQLite 页缓存限制在 512KB，Rust 侧不再常驻任何一份状态副本。

use anyhow::{Context as _, Result};
use rusqlite::{params, Connection, OptionalExtension, Transaction};
use serde::Deserialize;
use serde_json::{value::RawValue, Value};
use std::{
    collections::{HashMap, HashSet},
    path::Path,
    sync::Mutex,
};

/// 上游 `$.name`，同时也是“整份状态”这个虚拟键。
pub const STATE_KEY: &str = "sub-store";

/// 值是数组、且每个元素有稳定标识的键 → 按行存储。
const ARRAY_KEYS: &[&str] = &[
    "subs",
    "collections",
    "files",
    "modules",
    "artifacts",
    "tokens",
    "archives",
];

const SCHEMA: &str = "
CREATE TABLE IF NOT EXISTS app_kv (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS app_items (
    coll  TEXT    NOT NULL,
    id    TEXT    NOT NULL,
    pos   INTEGER NOT NULL,
    value TEXT    NOT NULL,
    PRIMARY KEY (coll, id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS app_items_pos ON app_items (coll, pos);
CREATE TABLE IF NOT EXISTS raw_kv (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) WITHOUT ROWID;
";

pub struct Store {
    conn: Mutex<Connection>,
}

#[derive(Deserialize)]
struct IdProbe {
    name: Option<Value>,
    token: Option<Value>,
    id: Option<Value>,
    #[serde(rename = "type")]
    ty: Option<Value>,
}

fn scalar(v: &Option<Value>) -> Option<String> {
    match v {
        Some(Value::String(s)) if !s.is_empty() => Some(s.clone()),
        Some(Value::Number(n)) => Some(n.to_string()),
        _ => None,
    }
}

/// 元素的稳定标识：tokens 用 token|type|name，其余优先 name，其次 id。
fn item_id(coll: &str, raw: &RawValue) -> Option<String> {
    let p: IdProbe = serde_json::from_str(raw.get()).ok()?;
    if coll == "tokens" {
        let parts: Vec<String> = [&p.token, &p.ty, &p.name]
            .into_iter()
            .filter_map(scalar)
            .collect();
        return (!parts.is_empty()).then(|| parts.join("|"));
    }
    scalar(&p.name).or_else(|| scalar(&p.id))
}

impl Store {
    /// `data_dir/substore.db`；首次打开时自动导入旧版 `cache.json`。
    pub fn open(data_dir: &Path) -> Result<Self> {
        std::fs::create_dir_all(data_dir).ok();
        let conn = Connection::open(data_dir.join("substore.db")).context("打开 substore.db")?;
        let store = Self::init(conn)?;
        store.migrate_legacy(&data_dir.join("cache.json"));
        Ok(store)
    }

    fn init(conn: Connection) -> Result<Self> {
        // WAL：写入只追加，进程被杀也不会损坏；NORMAL 在 WAL 下依然保证一致性。
        // 页缓存 512KB、不开 mmap，把常驻内存压到最低。
        conn.execute_batch(
            "PRAGMA journal_mode = WAL;
             PRAGMA synchronous = NORMAL;
             PRAGMA cache_size = -512;
             PRAGMA mmap_size = 0;
             PRAGMA wal_autocheckpoint = 256;",
        )
        .context("初始化 SQLite 参数")?;
        conn.execute_batch(SCHEMA).context("创建数据表")?;
        Ok(Self {
            conn: Mutex::new(conn),
        })
    }

    // ------------------------------------------------------------------
    // JS 侧入口
    // ------------------------------------------------------------------

    /// `SubStoreCache.get`：`sub-store` 返回整份状态；其余是 `#` 原始键。
    pub fn get(&self, key: &str) -> Option<String> {
        if key == STATE_KEY {
            return self.load_all().map_err(log_err("读取状态")).ok();
        }
        let conn = self.conn.lock().unwrap();
        conn.query_row("SELECT value FROM raw_kv WHERE key = ?1", [key], |r| r.get(0))
            .optional()
            .map_err(log_err("读取 raw_kv"))
            .ok()
            .flatten()
    }

    /// `SubStoreCache.set`：`sub-store` 整体替换状态（备份还原）；其余写原始键。
    pub fn set(&self, key: &str, value: &str) {
        let r = if key == STATE_KEY {
            self.replace_all(value)
        } else {
            self.with_tx(|tx| {
                let same: Option<String> = tx
                    .query_row("SELECT value FROM raw_kv WHERE key = ?1", [key], |r| r.get(0))
                    .optional()?;
                if same.as_deref() != Some(value) {
                    tx.execute(
                        "INSERT OR REPLACE INTO raw_kv (key, value) VALUES (?1, ?2)",
                        params![key, value],
                    )?;
                }
                Ok(())
            })
        };
        if let Err(e) = r {
            tracing::error!("[store] 写入 {key} 失败: {e:#}");
        }
    }

    pub fn remove(&self, key: &str) {
        let r = if key == STATE_KEY {
            self.replace_all("{}")
        } else {
            self.with_tx(|tx| {
                tx.execute("DELETE FROM raw_kv WHERE key = ?1", [key])?;
                Ok(())
            })
        };
        if let Err(e) = r {
            tracing::error!("[store] 删除 {key} 失败: {e:#}");
        }
    }

    /// 写入一个应用状态键（subs / settings …）。数组键做行级 diff。
    pub fn set_key(&self, key: &str, json: &str) {
        if let Err(e) = self.with_tx(|tx| set_key_tx(tx, key, json)) {
            tracing::error!("[store] 保存 {key} 失败: {e:#}");
        }
    }

    pub fn remove_key(&self, key: &str) {
        if let Err(e) = self.with_tx(|tx| remove_key_tx(tx, key)) {
            tracing::error!("[store] 删除 {key} 失败: {e:#}");
        }
    }

    // ------------------------------------------------------------------
    // 整体读写
    // ------------------------------------------------------------------

    /// 拼出 `{"subs":[...],"settings":{...},...}`，不经过 serde 重新序列化，保持元素原文。
    pub fn load_all(&self) -> Result<String> {
        let conn = self.conn.lock().unwrap();
        let mut out = String::from("{");
        let mut first = true;
        let mut push_key = |out: &mut String, k: &str| {
            if !first {
                out.push(',');
            }
            first = false;
            out.push_str(&serde_json::to_string(k).unwrap());
            out.push(':');
        };

        let mut stmt = conn.prepare("SELECT key, value FROM app_kv ORDER BY key")?;
        let rows = stmt.query_map([], |r| Ok((r.get::<_, String>(0)?, r.get::<_, String>(1)?)))?;
        for row in rows {
            let (k, v) = row?;
            push_key(&mut out, &k);
            out.push_str(&v);
        }

        let mut stmt =
            conn.prepare("SELECT value FROM app_items WHERE coll = ?1 ORDER BY pos")?;
        for coll in ARRAY_KEYS {
            let mut rows = stmt.query([coll])?;
            let mut any = false;
            while let Some(row) = rows.next()? {
                if !any {
                    push_key(&mut out, coll);
                    out.push('[');
                    any = true;
                } else {
                    out.push(',');
                }
                out.push_str(&row.get::<_, String>(0)?);
            }
            if any {
                out.push(']');
            }
        }
        out.push('}');
        Ok(out)
    }

    /// 用一整份状态 JSON 替换数据库里的应用状态（备份还原 / 旧数据导入）。
    pub fn replace_all(&self, json: &str) -> Result<()> {
        let map: HashMap<String, Box<RawValue>> =
            serde_json::from_str(json).context("状态不是合法的 JSON 对象")?;
        self.with_tx(|tx| {
            for (k, v) in &map {
                set_key_tx(tx, k, v.get())?;
            }
            // 删掉新状态里不存在的键
            let mut stale: Vec<String> = Vec::new();
            {
                let mut s = tx.prepare("SELECT key FROM app_kv")?;
                for k in s.query_map([], |r| r.get::<_, String>(0))? {
                    stale.push(k?);
                }
                let mut s = tx.prepare("SELECT DISTINCT coll FROM app_items")?;
                for k in s.query_map([], |r| r.get::<_, String>(0))? {
                    stale.push(k?);
                }
            }
            for k in stale {
                if !map.contains_key(&k) {
                    remove_key_tx(tx, &k)?;
                }
            }
            Ok(())
        })
    }

    fn with_tx<T>(&self, f: impl FnOnce(&Transaction) -> Result<T>) -> Result<T> {
        let mut conn = self.conn.lock().unwrap();
        let tx = conn.transaction()?;
        let out = f(&tx)?;
        tx.commit()?;
        Ok(out)
    }

    // ------------------------------------------------------------------
    // 旧版 cache.json → SQLite（只在数据库为空时执行一次）
    // ------------------------------------------------------------------

    fn is_empty(&self) -> bool {
        let conn = self.conn.lock().unwrap();
        ["app_kv", "app_items", "raw_kv"].iter().all(|t| {
            conn.query_row(&format!("SELECT COUNT(*) FROM {t}"), [], |r| r.get::<_, i64>(0))
                .unwrap_or(0)
                == 0
        })
    }

    fn migrate_legacy(&self, path: &Path) {
        if !path.exists() || !self.is_empty() {
            return;
        }
        let result = (|| -> Result<usize> {
            let raw = std::fs::read_to_string(path)?;
            let old: HashMap<String, String> = serde_json::from_str(&raw)?;
            let mut n = 0;
            for (k, v) in &old {
                if k == STATE_KEY {
                    self.replace_all(v)?;
                } else {
                    self.set(k, v);
                }
                n += 1;
            }
            Ok(n)
        })();
        match result {
            Ok(n) => {
                let bak = path.with_extension("json.migrated");
                let _ = std::fs::rename(path, &bak);
                tracing::info!(
                    "[store] 已把旧版 cache.json 的 {n} 个键导入 SQLite，原文件改名为 {}",
                    bak.display()
                );
            }
            // 失败就保留原文件、数据库保持为空，下次启动会再试，不会丢数据。
            Err(e) => tracing::error!("[store] 导入旧版 cache.json 失败（原文件未动）: {e:#}"),
        }
    }
}

fn log_err<E: std::fmt::Display>(what: &'static str) -> impl Fn(E) {
    move |e| tracing::error!("[store] {what}失败: {e}")
}

// ---------------------------------------------------------------------------
// 事务内操作
// ---------------------------------------------------------------------------

fn set_key_tx(tx: &Transaction, key: &str, json: &str) -> Result<()> {
    if ARRAY_KEYS.contains(&key) {
        if let Ok(elems) = serde_json::from_str::<Vec<Box<RawValue>>>(json) {
            return set_array_tx(tx, key, &elems);
        }
        // 不是数组（异常数据）→ 退回整体存储，但要清掉可能残留的行
        tx.execute("DELETE FROM app_items WHERE coll = ?1", [key])?;
    }
    let same: Option<String> = tx
        .query_row("SELECT value FROM app_kv WHERE key = ?1", [key], |r| r.get(0))
        .optional()?;
    if same.as_deref() != Some(json) {
        tx.execute(
            "INSERT OR REPLACE INTO app_kv (key, value) VALUES (?1, ?2)",
            params![key, json],
        )?;
    }
    Ok(())
}

fn set_array_tx(tx: &Transaction, coll: &str, elems: &[Box<RawValue>]) -> Result<()> {
    // 空数组需要保留“键存在”这个事实（JS 侧 `read(key)` 区分 undefined 和 []）
    if elems.is_empty() {
        tx.execute("DELETE FROM app_items WHERE coll = ?1", [coll])?;
        tx.execute(
            "INSERT OR REPLACE INTO app_kv (key, value) VALUES (?1, '[]')",
            [coll],
        )?;
        return Ok(());
    }
    tx.execute("DELETE FROM app_kv WHERE key = ?1", [coll])?;

    let mut existing: HashMap<String, (i64, String)> = HashMap::new();
    {
        let mut s = tx.prepare("SELECT id, pos, value FROM app_items WHERE coll = ?1")?;
        for row in s.query_map([coll], |r| {
            Ok((r.get::<_, String>(0)?, r.get::<_, i64>(1)?, r.get::<_, String>(2)?))
        })? {
            let (id, pos, v) = row?;
            existing.insert(id, (pos, v));
        }
    }

    let mut seen: HashSet<String> = HashSet::with_capacity(elems.len());
    let mut upsert = tx.prepare_cached(
        "INSERT OR REPLACE INTO app_items (coll, id, pos, value) VALUES (?1, ?2, ?3, ?4)",
    )?;
    for (pos, raw) in elems.iter().enumerate() {
        let mut id = item_id(coll, raw).unwrap_or_else(|| format!("#{pos}"));
        if !seen.insert(id.clone()) {
            // 重名元素：用位置区分，保证不丢数据
            id = format!("{id}#{pos}");
            seen.insert(id.clone());
        }
        let text = raw.get();
        match existing.get(&id) {
            Some((p, v)) if *p == pos as i64 && v == text => {}
            _ => {
                upsert.execute(params![coll, id, pos as i64, text])?;
            }
        }
    }
    drop(upsert);
    let mut del = tx.prepare_cached("DELETE FROM app_items WHERE coll = ?1 AND id = ?2")?;
    for id in existing.keys() {
        if !seen.contains(id) {
            del.execute(params![coll, id])?;
        }
    }
    Ok(())
}

fn remove_key_tx(tx: &Transaction, key: &str) -> Result<()> {
    tx.execute("DELETE FROM app_kv WHERE key = ?1", [key])?;
    tx.execute("DELETE FROM app_items WHERE coll = ?1", [key])?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn mem() -> Store {
        Store::init(Connection::open_in_memory().unwrap()).unwrap()
    }

    fn state(s: &Store) -> Value {
        serde_json::from_str(&s.load_all().unwrap()).unwrap()
    }

    #[test]
    fn array_roundtrip_keeps_order_and_text() {
        let s = mem();
        let subs = r#"[{"name":"b","z":1,"a":2},{"name":"a","url":"http://x"}]"#;
        s.set_key("subs", subs);
        // 元素原文（含键顺序）原样保留，数组顺序保留
        assert_eq!(s.load_all().unwrap(), format!(r#"{{"subs":{subs}}}"#));
    }

    #[test]
    fn diff_only_touches_changed_rows() {
        let s = mem();
        s.set_key("subs", r#"[{"name":"a","v":1},{"name":"b","v":1}]"#);
        let changes = |s: &Store| s.conn.lock().unwrap().total_changes();
        let before = changes(&s);
        s.set_key("subs", r#"[{"name":"a","v":1},{"name":"b","v":2}]"#);
        let delta = changes(&s) - before;
        // 只有 b 被改写（1 次 INSERT OR REPLACE，外加删除 app_kv 里不存在的键 0 行）
        assert_eq!(delta, 1, "只应写 1 行，实际 {delta}");
        assert_eq!(state(&s)["subs"][1]["v"], 2);
        // 完全相同的再次保存 → 零写入
        let before = changes(&s);
        s.set_key("subs", r#"[{"name":"a","v":1},{"name":"b","v":2}]"#);
        assert_eq!(changes(&s) - before, 0);
    }

    #[test]
    fn delete_reorder_and_empty() {
        let s = mem();
        s.set_key("subs", r#"[{"name":"a"},{"name":"b"},{"name":"c"}]"#);
        s.set_key("subs", r#"[{"name":"c"},{"name":"a"}]"#);
        let names: Vec<_> = state(&s)["subs"]
            .as_array()
            .unwrap()
            .iter()
            .map(|v| v["name"].as_str().unwrap().to_string())
            .collect();
        assert_eq!(names, ["c", "a"]);
        s.set_key("subs", "[]");
        assert_eq!(state(&s)["subs"], serde_json::json!([]));
        s.remove_key("subs");
        assert!(state(&s).get("subs").is_none());
    }

    #[test]
    fn duplicate_names_and_non_objects_do_not_lose_data() {
        let s = mem();
        s.set_key("subs", r#"[{"name":"x","n":1},{"name":"x","n":2},5,"s"]"#);
        let v = state(&s);
        assert_eq!(v["subs"].as_array().unwrap().len(), 4);
        assert_eq!(v["subs"][1]["n"], 2);
        assert_eq!(v["subs"][2], 5);
    }

    #[test]
    fn tokens_use_composite_id() {
        let s = mem();
        s.set_key(
            "tokens",
            r#"[{"token":"t","type":"sub","name":"a"},{"token":"t","type":"col","name":"a"}]"#,
        );
        assert_eq!(state(&s)["tokens"].as_array().unwrap().len(), 2);
    }

    #[test]
    fn kv_keys_and_virtual_state_key() {
        let s = mem();
        s.set_key("settings", r#"{"a":1}"#);
        s.set_key("subs", r#"[{"name":"a"}]"#);
        s.set("sub-store-cached-resource", "{\"u\":1}");
        assert_eq!(s.get("sub-store-cached-resource").as_deref(), Some("{\"u\":1}"));
        // 虚拟键：导出
        let dump = s.get(STATE_KEY).unwrap();
        assert_eq!(serde_json::from_str::<Value>(&dump).unwrap()["settings"]["a"], 1);
        // 虚拟键：整体替换（还原备份），旧键被清掉
        s.set(STATE_KEY, r#"{"settings":{"b":2},"collections":[{"name":"c"}]}"#);
        let v = state(&s);
        assert_eq!(v["settings"]["b"], 2);
        assert!(v.get("subs").is_none());
        assert_eq!(v["collections"][0]["name"], "c");
        // 原始键不受影响
        assert!(s.get("sub-store-cached-resource").is_some());
        s.remove("sub-store-cached-resource");
        assert!(s.get("sub-store-cached-resource").is_none());
    }

    #[test]
    fn migrates_legacy_cache_json_once() {
        let dir = std::env::temp_dir().join(format!("substore-mig-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let legacy = serde_json::json!({
            "sub-store": r#"{"subs":[{"name":"s1","url":"http://a"}],"settings":{"x":1}}"#,
            "sub-store-cached-resource": "{\"k\":2}"
        });
        std::fs::write(dir.join("cache.json"), legacy.to_string()).unwrap();

        let s = Store::open(&dir).unwrap();
        let v = state(&s);
        assert_eq!(v["subs"][0]["name"], "s1");
        assert_eq!(v["settings"]["x"], 1);
        assert_eq!(s.get("sub-store-cached-resource").as_deref(), Some("{\"k\":2}"));
        assert!(!dir.join("cache.json").exists());
        assert!(dir.join("cache.json.migrated").exists());
        drop(s);

        // 重新打开：数据还在，且不会被旧文件覆盖
        std::fs::write(dir.join("cache.json"), r#"{"sub-store":"{}"}"#).unwrap();
        let s = Store::open(&dir).unwrap();
        assert_eq!(state(&s)["subs"][0]["name"], "s1");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn bad_legacy_file_is_kept() {
        let dir = std::env::temp_dir().join(format!("substore-bad-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("cache.json"), "not json").unwrap();
        let s = Store::open(&dir).unwrap();
        assert_eq!(s.load_all().unwrap(), "{}");
        assert!(dir.join("cache.json").exists());
        let _ = std::fs::remove_dir_all(&dir);
    }
}
