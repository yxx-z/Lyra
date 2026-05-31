# 阶段 1：构建前端
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
# vite.config.ts 设置 outDir: ../ui/dist，输出到 /app/ui/dist
RUN npm run build

# 阶段 2：构建 Go 二进制
FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 用真实构建产物覆盖 .gitkeep 占位文件
COPY --from=frontend /app/ui/dist ./ui/dist
RUN go build -o lyra ./cmd/server

# 阶段 3：最小运行时镜像
FROM alpine:3.19
RUN apk add --no-cache ffmpeg chromaprint
COPY --from=builder /app/lyra /usr/local/bin/lyra
EXPOSE 4533
CMD ["lyra"]
