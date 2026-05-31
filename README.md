# Sub-Store（前后端合并版）

Go 后端编译时通过 `go:embed` 将 Vue 前端产物内嵌，**单一二进制文件**即可运行完整服务，无需 Node.js、无需 Docker、无需任何运行时依赖。

---

## 目录结构

```
.
├── frontend/                        # Vue 3 前端源码
├── backend/                         # Go 后端源码
│   └── internal/
│       ├── static/static.go         # go:embed 入口（新增）
│       ├── api/server.go            # 静态文件服务（修改）
│       └── api/router.go            # SPA fallback（修改）
├── Dockerfile                       # 多阶段 Docker 构建
├── docker-compose.yml
└── .github/workflows/build.yml      # CI/CD：二进制 + Docker + Release
```

---

## 纯二进制运行

### 方式一：下载预编译二进制（推荐）

从 [Releases](../../releases) 页面下载对应平台的文件：

| 文件 | 平台 |
|------|------|
| `sub-store-linux-amd64` | Linux x86_64 |
| `sub-store-linux-arm64` | Linux ARM64（树莓派、ARM 服务器） |
| `sub-store-darwin-amd64` | macOS Intel |
| `sub-store-darwin-arm64` | macOS Apple Silicon |
| `sub-store-windows-amd64.exe` | Windows x86_64 |

```bash
# Linux / macOS
chmod +x sub-store-linux-amd64
./sub-store-linux-amd64

# 访问 http://localhost:3000
```

### 方式二：从源码编译

```bash
# 1. 构建前端
cd frontend
pnpm install
VITE_API_URL="" pnpm build

# 2. 将 dist 放入 embed 目录
cp -r dist ../backend/internal/static/dist

# 3. 编译 Go（包含内嵌前端）
cd ../backend
go mod download
go build -ldflags="-s -w" -o sub-store .

# 4. 运行
SUB_STORE_BACKEND_MERGE=true ./sub-store
# 访问 http://localhost:3000
```

---

## Docker 运行

```bash
docker compose up -d
# 访问 http://your-server:3000
```

---

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SUB_STORE_BACKEND_API_HOST` | `0.0.0.0` | 监听地址 |
| `SUB_STORE_BACKEND_API_PORT` | `3000` | 监听端口 |
| `SUB_STORE_DATA_PATH` | `~/.sub-store/data.json` | 数据文件路径 |
| `SUB_STORE_BACKEND_MERGE` | `false` | **`true` = 前后端同端口（二进制运行必须设此项）** |
| `SUB_STORE_DATA_URL` | — | 启动时从 URL 恢复数据 |
| `SUB_STORE_BACKEND_SYNC_CRON` | — | Artifact 同步定时 |
| `SUB_STORE_BACKEND_DOWNLOAD_CRON` | — | Gist 下载定时 |
| `SUB_STORE_BACKEND_UPLOAD_CRON` | — | Gist 上传定时 |

---

## CI/CD

推送 `v*.*.*` tag 自动触发 Release：

```bash
git tag v1.0.0
git push origin v1.0.0
```

Workflow 会自动：
1. 构建 Vue 前端
2. 编译 5 平台二进制（前端已 embed 进去）
3. 构建 Docker 多架构镜像并推送到 GHCR
4. 创建 GitHub Release，附上所有二进制 + `dist.zip` + `checksums.txt`
