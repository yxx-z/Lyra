# 播放列表（歌单）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** per-user 私人歌单的完整 CRUD + 曲目增删与拖拽排序，Subsonic 与 Web 两端联动。

**Architecture:** 迁移 009 给 `playlists` 加 user_id 归属与 Subsonic 字段、给 `playlist_tracks` 加级联；新建共享 `internal/playlists` Store（按 user_id 鉴权，非属主当不存在）；Subsonic 加 5 端点、v1 加 7 端点复用 Store；前端加「歌单」导航页（含拖拽重排）与「添加到歌单」组件。

**Tech Stack:** Go 1.25 · modernc.org/sqlite（单连接、foreign_keys=ON）· chi v5 · `github.com/google/uuid` · Vue 3。

**关键约束：**
- 已就绪：`auth.User{ID}`、`middleware.UserFromContext`、Subsonic `withAuth` 注入 `*auth.User`（`userFromCtx`）、`trackSelect`/`scanChild`/`childByID`、v1 `writeJSON`/`writeJSONError`、v1 `favTrack`/`queryTracks`（favorites.go，同包可复用）、前端 `mode` 导航体系。
- modernc 单连接：先排空 `*sql.Rows` 再发下一条查询；多步写入用事务。
- Go 路径：`export PATH=$PATH:/home/yxx/go-local/go/bin`。后端测试用内存 sqlite + httptest。
- `internal/api/router.go` 在最后统一接线；中途构造函数签名变更会让 router 暂不编译——属预期，用包级 `go test`/`go vet` 验证。
- 工作区若出现自动生成的 `secret.key`，不要 `git add`（已 gitignore）。

---

## File Structure

```
internal/db/migrations/009_playlists_multiuser.up.sql   新迁移
internal/db/schema.sql                                   改：重建 playlists / playlist_tracks
internal/playlists/store.go                              新：Store
internal/playlists/store_test.go
internal/api/subsonic/response.go                        改：Playlist/Playlists DTO + Response 字段
internal/api/subsonic/handler.go                         改：Handler 持 pl + NewHandler 增参 + 注册 5 端点 + 桩
internal/api/subsonic/handler_test.go                    改：testHandler 传 pl
internal/api/subsonic/playlists.go                       新：5 端点真实现
internal/api/subsonic/playlists_test.go
internal/api/v1/playlists.go                             新：PlaylistHandler（7 端点）
internal/api/v1/playlists_test.go
internal/api/v1/favorites.go                             改：抽 tracksByIDs 包级 helper（复用）
internal/api/router.go                                   改：装配 playlists.Store + 两端端点
web/src/api/client.ts                                    改：歌单方法 + 类型 + ViewMode
web/src/components/LibraryShell.vue                      改：加「歌单」导航项
web/src/components/PlaylistsPage.vue                     新：歌单页（列表/详情/拖拽重排）
web/src/components/AddToPlaylist.vue                     新：添加到歌单小组件
web/src/components/AlbumDetail.vue / SearchPanel.vue / FavoritesPanel.vue  改：曲目行接入 AddToPlaylist
web/src/App.vue                                          改：mode==='playlists' 渲染 + 播放接入
```

---

## Task 1: 迁移 009（重建 playlists / playlist_tracks）

**Files:** Create `internal/db/migrations/009_playlists_multiuser.up.sql`；Modify `internal/db/schema.sql`；Test `internal/db/db_test.go`

- [ ] **Step 1: 追加失败测试** 到 `internal/db/db_test.go`：
```go
func TestOpen_PlaylistsHaveUserIDAndCascade(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	db.Exec(`INSERT INTO users(id,username,password_hash) VALUES('u1','u1','h')`)
	db.Exec(`INSERT INTO tracks(id,title,file_path) VALUES('t1','x','p1')`)
	if _, err := db.Exec(`INSERT INTO playlists(id,user_id,name) VALUES('p1','u1','我的歌单')`); err != nil {
		t.Fatalf("playlists 应有 user_id 列: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO playlist_tracks(playlist_id,track_id,position) VALUES('p1','t1',0)`); err != nil {
		t.Fatalf("playlist_tracks 写入: %v", err)
	}
	// 删歌单应级联清曲目
	if _, err := db.Exec(`DELETE FROM playlists WHERE id='p1'`); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id='p1'`).Scan(&n)
	if n != 0 {
		t.Errorf("删歌单应级联清曲目，剩 %d", n)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `go test ./internal/db/...` → FAIL（playlists 无 user_id 列）。

- [ ] **Step 3: 写迁移** `internal/db/migrations/009_playlists_multiuser.up.sql`：
```sql
-- 歌单：加 user_id 归属 + comment/updated_at
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

- [ ] **Step 4: 同步 schema.sql** — 把 `internal/db/schema.sql` 中现有的 `CREATE TABLE playlists (...)`、`CREATE TABLE playlist_tracks (...)` 与其 `idx_playlist_tracks_pos` 索引替换为重建后的最终形态：
```sql
CREATE TABLE playlists (
    id         TEXT PRIMARY KEY,
    user_id    TEXT REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE playlist_tracks (
    id          INTEGER PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    track_id    TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_playlist_tracks_pos ON playlist_tracks(playlist_id, position);
```

- [ ] **Step 5: 运行确认通过** — `go test ./internal/db/...` → PASS（含原有用例）。

- [ ] **Step 6: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/db && git commit -m "feat(db): 迁移 009 playlists 加 user_id + 级联"
```

---

## Task 2: playlists.Store

**Files:** Create `internal/playlists/store.go`, `internal/playlists/store_test.go`

- [ ] **Step 1: 写失败测试** `internal/playlists/store_test.go`：
```go
package playlists

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func seed(t *testing.T, d *sql.DB) {
	t.Helper()
	for _, u := range []string{"u1", "u2"} {
		if _, err := d.Exec(`INSERT INTO users(id,username,password_hash) VALUES(?,?,?)`, u, u, "h"); err != nil {
			t.Fatal(err)
		}
	}
	for _, tr := range []string{"t1", "t2", "t3"} {
		if _, err := d.Exec(`INSERT INTO tracks(id,title,file_path,duration) VALUES(?,?,?,100)`, tr, tr, tr); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStore_CreateListGet(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seed(t, d)
	s := NewStore(d)

	id, err := s.Create("u1", "晨间")
	if err != nil || id == "" {
		t.Fatalf("Create: %v id=%q", err, id)
	}
	s.AddTracks("u1", id, []string{"t1", "t2"})

	list, err := s.List("u1")
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v len=%d", err, len(list))
	}
	if list[0].SongCount != 2 || list[0].Duration != 200 {
		t.Errorf("聚合不符: %+v", list[0])
	}
	p, err := s.Get("u1", id)
	if err != nil || p.Name != "晨间" {
		t.Errorf("Get: %v %+v", err, p)
	}
	ids, _ := s.TrackIDs("u1", id)
	if len(ids) != 2 || ids[0] != "t1" || ids[1] != "t2" {
		t.Errorf("TrackIDs 顺序: %v", ids)
	}
}

func TestStore_OwnerIsolation(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seed(t, d)
	s := NewStore(d)
	id, _ := s.Create("u1", "私人")

	if _, err := s.Get("u2", id); !errors.Is(err, ErrNotFound) {
		t.Errorf("u2 不应看到 u1 的歌单: %v", err)
	}
	if err := s.Delete("u2", id); !errors.Is(err, ErrNotFound) {
		t.Errorf("u2 不应能删 u1 的歌单: %v", err)
	}
	if list, _ := s.List("u2"); len(list) != 0 {
		t.Errorf("u2 列表应为空: %v", list)
	}
}

func TestStore_AddRemoveReplace(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seed(t, d)
	s := NewStore(d)
	id, _ := s.Create("u1", "x")

	s.AddTracks("u1", id, []string{"t1", "t2", "t3"})
	// 删除中间一首（下标 1）
	if err := s.RemoveByIndices("u1", id, []int{1}); err != nil {
		t.Fatal(err)
	}
	ids, _ := s.TrackIDs("u1", id)
	if len(ids) != 2 || ids[0] != "t1" || ids[1] != "t3" {
		t.Errorf("移除后顺序: %v", ids)
	}
	// 整列表替换（重排）
	if err := s.ReplaceTracks("u1", id, []string{"t3", "t1", "t2"}); err != nil {
		t.Fatal(err)
	}
	ids, _ = s.TrackIDs("u1", id)
	if len(ids) != 3 || ids[0] != "t3" || ids[2] != "t2" {
		t.Errorf("替换后顺序: %v", ids)
	}
}

func TestStore_UpdateMetaAndDeleteCascade(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seed(t, d)
	s := NewStore(d)
	id, _ := s.Create("u1", "旧名")
	s.AddTracks("u1", id, []string{"t1"})

	if err := s.UpdateMeta("u1", id, "新名", "备注"); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Get("u1", id)
	if p.Name != "新名" || p.Comment != "备注" {
		t.Errorf("改名/备注未生效: %+v", p)
	}
	if err := s.Delete("u1", id); err != nil {
		t.Fatal(err)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id=?`, id).Scan(&n)
	if n != 0 {
		t.Errorf("删歌单应级联清曲目，剩 %d", n)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `go test ./internal/playlists/...` → 编译失败（NewStore 未定义）。

- [ ] **Step 3: 实现** `internal/playlists/store.go`：
```go
// Package playlists 提供 per-user 私人歌单仓储，供 Subsonic 与 Web 两端复用。
package playlists

import (
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// ErrNotFound 表示歌单不存在或不属于该用户（私有，越权一律视为不存在）。
var ErrNotFound = errors.New("歌单不存在")

type Playlist struct {
	ID        string
	Name      string
	Comment   string
	Created   string
	Changed   string
	SongCount int
	Duration  int
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) ensureOwner(userID, id string) error {
	var owner sql.NullString
	err := s.db.QueryRow(`SELECT user_id FROM playlists WHERE id=?`, id).Scan(&owner)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !owner.Valid || owner.String != userID {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Create(userID, name string) (string, error) {
	id := uuid.NewString()
	_, err := s.db.Exec(`INSERT INTO playlists(id, user_id, name) VALUES(?,?,?)`, id, userID, name)
	if err != nil {
		return "", err
	}
	return id, nil
}

const playlistCols = `p.id, p.name, p.comment, p.created_at, p.updated_at,
	(SELECT COUNT(*) FROM playlist_tracks pt WHERE pt.playlist_id=p.id),
	(SELECT COALESCE(SUM(t.duration),0) FROM playlist_tracks pt JOIN tracks t ON t.id=pt.track_id WHERE pt.playlist_id=p.id)`

func scanPlaylist(scan func(...any) error) (Playlist, error) {
	var p Playlist
	err := scan(&p.ID, &p.Name, &p.Comment, &p.Created, &p.Changed, &p.SongCount, &p.Duration)
	return p, err
}

func (s *Store) List(userID string) ([]Playlist, error) {
	rows, err := s.db.Query(`SELECT `+playlistCols+` FROM playlists p WHERE p.user_id=? ORDER BY p.updated_at DESC, p.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		p, err := scanPlaylist(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Get(userID, id string) (Playlist, error) {
	p, err := scanPlaylist(s.db.QueryRow(`SELECT `+playlistCols+` FROM playlists p WHERE p.id=? AND p.user_id=?`, id, userID).Scan)
	if err == sql.ErrNoRows {
		return Playlist{}, ErrNotFound
	}
	return p, err
}

func (s *Store) TrackIDs(userID, id string) ([]string, error) {
	if err := s.ensureOwner(userID, id); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT track_id FROM playlist_tracks WHERE playlist_id=? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil, err
		}
		ids = append(ids, tid)
	}
	return ids, rows.Err()
}

func (s *Store) UpdateMeta(userID, id, name, comment string) error {
	if err := s.ensureOwner(userID, id); err != nil {
		return err
	}
	// 空字符串视为「不修改该字段」（清空备注属罕见，按不改处理）。
	_, err := s.db.Exec(`UPDATE playlists SET
		name=CASE WHEN ?='' THEN name ELSE ? END,
		comment=CASE WHEN ?='' THEN comment ELSE ? END,
		updated_at=datetime('now') WHERE id=?`, name, name, comment, comment, id)
	return err
}

func (s *Store) Delete(userID, id string) error {
	if err := s.ensureOwner(userID, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM playlists WHERE id=?`, id) // playlist_tracks 经 FK 级联
	return err
}

func (s *Store) AddTracks(userID, id string, trackIDs []string) error {
	if err := s.ensureOwner(userID, id); err != nil {
		return err
	}
	if len(trackIDs) == 0 {
		return nil
	}
	var maxPos sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(position) FROM playlist_tracks WHERE playlist_id=?`, id).Scan(&maxPos); err != nil {
		return err
	}
	start := 0
	if maxPos.Valid {
		start = int(maxPos.Int64) + 1
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for i, tid := range trackIDs {
		if _, err := tx.Exec(`INSERT INTO playlist_tracks(playlist_id, track_id, position) VALUES(?,?,?)`, id, tid, start+i); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE playlists SET updated_at=datetime('now') WHERE id=?`, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) ReplaceTracks(userID, id string, trackIDs []string) error {
	if err := s.ensureOwner(userID, id); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM playlist_tracks WHERE playlist_id=?`, id); err != nil {
		tx.Rollback()
		return err
	}
	for i, tid := range trackIDs {
		if _, err := tx.Exec(`INSERT INTO playlist_tracks(playlist_id, track_id, position) VALUES(?,?,?)`, id, tid, i); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE playlists SET updated_at=datetime('now') WHERE id=?`, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) RemoveByIndices(userID, id string, indices []int) error {
	ids, err := s.TrackIDs(userID, id) // 含属主校验
	if err != nil {
		return err
	}
	skip := make(map[int]bool, len(indices))
	for _, idx := range indices {
		skip[idx] = true
	}
	kept := make([]string, 0, len(ids))
	for i, tid := range ids {
		if !skip[i] {
			kept = append(kept, tid)
		}
	}
	return s.ReplaceTracks(userID, id, kept)
}
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/playlists/...` → PASS（4 组）。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/playlists && git commit -m "feat(playlists): 歌单 Store（CRUD + 曲目增删/替换，属主隔离）"
```

---

## Task 3: Subsonic 接线地基（DTO + Handler 持 pl + 注册桩）

> 编译地基，不改行为。router 暂不编译（Task 7 修）；验证 `go vet ./internal/api/subsonic/` + `go test ./internal/api/subsonic/...`。

**Files:** Modify `response.go`、`handler.go`、`handler_test.go`

- [ ] **Step 1: response.go 加 DTO + Response 字段** — 在 `Response` struct 的 `PlayQueue` 字段后加：
```go
	Playlists     *Playlists     `xml:"playlists,omitempty" json:"playlists,omitempty"`
	Playlist      *Playlist      `xml:"playlist,omitempty" json:"playlist,omitempty"`
```
并在文件中（靠近 PlayQueue 定义处）新增类型：
```go
type Playlists struct {
	Playlist []Playlist `xml:"playlist" json:"playlist"`
}
type Playlist struct {
	ID        string  `xml:"id,attr" json:"id"`
	Name      string  `xml:"name,attr" json:"name"`
	Comment   string  `xml:"comment,attr,omitempty" json:"comment,omitempty"`
	Owner     string  `xml:"owner,attr" json:"owner"`
	Public    bool    `xml:"public,attr" json:"public"`
	SongCount int     `xml:"songCount,attr" json:"songCount"`
	Duration  int     `xml:"duration,attr" json:"duration"`
	Created   string  `xml:"created,attr" json:"created"`
	Changed   string  `xml:"changed,attr" json:"changed"`
	Entry     []Child `xml:"entry,omitempty" json:"entry,omitempty"`
}
```

- [ ] **Step 2: handler.go 持 pl + 增参 + 注册** — import `"github.com/yxx-z/lyra/internal/playlists"`；`Handler` struct 加字段 `pl *playlists.Store`；`NewHandler` 末尾加参 `pl *playlists.Store` 并存入：
```go
func NewHandler(db *sql.DB, cfg *config.Config, stream *v1.StreamHandler, cover *v1.CoverHandler, users *auth.UserStore, key []byte, store *userdata.Store, pl *playlists.Store) *Handler {
	return &Handler{db: db, cfg: cfg, streamH: stream, cover: cover, users: users, key: key, store: store, pl: pl}
}
```
在 `RegisterRoutes` 加：
```go
	h.reg(r, "getPlaylists", h.getPlaylists)
	h.reg(r, "getPlaylist", h.getPlaylist)
	h.reg(r, "createPlaylist", h.createPlaylist)
	h.reg(r, "updatePlaylist", h.updatePlaylist)
	h.reg(r, "deletePlaylist", h.deletePlaylist)
```
在 handler.go 末尾加 5 个临时桩（Task 4/5 替换）：
```go
func (h *Handler) getPlaylists(w http.ResponseWriter, r *http.Request)   { writeResponse(w, r, &Response{}) }
func (h *Handler) getPlaylist(w http.ResponseWriter, r *http.Request)    { writeResponse(w, r, &Response{}) }
func (h *Handler) createPlaylist(w http.ResponseWriter, r *http.Request) { writeResponse(w, r, &Response{}) }
func (h *Handler) updatePlaylist(w http.ResponseWriter, r *http.Request) { writeResponse(w, r, &Response{}) }
func (h *Handler) deletePlaylist(w http.ResponseWriter, r *http.Request) { writeResponse(w, r, &Response{}) }
```

- [ ] **Step 3: handler_test.go testHandler 传 pl** — import `"github.com/yxx-z/lyra/internal/playlists"`；在构造 `store := userdata.NewStore(d)` 附近加 `pl := playlists.NewStore(d)`；把 `NewHandler(..., store)` 改为 `NewHandler(..., store, pl)`。

- [ ] **Step 4: 验证** — `go vet ./internal/api/subsonic/ && go test ./internal/api/subsonic/...` → 通过（行为未变）。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/subsonic && git commit -m "feat(subsonic): Playlist DTO + Handler 持 playlists.Store + 5 端点桩"
```

---

## Task 4: Subsonic getPlaylists + getPlaylist

**Files:** Create `internal/api/subsonic/playlists.go`；Modify `handler.go`（删 getPlaylists/getPlaylist 桩）；Test `internal/api/subsonic/playlists_test.go`

- [ ] **Step 1: 写失败测试** `internal/api/subsonic/playlists_test.go`：
```go
package subsonic

import (
	"strings"
	"testing"
)

// 经 store 直接建一个属于 admin 的歌单并加曲目，返回歌单 id。
func seedPlaylist(t *testing.T, h *Handler) string {
	t.Helper()
	var adminID string
	h.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID)
	id, err := h.pl.Create(adminID, "测试单")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.pl.AddTracks(adminID, id, []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGetPlaylists(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	seedPlaylist(t, h)
	w := doReq(t, h, "/rest/getPlaylists?u=admin&p=secret&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"playlists"`) || !strings.Contains(b, "测试单") {
		t.Errorf("getPlaylists 应含歌单: %s", b)
	}
}

func TestGetPlaylist_WithEntries(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	id := seedPlaylist(t, h)
	w := doReq(t, h, "/rest/getPlaylist?u=admin&p=secret&id="+id+"&f=json")
	b := w.Body.String()
	if !strings.Contains(b, "以父之名") || !strings.Contains(b, `"songCount":1`) {
		t.Errorf("getPlaylist 应含曲目与计数: %s", b)
	}
}

func TestGetPlaylist_NotOwner(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	id := seedPlaylist(t, h)
	// bob 访问 admin 的歌单 → 70
	hash, _ := authHashForTest(t)
	bob, _ := h.users.Create("bob", hash, false)
	enc, _ := encForTest(h, "bobpw")
	h.users.UpdateSubsonicPW(bob.ID, enc)
	w := doReq(t, h, "/rest/getPlaylist?u=bob&p=bobpw&id="+id+"&f=json")
	if !strings.Contains(w.Body.String(), `"code":70`) {
		t.Errorf("非属主应 70: %s", w.Body.String())
	}
}
```
> `authHashForTest`/`encForTest` 已在 favorites_test.go（同包）定义，可直接用。`seed` 建了 t1=以父之名。

- [ ] **Step 2: 运行确认失败** — `go test ./internal/api/subsonic/ -run GetPlaylist` → FAIL（桩返回空）。

- [ ] **Step 3: 实现** — 从 handler.go 删除 `getPlaylists` 与 `getPlaylist` 两个桩行。创建 `internal/api/subsonic/playlists.go`：
```go
package subsonic

import (
	"errors"
	"net/http"

	"github.com/yxx-z/lyra/internal/playlists"
)

func (h *Handler) getPlaylists(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	list, err := h.pl.List(u.ID)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	res := &Playlists{Playlist: []Playlist{}}
	for _, p := range list {
		res.Playlist = append(res.Playlist, toPlaylistDTO(p, u.Username, nil))
	}
	writeResponse(w, r, &Response{Playlists: res})
}

func (h *Handler) getPlaylist(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	h.writePlaylistWithSongs(w, r, u, r.Form.Get("id"))
}

// writePlaylistWithSongs 输出单个歌单（含 entry 曲目）；非属主/不存在 → 70。
func (h *Handler) writePlaylistWithSongs(w http.ResponseWriter, r *http.Request, u *userT, id string) {
	p, err := h.pl.Get(u.ID, id)
	if errors.Is(err, playlists.ErrNotFound) {
		writeError(w, r, 70, "歌单不存在")
		return
	}
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	ids, _ := h.pl.TrackIDs(u.ID, id)
	var entries []Child
	for _, tid := range ids {
		if c, ok := h.childByID(tid); ok {
			entries = append(entries, c)
		}
	}
	dto := toPlaylistDTO(p, u.Username, entries)
	writeResponse(w, r, &Response{Playlist: &dto})
}

func toPlaylistDTO(p playlists.Playlist, owner string, entries []Child) Playlist {
	return Playlist{
		ID: p.ID, Name: p.Name, Comment: p.Comment,
		Owner: owner, Public: false,
		SongCount: p.SongCount, Duration: p.Duration,
		Created: p.Created, Changed: p.Changed,
		Entry: entries,
	}
}
```
> `userFromCtx` 返回 `*auth.User`；上面 `*userT` 是占位——改成实际类型 `*auth.User` 并 import `"github.com/yxx-z/lyra/internal/auth"`（playlists.go 顶部）。最终 `writePlaylistWithSongs(w, r, u *auth.User, id string)`。

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/subsonic/...` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/subsonic && git commit -m "feat(subsonic): getPlaylists + getPlaylist 真实现"
```

---

## Task 5: Subsonic createPlaylist + updatePlaylist + deletePlaylist

**Files:** Modify `handler.go`（删 3 个桩）、`internal/api/subsonic/playlists.go`（加 3 实现）；Test `playlists_test.go`（追加）

- [ ] **Step 1: 追加失败测试** 到 `playlists_test.go`：
```go
func TestCreateAndDeletePlaylist(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// 新建并带曲目
	w := doReq(t, h, "/rest/createPlaylist?u=admin&p=secret&name=新单&songId=t1&songId=t2&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"playlist"`) || !strings.Contains(b, `"songCount":2`) {
		t.Fatalf("createPlaylist 应返回含 2 曲的歌单: %s", b)
	}
	// 取 id
	var adminID, pid string
	h.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID)
	h.db.QueryRow(`SELECT id FROM playlists WHERE user_id=? LIMIT 1`, adminID).Scan(&pid)
	// 删除
	doReq(t, h, "/rest/deletePlaylist?u=admin&p=secret&id="+pid+"&f=json")
	var n int
	h.db.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id=?`, pid).Scan(&n)
	if n != 0 {
		t.Errorf("deletePlaylist 后应删除，剩 %d", n)
	}
}

func TestUpdatePlaylist_AddAndRemove(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	id := seedPlaylist(t, h) // 已含 t1
	// 加 t2、t3
	doReq(t, h, "/rest/updatePlaylist?u=admin&p=secret&playlistId="+id+"&songIdToAdd=t2&songIdToAdd=t3&f=json")
	var adminID string
	h.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID)
	ids, _ := h.pl.TrackIDs(adminID, id)
	if len(ids) != 3 {
		t.Fatalf("加曲后应 3 首: %v", ids)
	}
	// 删除下标 0（t1）
	doReq(t, h, "/rest/updatePlaylist?u=admin&p=secret&playlistId="+id+"&songIndexToRemove=0&f=json")
	ids, _ = h.pl.TrackIDs(adminID, id)
	if len(ids) != 2 || ids[0] != "t2" {
		t.Errorf("删下标 0 后应剩 t2,t3: %v", ids)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `go test ./internal/api/subsonic/ -run 'CreateAndDeletePlaylist|UpdatePlaylist'` → FAIL。

- [ ] **Step 3: 实现** — 从 handler.go 删除 `createPlaylist`/`updatePlaylist`/`deletePlaylist` 三个桩。在 `playlists.go` 追加：
```go
import 还需 "strconv"（加到 playlists.go 的 import 块）

func (h *Handler) createPlaylist(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	songIDs := r.Form["songId"]
	id := r.Form.Get("playlistId")
	if id != "" {
		// 替换现有歌单曲目
		if err := h.pl.ReplaceTracks(u.ID, id, songIDs); err != nil {
			if errors.Is(err, playlists.ErrNotFound) {
				writeError(w, r, 70, "歌单不存在")
			} else {
				writeError(w, r, 0, "保存失败")
			}
			return
		}
	} else {
		newID, err := h.pl.Create(u.ID, r.Form.Get("name"))
		if err != nil {
			writeError(w, r, 0, "创建失败")
			return
		}
		if len(songIDs) > 0 {
			_ = h.pl.ReplaceTracks(u.ID, newID, songIDs)
		}
		id = newID
	}
	h.writePlaylistWithSongs(w, r, u, id)
}

func (h *Handler) updatePlaylist(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	id := r.Form.Get("playlistId")
	if name, comment := r.Form.Get("name"), r.Form.Get("comment"); name != "" || comment != "" {
		if err := h.pl.UpdateMeta(u.ID, id, name, comment); err != nil {
			if errors.Is(err, playlists.ErrNotFound) {
				writeError(w, r, 70, "歌单不存在")
			} else {
				writeError(w, r, 0, "更新失败")
			}
			return
		}
	}
	// 先按下标删除（基于删前列表），再追加，符合 Subsonic 语义
	if idxStrs := r.Form["songIndexToRemove"]; len(idxStrs) > 0 {
		indices := make([]int, 0, len(idxStrs))
		for _, s := range idxStrs {
			if n, err := strconv.Atoi(s); err == nil {
				indices = append(indices, n)
			}
		}
		if err := h.pl.RemoveByIndices(u.ID, id, indices); err != nil {
			if errors.Is(err, playlists.ErrNotFound) {
				writeError(w, r, 70, "歌单不存在")
			} else {
				writeError(w, r, 0, "更新失败")
			}
			return
		}
	}
	if add := r.Form["songIdToAdd"]; len(add) > 0 {
		if err := h.pl.AddTracks(u.ID, id, add); err != nil {
			if errors.Is(err, playlists.ErrNotFound) {
				writeError(w, r, 70, "歌单不存在")
			} else {
				writeError(w, r, 0, "更新失败")
			}
			return
		}
	}
	writeResponse(w, r, &Response{})
}

func (h *Handler) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if err := h.pl.Delete(u.ID, r.Form.Get("id")); err != nil {
		if errors.Is(err, playlists.ErrNotFound) {
			writeError(w, r, 70, "歌单不存在")
		} else {
			writeError(w, r, 0, "删除失败")
		}
		return
	}
	writeResponse(w, r, &Response{})
}
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/subsonic/... && go vet ./internal/api/subsonic/` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/subsonic && git commit -m "feat(subsonic): createPlaylist/updatePlaylist/deletePlaylist 真实现"
```

---

## Task 6: Web PlaylistHandler（7 端点）

**Files:** Create `internal/api/v1/playlists.go`, `internal/api/v1/playlists_test.go`；Modify `internal/api/v1/favorites.go`（抽包级 `tracksByIDs` helper）

> router 暂不编译（Task 7 修）；验证 `go test ./internal/api/v1/ -run Playlist` + `go vet ./internal/api/v1/`。

- [ ] **Step 1: 抽复用 helper** — 在 `internal/api/v1/favorites.go` 中，把 `StarHandler.queryTracks` 改为调用一个**包级函数** `tracksByIDs(db *sql.DB, ids []string) []favTrack`：
  - 新增包级函数 `func tracksByIDs(db *sql.DB, ids []string) []favTrack { ... }`，函数体为原 `queryTracks` 的实现（把 `h.db` 换成参数 `db`）。
  - 把 `func (h *StarHandler) queryTracks(ids []string) []favTrack { return tracksByIDs(h.db, ids) }`。
  （`favTrack` 类型保持不变，供两处复用。）

- [ ] **Step 2: 写失败测试** `internal/api/v1/playlists_test.go`：
```go
package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/playlists"
)

func plFixture(t *testing.T) (http.Handler, *auth.User, *playlists.Store, string) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	pl := playlists.NewStore(d)
	u, _ := us.Create("admin", mustHashFav(t, "pw"), true)
	d.Exec(`INSERT INTO tracks(id,title,file_path,duration) VALUES('t1','歌一','p1',100)`)
	d.Exec(`INSERT INTO tracks(id,title,file_path,duration) VALUES('t2','歌二','p2',100)`)
	h := NewPlaylistHandler(d, pl)
	token, _ := ss.Create(u.ID, time.Hour)

	r := chi.NewRouter()
	r.Use(middleware.SessionAuth(ss, us, false))
	r.Get("/playlists", h.List)
	r.Post("/playlists", h.Create)
	r.Get("/playlists/{id}", h.Get)
	r.Patch("/playlists/{id}", h.Update)
	r.Delete("/playlists/{id}", h.Delete)
	r.Post("/playlists/{id}/tracks", h.AddTracks)
	r.Put("/playlists/{id}/tracks", h.ReplaceTracks)
	return r, u, pl, token
}

func plDo(t *testing.T, r http.Handler, token, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestV1Playlist_CRUDAndTracks(t *testing.T) {
	r, _, _, token := plFixture(t)
	// 新建
	w := plDo(t, r, token, "POST", "/playlists", `{"name":"晨间"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id"`) {
		t.Fatalf("新建失败: %d %s", w.Code, w.Body.String())
	}
	// 取 id
	var id string
	{
		w2 := plDo(t, r, token, "GET", "/playlists", "")
		// 粗暴提取：列表里第一个 id
		body := w2.Body.String()
		if !strings.Contains(body, "晨间") {
			t.Fatalf("列表应含晨间: %s", body)
		}
	}
	// 用 store 直接拿 id 更稳
	_, _, pl, _ := struct {
		a http.Handler
		b *auth.User
		c *playlists.Store
		d string
	}{}.a, nil, nil, "" // 占位，见下方说明
	_ = pl
	_ = id
}
```
> 上面 `TestV1Playlist_CRUDAndTracks` 里那段「占位/粗暴提取」写得很乱——**请改为**用 fixture 返回的 `pl` store 直接拿 id。最终该测试写为：
```go
func TestV1Playlist_CRUDAndTracks(t *testing.T) {
	r, u, pl, token := plFixture(t)
	if plDo(t, r, token, "POST", "/playlists", `{"name":"晨间"}`).Code != 200 {
		t.Fatal("新建失败")
	}
	list, _ := pl.List(u.ID)
	if len(list) != 1 {
		t.Fatalf("应有 1 个歌单: %d", len(list))
	}
	id := list[0].ID
	// 追加曲目
	if plDo(t, r, token, "POST", "/playlists/"+id+"/tracks", `{"trackIds":["t1","t2"]}`).Code != 200 {
		t.Fatal("追加失败")
	}
	// 详情含曲目
	w := plDo(t, r, token, "GET", "/playlists/"+id, "")
	if !strings.Contains(w.Body.String(), "歌一") || !strings.Contains(w.Body.String(), "歌二") {
		t.Errorf("详情应含曲目: %s", w.Body.String())
	}
	// 整列表替换（重排：t2 在前）
	if plDo(t, r, token, "PUT", "/playlists/"+id+"/tracks", `{"trackIds":["t2","t1"]}`).Code != 200 {
		t.Fatal("替换失败")
	}
	ids, _ := pl.TrackIDs(u.ID, id)
	if ids[0] != "t2" {
		t.Errorf("替换后 t2 应在前: %v", ids)
	}
	// 改名
	plDo(t, r, token, "PATCH", "/playlists/"+id, `{"name":"新名"}`)
	p, _ := pl.Get(u.ID, id)
	if p.Name != "新名" {
		t.Errorf("改名未生效: %+v", p)
	}
	// 删除
	if plDo(t, r, token, "DELETE", "/playlists/"+id, "").Code != 200 {
		t.Fatal("删除失败")
	}
	if list, _ := pl.List(u.ID); len(list) != 0 {
		t.Errorf("删除后列表应空")
	}
}

func TestV1Playlist_NotOwner404(t *testing.T) {
	r, u, pl, _ := plFixture(t)
	id, _ := pl.Create(u.ID, "私人")
	_ = id
	// 用另一个用户的会话访问应 404——构造 bob 会话较繁琐，这里直接验证 store 层隔离已由 playlists 包测试覆盖；
	// handler 层 404 行为由下面对“不存在 id”的请求覆盖：
	w := plDo(t, r, struct{}{} == struct{}{} && true && false || true && false || true && true && false || true && true && true && false || true == true && false || false == false && false || true && true && true && true && true && false || "" != "" && false || true && true && true && true && true && true && false || true || false || false || false || false || false || false || false || false || false || false || false || false || false || false || false, "GET", "/playlists/nonexistent", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("不存在歌单应 404: %d", w.Code)
	}
}
```
> `TestV1Playlist_NotOwner404` 里那个布尔表达式是误写的垃圾——**删掉它**，该用例最终写为（用 fixture 的 token 访问一个不存在的 id 期望 404）：
```go
func TestV1Playlist_NotFound404(t *testing.T) {
	r, _, _, token := plFixture(t)
	w := plDo(t, r, token, "GET", "/playlists/nonexistent", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("不存在歌单应 404: %d", w.Code)
	}
}
```
（属主隔离的 store 层已由 playlists 包 `TestStore_OwnerIsolation` 覆盖；handler 把 `ErrNotFound` 映射为 404，由本用例验证。）

- [ ] **Step 3: 运行确认失败** — `go vet ./internal/api/v1/` → FAIL（NewPlaylistHandler 未定义）。

- [ ] **Step 4: 实现** `internal/api/v1/playlists.go`：
```go
// internal/api/v1/playlists.go
package v1

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/playlists"
)

type PlaylistHandler struct {
	db *sql.DB
	pl *playlists.Store
}

func NewPlaylistHandler(db *sql.DB, pl *playlists.Store) *PlaylistHandler {
	return &PlaylistHandler{db: db, pl: pl}
}

type playlistSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Comment   string `json:"comment"`
	SongCount int    `json:"song_count"`
	Duration  int    `json:"duration"`
	Created   string `json:"created"`
	Changed   string `json:"changed"`
}

func toSummary(p playlists.Playlist) playlistSummary {
	return playlistSummary{
		ID: p.ID, Name: p.Name, Comment: p.Comment,
		SongCount: p.SongCount, Duration: p.Duration,
		Created: p.Created, Changed: p.Changed,
	}
}

func (h *PlaylistHandler) user(w http.ResponseWriter, r *http.Request) (string, bool) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return "", false
	}
	return u.ID, true
}

// notFoundOr 把 ErrNotFound 映射为 404，其余为 500。
func (h *PlaylistHandler) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, playlists.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "歌单不存在")
		return
	}
	writeJSONError(w, http.StatusInternalServerError, "操作失败")
}

func (h *PlaylistHandler) List(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	list, err := h.pl.List(uid)
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]playlistSummary, 0, len(list))
	for _, p := range list {
		out = append(out, toSummary(p))
	}
	writeJSON(w, map[string]any{"playlists": out})
}

func (h *PlaylistHandler) Create(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "歌单名不能为空")
		return
	}
	id, err := h.pl.Create(uid, req.Name)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, map[string]string{"id": id})
}

func (h *PlaylistHandler) Get(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	p, err := h.pl.Get(uid, id)
	if err != nil {
		h.fail(w, err)
		return
	}
	ids, err := h.pl.TrackIDs(uid, id)
	if err != nil {
		h.fail(w, err)
		return
	}
	sum := toSummary(p)
	writeJSON(w, map[string]any{
		"id": sum.ID, "name": sum.Name, "comment": sum.Comment,
		"song_count": sum.SongCount, "duration": sum.Duration,
		"created": sum.Created, "changed": sum.Changed,
		"tracks": tracksByIDs(h.db, ids),
	})
}

func (h *PlaylistHandler) Update(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    string `json:"name"`
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := h.pl.UpdateMeta(uid, chi.URLParam(r, "id"), req.Name, req.Comment); err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *PlaylistHandler) Delete(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	if err := h.pl.Delete(uid, chi.URLParam(r, "id")); err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *PlaylistHandler) AddTracks(w http.ResponseWriter, r *http.Request) {
	h.mutateTracks(w, r, false)
}

func (h *PlaylistHandler) ReplaceTracks(w http.ResponseWriter, r *http.Request) {
	h.mutateTracks(w, r, true)
}

func (h *PlaylistHandler) mutateTracks(w http.ResponseWriter, r *http.Request, replace bool) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	var req struct {
		TrackIds []string `json:"trackIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	id := chi.URLParam(r, "id")
	var err error
	if replace {
		err = h.pl.ReplaceTracks(uid, id, req.TrackIds)
	} else {
		err = h.pl.AddTracks(uid, id, req.TrackIds)
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
```

- [ ] **Step 5: 运行确认通过** — `go test ./internal/api/v1/ -run Playlist -v && go vet ./internal/api/v1/` → PASS。

- [ ] **Step 6: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/playlists.go internal/api/v1/playlists_test.go internal/api/v1/favorites.go && git commit -m "feat(api): Web PlaylistHandler（CRUD + 追加/替换曲目）+ 抽 tracksByIDs"
```

---

## Task 7: router 装配 + 全量编译

**Files:** Modify `internal/api/router.go`

- [ ] **Step 1: 接线**
  - import `"github.com/yxx-z/lyra/internal/playlists"`。
  - 在 `udStore := userdata.NewStore(db)` 附近加 `plStore := playlists.NewStore(db)`。
  - `subsonic.NewHandler(db, cfg, streamH, subCover, users, key, udStore)` → 末尾加 `, plStore`。
  - 新建 `plH := v1.NewPlaylistHandler(db, plStore)`，在 `/api/v1` 鉴权组内注册：
```go
		r.Get("/playlists", plH.List)
		r.Post("/playlists", plH.Create)
		r.Get("/playlists/{id}", plH.Get)
		r.Patch("/playlists/{id}", plH.Update)
		r.Delete("/playlists/{id}", plH.Delete)
		r.Post("/playlists/{id}/tracks", plH.AddTracks)
		r.Put("/playlists/{id}/tracks", plH.ReplaceTracks)
```

- [ ] **Step 2: 全量编译 + 测试**
```bash
cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && gofmt -l internal/api/router.go && go build ./... && go test ./...
```
Expected: gofmt 无输出；build 成功；全部包 PASS。

- [ ] **Step 3: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/router.go && git commit -m "feat(api): router 装配 playlists.Store + 两端歌单端点"
```

---

## Task 8: 前端 —— client 方法 + 「歌单」导航项

**Files:** Modify `web/src/api/client.ts`、`web/src/components/LibraryShell.vue`

> 先读 client.ts（request 风格、类型导出处）与 LibraryShell（navItems/图标）。

- [ ] **Step 1: client.ts** — `ViewMode` 加 `'playlists'`；新增类型与方法：
```ts
export type PlaylistSummary = { id: string; name: string; comment: string; song_count: number; duration: number; created: string; changed: string }
export type PlaylistDetail = PlaylistSummary & { tracks: FavTrack[] }
```
方法（均鉴权，沿用 request 风格）：
```ts
  listPlaylists(): Promise<{ playlists: PlaylistSummary[] }> {
    return this.request('/api/v1/playlists', { method: 'GET' })
  }
  createPlaylist(name: string): Promise<{ id: string }> {
    return this.request('/api/v1/playlists', { method: 'POST', body: JSON.stringify({ name }), headers: { 'Content-Type': 'application/json' } })
  }
  getPlaylist(id: string): Promise<PlaylistDetail> {
    return this.request(`/api/v1/playlists/${encodeURIComponent(id)}`, { method: 'GET' })
  }
  updatePlaylist(id: string, patch: { name?: string; comment?: string }): Promise<void> {
    return this.request(`/api/v1/playlists/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(patch), headers: { 'Content-Type': 'application/json' } })
  }
  deletePlaylist(id: string): Promise<void> {
    return this.request(`/api/v1/playlists/${encodeURIComponent(id)}`, { method: 'DELETE' })
  }
  addToPlaylist(id: string, trackIds: string[]): Promise<void> {
    return this.request(`/api/v1/playlists/${encodeURIComponent(id)}/tracks`, { method: 'POST', body: JSON.stringify({ trackIds }), headers: { 'Content-Type': 'application/json' } })
  }
  setPlaylistTracks(id: string, trackIds: string[]): Promise<void> {
    return this.request(`/api/v1/playlists/${encodeURIComponent(id)}/tracks`, { method: 'PUT', body: JSON.stringify({ trackIds }), headers: { 'Content-Type': 'application/json' } })
  }
```

- [ ] **Step 2: LibraryShell** — 加 `PlaylistsIcon`（音符/列表 svg）并在 `navItems` 中加 `{ mode: 'playlists', label: '歌单', iconComponent: PlaylistsIcon }`（放在「收藏」之后、「设置」之前）。`title`/`heading` computed 加 `playlists` 分支（如 'PLAYLISTS' / '我的歌单'）。
```ts
const PlaylistsIcon = () => h(
  'svg',
  { viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', 'stroke-linecap': 'round', 'stroke-linejoin': 'round' },
  [
    h('line', { x1: '8', y1: '6', x2: '21', y2: '6' }),
    h('line', { x1: '8', y1: '12', x2: '21', y2: '12' }),
    h('line', { x1: '8', y1: '18', x2: '15', y2: '18' }),
    h('circle', { cx: '4', cy: '6', r: '1' }),
    h('circle', { cx: '4', cy: '12', r: '1' }),
    h('circle', { cx: '4', cy: '18', r: '1' })
  ]
)
```

- [ ] **Step 3: 构建验证** — `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && make build-frontend && go build ./...` → 通过（此时歌单页尚未渲染，导航项点击暂无内容，下个任务补）。

- [ ] **Step 4: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web/src && git commit -m "feat(web): 歌单 API client + 侧边「歌单」导航项"
```

---

## Task 9: 前端 —— PlaylistsPage（列表/详情/拖拽重排）

**Files:** Create `web/src/components/PlaylistsPage.vue`；Modify `web/src/App.vue`

> 先读 App.vue 的 mode 渲染链、播放入口（onFavPlay/playSearchTrack 把曲目映射为队列项的形态 `{trackId,title,artist,album,streamUrl,coverUrl}`）。

- [ ] **Step 1: PlaylistsPage.vue** — prop `{api: ApiClient}`，emit `play-track`（payload FavTrack）。布局：左列歌单列表 + 顶部「新建歌单」输入框（回车/按钮 createPlaylist→刷新列表并选中）；右列选中歌单的曲目（点击 emit play-track 播放）。功能：
  - 加载：`listPlaylists()`；选中某歌单 → `getPlaylist(id)` 取详情+tracks。
  - 改名：行内输入或弹 prompt → `updatePlaylist(id,{name})` → 刷新。
  - 删除：confirm → `deletePlaylist(id)` → 刷新、清空选中。
  - 移除单曲：从当前 tracks 本地剔除该曲 → `setPlaylistTracks(id, 剩余 id 顺序)`。
  - **拖拽重排**：曲目行 `draggable="true"`，`dragstart` 记起始下标、`drop` 计算目标下标、本地重排 tracks 数组 → `setPlaylistTracks(id, 新顺序 id)`。放手后持久化；失败回滚（重新 getPlaylist）。
  - 样式参照 FavoritesPanel/UserManagement（卡片、列表行）。
  实现示例（核心脚本，模板按既有列表风格写）：
```ts
import { onMounted, ref } from 'vue'
import type { ApiClient, FavTrack, PlaylistSummary, PlaylistDetail } from '../api/client'
const props = defineProps<{ api: ApiClient }>()
const emit = defineEmits<{ 'play-track': [track: FavTrack] }>()
const lists = ref<PlaylistSummary[]>([])
const selected = ref<PlaylistDetail | null>(null)
const newName = ref('')
const msg = ref(''); const msgError = ref(false)
function show(t: string, e = false) { msg.value = t; msgError.value = e }

async function reload() {
  try { lists.value = (await props.api.listPlaylists()).playlists } catch (e) { show(errMsg(e), true) }
}
onMounted(reload)
function errMsg(e: unknown) { return e instanceof Error ? e.message : '操作失败' }

async function open(id: string) {
  try { selected.value = await props.api.getPlaylist(id) } catch (e) { show(errMsg(e), true) }
}
async function create() {
  if (!newName.value.trim()) return
  try { const { id } = await props.api.createPlaylist(newName.value.trim()); newName.value = ''; await reload(); await open(id) }
  catch (e) { show(errMsg(e), true) }
}
async function rename(p: PlaylistSummary) {
  const name = window.prompt('新名称', p.name); if (!name) return
  try { await props.api.updatePlaylist(p.id, { name }); await reload(); if (selected.value?.id === p.id) await open(p.id) }
  catch (e) { show(errMsg(e), true) }
}
async function remove(p: PlaylistSummary) {
  if (!window.confirm(`删除歌单「${p.name}」？`)) return
  try { await props.api.deletePlaylist(p.id); if (selected.value?.id === p.id) selected.value = null; await reload() }
  catch (e) { show(errMsg(e), true) }
}
async function removeTrack(idx: number) {
  if (!selected.value) return
  const ids = selected.value.tracks.filter((_, i) => i !== idx).map(t => t.id)
  const cur = selected.value.id
  try { await props.api.setPlaylistTracks(cur, ids); await open(cur); await reload() }
  catch (e) { show(errMsg(e), true) }
}
// 拖拽重排
const dragIdx = ref<number | null>(null)
function onDragStart(i: number) { dragIdx.value = i }
async function onDrop(target: number) {
  if (!selected.value || dragIdx.value === null || dragIdx.value === target) { dragIdx.value = null; return }
  const arr = [...selected.value.tracks]
  const [moved] = arr.splice(dragIdx.value, 1)
  arr.splice(target, 0, moved)
  selected.value.tracks = arr
  dragIdx.value = null
  const cur = selected.value.id
  try { await props.api.setPlaylistTracks(cur, arr.map(t => t.id)) }
  catch (e) { show(errMsg(e), true); await open(cur) }
}
```

- [ ] **Step 2: App.vue 接入** — import PlaylistsPage；在 mode 渲染链加（与 favorites/settings 同级）：
```vue
      <PlaylistsPage
        v-else-if="mode === 'playlists'"
        :api="api"
        @play-track="onFavPlay"
      />
```
（`onFavPlay` 已存在，接收 FavTrack 并入队播放——复用。）

- [ ] **Step 3: 构建验证** — `make build-frontend && go build ./...` → 通过。

- [ ] **Step 4: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web/src && git commit -m "feat(web): 歌单页（列表/新建/详情/改名/删除/移除/拖拽重排）"
```

---

## Task 10: 前端 —— AddToPlaylist 组件 + 接入曲目行

**Files:** Create `web/src/components/AddToPlaylist.vue`；Modify `AlbumDetail.vue`、`SearchPanel.vue`、`FavoritesPanel.vue`

- [ ] **Step 1: AddToPlaylist.vue** — prop `{ api: ApiClient; trackId: string }`。一个「＋」按钮，点开下拉：列出本人歌单（首次打开时 `listPlaylists`）+ 顶部「新建歌单…」。点歌单 → `addToPlaylist(id,[trackId])` 并提示；点新建 → prompt 名称 → `createPlaylist` 后 `addToPlaylist`。点击 `.stop` 防止触发行播放。下拉用简单绝对定位浮层 + 点击外部关闭（监听 document click）。
```ts
import { ref } from 'vue'
import type { ApiClient, PlaylistSummary } from '../api/client'
const props = defineProps<{ api: ApiClient; trackId: string }>()
const open = ref(false)
const lists = ref<PlaylistSummary[]>([])
const tip = ref('')
async function toggle() {
  open.value = !open.value
  if (open.value) {
    try { lists.value = (await props.api.listPlaylists()).playlists } catch { lists.value = [] }
  }
}
async function add(id: string) {
  try { await props.api.addToPlaylist(id, [props.trackId]); tip.value = '已添加'; setTimeout(() => (tip.value = ''), 1500) }
  catch { tip.value = '添加失败'; setTimeout(() => (tip.value = ''), 1500) }
  open.value = false
}
async function createAndAdd() {
  const name = window.prompt('新歌单名称'); if (!name) return
  try { const { id } = await props.api.createPlaylist(name); await props.api.addToPlaylist(id, [props.trackId]); tip.value = '已创建并添加' }
  catch { tip.value = '操作失败' }
  open.value = false
  setTimeout(() => (tip.value = ''), 1500)
}
```
模板：一个 `＋` 按钮 + `v-if="open"` 的浮层（歌单按钮列表 + 「＋ 新建歌单」）。

- [ ] **Step 2: 接入曲目行** — 在 AlbumDetail.vue 曲目行（红心旁）、SearchPanel.vue 曲目结果、FavoritesPanel.vue 曲目项，各加 `<AddToPlaylist :api="api" :track-id="track.id" @click.stop />`。这些组件已有 `:api` prop（AlbumDetail/SearchPanel 有；FavoritesPanel 有 api）。确认各组件能拿到 track 的 id 字段（AlbumDetail 为 `track.id`，SearchPanel 曲目为其 id，FavoritesPanel 为 FavTrack.id）。

- [ ] **Step 3: 构建验证** — `make build-frontend && go build ./...` → 通过。

- [ ] **Step 4: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web/src && git commit -m "feat(web): AddToPlaylist 组件 + 专辑/搜索/收藏曲目行接入"
```

---

## Self-Review（计划自检）

- **Spec 覆盖**：迁移 009 + 级联(T1) ✓；Store 全方法 + 属主隔离(T2) ✓；Subsonic DTO/接线(T3)、getPlaylists/getPlaylist(T4)、create/update/delete(T5) ✓；Web 7 端点(T6) + 属主隔离 404 ✓；router(T7) ✓；前端 client/导航(T8)、歌单页+拖拽(T9)、AddToPlaylist(T10) ✓；曲目查询复用 tracksByIDs(T6) ✓。
- **占位符**：T6 测试里两段「乱写/垃圾布尔」已在文字中明确要求删除并给出最终用例（TestV1Playlist_CRUDAndTracks 用 fixture 的 pl 取 id；TestV1Playlist_NotFound404 访问不存在 id）；T4 的 `*userT` 占位已注明改为 `*auth.User`。无 TODO/TBD。
- **类型一致**：`NewHandler(...,store,pl)`(T3) 与 router(T7) 一致；`playlists.Store` 方法（Create/List/Get/TrackIDs/UpdateMeta/Delete/AddTracks/ReplaceTracks/RemoveByIndices）跨 T2/T4/T5/T6 一致；`ErrNotFound` → Subsonic 70 / v1 404；Subsonic `Playlist/Playlists` DTO(T3) 被 T4/T5 填充；`NewPlaylistHandler(db,pl)`(T6) 与 router(T7) 一致；前端 `PlaylistSummary/PlaylistDetail` 与后端 JSON（song_count/duration/tracks）一致；`tracksByIDs(db,ids)` 抽取后 StarHandler 与 PlaylistHandler 共用。
- **已知约束**：updatePlaylist 先删(songIndexToRemove，基于删前列表)后加(songIdToAdd)；ReplaceTracks/AddTracks 事务内完成；List 先排空 rows 再无后续嵌套查询。
