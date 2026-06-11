# 播放列表（歌单）设计文档

> 版本：1.0 · 日期：2026-06-11 · 状态：已批准

---

## 背景

`playlists` / `playlist_tracks` 两表在多用户改造之前就已存在，但**从未接任何端点**（表为空），且 `playlists` 缺 `user_id`（归属）与 Subsonic 所需字段。本设计补齐 per-user 私人歌单的完整功能，Subsonic 与 Web 两端联动。歌单与书签/收藏/播放队列一样**按用户隔离**。

现状：
- `playlists(id, name, created_at)` —— 无 user_id。
- `playlist_tracks(id, playlist_id, track_id, position)` + `UNIQUE(playlist_id, position)` —— FK 无 `ON DELETE CASCADE`。
- 无任何 playlist 端点（Subsonic 与 v1 皆无）。

可复用件：`auth.User{ID}`、`middleware.UserFromContext`、Subsonic `withAuth` 注入用户 + `userFromCtx`、`trackSelect`/`scanChild`/`childByID`、v1 `writeJSON`/`writeJSONError`、前端导航 `mode` 体系（专辑/歌手/收藏/设置）。

---

## 范围

**做**：

- per-user **私人**歌单的完整 CRUD：创建 / 列表 / 详情 / 改名(+备注) / 删除。
- 曲目操作：追加、移除、整列表替换（用于重排）。
- 共享 `internal/playlists` 包（Store），供 Subsonic 与 v1 复用，所有操作按 user_id 鉴权。
- Subsonic：getPlaylists / getPlaylist / createPlaylist / updatePlaylist / deletePlaylist。
- Web：歌单页（列表 + 新建 + 详情 + 拖拽重排 + 移除 + 改名 + 删除 + 播放）；专辑/搜索/收藏曲目行的「添加到歌单」入口。

**不做**（YAGNI）：

- 公开 / 共享歌单（所有歌单私有；Subsonic `public` 恒为 false）。
- 智能歌单 / 自动歌单（如"最近添加"规则歌单）。
- 协作编辑、歌单封面上传。

---

## 数据模型（新迁移 `009`）

`internal/db/migrations/009_playlists_multiuser.up.sql`，并同步 `internal/db/schema.sql`。重建两表（表为空，重建即可；旧行若存在则 user_id 置 NULL）：

```sql
-- 歌单：加 user_id 归属 + Subsonic 字段
CREATE TABLE playlists_new (
    id         TEXT PRIMARY KEY,
    user_id    TEXT REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO playlists_new (id, user_id, name, created_at)
    SELECT id, NULL, name, created_at FROM playlists;
DROP TABLE playlists;
ALTER TABLE playlists_new RENAME TO playlists;

-- 歌单曲目：FK 加 ON DELETE CASCADE
CREATE TABLE playlist_tracks_new (
    id          INTEGER PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    track_id    TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL
);
INSERT INTO playlist_tracks_new (id, playlist_id, track_id, position)
    SELECT id, playlist_id, track_id, position FROM playlist_tracks;
DROP TABLE playlist_tracks;
ALTER TABLE playlist_tracks_new RENAME TO playlist_tracks;
CREATE UNIQUE INDEX idx_playlist_tracks_pos ON playlist_tracks(playlist_id, position);
```

`schema.sql` 同步为重建后的最终形态（含两表与唯一索引）。

---

## 共享 Store（新包 `internal/playlists`）

`internal/playlists/store.go` 的 `Store`（`NewStore(db)`）。所有方法以 `userID` 为第一参数并据其鉴权：非属主的歌单一律当作「不存在」（返回 `ErrNotFound`），杜绝越权读写。

类型：

```go
type Playlist struct {
    ID        string
    Name      string
    Comment   string
    Created   string
    Changed   string
    SongCount int
    Duration  int
}

var ErrNotFound = errors.New("歌单不存在")
```

方法：

- `Create(userID, name string) (string, error)`：生成 UUID，插入；返回 id。
- `List(userID string) ([]Playlist, error)`：本人全部歌单，含 SongCount/Duration（子查询聚合 playlist_tracks join tracks），按 updated_at DESC。
- `Get(userID, id string) (Playlist, error)`：单个歌单元数据；非属主 → `ErrNotFound`。
- `TrackIDs(userID, id string) ([]string, error)`：按 position 升序的有序 track id（先校验属主）。
- `UpdateMeta(userID, id, name, comment string) error`：改名/备注（name 为空则不改）；`updated_at=now`；非属主 → `ErrNotFound`。
- `Delete(userID, id string) error`：删歌单（playlist_tracks 经 FK 级联清理）；非属主 → `ErrNotFound`。
- `AddTracks(userID, id string, trackIDs []string) error`：追加到末尾（从 `MAX(position)+1` 起顺序插入）；非属主 → `ErrNotFound`；`updated_at=now`。
- `RemoveByIndices(userID, id string, indices []int) error`：按 0 基位置删除若干曲目并重排 position（先读有序列表，过滤掉指定下标，再 ReplaceTracks）；非属主 → `ErrNotFound`。
- `ReplaceTracks(userID, id string, trackIDs []string) error`：清空该歌单 playlist_tracks，按给定顺序重插（position 0..n-1）；用于重排与 createPlaylist 的整列表替换；非属主 → `ErrNotFound`；`updated_at=now`。在单事务内 DELETE + 批量 INSERT。

> modernc 单连接约束：List/聚合查询先排空 rows 再做后续查询；ReplaceTracks 在事务内完成。

---

## Subsonic 端（5 端点）

`Handler` 增持 `*playlists.Store`（构造注入；router 装配）。DTO 加到 response.go：

```go
type Playlists struct{ Playlist []Playlist `xml:"playlist" json:"playlist"` }
type Playlist struct {
    ID        string `xml:"id,attr" json:"id"`
    Name      string `xml:"name,attr" json:"name"`
    Comment   string `xml:"comment,attr,omitempty" json:"comment,omitempty"`
    Owner     string `xml:"owner,attr" json:"owner"`
    Public    bool   `xml:"public,attr" json:"public"`
    SongCount int    `xml:"songCount,attr" json:"songCount"`
    Duration  int    `xml:"duration,attr" json:"duration"`
    Created   string `xml:"created,attr" json:"created"`
    Changed   string `xml:"changed,attr" json:"changed"`
    Entry     []Child `xml:"entry,omitempty" json:"entry,omitempty"` // 仅 getPlaylist 填充
}
```
Response 增 `Playlists *Playlists` 与 `Playlist *Playlist` 字段。

端点（均在 withAuth 后，用 `userFromCtx`）：

- **getPlaylists**：`store.List(user)` → `Playlists{[]Playlist}`（Owner=用户名，Public=false，不含 Entry）。
- **getPlaylist**(`id`)：`store.Get` + `store.TrackIDs` → 逐个 `childByID` 构造 Entry；非属主/不存在 → error 70。
- **createPlaylist**(`name` 或 `playlistId`，`songId` 多值)：
  - 有 `playlistId` → `ReplaceTracks(user, playlistId, songIds)`（替换现有）。
  - 否则 `Create(user, name)` → 若有 songIds 则 `ReplaceTracks`。
  - 返回该歌单的 getPlaylist 形态（Subsonic 约定 createPlaylist 返回新歌单）。
- **updatePlaylist**(`playlistId`，`name?`、`comment?`、`songIdToAdd` 多值、`songIndexToRemove` 多值)：
  - `UpdateMeta`（若给了 name/comment）；先 `RemoveByIndices(songIndexToRemove)` 再 `AddTracks(songIdToAdd)`（先删后加，索引基于删前列表，符合 Subsonic 语义）；空 ok。
- **deletePlaylist**(`id`)：`store.Delete`；非属主 → 70；空 ok。

---

## Web 端（v1）

`PlaylistHandler`（`internal/api/v1/playlists.go`，持 `*sql.DB` + `*playlists.Store`），从 `middleware.UserFromContext` 取用户。`ErrNotFound` → 404。

- `GET /api/v1/playlists` → `{playlists:[{id,name,comment,song_count,duration,created,changed}]}`。
- `POST /api/v1/playlists` `{name}` → `{id}`（name 空 → 400）。
- `GET /api/v1/playlists/{id}` → `{id,name,comment,...,tracks:[favTrack 形态]}`（复用收藏里 favTrack 的曲目结构：id/title/album/album_id/artist/duration/stream_url/cover_url）。
- `PATCH /api/v1/playlists/{id}` `{name?,comment?}` → 改名/备注。
- `DELETE /api/v1/playlists/{id}`。
- `POST /api/v1/playlists/{id}/tracks` `{trackIds:[]}` → 追加（「添加到歌单」用）。
- `PUT /api/v1/playlists/{id}/tracks` `{trackIds:[]}` → 整列表替换（移除单曲与拖拽重排：前端发新顺序的完整 id 列表）。

> 曲目查询助手与收藏端点 `queryTracks` 一致——抽到可复用处或各自实现保持字段一致；实现时优先复用 `favorites.go` 已有的 `favTrack`/`queryTracks`（同包 v1，可直接调用）。

---

## 前端

- API client 新增：`listPlaylists`、`createPlaylist(name)`、`getPlaylist(id)`、`updatePlaylist(id,{name?,comment?})`、`deletePlaylist(id)`、`addToPlaylist(id,trackIds)`、`setPlaylistTracks(id,trackIds)`。类型 `PlaylistSummary`、`PlaylistDetail`。
- 侧边导航加 **歌单**（mode `'playlists'`，`ViewMode` 扩展），与专辑/歌手/收藏并列；音符/列表图标。
- `PlaylistsPage.vue`：左列歌单列表（含「新建歌单」输入）；选中后右侧显示曲目，支持：点击播放、**拖拽重排**（HTML5 draggable，放手后 `setPlaylistTracks` 发新顺序）、移除单曲（本地剔除后 `setPlaylistTracks`）、改名、删除歌单。
- `AddToPlaylist.vue`：一个可复用小组件（按钮 → 下拉：列出本人歌单 + 「新建歌单…」），点选调 `addToPlaylist`。挂在 **专辑曲目行**、**搜索结果曲目**、**收藏页曲目** 上。
- App.vue：渲染 `mode==='playlists'` → PlaylistsPage；播放歌单/曲目复用现有播放入口（FavTrack→队列项映射）。

---

## 测试策略

| 测试 | 方式 |
|------|------|
| 迁移 009 可执行、schema 一致、playlist_tracks 级联（删歌单清曲目） | `go test ./internal/db/...` + Store 测试 |
| Store：Create/List（SongCount/Duration 正确）/Get | 内存 sqlite，seed 用户+曲目 |
| Store：属主隔离（用户 B 看不到/不能改 A 的歌单 → ErrNotFound） | seed 两用户 |
| Store：AddTracks 追加顺序、RemoveByIndices 删并重排、ReplaceTracks 替换顺序 | 内存 sqlite |
| Store：UpdateMeta、Delete（级联） | 内存 sqlite |
| Subsonic getPlaylists/getPlaylist（entry 顺序、非属主 70） | httptest |
| Subsonic createPlaylist（name 新建 / playlistId 替换）、updatePlaylist（加+删）、deletePlaylist | httptest |
| v1 列表/新建/详情/改名/删除/追加/替换 + 属主隔离 404 | httptest |
| v1 PUT tracks 重排后顺序正确 | httptest |

全部 httptest + 内存 sqlite，不打网络。前端依赖构建 + 真机验证（见记忆 `verify-real-playback-early`：docker + 浏览器实测歌单增删/拖拽/添加到歌单 + Symfonium 同步歌单）。

---

## 不在本次范围内

- 公开 / 共享 / 协作歌单。
- 智能 / 规则歌单。
- 歌单封面、导入导出（m3u 等）。
