//! 对编译产物做一次极小的字符串补丁，把「单请求脚本模型」改成可重复调用的宿主接口。
//!
//! 上游 `src/vendor/express.js` 的非 Node 分支是：
//!     app.start = () => { dispatch($request) }
//! 即脚本被宿主调用一次、处理一个请求、用 $done 回吐响应。
//! 这里把它替换为注册一个全局函数，由 Rust 每次请求主动调用。
//! 不改动任何业务逻辑，也不需要重新用 esbuild 打包。

const PATCH_FROM: &str = "app.start=()=>{dispatch($request)},app;";

const PATCH_TO: &str = concat!(
    "app.start=()=>{",
    "globalThis.__substoreDispatch=(reqJson)=>{",
    "globalThis.$request=JSON.parse(reqJson);",
    "dispatch(globalThis.$request)",
    "}},app;"
);

/// 校验并生成可供 QuickJS 直接 eval 的源码。
pub fn prepare(source: &str) -> anyhow::Result<String> {
    let hits = source.matches(PATCH_FROM).count();
    anyhow::ensure!(
        hits == 1,
        "bundle 补丁锚点必须且只能出现 1 次，实际 {hits} 次；产物版本可能不是 2.44.7"
    );
    Ok(source.replace(PATCH_FROM, PATCH_TO))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn patch_is_applied_once() {
        let src = format!("prefix{PATCH_FROM}suffix");
        let out = prepare(&src).unwrap();
        assert!(out.contains("__substoreDispatch"));
        assert!(!out.contains(PATCH_FROM));
    }

    #[test]
    fn rejects_unknown_bundle() {
        assert!(prepare("var a=1;").is_err());
    }
}
