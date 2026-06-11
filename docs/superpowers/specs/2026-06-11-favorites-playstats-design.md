# 收藏 + 播放统计 设计文档

> 版本：1.0 · 日期：2026-06-11 · 状态：已批准

---

## 背景

多用户改造已完成（认证地基 + 用户管理）。当前缺「用户沉淀」：

- `getStarred2` 是空桩，**无 star/unstar 端点**；Web 端无任何收藏。
- `scrobble` 只更新 `tracks` 表上的**全局** `play_count`/`last_played_at`（非按用户）。
- `getAlbumList2` 的 `recent` 错误地等同于「最新创建」，`frequent`（最常听）缺失。

本设计补上 per-user 收藏与播放统计，Subsonic 与 Web 两端联动。收藏与播放统计均**按用户**（复用刚建好的多用户模型）。

可复用件：`auth.User`（含 `ID`）、`middleware.UserFromContext`（v1 当前用户）、Subsonic 的 `withAuth` 已把 `*auth.User` 注入 context（`userFromCtx`）、Subsonic `childByID`/`trackSelect`/`scanChild` 与专辑/歌手对象构建、v1 albums/artists/search handler。

---

## 范围

**做**：

- per-user 收藏：歌曲 / 专辑 / 歌手三类（Subsonic 全支持）。
- per-user 播放统计：播放次数 + 最近播放时间。
- 共享 `internal/userdata` 包（收藏 + 播放统计的 Store），供 Subsonic 与 v1 复用。
- Subsonic：`star` / `unstar` / `getStarred2` 真实现；`scrobble` 改记当前用户；`getAlbumList2` 修正 `recent`、新增 `frequent` / `starred`；浏览响应标注 `starred` 属性。
- Web：`POST /star` `/unstar`、播放记录 `scrobble`、`GET /favorites` `/recently-played` `/most-played`；现有 albums/tracks/search 响应补 `starred` 布尔；前端歌曲/专辑红心 + 「我的收藏 / 最近播放」面板。

**不做**（YAGNI）：

- 评分（`setRating` / `getStarred2` 的 rating 字段）。
- 播放历史明细时间线（只存聚合 count + 最近时间）。
- 歌手详情页的 Web 红心（歌手收藏仅 Subsonic 端支持；Web 红心只做歌曲 + 专辑）。

---

## 数据模型（新迁移 `008`）

`internal/db/migrations/008_favorites_playstats.up.sql`，并同步 `internal/db/schema.sql`：

```sql
-- 收藏（per-user，多态：歌曲/专辑/歌手）
CREATE TABLE starred (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_type  TEXT NOT NULL,        -- 'song' | 'album' | 'artist'
    item_id    TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, item_type, item_id)
);
CREATE INDEX idx_starred_user_type ON starred(user_id, item_type);

-- 播放统计（per-user，每曲一行）
CREATE TABLE play_stats (
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id       TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    play_count     INTEGER NOT NULL DEFAULT 0,
    last_played_at DATETIME,
    PRIMARY KEY (user_id, track_id)
);
CREATE INDEX idx_play_stats_user_count  ON play_stats(user_id, play_count DESC);
CREATE INDEX idx_play_stats_user_recent ON play_stats(user_id, last_played_at DESC);
```

`item_id` 不设外键（多态指向 tracks/albums/artists）；查询时按可用性过滤，删除对象后残留行不影响（查不到对象即跳过）。`tracks.play_count` / `tracks.last_played_at` 退为遗留列，`scrobble` 不再写它们（保留列以免改表）。

---

## 共享 Store（新包 `internal/userdata`）

收藏与播放统计被两端共用，抽 `internal/userdata/store.go` 的 `Store`（`NewStore(db)`）：

- `Star(userID, itemType, itemID string) error`：`INSERT ... ON CONFLICT DO NOTHING`（幂等）。
- `Unstar(userID, itemType, itemID string) error`：`DELETE`。
- `IsStarred(userID, itemType, itemID string) (bool, error)`。
- `StarredIDs(userID, itemType string) ([]string, error)`：按 `created_at DESC` 返回该类型收藏的 id 列表（用于 getStarred2、starred 专辑列表、favorites）。
- `StarredMap(userID, itemType string) (map[string]bool, error)`：批量标注列表用，避免逐条查询。
- `RecordPlay(userID, trackID string) error`：upsert `play_count=play_count+1, last_played_at=datetime('now')`。
- `RecentTrackIDs(userID string, limit int) ([]string, error)`：`last_played_at DESC`（仅 last_played_at 非空）。
- `FrequentTrackIDs(userID string, limit int) ([]string, error)`：`play_count DESC`。

`item_type` 用常量 `TypeSong="song"` / `TypeAlbum="album"` / `TypeArtist="artist"`。getAlbumList2 的 recent/frequent/starred 专辑列表在 Subsonic handler 内 join `play_stats` / `starred` 实现（仅 album 维度的聚合），不进通用 Store。

---

## Subsonic 端

`Handler` 增持 `*userdata.Store`（构造注入；router 装配）。

- **`star`** / **`unstar`**：读 `id`（多值，视为歌曲）、`albumId`（多值）、`artistId`（多值），分别 `Star/Unstar(user, 类型, 每个 id)`。空 ok。
- **`getStarred2`**：`StarredIDs(user, song/album/artist)` → 复用 `childByID`（歌曲）与专辑/歌手对象构建器，跳过查不到/不可用的，返回 `Starred2{Artist,Album,Song}`。
- **`scrobble`**：改为 `store.RecordPlay(userFromCtx(r).ID, id)`（不再写 tracks 全局列）。
- **`getAlbumList2`**：
  - `newest`：`al.created_at DESC`（最新加入，不变）。
  - `recent`：**修正**为本人最近播放——`JOIN play_stats ps ON ps.track... ` 经专辑聚合 `MAX(ps.last_played_at)`，仅含有播放记录的专辑，按时间 DESC。
  - `frequent`：本人最常听——专辑聚合 `SUM(ps.play_count)`，DESC。
  - `starred`：本人收藏的专辑（join starred where item_type='album'）。
  - `alphabeticalByName` / `random`：不变。
- **`starred` 属性标注**：`Child`、`AlbumID3`、`ArtistID3` 增 `Starred string`（Subsonic 约定为加星时间的 ISO 串；存在即表示已加星）。在 `getAlbum`（歌曲）、`getSong`、`search3`、`getAlbumList2`、`getArtist` 中用对应类型的 `StarredMap` 批量填充（命中则填 `created` 时间或固定占位时间串）。

> getAlbumList2 的 recent/frequent 按专辑聚合：以 `tracks.album_id` 关联 `play_stats`，`GROUP BY al.id`。

---

## Web 端（v1）

`StarHandler` / 统计相关 handler 持 `*userdata.Store`，从 `middleware.UserFromContext` 取当前用户。

- **`POST /api/v1/star`** body `{type, id}`：`type ∈ {song,album,artist}`；校验后 `Star(user,type,id)` → `{ok:true}`。
- **`POST /api/v1/unstar`** body `{type, id}`：`Unstar` → `{ok:true}`。
- **`POST /api/v1/tracks/{id}/scrobble`**：`RecordPlay(user, id)` → `{ok:true}`（前端在开始播放某曲时调用）。
- **`GET /api/v1/favorites`** → `{tracks:[...], albums:[...]}`（当前用户收藏的歌曲与专辑；歌手暂不在 Web 展示）。
- **`GET /api/v1/recently-played`** / **`GET /api/v1/most-played`** → `{tracks:[...]}`（按 RecentTrackIDs / FrequentTrackIDs，默认 limit 50）。
- **现有响应补 `starred`**：`ListAlbums`/`GetAlbum`（专辑及其曲目）、`search` 的曲目/专辑结果项增 `starred bool`，用 `StarredMap` 批量标注，供前端渲染红心。

> 复用现有曲目/专辑 DTO 与查询；`starred` 字段按既有 JSON 命名风格（snake/camel 随现状）追加。实现时先读现有 DTO 确认命名。

---

## 前端

- API client 新增：`star(type,id)`、`unstar(type,id)`、`scrobble(trackId)`、`getFavorites()`、`getRecentlyPlayed()`、`getMostPlayed()`。
- 歌曲行与专辑卡片/详情加**红心按钮**（实心=已收藏），点击 toggle 调 star/unstar 并就地更新状态。
- 播放器开始播放某曲时调用 `scrobble(trackId)`（去重：同一曲目连续重复触发只记一次，按"开始播放"语义）。
- 新增「我的收藏 / 最近播放」面板（仿 AccountSettings/UserManagement 的入口模式，从 LibraryShell 进入）：标签切换收藏的歌曲/专辑、最近播放、最常听三个列表，点击可播放。
- album/track 列表渲染时读响应里的 `starred` 初始化红心状态。

---

## 代码落点

```
internal/db/migrations/008_favorites_playstats.up.sql   新迁移
internal/db/schema.sql                                   改：加 starred / play_stats
internal/userdata/store.go                               新：Store（收藏 + 播放统计）
internal/userdata/store_test.go
internal/api/subsonic/favorites.go                       新：star/unstar/getStarred2
internal/api/subsonic/media.go                           改：scrobble 改 per-user
internal/api/subsonic/browse.go                          改：getAlbumList2 recent/frequent/starred + starred 注解
internal/api/subsonic/response.go                        改：Child/AlbumID3/ArtistID3 加 Starred
internal/api/subsonic/handler.go                         改：Handler 持 store；注册 star/unstar
internal/api/v1/favorites.go                             新：star/unstar/scrobble/favorites/recently/most-played
internal/api/v1/albums.go / search.go                    改：响应补 starred 布尔
internal/api/router.go                                   改：装配 userdata.Store + 新端点
web/src/api/client.ts                                    改：收藏/统计方法 + DTO starred
web/src/components/FavoritesPanel.vue                    新：我的收藏 / 最近播放 / 最常听
web/src/components/*（曲目/专辑相关）                      改：红心按钮
web/src/App.vue / LibraryShell.vue                       改：面板入口 + scrobble 接入播放器
```

---

## 测试策略

| 测试 | 方式 |
|------|------|
| 迁移 008 可执行、schema 一致 | `go test ./internal/db/...` |
| Store：Star 幂等（重复 star 不报错、不重复）| 内存 sqlite |
| Store：Unstar、IsStarred、StarredIDs 顺序、StarredMap | 内存 sqlite |
| Store：RecordPlay upsert（首次=1、再次=2、last_played 更新）| 内存 sqlite |
| Store：RecentTrackIDs / FrequentTrackIDs 顺序与 limit | seed 多曲多次播放 |
| Store：删用户级联清 starred/play_stats | 内存 sqlite |
| Subsonic star/unstar（song/album/artist 三类参数）→ getStarred2 命中 | httptest |
| Subsonic scrobble → play_stats 记到当前用户 | httptest |
| Subsonic getAlbumList2 recent（按本人最近播放）/ frequent / starred | httptest，seed 播放与收藏 |
| Subsonic 浏览响应 starred 注解（getAlbum/getSong/search3）| httptest |
| v1 star/unstar/scrobble/favorites/recently/most-played | httptest |
| v1 albums/search 响应含 starred 布尔且反映收藏 | httptest |
| per-user 隔离：A 的收藏/统计不出现在 B | httptest，seed 两用户 |

全部 httptest + 内存 sqlite，不打网络。前端不强制单测，依赖构建 + 真机验证（见记忆 `verify-real-playback-early`：docker + 浏览器/Symfonium 实测红心与最近播放）。

---

## 不在本次范围内

- 评分（setRating）。
- 播放历史明细时间线。
- 歌手详情的 Web 红心（仅 Subsonic 支持歌手收藏）。
- 基于收藏/统计的推荐。
