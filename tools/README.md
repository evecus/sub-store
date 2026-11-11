# tools/build-bundle.cjs

从上游 Sub-Store 后端源码构建瘦身 bundle（3.35MB → 0.93MB，字节码 3.78MB → 1.41MB）。

    git clone/解压 Sub-Store 到 ../Sub-Store && cd ../Sub-Store/backend
    pnpm install --ignore-scripts --frozen-lockfile
    node <本项目>/tools/build-bundle.cjs . <本项目>/assets/sub-store.bundle.js
    cd <本项目> && cargo run --release --bin gen_bytecode && cargo build --release

- 只构建上游的「浏览器版」第一段；Node 依赖（undici/express/iconv-lite/mime-db…）在 Rust 宿主里是死代码，不再打包。
- jsrsasign 换成 1KB 的 SHA-256 指纹实现；age 加密默认移除（`KEEP_AGE=1` 保留）。
- 升级上游版本后，若构建报“补丁锚点出现次数不为 1”，说明 `src/bundle.rs` 的补丁需要跟着改。
