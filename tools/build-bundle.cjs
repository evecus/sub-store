#!/usr/bin/env node
// 从上游 Sub-Store 后端源码构建「瘦身版」bundle，供 Rust/QuickJS 宿主使用。
//
// 与上游 `pnpm bundle:esbuild` 的区别：
//   上游分两段：① 浏览器版 IIFE（src/main.js）→ ② 再用 platform=node 把 undici / express /
//   iconv-lite / mime-db / geoip2 等 Node 依赖整包塞进去（约 +1.9MB）。
//   Rust 宿主走非 Node 分支（isNode=false），这些依赖永远不会被执行，纯属白占字节码与堆。
//   这里只做第 ① 段，并再做两处替换（见下）。
//
// 用法：
//   cd <Sub-Store>/backend && pnpm install --ignore-scripts --frozen-lockfile
//   node <本项目>/tools/build-bundle.cjs <Sub-Store>/backend [输出路径=assets/sub-store.bundle.js]
//   cargo run --bin gen_bytecode      # 重新生成字节码
//
// 环境变量：KEEP_AGE=1  保留 age 加密（仅用到 age 加密订阅输出时才需要，约 +140KB）
const path = require('path');
const fs = require('fs');
const { builtinModules, createRequire } = require('module');

const backend = path.resolve(process.argv[2] || '');
if (!fs.existsSync(path.join(backend, 'package.json'))) {
    console.error('用法: node build-bundle.cjs <Sub-Store/backend> [out.js]');
    process.exit(2);
}
const out = path.resolve(process.argv[3] || path.join(__dirname, '..', 'assets', 'sub-store.bundle.js'));
const req = createRequire(path.join(backend, 'package.json'));
const { build } = req('esbuild');
const version = JSON.parse(fs.readFileSync(path.join(backend, 'package.json'), 'utf8')).version.trim();
const builtins = new Set(builtinModules.map((n) => n.replace(/^node:/, '')));

// jsrsasign（306KB）在上游只用来算 CA 证书的 SHA-256 指纹：pemtohex + hashHex。
// 换成 ~1KB 的等价实现。
const RS_SHIM = `
const K=new Uint32Array([0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2]);
function sha256(m){
  const l=m.length,n=((l+9+63)>>6)<<6,b=new Uint8Array(n);b.set(m);b[l]=0x80;
  const dv=new DataView(b.buffer);dv.setUint32(n-8,Math.floor(l/0x20000000));dv.setUint32(n-4,(l<<3)>>>0);
  const H=new Uint32Array([0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19]),w=new Uint32Array(64);
  const R=(x,c)=>(x>>>c)|(x<<(32-c));
  for(let o=0;o<n;o+=64){
    for(let i=0;i<16;i++)w[i]=dv.getUint32(o+i*4);
    for(let i=16;i<64;i++){const a=R(w[i-15],7)^R(w[i-15],18)^(w[i-15]>>>3),c=R(w[i-2],17)^R(w[i-2],19)^(w[i-2]>>>10);w[i]=(w[i-16]+a+w[i-7]+c)|0}
    let [a,b2,c,d,e,f,g,h]=H;
    for(let i=0;i<64;i++){
      const t1=(h+(R(e,6)^R(e,11)^R(e,25))+((e&f)^(~e&g))+K[i]+w[i])|0,t2=((R(a,2)^R(a,13)^R(a,22))+((a&b2)^(a&c)^(b2&c)))|0;
      h=g;g=f;f=e;e=(d+t1)|0;d=c;c=b2;b2=a;a=(t1+t2)|0;
    }
    H[0]+=a;H[1]+=b2;H[2]+=c;H[3]+=d;H[4]+=e;H[5]+=f;H[6]+=g;H[7]+=h;
  }
  return Array.from(H,x=>x.toString(16).padStart(8,'0')).join('');
}
const B64='ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
function b64hex(s){
  let bits=0,acc=0,out='';
  for(const ch of s.replace(/=+$/,'')){const v=B64.indexOf(ch);if(v<0)continue;acc=(acc<<6)|v;bits+=6;if(bits>=8){bits-=8;out+=((acc>>bits)&255).toString(16).padStart(2,'0');acc&=(1<<bits)-1}}
  return out;
}
const rs={
  pemtohex(s){
    if(!/-----BEGIN [^-]+-----/.test(s))throw new Error('pem header not found');
    return b64hex(s.replace(/-----(BEGIN|END) [^-]+-----/g,'').replace(/\\s+/g,''));
  },
  KJUR:{crypto:{Util:{hashHex(hex,alg){
    if(alg!=='sha256')throw new Error('slim build: only sha256 is supported');
    const m=new Uint8Array(hex.length/2);for(let i=0;i<m.length;i++)m[i]=parseInt(hex.substr(i*2,2),16);
    return sha256(m);
  }}}}
};
export default rs;
`;
const AGE_STUB = `
const no=()=>{throw new Error('age 加密在瘦身 bundle 中已移除；用 KEEP_AGE=1 重新构建')};
export class Encrypter{constructor(){no()}}
export class Decrypter{constructor(){no()}}
export const identityToRecipient=no, generateX25519Identity=no, generateHybridIdentity=no;
export const armor={encode:no,decode:no};
`;


// ---------------------------------------------------------------------------
// 持久化改造（配合 src/store.rs 的 SQLite）。对上游源码做精确字符串替换，
// 每处都断言只命中预期次数；上游改动导致不匹配时构建直接失败，而不是静默产出坏 bundle。
// ---------------------------------------------------------------------------
const SOURCE_PATCHES = [
    {
        file: /src[\\/]vendor[\\/]open-api\.js$/,
        edits: [
            // persistCache：GUI.for.Cores 分支不再先把整份状态 pretty 序列化一遍，
            // 且整体同步（仅用于 `$.cache = content` 之后）直接交给 Rust 做行级 diff。
            [
                "        const data = JSON.stringify(this.cache, null, 2);\n",
                "        const data = isGUIforCores ? '' : JSON.stringify(this.cache, null, 2);\n",
                1,
            ],
            [
                "        if (isGUIforCores) $Plugins.SubStoreCache.set(this.name, data);\n",
                "        if (isGUIforCores)\n            $Plugins.SubStoreCache.set(this.name, JSON.stringify(this.cache));\n",
                1,
            ],
            // write：只把这一个键交给 Rust（数组键在 Rust 侧按行 diff，只写变化的行）
            [
                "        } else {\n            this.cache[key] = data;\n        }\n        this.persistCache();\n",
                "        } else {\n            this.cache[key] = data;\n            if (isGUIforCores) {\n                return data === undefined\n                    ? $Plugins.SubStoreCache.removeKey(key)\n                    : $Plugins.SubStoreCache.setKey(key, JSON.stringify(data));\n            }\n        }\n        this.persistCache();\n",
                1,
            ],
            [
                "        } else {\n            delete this.cache[key];\n        }\n        this.persistCache();\n",
                "        } else {\n            delete this.cache[key];\n            if (isGUIforCores) return $Plugins.SubStoreCache.removeKey(key);\n        }\n        this.persistCache();\n",
                1,
            ],
        ],
    },
    {
        // 还原备份：上游只在 Node 下同步内存里的 $.cache，其它环境会让内存保留旧状态，
        // 下一次保存就把刚还原的数据覆盖掉。这里让 GUI.for.Cores 也同步。
        file: /src[\\/]restful[\\/]miscs\.js$/,
        edits: [
            [
                "if ($.env.isNode) {\n                    $.cache = content;",
                "if ($.env.isNode || $.env.isGUIforCores) {\n                    $.cache = content;",
                1,
            ],
            [
                "if ($.env.isNode) {\n                $.cache = content;",
                "if ($.env.isNode || $.env.isGUIforCores) {\n                $.cache = content;",
                1,
            ],
        ],
    },
];

function applySourcePatches(file, text) {
    for (const p of SOURCE_PATCHES) {
        if (!p.file.test(file)) continue;
        for (const [from, to, count] of p.edits) {
            const n = text.split(from).length - 1;
            if (n !== count) {
                throw new Error(`源码补丁未命中 ${path.basename(file)}（期望 ${count} 处，实际 ${n} 处）：${JSON.stringify(from.slice(0, 60))}`);
            }
            text = text.split(from).join(to);
        }
    }
    return text;
}

build({
    entryPoints: [path.join(backend, 'src/main.js')],
    absWorkingDir: backend,
    bundle: true, minify: true, sourcemap: false,
    platform: 'browser', format: 'iife',
    outfile: out,
    inject: [req.resolve('core-js/actual/object/has-own')],
    banner: { js: `// SUB_STORE_BACKEND_VERSION: ${version}` },
    plugins: [{
        name: 'slim',
        setup(b) {
            b.onResolve({ filter: /.*/ }, (a) => {
                if (a.path !== 'buffer' && builtins.has(a.path.replace(/^node:/, ''))) return { path: a.path, external: true };
                if (a.path === 'jsrsasign') return { path: 'jsrsasign', namespace: 'slim' };
                if (a.path === 'age-encryption' && !process.env.KEEP_AGE) return { path: 'age-encryption', namespace: 'slim' };
            });
            b.onLoad({ filter: /src[\\/](vendor[\\/]open-api|restful[\\/]miscs)\.js$/ }, (a) => ({
                contents: applySourcePatches(a.path, fs.readFileSync(a.path, 'utf8')),
                loader: 'js',
                resolveDir: path.dirname(a.path),
            }));
            b.onLoad({ filter: /.*/, namespace: 'slim' }, (a) => ({
                contents: a.path === 'jsrsasign' ? RS_SHIM : AGE_STUB, loader: 'js',
            }));
        },
    }],
    logLevel: 'error',
}).then(() => {
    const s = fs.readFileSync(out, 'utf8');
    const n = s.split('app.start=()=>{dispatch($request)},app;').length - 1;
    if (n !== 1) throw new Error(`补丁锚点应出现 1 次，实际 ${n} 次（上游代码变了？）`);
    console.log(`ok: ${out} (${s.length} 字节, 版本 ${version})`);
}).catch((e) => { console.error(e.message); process.exit(1); });
