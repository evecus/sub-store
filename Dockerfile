# ============================================================
# Stage 1: Build frontend (Vue + Vite)
# ============================================================
FROM node:20-alpine AS frontend-builder

WORKDIR /frontend

# 安装 pnpm
RUN npm install -g pnpm

# 先复制依赖文件，利用 Docker 层缓存
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# 复制前端源码并构建
COPY frontend/ .
# 构建时将 API_URL 设为空，运行时由后端同端口提供
RUN VITE_API_URL="" pnpm build

# ============================================================
# Stage 2: Build Go backend
# ============================================================
FROM golang:1.22-alpine AS backend-builder

WORKDIR /app

# 复制 Go 源码
COPY backend/go.mod ./
RUN go mod download 2>/dev/null || true

COPY backend/ .

# 将前端产物复制进来，供 embed 使用
COPY --from=frontend-builder /frontend/dist ./internal/static/dist

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o sub-store .

# ============================================================
# Stage 3: 最终镜像（仅包含可执行文件）
# ============================================================
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=backend-builder /app/sub-store .

# 数据目录
RUN mkdir -p /root/.sub-store

# 后端 API 端口
EXPOSE 3000

ENV SUB_STORE_BACKEND_API_HOST=0.0.0.0
ENV SUB_STORE_BACKEND_API_PORT=3000
ENV SUB_STORE_DATA_PATH=/root/.sub-store/data.json
# 启用前后端合并模式（前端静态文件由后端同端口提供）
ENV SUB_STORE_BACKEND_MERGE=true

CMD ["/app/sub-store"]
