# Sub-Store Go Backend

Go 语言完整重写的 [Sub-Store](https://github.com/sub-store-org/Sub-Store) 后端。

**零外部依赖** — 仅使用 Go 标准库。

---

## 特性

| 功能 | 状态 |
|------|------|
| 订阅管理 (CRUD) | ✅ |
| 订阅组/集合管理 | ✅ |
| 多格式输出 | ✅ ClashMeta / Surge / QX / Loon / SingBox / Shadowrocket / V2Ray / Stash |
| 协议解析 | ✅ SS / SSR / VMess / VLESS / Trojan / Hysteria / Hysteria2 / TUIC / HTTP / SOCKS5 / WireGuard / Snell |
| Clash YAML 解析 | ✅ |
| URI 列表解析 | ✅ |
| Surge 格式解析 | ✅ |
| Base64 自动解码 | ✅ |
| 流量信息 (Flow Headers) | ✅ |
| 设置管理 | ✅ |
| Artifact 管理 | ✅ |
| Token / Share 链接 | ✅ |
| 文件 / 模块管理 | ✅ |
| 归档 / 恢复 | ✅ |
| 日志管理 | ✅ |
| 排序 | ✅ |
| 备份 (JSON) | ✅ |
| Gist 备份/还原 | ✅ GitHub / GitLab |
| 定时任务 (Cron) | ✅ |
| 数据启动时从 URL 恢复 | ✅ |
| UA 自动检测目标格式 | ✅ |

---

## 快速开始

### 二进制运行

```bash
go build -o sub-store-backend .
./sub-store-backend
```

### Docker

```bash
docker build -t sub-store-go .
docker run -d \
  -p 3000:3000 \
  -v $(pwd)/data:/root/.sub-store \
  sub-store-go
```

### Docker Compose

```bash
docker compose up -d
```

访问前端：`https://sub-store.vercel.app`，后端 API 填写 `http://your-server:3000`

---

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SUB_STORE_BACKEND_API_HOST` | `0.0.0.0` | 监听地址 |
| `SUB_STORE_BACKEND_API_PORT` | `3000` | 监听端口 |
| `SUB_STORE_DATA_PATH` | `~/.sub-store/data.json` | 数据文件路径 |
| `SUB_STORE_DATA_URL` | - | 启动时从该 URL 下载并恢复数据 |
| `SUB_STORE_BACKEND_SYNC_CRON` | - | Artifact 同步定时表达式 |
| `SUB_STORE_BACKEND_DOWNLOAD_CRON` | - | Gist 下载定时表达式 |
| `SUB_STORE_BACKEND_UPLOAD_CRON` | - | Gist 上传定时表达式 |
| `SUB_STORE_PRODUCE_CRON` | - | 产物生成定时（格式：`cron,type,name;...`）|
| `SUB_STORE_MMDB_CRON` | - | MaxMindDB 更新定时 |
| `SUB_STORE_MMDB_COUNTRY_PATH` | - | Country.mmdb 本地路径 |
| `SUB_STORE_MMDB_COUNTRY_URL` | - | Country.mmdb 下载地址 |
| `SUB_STORE_MMDB_ASN_PATH` | - | ASN.mmdb 本地路径 |
| `SUB_STORE_MMDB_ASN_URL` | - | ASN.mmdb 下载地址 |

Cron 表达式支持：`@every 5m` / `@hourly` / `@daily` / `*/5 * * * *` 等格式。

---

## API 端点

### 订阅
```
GET    /api/subs               # 获取所有订阅
POST   /api/subs               # 创建订阅
PUT    /api/subs               # 批量替换
GET    /api/sub/:name          # 获取单个
PATCH  /api/sub/:name          # 更新
DELETE /api/sub/:name          # 删除 (?mode=archive|permanent)
GET    /api/sub/flow/:name     # 获取流量信息
POST   /api/subs/sort          # 排序
```

### 订阅组
```
GET    /api/collections
POST   /api/collections
PUT    /api/collections
GET    /api/collection/:name
PATCH  /api/collection/:name
DELETE /api/collection/:name
POST   /api/collections/sort
```

### 下载 / 分发
```
GET /download/:name[/:target]                   # 下载订阅
GET /download/collection/:name[/:target]        # 下载订阅组
GET /share/sub/:name[/:target]?token=xxx        # 带 Token 分享
GET /share/col/:name[/:target]?token=xxx        # 带 Token 分享组
```

`target` 支持：`ClashMeta` `Clash` `Surge` `QX` `Loon` `SingBox` `Shadowrocket` `Stash` `V2Ray` `JSON`

不指定 target 时，根据 `User-Agent` 自动判断。

### 其他
```
GET    /api/utils/env          # 环境信息
GET    /api/utils/refresh      # 清除缓存
GET    /api/utils/backup       # Gist 备份 (?action=upload|download)
GET    /api/storage            # 导出全部数据
POST   /api/storage            # 导入数据
GET    /api/settings           # 获取设置
PATCH  /api/settings           # 更新设置
GET    /api/artifacts          # Artifact 管理
GET    /api/tokens             # Token 管理
GET    /api/archives           # 归档列表
GET    /api/logs               # 日志
DELETE /api/logs               # 清除日志
GET    /api/preview/sub/:name  # 预览订阅
GET    /api/preview/collection/:name
POST   /api/utils/parser       # 解析测试
```

---

## 架构

```
sub-store-go/
├── main.go
├── internal/
│   ├── config/     # 环境变量配置
│   ├── store/      # JSON 文件持久化存储
│   ├── api/        # HTTP 路由 + 所有处理器
│   │   ├── router.go         # 零依赖 HTTP 路由器
│   │   ├── server.go         # 服务器初始化
│   │   ├── subscriptions.go
│   │   ├── collections.go
│   │   ├── settings.go
│   │   ├── artifacts.go
│   │   ├── generics.go       # File / Module / Token
│   │   ├── archives.go
│   │   ├── download.go       # 核心下载分发
│   │   ├── miscs.go          # 备份/恢复/Gist
│   │   ├── gist.go           # GitHub/GitLab Gist 客户端
│   │   ├── cron.go           # 定时任务
│   │   └── ...
│   └── proxy/
│       ├── types.go          # Proxy 结构体
│       ├── fetch.go          # HTTP 下载
│       ├── parser.go         # 多格式解析
│       ├── producer.go       # 多格式输出
│       ├── yaml.go           # YAML 编码器
│       └── yaml_parser.go    # YAML 解析器
└── Dockerfile
```
