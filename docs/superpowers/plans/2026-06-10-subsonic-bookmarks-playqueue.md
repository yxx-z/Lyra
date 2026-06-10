# Subsonic 书签 + 播放队列 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Subsonic 书签（createBookmark/getBookmarks/deleteBookmark）与播放队列（savePlayQueue/getPlayQueue），让客户端能单曲续播、跨设备续播整队列。

**Architecture:** 新迁移 005 加 `bookmarks` 与 `play_queue` 两表（单用户）。新文件 `internal/api/subsonic/bookmarks.go` 实现 5 个端点，复用现有 `trackSelect`/`scanChild` 构造 Child。response.go 把 Bookmarks 空桩换成真实结构并加 PlayQueue。

**Tech Stack:** Go 1.25、modernc.org/sqlite（支持 `ON CONFLICT … DO UPDATE` upsert）、chi、httptest。

**Go 环境：** 含 `go` 命令的步骤前 `export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## 设计参考

实现前读 `docs/superpowers/specs/2026-06-10-subsonic-bookmarks-playqueue-design.md`。

**关键既有代码：**
- `internal/api/subsonic/browse.go`：`const trackSelect`（11 列 SELECT，LEFT JOIN albums/artists）、`func scanChild(rows *sql.Rows) (Child, error)`。**本计划复用。**
- `internal/api/subsonic/handler.go`：`Handler{db, cfg *config.Config, streamH, cover}`；`RegisterRoutes` 末尾已注册 `getGenres`/`getStarred2`/`getBookmarks`（均指向 stubs.go 空桩），再之后是 `r.NotFound(...)` 兜底。`h.cfg.Auth.Username` 可取用户名。
- `internal/api/subsonic/stubs.go`：含 `getGenres`/`getStarred2`/`getBookmarks` 三个空桩。**本计划删除其中的 getBookmarks。**
- `internal/api/subsonic/response.go`：`Response` 结构含 `Bookmarks *Bookmarks` 字段；`type Bookmarks struct { Bookmark []struct{} … }`（空桩）。`Child` DTO 已定义。
- `internal/api/subsonic/handler_test.go` / `browse_test.go`：测试助手 `testHandler(t) (*Handler, *config.Config)`（cfg.Auth.Username="admin"、cfg.Subsonic.Password="secret"）、`doReq(t, h, target) *httptest.ResponseRecorder`（走完整 chi 路由 + 认证，GET）、`seed(t, d)`（插入 ar1 / al1 / t1「以父之名」/ t2「晴天」）。
- `internal/db/migrations/`：`*.up.sql` 按字母序执行；最新为 `004_lyrics_sync_checked.up.sql`。下一个为 `005`。
- `internal/db/db.go`：`func Open(path string) (*sql.DB, error)` 跑全部迁移。
- `internal/db/db_test.go`：`TestOpen_CreatesTablesOnFirstRun` 用 `tables` 列表断言建表。
- `internal/db/schema.sql`：库结构快照，需同步。

**文件结构：**
```
internal/db/migrations/005_bookmarks_playqueue.up.sql   新建两表
internal/db/schema.sql                                   改：加两表
internal/db/db_test.go                                   改：tables 列表加 bookmarks/play_queue
internal/api/subsonic/response.go                        改：Bookmarks 真实结构 + Bookmark + PlayQueue
internal/api/subsonic/response_test.go                   改：加 DTO 编解码测试
internal/api/subsonic/bookmarks.go                       新建：5 端点 + childByID 助手
internal/api/subsonic/bookmarks_test.go                  新建
internal/api/subsonic/handler.go                         改：注册 4 个新端点（getBookmarks 改指真实现）
internal/api/subsonic/stubs.go                           改：删除 getBookmarks 空桩
```

---

### Task 1: 迁移 005（bookmarks + play_queue 两表）

**Files:** Create `internal/db/migrations/005_bookmarks_playqueue.up.sql`; Modify `internal/db/schema.sql`, `internal/db/db_test.go`

- [ ] **Step 1: 改失败测试** — `internal/db/db_test.go` 的 `TestOpen_CreatesTablesOnFirstRun` 里 `tables` 列表加两项：
```go
	tables := []string{"artists", "albums", "tracks", "lyrics", "playlists", "playlist_tracks", "bookmarks", "play_queue"}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/ -run TestOpen_CreatesTablesOnFirstRun -v`
Expected: FAIL（bookmarks / play_queue 表不存在）。

- [ ] **Step 3: 建迁移** — `internal/db/migrations/005_bookmarks_playqueue.up.sql`：
```sql
CREATE TABLE bookmarks (
    track_id   TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE play_queue (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    track_ids  TEXT NOT NULL DEFAULT '',
    current    TEXT NOT NULL DEFAULT '',
    position   INTEGER NOT NULL DEFAULT 0,
    changed_at TEXT NOT NULL DEFAULT (datetime('now')),
    changed_by TEXT NOT NULL DEFAULT ''
);
```

- [ ] **Step 4: 同步 schema.sql** — `internal/db/schema.sql` 末尾（索引定义之前的建表区）追加同样两段 `CREATE TABLE bookmarks (...)` 与 `CREATE TABLE play_queue (...)`（与上面 SQL 完全一致）。

- [ ] **Step 5: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/ -v`
Expected: PASS（含表存在断言）。

- [ ] **Step 6: 提交**
```bash
git add internal/db/migrations/005_bookmarks_playqueue.up.sql internal/db/schema.sql internal/db/db_test.go
git commit -m "feat(db): 迁移 005 — bookmarks + play_queue 两表"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 2: DTO（Bookmarks 真实结构 + Bookmark + PlayQueue）

**Files:** Modify `internal/api/subsonic/response.go`, `internal/api/subsonic/response_test.go`

- [ ] **Step 1: 写失败测试** — 在 `internal/api/subsonic/response_test.go` 末尾追加：
```go
func TestBookmarkDTO_XMLJSON(t *testing.T) {
	resp := &Response{Bookmarks: &Bookmarks{Bookmark: []Bookmark{
		{Position: 42000, Username: "admin", Comment: "hi", Created: "2026-06-10 00:00:00", Changed: "2026-06-10 00:01:00", Entry: Child{ID: "t1", Title: "X"}},
	}}}
	out, _ := xml.Marshal(resp)
	if !strings.Contains(string(out), `<bookmark position="42000"`) || !strings.Contains(string(out), `<entry id="t1"`) {
		t.Errorf("书签 XML 不符: %s", out)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x?f=json", nil)
	writeResponse(w, r, resp)
	b := w.Body.String()
	if !strings.Contains(b, `"position":42000`) || !strings.Contains(b, `"username":"admin"`) || !strings.Contains(b, `"comment":"hi"`) {
		t.Errorf("书签 JSON 不符: %s", b)
	}
}

func TestPlayQueueDTO_Order(t *testing.T) {
	resp := &Response{PlayQueue: &PlayQueue{
		Current: "t2", Position: 5000, Username: "admin", Changed: "2026-06-10 00:00:00", ChangedBy: "Symfonium",
		Entry: []Child{{ID: "t1", Title: "A"}, {ID: "t2", Title: "B"}},
	}}
	out, _ := xml.Marshal(resp)
	s := string(out)
	if !strings.Contains(s, `<playQueue current="t2"`) || strings.Index(s, `id="t1"`) > strings.Index(s, `id="t2"`) {
		t.Errorf("播放队列 XML 顺序/属性不符: %s", s)
	}
}
```
（`response_test.go` 已 import `encoding/xml`、`net/http/httptest`、`strings`、`testing`。）

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -run 'TestBookmarkDTO|TestPlayQueueDTO' -v`
Expected: 编译失败（Bookmark / PlayQueue 未定义；Bookmarks 无 Bookmark []Bookmark 字段）。

- [ ] **Step 3: 改 response.go** —
（a）`Response` 结构里，在 `Bookmarks *Bookmarks …` 字段那一行**之后**加 PlayQueue 字段：
```go
	PlayQueue *PlayQueue `xml:"playQueue,omitempty" json:"playQueue,omitempty"`
```
（b）把现有的空桩
```go
type Bookmarks struct {
	Bookmark []struct{} `xml:"bookmark,omitempty" json:"bookmark,omitempty"`
}
```
替换为：
```go
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

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -v`
Expected: PASS（含既有测试，stubs.go 的 `&Bookmarks{}` 仍编译通过）。

- [ ] **Step 5: 提交**
```bash
git add internal/api/subsonic/response.go internal/api/subsonic/response_test.go
git commit -m "feat(subsonic): 书签/播放队列 DTO（Bookmark + PlayQueue）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 3: 5 个端点 + 注册 + 删空桩（bookmarks.go）

**Files:** Create `internal/api/subsonic/bookmarks.go`, `internal/api/subsonic/bookmarks_test.go`; Modify `internal/api/subsonic/handler.go`, `internal/api/subsonic/stubs.go`

- [ ] **Step 1: 写失败测试** — `internal/api/subsonic/bookmarks_test.go`:
```go
package subsonic

import (
	"strings"
	"testing"
)

func TestBookmarks_CreateGetDelete(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)

	// 创建
	w := doReq(t, h, "/rest/createBookmark?u=admin&p=secret&id=t1&position=42000&comment=hi&f=json")
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("createBookmark: %s", w.Body.String())
	}
	// 获取
	w = doReq(t, h, "/rest/getBookmarks?u=admin&p=secret&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"position":42000`) || !strings.Contains(b, `"comment":"hi"`) ||
		!strings.Contains(b, `"username":"admin"`) || !strings.Contains(b, `以父之名`) {
		t.Errorf("getBookmarks 应含书签与 Entry: %s", b)
	}
	// 删除
	doReq(t, h, "/rest/deleteBookmark?u=admin&p=secret&id=t1&f=json")
	w = doReq(t, h, "/rest/getBookmarks?u=admin&p=secret&f=json")
	if strings.Contains(w.Body.String(), `以父之名`) {
		t.Errorf("删除后不应再含该书签: %s", w.Body.String())
	}
}

func TestBookmarks_Upsert(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	doReq(t, h, "/rest/createBookmark?u=admin&p=secret&id=t1&position=1000&f=json")
	doReq(t, h, "/rest/createBookmark?u=admin&p=secret&id=t1&position=2000&f=json")
	var count int
	var pos int64
	h.db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(position),0) FROM bookmarks WHERE track_id='t1'`).Scan(&count, &pos)
	if count != 1 || pos != 2000 {
		t.Errorf("同曲应 upsert 覆盖：count=%d position=%d（期望 1 / 2000）", count, pos)
	}
}

func TestBookmark_TrackNotFound(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/createBookmark?u=admin&p=secret&id=nope&position=1&f=json")
	if !strings.Contains(w.Body.String(), `"code":70`) {
		t.Errorf("不存在曲目应 70: %s", w.Body.String())
	}
}

func TestPlayQueue_SaveGet(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// 保存：队列 t1,t2，当前 t2，位置 5000
	doReq(t, h, "/rest/savePlayQueue?u=admin&p=secret&id=t1&id=t2&current=t2&position=5000&c=Symfonium&f=json")
	w := doReq(t, h, "/rest/getPlayQueue?u=admin&p=secret&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"current":"t2"`) || !strings.Contains(b, `"position":5000`) ||
		!strings.Contains(b, `以父之名`) || !strings.Contains(b, `晴天`) {
		t.Errorf("getPlayQueue 应含队列与 current/position: %s", b)
	}
	// 顺序：t1（以父之名）在 t2（晴天）之前
	if strings.Index(b, `以父之名`) > strings.Index(b, `晴天`) {
		t.Errorf("队列顺序应为 t1 在前: %s", b)
	}
}

func TestPlayQueue_Empty(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// 未保存 → ok 且无 playQueue
	w := doReq(t, h, "/rest/getPlayQueue?u=admin&p=secret&f=json")
	if !strings.Contains(w.Body.String(), `"status":"ok"`) || strings.Contains(w.Body.String(), `"playQueue"`) {
		t.Errorf("未保存队列应 ok 且无 playQueue: %s", w.Body.String())
	}
	// 保存空 id → 清空
	doReq(t, h, "/rest/savePlayQueue?u=admin&p=secret&id=t1&f=json")
	doReq(t, h, "/rest/savePlayQueue?u=admin&p=secret&f=json") // 无 id → 清空
	w = doReq(t, h, "/rest/getPlayQueue?u=admin&p=secret&f=json")
	if strings.Contains(w.Body.String(), `以父之名`) {
		t.Errorf("清空后不应再含曲目: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -run 'TestBookmark|TestPlayQueue' -v`
Expected: 编译失败（createBookmark/deleteBookmark/savePlayQueue/getPlayQueue/childByID 未定义；getBookmarks 在 stubs.go 重复定义需先迁移）。

- [ ] **Step 3: 实现** — `internal/api/subsonic/bookmarks.go`:
```go
package subsonic

import (
	"net/http"
	"strconv"
	"strings"
)

// childByID 按 trackID 查一首可用曲目并构造 Child；不存在/不可用返回 ok=false。
// 复用 browse.go 的 trackSelect 与 scanChild。
func (h *Handler) childByID(trackID string) (Child, bool) {
	rows, err := h.db.Query(trackSelect+` WHERE tr.id=? AND tr.is_available=1`, trackID)
	if err != nil {
		return Child{}, false
	}
	defer rows.Close()
	if !rows.Next() {
		return Child{}, false
	}
	c, err := scanChild(rows)
	if err != nil {
		return Child{}, false
	}
	return c, true
}

func (h *Handler) createBookmark(w http.ResponseWriter, r *http.Request) {
	id := r.Form.Get("id")
	position, _ := strconv.ParseInt(r.Form.Get("position"), 10, 64)
	comment := r.Form.Get("comment")

	var exists string
	if err := h.db.QueryRow(`SELECT id FROM tracks WHERE id=? AND is_available=1`, id).Scan(&exists); err != nil {
		writeError(w, r, 70, "曲目不存在")
		return
	}
	if _, err := h.db.Exec(`
		INSERT INTO bookmarks(track_id, position, comment) VALUES(?,?,?)
		ON CONFLICT(track_id) DO UPDATE SET
			position=excluded.position, comment=excluded.comment, updated_at=datetime('now')`,
		id, position, comment); err != nil {
		writeError(w, r, 0, "保存书签失败")
		return
	}
	writeResponse(w, r, &Response{})
}

func (h *Handler) getBookmarks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`SELECT track_id, position, comment, created_at, updated_at FROM bookmarks ORDER BY updated_at DESC`)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	defer rows.Close()
	bms := &Bookmarks{}
	for rows.Next() {
		var trackID, comment, created, changed string
		var position int64
		if err := rows.Scan(&trackID, &position, &comment, &created, &changed); err != nil {
			continue
		}
		child, ok := h.childByID(trackID)
		if !ok {
			continue
		}
		bms.Bookmark = append(bms.Bookmark, Bookmark{
			Position: position,
			Username: h.cfg.Auth.Username,
			Comment:  comment,
			Created:  created,
			Changed:  changed,
			Entry:    child,
		})
	}
	writeResponse(w, r, &Response{Bookmarks: bms})
}

func (h *Handler) deleteBookmark(w http.ResponseWriter, r *http.Request) {
	_, _ = h.db.Exec(`DELETE FROM bookmarks WHERE track_id=?`, r.Form.Get("id"))
	writeResponse(w, r, &Response{})
}

func (h *Handler) savePlayQueue(w http.ResponseWriter, r *http.Request) {
	trackIDs := strings.Join(r.Form["id"], ",")
	current := r.Form.Get("current")
	position, _ := strconv.ParseInt(r.Form.Get("position"), 10, 64)
	changedBy := r.Form.Get("c")
	if _, err := h.db.Exec(`
		INSERT INTO play_queue(id, track_ids, current, position, changed_at, changed_by)
		VALUES(1, ?, ?, ?, datetime('now'), ?)
		ON CONFLICT(id) DO UPDATE SET
			track_ids=excluded.track_ids, current=excluded.current,
			position=excluded.position, changed_at=datetime('now'), changed_by=excluded.changed_by`,
		trackIDs, current, position, changedBy); err != nil {
		writeError(w, r, 0, "保存播放队列失败")
		return
	}
	writeResponse(w, r, &Response{})
}

func (h *Handler) getPlayQueue(w http.ResponseWriter, r *http.Request) {
	var trackIDs, current, changed, changedBy string
	var position int64
	err := h.db.QueryRow(`SELECT track_ids, current, position, changed_at, changed_by FROM play_queue WHERE id=1`).
		Scan(&trackIDs, &current, &position, &changed, &changedBy)
	if err != nil {
		// 无保存的队列 → ok 且不带 playQueue
		writeResponse(w, r, &Response{})
		return
	}
	pq := &PlayQueue{
		Current:   current,
		Position:  position,
		Username:  h.cfg.Auth.Username,
		Changed:   changed,
		ChangedBy: changedBy,
	}
	if trackIDs != "" {
		for _, id := range strings.Split(trackIDs, ",") {
			if c, ok := h.childByID(id); ok {
				pq.Entry = append(pq.Entry, c)
			}
		}
	}
	writeResponse(w, r, &Response{PlayQueue: pq})
}
```

- [ ] **Step 3b: 从 stubs.go 删除 getBookmarks** — 删掉 `internal/api/subsonic/stubs.go` 里的整个
```go
func (h *Handler) getBookmarks(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, r, &Response{Bookmarks: &Bookmarks{}})
}
```
（保留 `getGenres`、`getStarred2` 不动。）

- [ ] **Step 3c: 注册路由** — `internal/api/subsonic/handler.go` 的 `RegisterRoutes` 里，`h.reg(r, "getBookmarks", h.getBookmarks)` 那一行**之后**加：
```go
	h.reg(r, "createBookmark", h.createBookmark)
	h.reg(r, "deleteBookmark", h.deleteBookmark)
	h.reg(r, "savePlayQueue", h.savePlayQueue)
	h.reg(r, "getPlayQueue", h.getPlayQueue)
```
（`getBookmarks` 注册行保留不动，它现在指向 bookmarks.go 的真实现。）

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./internal/api/subsonic/ -v 2>&1 | tail -30`
Expected: build 成功；书签/队列全部测试 + 既有测试 PASS。

- [ ] **Step 5: 提交**
```bash
git add internal/api/subsonic/bookmarks.go internal/api/subsonic/bookmarks_test.go internal/api/subsonic/handler.go internal/api/subsonic/stubs.go
git commit -m "feat(subsonic): 书签 + 播放队列端点（create/get/deleteBookmark, save/getPlayQueue）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## 完成标准

- `go build ./...` 成功；`go test ./...` 全绿
- createBookmark→getBookmarks→deleteBookmark 闭环可用，同曲 upsert 覆盖，不存在曲目返回 70
- savePlayQueue→getPlayQueue 按序返回，current/position 正确，空 id 清空，未保存时无 playQueue 元素
- username 一律 `cfg.Auth.Username`；getBookmarks/getPlayQueue 跳过不可用曲目
- 全部 httptest + 内存 sqlite，不打网络

## 验证（手动，docker）

1. `make docker-build && docker compose up -d`
2. `curl "http://127.0.0.1:4533/rest/createBookmark.view?u=admin&p=admin&id=<id>&position=30000"` → ok
3. `curl "http://127.0.0.1:4533/rest/getBookmarks.view?u=admin&p=admin&f=json"` → 含该书签 + entry
4. Symfonium 播放中途切走再回来，验证能从断点续播（createBookmark 不再 404）
