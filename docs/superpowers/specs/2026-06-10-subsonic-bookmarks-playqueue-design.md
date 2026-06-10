# Subsonic 书签 + 播放队列设计文档

> 版本：1.0 · 日期：2026-06-10 · 状态：已批准

---

## 目标

实现 Subsonic API 的书签与播放队列（续播）：

- **书签（Bookmark）**：`createBookmark` / `getBookmarks` / `deleteBookmark` —— 单曲播放位置续播（Symfonium 已在用，日志可见 6 次 `createBookmark`）。
- **播放队列（PlayQueue）**：`savePlayQueue` / `getPlayQueue` —— 整个队列 + 当前曲目 + 位置的跨设备续播。

属 Subsonic 第二期（第一期 spec：`2026-06-09-subsonic-api-design.md` 已把这些列为延后项）。

---

## 范围

5 个端点：createBookmark、getBookmarks、deleteBookmark、savePlayQueue、getPlayQueue。

**单用户模型**：本项目单用户（`cfg.Auth.Username`），书签/队列无需 user 列；响应里的 `username` 一律填 `cfg.Auth.Username`。

不在本次：多用户、star/rating、播放列表（getPlaylists 等，另行独立 spec）。

---

## 数据模型（新迁移 `005`）

`internal/db/migrations/005_bookmarks_playqueue.up.sql`，并同步更新 `internal/db/schema.sql`：

```sql
-- 书签：单曲播放位置（单用户，每曲唯一，曲目删除级联清理）
CREATE TABLE bookmarks (
    track_id   TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,                 -- 毫秒（Subsonic position 单位）
    comment    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 播放队列：单用户单行
CREATE TABLE play_queue (
    id         INTEGER PRIMARY KEY CHECK (id = 1),  -- 强制单行
    track_ids  TEXT NOT NULL DEFAULT '',            -- 逗号分隔的有序 track id（UUID 无逗号，安全）
    current    TEXT NOT NULL DEFAULT '',            -- 当前曲目 id
    position   INTEGER NOT NULL DEFAULT 0,          -- 毫秒
    changed_at TEXT NOT NULL DEFAULT (datetime('now')),
    changed_by TEXT NOT NULL DEFAULT ''             -- 保存时的客户端名（c 参数）
);
```

`track_ids` 用逗号拼接：track id 是 UUID，不含逗号，拼接/拆分安全可逆。`play_queue` 单行（`id=1`），每次 save 用 upsert 覆盖。

---

## 端点行为（bookmarks.go）

所有端点已在 `withAuth` 之后（认证由中间件保证）。position 单位毫秒。

- **createBookmark**(`id`, `position`, `comment?`)：
  upsert `INSERT INTO bookmarks(track_id,position,comment) VALUES(?,?,?) ON CONFLICT(track_id) DO UPDATE SET position=excluded.position, comment=excluded.comment, updated_at=datetime('now')`。
  先校验 track 存在且 `is_available=1`，否则 error 70。成功返回空 ok。
- **getBookmarks**：
  `SELECT b.position,b.comment,b.created_at,b.updated_at,<trackcols> FROM bookmarks b JOIN tracks tr ON tr.id=b.track_id … WHERE tr.is_available=1 ORDER BY b.updated_at DESC`。
  每行构造 `Bookmark{Position, Username:cfg.Auth.Username, Comment, Created:created_at, Changed:updated_at, Entry:Child}`，Entry 复用 `scanChild`/`trackSelect` 的列与逻辑。返回 `Response{Bookmarks:&Bookmarks{...}}`。
- **deleteBookmark**(`id`)：`DELETE FROM bookmarks WHERE track_id=?`。空 ok（删不存在的也返回 ok）。
- **savePlayQueue**(`id` 多值=有序队列, `current?`, `position?`)：
  读 `r.Form["id"]`（多值）拼成逗号串；`position` atoi；`changed_by`=`r.Form.Get("c")`。
  `INSERT INTO play_queue(id,track_ids,current,position,changed_at,changed_by) VALUES(1,?,?,?,datetime('now'),?) ON CONFLICT(id) DO UPDATE SET …`。
  无 `id`（空队列）→ 写入空 track_ids（即清空）。空 ok。
- **getPlayQueue**：
  读单行；无行 → 返回 ok 且不带 playQueue 元素（`Response{}`）。
  有行：拆分 track_ids，按顺序对每个 id 查 `trackSelect`（跳过查不到/不可用的），构造 `Entry []Child`；返回 `PlayQueue{Current, Position, Username:cfg.Auth.Username, Changed:changed_at, ChangedBy:changed_by, Entry}`。
  （顺序保持：逐个 id 查询并按原顺序追加，而非一条 IN 查询乱序。）

---

## DTO（response.go）

把现有的 `Bookmarks` 空桩替换为真实结构，并新增 `PlayQueue`：

```go
// Response 增加（Bookmarks 字段已存在，复用；新增 PlayQueue 字段）
PlayQueue *PlayQueue `xml:"playQueue,omitempty" json:"playQueue,omitempty"`

type Bookmarks struct {
    Bookmark []Bookmark `xml:"bookmark" json:"bookmark"`
}
type Bookmark struct {
    Position int64  `xml:"position,attr" json:"position"`
    Username string `xml:"username,attr" json:"username"`
    Comment  string `xml:"comment,attr,omitempty" json:"comment,omitempty"`
    Created  string `xml:"created,attr" json:"created"`
    Changed  string `xml:"changed,attr" json:"changed"`
    Entry    Child  `xml:"entry" json:"entry"`
}
type PlayQueue struct {
    Current   string  `xml:"current,attr,omitempty" json:"current,omitempty"`
    Position  int64   `xml:"position,attr,omitempty" json:"position,omitempty"`
    Username  string  `xml:"username,attr" json:"username"`
    Changed   string  `xml:"changed,attr" json:"changed"`
    ChangedBy string  `xml:"changedBy,attr,omitempty" json:"changedBy,omitempty"`
    Entry     []Child `xml:"entry,omitempty" json:"entry,omitempty"`
}
```

现有 `stubs.go` 里的 `getBookmarks` 空实现删除（由 bookmarks.go 的真实现取代）；`stubs.go` 保留 `getGenres`/`getStarred2`（仍是第二期其它项的桩）。

---

## 代码落点

```
internal/api/subsonic/bookmarks.go        createBookmark/getBookmarks/deleteBookmark/savePlayQueue/getPlayQueue
internal/api/subsonic/bookmarks_test.go
internal/api/subsonic/response.go          改：Bookmarks 真实结构 + 新增 PlayQueue
internal/api/subsonic/handler.go           改：注册 4 个新端点；getBookmarks 改指向真实现；从 stubs.go 移除 getBookmarks
internal/api/subsonic/stubs.go             改：删除 getBookmarks（保留 getGenres/getStarred2）
internal/db/migrations/005_bookmarks_playqueue.up.sql
internal/db/schema.sql                     改：加两表
```

handler.go 的 `RegisterRoutes` 里：`getBookmarks` 已注册（当前指向 stub）→ 改指向真实现；新增 `h.reg(r,"createBookmark",…)`、`deleteBookmark`、`savePlayQueue`、`getPlayQueue`。

---

## 测试策略

| 测试 | 方式 |
|------|------|
| 迁移 005 可执行、schema 一致 | `go test ./internal/db/...`（内存 sqlite 跑全部迁移） |
| createBookmark 写入 → getBookmarks 命中（position/comment/Entry 字段、username）| httptest + 内存 sqlite，seed 曲目 |
| createBookmark 再次同曲 → upsert 覆盖（position 更新，不重复）| 验 DB 行数=1、position 为新值 |
| createBookmark 不存在曲目 → error 70 | httptest |
| deleteBookmark → getBookmarks 不再含该曲 | httptest |
| savePlayQueue（多 id + current + position）→ getPlayQueue 按序返回、current/position 正确 | httptest，验 Entry 顺序 |
| savePlayQueue 空 id → getPlayQueue 队列清空 | httptest |
| getPlayQueue 无保存 → ok 且无 playQueue 元素 | httptest |
| getBookmarks XML 与 JSON 两种格式 | httptest 各验一次 |

全部 httptest + 内存 sqlite，不打网络。

---

## 不在本次范围内

- 多用户（per-user 书签/队列）
- star/rating、播放列表、getLyrics 等其它第二期项
