# 浏览 REST API 设计文档

> 版本：1.0 · 日期：2026-05-31 · 状态：已批准

---

## 目标

为 Web UI 提供一套干净的 JSON REST API，支持浏览音乐库（专辑/艺术家/曲目）、音频流、封面图和搜索，并带有用户名+密码认证。

对应 PRD：US-12（浏览）、US-14（搜索）、US-18（Web 播放）

---

## 架构决策

- **薄 handler + 直接查 DB**：handler 直接使用 `*sql.DB`，无中间 service 层。v0.2 重构时自然提取查询层。
- **不使用 Subsonic API**：Subsonic 是给第三方客户端（DSub 等）用的，Web UI 使用独立的 REST API，设计目标不同。
- **封面优先级**：内嵌标签（ID3 APIC / FLAC PICTURE）> 同目录 `cover.jpg` / `folder.jpg` > 404
- **v0.1 不转码**：音频流直传，支持 HTTP Range 请求

---

## 配置变更

`internal/config/config.go` 的 `AuthConfig` 新增两个字段：

```go
type AuthConfig struct {
    Disable  bool   `yaml:"disable"`
    Token    string `yaml:"token"`
    Username string `yaml:"username"` // 新增，默认 "admin"
    Password string `yaml:"password"` // 新增，空时启动打印警告
}
```

`config.example.yaml` 对应更新：

```yaml
auth:
  disable: false
  username: admin        # Web UI 登录用户名
  password: ""           # Web UI 登录密码，留空时启动打印警告
  token: ""              # Bearer token，留空时自动生成并写回配置
```

默认值：`Username: "admin"`，`Password: ""`。

---

## API 端点

### 认证

```
POST /api/v1/auth/login
    Content-Type: application/json
    Body: {"username": "admin", "password": "xxx"}

    成功 200:
    {"token": "<bearer-token>"}

    失败 401:
    {"error": "用户名或密码错误"}
```

`/api/v1/auth/login` 是唯一不需要 token 的端点。

所有其他 `/api/v1/*` 端点需要：
```
Authorization: Bearer <token>
```

`auth.disable: true` 时跳过所有认证检查。

---

### 浏览

**列出所有专辑**

```
GET /api/v1/albums

响应 200:
{
  "albums": [
    {
      "id": "uuid",
      "title": "金片子",
      "artist": "蔡琴",
      "artist_id": "uuid",
      "year": 1984,
      "track_count": 10,
      "cover_url": "/api/v1/cover/uuid"
    }
  ]
}
```

`cover_url` 字段：若该专辑有封面（内嵌或 cover.jpg）则返回路径，否则返回空字符串 `""`。

**专辑详情 + 曲目**

```
GET /api/v1/albums/:id

响应 200:
{
  "id": "uuid",
  "title": "金片子",
  "artist": "蔡琴",
  "artist_id": "uuid",
  "year": 1984,
  "cover_url": "/api/v1/cover/uuid",
  "tracks": [
    {
      "id": "uuid",
      "title": "渡口",
      "track_number": 1,
      "disc_number": 1,
      "duration": 245,
      "format": "flac",
      "bitrate": 0,
      "stream_url": "/api/v1/tracks/uuid/stream"
    }
  ]
}
```

`响应 404`：专辑不存在。

**列出所有艺术家**

```
GET /api/v1/artists

响应 200:
{
  "artists": [
    {
      "id": "uuid",
      "name": "蔡琴",
      "album_count": 3
    }
  ]
}
```

**艺术家详情 + 专辑列表**

```
GET /api/v1/artists/:id

响应 200:
{
  "id": "uuid",
  "name": "蔡琴",
  "albums": [
    {
      "id": "uuid",
      "title": "金片子",
      "year": 1984,
      "track_count": 10,
      "cover_url": "/api/v1/cover/uuid"
    }
  ]
}
```

---

### 媒体

**音频流**

```
GET /api/v1/tracks/:id/stream

- 支持 HTTP Range 请求（拖拽 seek）
- 直传原始文件，不转码
- Content-Type 映射：mp3→audio/mpeg，flac→audio/flac，m4a→audio/mp4，ogg→audio/ogg，opus→audio/ogg，wav→audio/wav，aiff/aif→audio/aiff，wma→audio/x-ms-wma，未知→application/octet-stream
- 响应 404：曲目不存在或文件不可用（is_available=0）
- 响应 404：文件路径对应磁盘文件不存在
```

**专辑封面**

```
GET /api/v1/cover/:id    （id 为 album_id）

提取顺序：
1. 读专辑任一曲目文件的内嵌封面（ID3 APIC / FLAC PICTURE / M4A cover）
2. 读专辑目录下 cover.jpg 或 folder.jpg（不区分大小写）
3. 以上都无 → 404

- Content-Type: image/jpeg 或 image/png（按实际内容）
- 不缓存到磁盘（v0.1，v0.2 刮削阶段才缓存）
```

---

### 搜索

```
GET /api/v1/search?q=渡口

- q 为空时返回 400
- 搜索范围：曲名、艺术家名、专辑名（SQLite LIKE '%q%'，v0.1 不使用 FTS5）
- 最多返回各类型前 20 条

响应 200:
{
  "tracks": [
    {
      "id": "uuid",
      "title": "渡口",
      "artist": "蔡琴",
      "album": "金片子",
      "album_id": "uuid",
      "duration": 245,
      "stream_url": "/api/v1/tracks/uuid/stream"
    }
  ],
  "albums": [
    {
      "id": "uuid",
      "title": "金片子",
      "artist": "蔡琴",
      "cover_url": "/api/v1/cover/uuid"
    }
  ],
  "artists": [
    {
      "id": "uuid",
      "name": "蔡琴"
    }
  ]
}
```

---

## 认证中间件

```
internal/api/middleware/auth.go

func BearerAuth(token string, disabled bool) func(http.Handler) http.Handler
    - disabled=true → 直接放行
    - 检查 Authorization: Bearer <token> 头
    - 不匹配 → 401 {"error": "未授权"}
```

Router 挂载：
```go
r.Route("/api/v1", func(r chi.Router) {
    r.Post("/auth/login", authHandler.Login)
    r.Group(func(r chi.Router) {
        r.Use(middleware.BearerAuth(cfg.Auth.Token, cfg.Auth.Disable))
        // 其他所有端点
    })
})
```

---

## 文件结构

```
internal/api/v1/
├── auth.go          POST /auth/login
├── auth_test.go
├── albums.go        GET /albums, GET /albums/:id
├── albums_test.go
├── artists.go       GET /artists, GET /artists/:id
├── artists_test.go
├── cover.go         GET /cover/:id（内嵌封面 + cover.jpg）
├── cover_test.go
├── stream.go        GET /tracks/:id/stream
├── stream_test.go
├── search.go        GET /search?q=
├── search_test.go
└── library.go       ← 已有（扫描端点），保持不动

internal/api/middleware/
└── auth.go          BearerAuth 中间件

internal/api/router.go   ← 更新路由注册
internal/config/config.go ← 新增 Username、Password 字段
```

---

## 测试策略

| 测试 | 方式 |
|------|------|
| 登录成功/失败 | httptest，验证 200/401 |
| 无 token 访问保护路由 | httptest，验证 401 |
| albums/artists 列表 | 内存 SQLite 插入测试数据，验证字段 |
| stream 端点 | 临时文件，验证 Range 支持 |
| cover 提取 | 测试 cover.jpg 文件路径 |
| search 返回结构 | 内存 SQLite，验证三类结果 |

---

## 不在本次范围内

- FTS5 全文索引搜索（v0.3，US-14 高级版本）
- 转码（v0.4，US-22）
- 播放统计 scrobble（v0.4）
- 封面缓存到磁盘（v0.2）
- 多用户（v1.0）
