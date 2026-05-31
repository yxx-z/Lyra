# Lyra — Claude Code 参考文档

自托管音乐服务器，Go 后端 + Vue 3 前端打包为单一二进制文件。

## 常用命令

| 任务 | 命令 |
|------|------|
| 完整构建 | `make build` |
| 仅构建前端 | `make build-frontend` |
| 运行后端（开发） | `make dev-backend` |
| 运行前端（开发） | `make dev-frontend` |
| 运行全部测试 | `make test` |
| 构建 Docker 镜像 | `make docker-build` |

## 开发工作流

开两个终端：
1. `make dev-frontend` —— Vite 开发服务器监听 :5173，将 /api、/rest 代理到 :4533
2. `make dev-backend` —— Go 服务器监听 :4533

前端访问 http://localhost:5173，API 访问 http://localhost:4533。

## 架构

```
cmd/server/main.go      入口，组装 config + db + router
internal/config/        YAML 配置加载器
internal/db/            SQLite（modernc 纯 Go，无 CGo）
  migrations/           *.up.sql 按字母顺序依次执行
internal/api/           Chi 路由 —— /health、静态前端、后续 API 路由
ui/ui.go                //go:embed all:dist —— 编译时嵌入前端
web/                    Vue 3 源码（npm 项目）
  vite.config.ts        outDir: ../ui/dist（而非 web/dist，受 Go embed 路径约束）
```

## 关键技术决策

- **modernc.org/sqlite**（非 mattn/go-sqlite3）：纯 Go，无 CGo，可轻松交叉编译到 arm64
- **手写迁移运行器**：按字母顺序读取 `internal/db/migrations/*.up.sql`，在 `schema_migrations` 表中追踪已执行版本；避免 golang-migrate 的 CGo sqlite 依赖
- **ui/ 包负责 embed**：Go `//go:embed` 不能使用 `..` 路径，前端输出放到 `ui/dist/`（`web/` 的兄弟目录），由 `ui/ui.go` 嵌入
- **Subsonic 密码独立**：`subsonic.password` 配置项与管理员 Token 隔离

## 添加数据库迁移

1. 创建 `internal/db/migrations/NNN_描述.up.sql`（NNN 为下一个序号，补零对齐）
2. 同步更新 `internal/db/schema.sql` 至最新状态
3. 运行 `go test ./internal/db/...` 验证迁移可以正常执行

## Go 环境

当前 Go 安装在：`/home/yxx/go-local/go/bin/go`
使用前执行：`export PATH=$PATH:/home/yxx/go-local/go/bin`
