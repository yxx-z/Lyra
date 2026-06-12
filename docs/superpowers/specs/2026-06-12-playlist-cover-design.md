# 歌单封面（自动 + 自定义上传）设计文档

> 版本：1.0 · 日期：2026-06-12 · 状态：已批准

---

## 背景

自建歌单当前没有封面，列表里只有占位，不美观。需要：

1. **自动封面**：没上传自定义图时，用歌单**第一首歌所属专辑**的封面。
2. **自定义封面**：用户可上传一张图作为歌单封面。

封面目前都按 album ID 实时输出（`GET /api/v1/cover/{albumID}`，从音轨内嵌图 / 同目录 cover.jpg / 刮削缓存兜底）。歌单需要自己的封面入口。

---

## 范围

**做**：

- 歌单封面动态输出：自定义图优先，否则回退到首曲专辑封面，都没有则 404（前端占位兜底）。
- 上传自定义封面（jpeg/png，≤5MB），存到现有 `artwork_dir`。
- 删除自定义封面 → 恢复自动封面。
- 歌单列表/详情 DTO 带 `cover_url`，前端统一显示；详情头部加「上传封面」/「恢复自动封面」入口。

**不做**（YAGNI）：

- 四宫格拼图 / 多专辑合成封面（已选「单张封面」方案）。
- 物化预生成封面文件（自动封面纯动态，跟随首曲，无存储无过期）。
- 封面裁剪 / 缩放 / 多尺寸 / 在线编辑。

---

## 服务方式：动态输出（不预生成）

`playlists` 表只存**自定义上传**的封面路径。请求歌单封面时：

1. 有自定义图（`cover_path` 非空且文件存在）→ 输出它。
2. 否则查歌单内 `position` 最小的曲目的 `album_id`，复用现有 `CoverHandler.ServeCover` 输出该专辑封面。
3. 都没有（空歌单且无自定义图，或专辑无封面）→ 404。

自动封面永远跟随当前第一首歌，随歌单增删/重排自然变化，零额外存储、无过期。

---

## 数据 & 存储

- `playlists` 表加一列：`cover_path TEXT`（仅自定义上传时有值；NULL/'' 表示用自动封面）。
- 新增迁移 `internal/db/migrations/010_playlist_cover.up.sql`：
  ```sql
  ALTER TABLE playlists ADD COLUMN cover_path TEXT NOT NULL DEFAULT '';
  ```
  同步更新 `internal/db/schema.sql` 的 `playlists` 建表语句。
- 上传图存到现有 `artwork_dir`（配置默认 `./data/artwork`，与专辑刮削封面同目录），命名 `playlist_<id>.<ext>`（ext 取自上传类型：`.jpg` / `.png`）。同一歌单重复上传时，先删旧文件再写新文件，避免 jpg→png 残留。
- 接受 `image/jpeg`、`image/png`；大小上限 5MB。

---

## 后端 API

全部在 `/api/v1` 鉴权组内，且**仅歌单 owner** 可操作（歌单按现有约定是按用户私有的；非 owner 返回 404，与现有 `playlists.ErrNotFound` 一致，不泄露存在性）。

新增一个 handler（落在 `internal/api/v1/playlist_cover.go`），持有 `*sql.DB`、`*playlists.Store`、`artworkDir string`，并复用现有 `*CoverHandler`（用于回退到专辑封面的输出逻辑）：

- **`GET /api/v1/playlists/{id}/cover`**
  - 校验 owner（查 `playlists.user_id == 当前用户`，否则 404）。
  - 有自定义图 → 读 `cover_path` 文件输出（按扩展名设 Content-Type）。
  - 否则查 `SELECT t.album_id FROM playlist_tracks pt JOIN tracks t ON t.id=pt.track_id WHERE pt.playlist_id=? ORDER BY pt.position LIMIT 1`，拿到 albumID → 调 `coverHandler.ServeCover(w, r, albumID)`。
  - 无曲目 → 404。

- **`PUT /api/v1/playlists/{id}/cover`**（multipart/form-data，字段名 `cover`）
  - 校验 owner。
  - 限制请求体大小（`http.MaxBytesReader`，5MB）；读 file header，校验 Content-Type ∈ {jpeg, png}，否则 400。
  - 删除该歌单已有的自定义文件（若 `cover_path` 非空）。
  - 写到 `artworkDir/playlist_<id>.<ext>`（`os.MkdirAll` 保证目录存在），`UPDATE playlists SET cover_path=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`。
  - 返回 `{"cover_url": "/api/v1/playlists/<id>/cover"}` 或 204。

- **`DELETE /api/v1/playlists/{id}/cover`**
  - 校验 owner。
  - 删除自定义文件（若存在），`UPDATE playlists SET cover_path='' WHERE id=? AND user_id=?` → 恢复自动封面。
  - 返回 204。

DTO：`playlistSummary` 与歌单详情响应都加字段 `cover_url string `json:"cover_url"``，值固定为 `/api/v1/playlists/<id>/cover`（前端始终请求这个 URL，回退逻辑在后端）。

路由注册：在现有歌单路由分组下加这三条（`internal/api/router.go` 中歌单注册处）。`artworkDir` 从已构造的 config 传入（与 `NewMetadataService` 用的同一个 `cfg.Cache.ArtworkDir`）。

---

## 前端（`PlaylistsPage.vue` + `client.ts`）

- **显示**：歌单卡片与详情头部用 `<img :src="pl.cover_url">` 显示封面；`@error` 时切到现有占位（无封面的空歌单 / 无自定义图）。
- **详情头部操作**（仅自己的歌单）：
  - 「上传封面」：`<input type="file" accept="image/jpeg,image/png">`，选图 → `api.uploadPlaylistCover(id, file)`，成功后刷新封面 —— 给 `cover_url` 加时间戳 query（`?t=Date.now()`）破浏览器缓存。
  - 「恢复自动封面」：调 `api.deletePlaylistCover(id)`，成功后同样刷新。（始终显示即可；对已是自动封面的歌单点它无害。）
- `client.ts` 新增：
  - `uploadPlaylistCover(id: string, file: File): Promise<void>`（FormData，字段 `cover`，PUT）。
  - `deletePlaylistCover(id: string): Promise<void>`（DELETE）。
- 歌单 DTO 类型（`PlaylistSummary` / `PlaylistDetail`）加 `cover_url: string`。

---

## 测试

| 测试 | 方式 |
|------|------|
| 迁移 `010` 可执行、`schema.sql` 与迁移一致 | `go test ./internal/db/...` |
| GET 封面：自定义优先 / 回退首曲专辑 / 空歌单 404 / 非 owner 404 | httptest（seed 用户+歌单+曲目+专辑封面） |
| PUT 上传：owner 成功写文件 + 置 cover_path；非 owner 404；非 jpeg/png 400；超 5MB 拒绝；jpg→png 重传清旧文件 | httptest（multipart 请求，临时 artworkDir） |
| DELETE：删文件 + 清空 cover_path → GET 回退自动封面 | httptest |
| 前端上传/显示/恢复 | 构建 + 真机实测 |

后端全部 httptest + 临时目录（不碰真实 artwork 目录）。

---

## 不在本次范围内

- 四宫格 / 多专辑拼图封面。
- 封面裁剪、缩放、多尺寸缓存、在线编辑。
- 公共/共享歌单的封面权限模型（当前歌单均为用户私有）。
