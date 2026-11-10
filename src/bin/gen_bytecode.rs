//! 构建期工具：把后端 JS bundle 预编译成 QuickJS 字节码。
//!
//! 用法（工程根目录）：`cargo run --bin gen_bytecode`
//! 产物：`assets/sub-store.bundle.qbc`，随后由 `assets.rs` 内嵌进二进制。
//!
//! 收益：运行时不再逐字节解析 3.3MB 源码（冷启动主要开销），
//! 且 `strip_source` 让字节码不携带源码文本，JS 堆进一步变小。
//!
//! 注意：字节码与 QuickJS 版本/CPU 字节序绑定，升级 rquickjs 后必须重新生成。

#[path = "../bundle.rs"]
mod bundle;

use anyhow::{anyhow, Context as _, Result};
use rquickjs::{Context, Module, Runtime, WriteOptions};

fn q(e: rquickjs::Error) -> anyhow::Error {
    anyhow!("QuickJS: {e}")
}

fn main() -> Result<()> {
    let src = std::fs::read_to_string("assets/sub-store.bundle.js")
        .context("读取 assets/sub-store.bundle.js 失败（请在工程根目录运行）")?;
    let patched = bundle::prepare(&src)?;

    let rt = Runtime::new().map_err(q).context("创建 Runtime 失败")?;
    let ctx = Context::full(&rt).map_err(q).context("创建 Context 失败")?;

    let bytes: Vec<u8> = ctx
        .with(|ctx| -> rquickjs::Result<Vec<u8>> {
            let module = Module::declare(
                ctx.clone(),
                "sub-store.bundle.js",
                patched.as_bytes().to_vec(),
            )?;
            module.write(WriteOptions {
                strip_source: true,
                strip_debug: true,
                ..Default::default()
            })
        })
        .map_err(q)
        .context("编译 bundle 为字节码失败（ES module 语法不兼容？）")?;

    anyhow::ensure!(!bytes.is_empty(), "生成的字节码为空");
    std::fs::write("assets/sub-store.bundle.qbc", &bytes)?;

    println!(
        "源码 {} 字节 -> 字节码 {} 字节 ({:.1}%)",
        patched.len(),
        bytes.len(),
        bytes.len() as f64 / patched.len() as f64 * 100.0
    );
    println!("已写入 assets/sub-store.bundle.qbc");
    Ok(())
}
