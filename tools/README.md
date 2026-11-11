# tools/build-bundle.cjs

从上游 Sub-Store 后端源码构建瘦身 bundle（3.35MB → 0.93MB，字节码 3.78MB → 1.41MB）。

    git clone/解压 Sub-Store 到 ../Sub-Store && cd ../Sub-Store/backend
    pnpm install --ignore-scripts --frozen-lockfile
    node <本项目>/tools/build-bundle.cjs . <本项目>/assets/sub-store.bundle.js
    cd <本项目> && cargo run --release --bin gen_bytecode && cargo build --release

- 只构建上游的「浏览器版」第一段；Node 依赖（undici/express/iconv-lite/mime-db…）在 Rust 宿主里是死代码，不再打包。
- jsrsasign 换成 1KB 的 SHA-256 指纹实现；age 加密默认移除（`KEEP_AGE=1` 保留）。
- 升级上游版本后，若构建报“补丁锚点出现次数不为 1”，说明 `src/bundle.rs` 的补丁需要跟着改。

## 持久化（SQLite）

数据存放在 `$SUB_STORE_DATA_DIR/substore.db`（WAL 模式，会有 `-wal`/`-shm` 伴随文件）：

| 表 | 内容 |
|---|---|
| `app_items` | subs / collections / files / modules / artifacts / tokens / archives，**一条一行**，保存时只写变化的行 |
| `app_kv` | settings、schemaVersion、rules 等 |
| `raw_kv` | 资源缓存、响应头缓存、脚本缓存、日志 |

- 首次启动若发现旧版 `cache.json` 且数据库为空，会自动导入并把原文件改名为 `cache.json.migrated`；导入失败则原文件不动。
- 备份导出/还原（`/api/storage`）、Gist 同步仍然可用；还原后内存状态会同步刷新。
- 备份数据库：`sqlite3 substore.db ".backup backup.db"`（服务运行中也安全）。
- `build-bundle.cjs` 里的 `SOURCE_PATCHES` 是对上游 `open-api.js` / `miscs.js` 的精确替换，每处都断言命中次数，上游改动导致不匹配时构建会直接失败。
