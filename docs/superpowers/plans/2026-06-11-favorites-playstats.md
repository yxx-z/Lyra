# 收藏 + 播放统计 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** per-user 收藏（歌曲/专辑/歌手）与播放统计（次数/最近播放），Subsonic 与 Web 两端联动。

**Architecture:** 新增 `starred` / `play_stats` 两表（迁移 008）与共享 `internal/userdata` Store；Subsonic 加 star/unstar/getStarred2、scrobble 改 per-user、getAlbumList2 修正 recent + 新增 frequent/starred、浏览响应标注 starred；Web 加 star/unstar/scrobble/favorites/recently/most-played 端点与 starred 布尔，前端红心 + 收藏面板。

**Tech Stack:** Go 1.25 · modernc.org/sqlite（单连接、foreign_keys=ON）· chi v5 · Vue 3。

**关键约束：**
- 已就绪：`auth.User{ID}`、`middleware.UserFromContext`、Subsonic `withAuth` 已注入 `*auth.User`（`userFromCtx(ctx)`）、`trackSelect`/`scanChild`/`childByID`、v1 albums/search handler。
- Subsonic `Handler` 与 v1 handler 直接查 db；新 Store 供两端复用以避免重复。
- Go 路径：`export PATH=$PATH:/home/yxx/go-local/go/bin`。后端测试用内存 sqlite + httptest，不打网络。
- `internal/api/router.go` 在最后一个后端任务统一接线；中途构造函数签名变更会令 router 暂不编译——属预期，用包级 `go test`/`go vet` 验证。

---

## File Structure

```
internal/db/migrations/008_favorites_playstats.up.sql   新迁移
internal/db/schema.sql                                   改：加 starred / play_stats
internal/userdata/store.go                               新：Store
internal/userdata/store_test.go
internal/api/subsonic/response.go                        改：Child/AlbumID3/ArtistID3 加 Starred
internal/api/subsonic/handler.go                         改：Handler 持 store + NewHandler 增参 + 注册 star/unstar
internal/api/subsonic/handler_test.go                    改：testHandler 传 store
internal/api/subsonic/favorites.go                       新：star/unstar/getStarred2
internal/api/subsonic/favorites_test.go
internal/api/subsonic/media.go                           改：scrobble per-user
internal/api/subsonic/browse.go                          改：getAlbumList2 recent/frequent/starred + starred 注解
internal/api/subsonic/browse_test.go / search_test.go    改/加：注解与列表测试
internal/api/v1/favorites.go                             新：StarHandler（star/unstar/scrobble/favorites/recently/most-played）
internal/api/v1/favorites_test.go
internal/api/v1/albums.go                                改：AlbumSummary/TrackSummary 加 starred + 标注
internal/api/v1/search.go                                改：结果项加 starred
internal/api/router.go                                   改：装配 userdata.Store + 新端点
web/src/api/client.ts                                    改：收藏/统计方法 + DTO starred
web/src/components/FavoritesPanel.vue                    新
web/src/components/*（曲目/专辑）                          改：红心
web/src/App.vue / LibraryShell.vue                       改：面板入口 + 播放器 scrobble
```

---

## Task 1: 迁移 008（starred + play_stats）

**Files:**
- Create: `internal/db/migrations/008_favorites_playstats.up.sql`
- Modify: `internal/db/schema.sql`
- Test: `internal/db/db_test.go`（追加）

- [ ] **Step 1: 写失败测试** — 在 `internal/db/db_test.go` 末尾追加：
```go
func TestOpen_HasFavoritesAndPlayStats(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO tracks(id,title,file_path) VALUES('t1','x','p1')`); err != nil {
		t.Fatalf("seed track: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO starred(user_id,item_type,item_id) VALUES('u1','song','t1')`); err != nil {
		t.Errorf("starred 表应可写: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO play_stats(user_id,track_id,play_count) VALUES('u1','t1',1)`); err != nil {
		t.Errorf("play_stats 表应可写: %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/...` → FAIL（表不存在）。

- [ ] **Step 3: 写迁移** — `internal/db/migrations/008_favorites_playstats.up.sql`:
```sql
-- 收藏（per-user，多态：歌曲/专辑/歌手）
CREATE TABLE starred (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_type  TEXT NOT NULL,
    item_id    TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, item_type, item_id)
);
CREATE INDEX idx_starred_user_type ON starred(user_id, item_type);

-- 播放统计（per-user）
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

- [ ] **Step 4: 同步 schema.sql** — 在 `internal/db/schema.sql` 末尾（app_settings 之后）追加与上面迁移完全相同的两个 `CREATE TABLE` 与三个 `CREATE INDEX`。

- [ ] **Step 5: 运行确认通过** — `go test ./internal/db/...` → PASS（含原有用例）。

- [ ] **Step 6: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/db && git commit -m "feat(db): 迁移 008 starred + play_stats"
```

---

## Task 2: userdata.Store

**Files:**
- Create: `internal/userdata/store.go`, `internal/userdata/store_test.go`

- [ ] **Step 1: 写失败测试** — `internal/userdata/store_test.go`:
```go
package userdata

import (
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func TestStore_StarUnstarIsStarred(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewStore(d)

	if err := s.Star("u1", TypeSong, "t1"); err != nil {
		t.Fatalf("Star: %v", err)
	}
	if err := s.Star("u1", TypeSong, "t1"); err != nil {
		t.Fatalf("重复 Star 应幂等: %v", err)
	}
	ok, _ := s.IsStarred("u1", TypeSong, "t1")
	if !ok {
		t.Error("应已收藏")
	}
	ids, _ := s.StarredIDs("u1", TypeSong)
	if len(ids) != 1 || ids[0] != "t1" {
		t.Errorf("StarredIDs 不符: %v", ids)
	}
	if err := s.Unstar("u1", TypeSong, "t1"); err != nil {
		t.Fatal(err)
	}
	ok, _ = s.IsStarred("u1", TypeSong, "t1")
	if ok {
		t.Error("取消后不应收藏")
	}
}

func TestStore_StarredMapAndIsolation(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewStore(d)
	s.Star("u1", TypeAlbum, "a1")
	s.Star("u1", TypeAlbum, "a2")
	s.Star("u2", TypeAlbum, "a3")

	m, _ := s.StarredMap("u1", TypeAlbum)
	if len(m) != 2 || m["a1"] == "" || m["a2"] == "" {
		t.Errorf("u1 StarredMap 不符: %v", m)
	}
	if _, ok := m["a3"]; ok {
		t.Error("不应含 u2 的收藏（per-user 隔离）")
	}
}

func TestStore_RecordPlayAndOrder(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewStore(d)
	// t1 播 1 次，t2 播 3 次
	s.RecordPlay("u1", "t1")
	for i := 0; i < 3; i++ {
		s.RecordPlay("u1", "t2")
	}
	freq, _ := s.FrequentTrackIDs("u1", 10)
	if len(freq) != 2 || freq[0] != "t2" {
		t.Errorf("FrequentTrackIDs 应 t2 在前: %v", freq)
	}
	recent, _ := s.RecentTrackIDs("u1", 10)
	if len(recent) != 2 {
		t.Errorf("RecentTrackIDs 应有 2 条: %v", recent)
	}
	// 再播 t1，使其成为最近
	s.RecordPlay("u1", "t1")
	recent, _ = s.RecentTrackIDs("u1", 10)
	if recent[0] != "t1" {
		t.Errorf("RecentTrackIDs 应 t1 在前: %v", recent)
	}
}

func TestStore_RecordPlayUpsert(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewStore(d)
	s.RecordPlay("u1", "t1")
	s.RecordPlay("u1", "t1")
	freq, _ := s.FrequentTrackIDs("u1", 10)
	if len(freq) != 1 {
		t.Fatalf("应只有 1 行: %v", freq)
	}
	var cnt int
	d.QueryRow(`SELECT play_count FROM play_stats WHERE user_id='u1' AND track_id='t1'`).Scan(&cnt)
	if cnt != 2 {
		t.Errorf("play_count 应为 2，实际 %d", cnt)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `go test ./internal/userdata/...` → 编译失败（NewStore 未定义）。

- [ ] **Step 3: 实现** — `internal/userdata/store.go`:
```go
// Package userdata 提供 per-user 的收藏与播放统计仓储，供 Subsonic 与 Web 两端复用。
package userdata

import "database/sql"

// 收藏对象类型。
const (
	TypeSong   = "song"
	TypeAlbum  = "album"
	TypeArtist = "artist"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Star(userID, itemType, itemID string) error {
	_, err := s.db.Exec(
		`INSERT INTO starred(user_id, item_type, item_id) VALUES(?,?,?) ON CONFLICT DO NOTHING`,
		userID, itemType, itemID,
	)
	return err
}

func (s *Store) Unstar(userID, itemType, itemID string) error {
	_, err := s.db.Exec(
		`DELETE FROM starred WHERE user_id=? AND item_type=? AND item_id=?`,
		userID, itemType, itemID,
	)
	return err
}

func (s *Store) IsStarred(userID, itemType, itemID string) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM starred WHERE user_id=? AND item_type=? AND item_id=?`,
		userID, itemType, itemID,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// StarredIDs 按收藏时间倒序返回该类型已收藏的 id。
func (s *Store) StarredIDs(userID, itemType string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT item_id FROM starred WHERE user_id=? AND item_type=? ORDER BY created_at DESC, item_id`,
		userID, itemType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// StarredMap 返回 id→收藏时间（字符串）的映射，用于批量标注列表；存在即表示已收藏。
func (s *Store) StarredMap(userID, itemType string) (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT item_id, created_at FROM starred WHERE user_id=? AND item_type=?`,
		userID, itemType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var id, ts string
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, err
		}
		m[id] = ts
	}
	return m, rows.Err()
}

// RecordPlay 记一次播放（upsert：次数 +1，更新最近播放时间）。
func (s *Store) RecordPlay(userID, trackID string) error {
	_, err := s.db.Exec(`
		INSERT INTO play_stats(user_id, track_id, play_count, last_played_at)
		VALUES(?, ?, 1, datetime('now'))
		ON CONFLICT(user_id, track_id) DO UPDATE SET
			play_count = play_count + 1,
			last_played_at = datetime('now')`,
		userID, trackID,
	)
	return err
}

func (s *Store) trackIDsBy(userID, orderBy string, limit int) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT track_id FROM play_stats WHERE user_id=? AND `+orderClause(orderBy)+` LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RecentTrackIDs 按最近播放倒序（仅含有播放时间的行）。
func (s *Store) RecentTrackIDs(userID string, limit int) ([]string, error) {
	return s.trackIDsBy(userID, "recent", limit)
}

// FrequentTrackIDs 按播放次数倒序。
func (s *Store) FrequentTrackIDs(userID string, limit int) ([]string, error) {
	return s.trackIDsBy(userID, "frequent", limit)
}

// orderClause 把内部排序标识映射为安全的 WHERE/ORDER 片段（不接受外部原文，杜绝注入）。
func orderClause(kind string) string {
	switch kind {
	case "frequent":
		return `1=1 ORDER BY play_count DESC, last_played_at DESC`
	default: // recent
		return `last_played_at IS NOT NULL ORDER BY last_played_at DESC`
	}
}
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/userdata/...` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/userdata && git commit -m "feat(userdata): 收藏 + 播放统计 Store"
```

---

## Task 3: Subsonic 接线地基（DTO Starred 字段 + Handler 持 store）

> 仅做编译地基，不改行为。`router.go` 会暂不编译（Task 10 修）；用 `go vet ./internal/api/subsonic/` 验证。

**Files:**
- Modify: `internal/api/subsonic/response.go`、`internal/api/subsonic/handler.go`、`internal/api/subsonic/handler_test.go`

- [ ] **Step 1: response.go 加 Starred 字段** — 在 `Child`、`AlbumID3`、`ArtistID3` 三个 struct 末尾各加一个字段（放在最后一个字段之后）：
```go
	Starred string `xml:"starred,attr,omitempty" json:"starred,omitempty"`
```
（三处分别添加。）

- [ ] **Step 2: handler.go 持 store + 增参 + 注册** — 加 import `"github.com/yxx-z/lyra/internal/userdata"`；`Handler` struct 增字段 `store *userdata.Store`；`NewHandler` 改签名（在 `key []byte` 之后加 `store *userdata.Store`）并存入：
```go
func NewHandler(db *sql.DB, cfg *config.Config, stream *v1.StreamHandler, cover *v1.CoverHandler, users *auth.UserStore, key []byte, store *userdata.Store) *Handler {
	return &Handler{db: db, cfg: cfg, streamH: stream, cover: cover, users: users, key: key, store: store}
}
```
在 `RegisterRoutes` 中（getStarred2 注册行附近）新增：
```go
	h.reg(r, "star", h.star)
	h.reg(r, "unstar", h.unstar)
```
（`getStarred2` 已注册，保持；其真实现在 Task 5 替换 stub。）

- [ ] **Step 3: handler_test.go 的 testHandler 传 store** — 在 `testHandler` 里 `tcache`/`tsvc` 附近构造 store 并传入 NewHandler：
```go
	store := userdata.NewStore(d)
```
并把 `return NewHandler(d, cfg, stream, cover, users, key)` 改为 `return NewHandler(d, cfg, stream, cover, users, key, store)`。加 import `"github.com/yxx-z/lyra/internal/userdata"`。

- [ ] **Step 4: 占位 star/unstar 以便编译** — 在 handler.go 末尾临时加最小桩（Task 4 用真实现替换）：
```go
func (h *Handler) star(w http.ResponseWriter, r *http.Request)   { writeResponse(w, r, &Response{}) }
func (h *Handler) unstar(w http.ResponseWriter, r *http.Request) { writeResponse(w, r, &Response{}) }
```

- [ ] **Step 5: 验证** — `go vet ./internal/api/subsonic/ && go test ./internal/api/subsonic/...` → 通过（行为未变，原测试仍绿）。

- [ ] **Step 6: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/subsonic && git commit -m "feat(subsonic): DTO Starred 字段 + Handler 持 userdata.Store + star/unstar 桩"
```

---

## Task 4: Subsonic star / unstar

**Files:**
- Create: `internal/api/subsonic/favorites.go`
- Modify: `internal/api/subsonic/handler.go`（移除 Task 3 的 star/unstar 桩）
- Test: `internal/api/subsonic/favorites_test.go`

- [ ] **Step 1: 写失败测试** — `internal/api/subsonic/favorites_test.go`:
```go
package subsonic

import (
	"strings"
	"testing"
)

func TestStarUnstar_Song(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	doReq(t, h, "/rest/star?u=admin&p=secret&id=t1&f=json")
	// 验证 DB 落库
	var n int
	h.db.QueryRow(`SELECT COUNT(*) FROM starred WHERE item_type='song' AND item_id='t1'`).Scan(&n)
	if n != 1 {
		t.Fatalf("star 后应有 1 行，实际 %d", n)
	}
	doReq(t, h, "/rest/unstar?u=admin&p=secret&id=t1&f=json")
	h.db.QueryRow(`SELECT COUNT(*) FROM starred WHERE item_type='song' AND item_id='t1'`).Scan(&n)
	if n != 0 {
		t.Errorf("unstar 后应为 0，实际 %d", n)
	}
}

func TestStar_AlbumAndArtist(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	doReq(t, h, "/rest/star?u=admin&p=secret&albumId=al1&artistId=ar1&f=json")
	var albums, artists int
	h.db.QueryRow(`SELECT COUNT(*) FROM starred WHERE item_type='album' AND item_id='al1'`).Scan(&albums)
	h.db.QueryRow(`SELECT COUNT(*) FROM starred WHERE item_type='artist' AND item_id='ar1'`).Scan(&artists)
	if albums != 1 || artists != 1 {
		t.Errorf("专辑/歌手收藏应各 1：albums=%d artists=%d", albums, artists)
	}
}
```
> `seed` 助手（已存在于 subsonic 测试包）会建 `al1`/`ar1`/`t1` 等；若 seed 的专辑/歌手 id 不是 `al1`/`ar1`，star 仍会无条件落库（star 不校验对象存在），断言只查 starred 表，故与 seed 的具体 id 无关——但 `id=t1` 用的是 seed 的曲目 id，请确认 seed 建了 `t1`（现有书签测试已依赖 `t1`，成立）。专辑/歌手用任意字符串 `al1`/`ar1` 即可。

- [ ] **Step 2: 运行确认失败** — `go test ./internal/api/subsonic/ -run StarUnstar` → 失败（桩不落库）。

- [ ] **Step 3: 实现** — 先从 `handler.go` 末尾**删除 Task 3 的 star/unstar 桩两行**。再创建 `internal/api/subsonic/favorites.go`:
```go
package subsonic

import (
	"net/http"

	"github.com/yxx-z/lyra/internal/userdata"
)

// star 处理 star：id（歌曲，多值）、albumId（多值）、artistId（多值）。
func (h *Handler) star(w http.ResponseWriter, r *http.Request) {
	h.setStar(w, r, true)
}

func (h *Handler) unstar(w http.ResponseWriter, r *http.Request) {
	h.setStar(w, r, false)
}

func (h *Handler) setStar(w http.ResponseWriter, r *http.Request, on bool) {
	u := userFromCtx(r.Context())
	apply := func(itemType string, ids []string) {
		for _, id := range ids {
			if id == "" {
				continue
			}
			if on {
				_ = h.store.Star(u.ID, itemType, id)
			} else {
				_ = h.store.Unstar(u.ID, itemType, id)
			}
		}
	}
	apply(userdata.TypeSong, r.Form["id"])
	apply(userdata.TypeAlbum, r.Form["albumId"])
	apply(userdata.TypeArtist, r.Form["artistId"])
	writeResponse(w, r, &Response{})
}
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/subsonic/...` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/subsonic && git commit -m "feat(subsonic): star/unstar 真实现（歌曲/专辑/歌手）"
```

---

## Task 5: Subsonic getStarred2

**Files:**
- Modify: `internal/api/subsonic/stubs.go`（移除 getStarred2 桩）、`internal/api/subsonic/favorites.go`（加真实现）
- Test: `internal/api/subsonic/favorites_test.go`（追加）

- [ ] **Step 1: 写失败测试** — 在 `favorites_test.go` 追加：
```go
func TestGetStarred2(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	doReq(t, h, "/rest/star?u=admin&p=secret&id=t1&f=json")
	w := doReq(t, h, "/rest/getStarred2?u=admin&p=secret&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"starred2"`) || !strings.Contains(b, "以父之名") {
		t.Errorf("getStarred2 应含已收藏歌曲: %s", b)
	}
}

func TestGetStarred2_PerUserIsolation(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// admin 收藏 t1
	doReq(t, h, "/rest/star?u=admin&p=secret&id=t1&f=json")
	// 另建用户 bob（Subsonic 密码 bobpw）
	hash, _ := authHashForTest(t)
	bob, _ := h.users.Create("bob", hash, false)
	encBob, _ := encForTest(h, "bobpw")
	h.users.UpdateSubsonicPW(bob.ID, encBob)
	w := doReq(t, h, "/rest/getStarred2?u=bob&p=bobpw&f=json")
	if strings.Contains(w.Body.String(), "以父之名") {
		t.Errorf("bob 不应看到 admin 的收藏: %s", w.Body.String())
	}
}
```
> 该测试需要两个小助手把「建用户 + 设 Subsonic 密码」写顺。请在 `favorites_test.go` 顶部加：
```go
import "github.com/yxx-z/lyra/internal/auth"

func authHashForTest(t *testing.T) (string, error) { return auth.HashPassword("loginpw") }
func encForTest(h *Handler, pw string) ([]byte, error) { return auth.Encrypt(h.key, pw) }
```
（`h.key`、`h.users` 同包可访问。）

- [ ] **Step 2: 运行确认失败** — `go test ./internal/api/subsonic/ -run GetStarred2` → 失败（stub 返回空）。

- [ ] **Step 3: 实现** — 从 `internal/api/subsonic/stubs.go` 删除 `getStarred2` 函数（保留 `getGenres`）。在 `favorites.go` 追加：
```go
// getStarred2 返回当前用户收藏的歌曲/专辑/歌手。
func (h *Handler) getStarred2(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	res := &Starred2{}

	songIDs, _ := h.store.StarredIDs(u.ID, userdata.TypeSong)
	for _, id := range songIDs {
		if c, ok := h.childByID(id); ok {
			c.Starred = "starred"
			res.Song = append(res.Song, c)
		}
	}
	albumIDs, _ := h.store.StarredIDs(u.ID, userdata.TypeAlbum)
	for _, id := range albumIDs {
		if al, ok := h.albumSummaryByID(id); ok {
			al.Starred = "starred"
			res.Album = append(res.Album, al)
		}
	}
	artistIDs, _ := h.store.StarredIDs(u.ID, userdata.TypeArtist)
	for _, id := range artistIDs {
		if ar, ok := h.artistSummaryByID(id); ok {
			ar.Starred = "starred"
			res.Artist = append(res.Artist, ar)
		}
	}
	writeResponse(w, r, &Response{Starred2: res})
}

// albumSummaryByID 构造一个不含曲目的 AlbumID3（用于 starred 列表）。
func (h *Handler) albumSummaryByID(id string) (AlbumID3, bool) {
	var al AlbumID3
	var date, genre, artistID string
	err := h.db.QueryRow(`
		SELECT al.id, al.title, COALESCE(al.artist_id,''), COALESCE(ar.name,''),
		       COALESCE(al.release_date,''), COALESCE(al.genre,''),
		       (SELECT COUNT(*) FROM tracks WHERE album_id=al.id AND is_available=1),
		       (SELECT COALESCE(SUM(duration),0) FROM tracks WHERE album_id=al.id AND is_available=1)
		FROM albums al LEFT JOIN artists ar ON ar.id=al.artist_id WHERE al.id=?`, id).
		Scan(&al.ID, &al.Name, &artistID, &al.Artist, &date, &genre, &al.SongCount, &al.Duration)
	if err != nil {
		return AlbumID3{}, false
	}
	al.ArtistID = artistID
	al.CoverArt = al.ID
	al.Year = yearFromDate(date)
	al.Genre = genre
	return al, true
}

// artistSummaryByID 构造一个不含专辑的 ArtistID3。
func (h *Handler) artistSummaryByID(id string) (ArtistID3, bool) {
	var ar ArtistID3
	err := h.db.QueryRow(`
		SELECT ar.id, ar.name, (SELECT COUNT(*) FROM albums WHERE artist_id=ar.id)
		FROM artists ar WHERE ar.id=?`, id).Scan(&ar.ID, &ar.Name, &ar.AlbumCount)
	if err != nil {
		return ArtistID3{}, false
	}
	return ar, true
}
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/subsonic/...` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/subsonic && git commit -m "feat(subsonic): getStarred2 真实现 + 专辑/歌手摘要构建"
```

---

## Task 6: Subsonic scrobble per-user + getAlbumList2 recent/frequent/starred

**Files:**
- Modify: `internal/api/subsonic/media.go`、`internal/api/subsonic/browse.go`
- Test: `internal/api/subsonic/browse_test.go`（追加）

- [ ] **Step 1: 写失败测试** — 在 `internal/api/subsonic/browse_test.go` 追加（若该文件不存在则创建，package subsonic）：
```go
func TestScrobble_PerUser(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	doReq(t, h, "/rest/scrobble?u=admin&p=secret&id=t1&f=json")
	var cnt int
	var uid string
	h.db.QueryRow(`SELECT play_count, user_id FROM play_stats WHERE track_id='t1'`).Scan(&cnt, &uid)
	if cnt != 1 || uid == "" {
		t.Errorf("scrobble 应记入 play_stats: cnt=%d uid=%q", cnt, uid)
	}
}

func TestGetAlbumList2_Frequent(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// seed 至少有曲目 t1（属某专辑）。多次 scrobble t1 使其专辑成为 frequent。
	for i := 0; i < 3; i++ {
		doReq(t, h, "/rest/scrobble?u=admin&p=secret&id=t1&f=json")
	}
	w := doReq(t, h, "/rest/getAlbumList2?u=admin&p=secret&type=frequent&f=json")
	if !strings.Contains(w.Body.String(), `"albumList2"`) {
		t.Errorf("frequent 应返回 albumList2: %s", w.Body.String())
	}
}

func TestGetAlbumList2_Starred(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// 取 t1 的专辑 id 并收藏它
	var albumID string
	h.db.QueryRow(`SELECT COALESCE(album_id,'') FROM tracks WHERE id='t1'`).Scan(&albumID)
	if albumID != "" {
		doReq(t, h, "/rest/star?u=admin&p=secret&albumId="+albumID+"&f=json")
		w := doReq(t, h, "/rest/getAlbumList2?u=admin&p=secret&type=starred&f=json")
		if !strings.Contains(w.Body.String(), albumID) {
			t.Errorf("starred 列表应含已收藏专辑 %s: %s", albumID, w.Body.String())
		}
	}
}
```
> 需要 `import "strings"`（若文件已 import 则不重复）。

- [ ] **Step 2: 运行确认失败** — `go test ./internal/api/subsonic/ -run 'Scrobble_PerUser|AlbumList2_Frequent|AlbumList2_Starred'` → 失败。

- [ ] **Step 3a: 改 scrobble**（`internal/api/subsonic/media.go`）— 把 `scrobble` 改为：
```go
func (h *Handler) scrobble(w http.ResponseWriter, r *http.Request) {
	if id := r.Form.Get("id"); id != "" {
		if u := userFromCtx(r.Context()); u != nil {
			_ = h.store.RecordPlay(u.ID, id)
		}
	}
	writeResponse(w, r, &Response{})
}
```

- [ ] **Step 3b: 改 getAlbumList2**（`internal/api/subsonic/browse.go`）— 把 `getAlbumList2` 整个函数替换为下面版本（支持 recent/frequent/starred 的 per-user join，其余不变）：
```go
func (h *Handler) getAlbumList2(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	typ := r.Form.Get("type")
	size := atoiDefault(r.Form.Get("size"), 10)
	if size > 500 {
		size = 500
	}
	offset := atoiDefault(r.Form.Get("offset"), 0)

	// 公共 SELECT 前缀（带聚合曲目数/时长）
	const base = `
		SELECT al.id, al.title, COALESCE(al.artist_id,''), COALESCE(ar.name,''),
		       COALESCE(al.release_date,''), COALESCE(al.genre,''),
		       (SELECT COUNT(*) FROM tracks WHERE album_id=al.id AND is_available=1),
		       (SELECT COALESCE(SUM(duration),0) FROM tracks WHERE album_id=al.id AND is_available=1)
		FROM albums al LEFT JOIN artists ar ON ar.id=al.artist_id `

	var query string
	var args []any
	switch typ {
	case "newest":
		query = base + `ORDER BY al.created_at DESC LIMIT ? OFFSET ?`
		args = []any{size, offset}
	case "alphabeticalByName", "":
		query = base + `ORDER BY al.title LIMIT ? OFFSET ?`
		args = []any{size, offset}
	case "random":
		query = base + `ORDER BY RANDOM() LIMIT ? OFFSET ?`
		args = []any{size, offset}
	case "recent":
		// 本人最近播放：经曲目关联 play_stats，取每张专辑的最近播放时间
		query = base + `JOIN (
			SELECT t.album_id AS aid, MAX(ps.last_played_at) AS lp
			FROM play_stats ps JOIN tracks t ON t.id=ps.track_id
			WHERE ps.user_id=? AND ps.last_played_at IS NOT NULL AND t.album_id IS NOT NULL
			GROUP BY t.album_id
		) r ON r.aid=al.id ORDER BY r.lp DESC LIMIT ? OFFSET ?`
		args = []any{userID(u), size, offset}
	case "frequent":
		query = base + `JOIN (
			SELECT t.album_id AS aid, SUM(ps.play_count) AS pc
			FROM play_stats ps JOIN tracks t ON t.id=ps.track_id
			WHERE ps.user_id=? AND t.album_id IS NOT NULL
			GROUP BY t.album_id
		) f ON f.aid=al.id ORDER BY f.pc DESC LIMIT ? OFFSET ?`
		args = []any{userID(u), size, offset}
	case "starred":
		query = base + `JOIN starred s ON s.item_id=al.id AND s.item_type='album' AND s.user_id=?
			ORDER BY s.created_at DESC LIMIT ? OFFSET ?`
		args = []any{userID(u), size, offset}
	default:
		writeError(w, r, 10, "不支持的 type")
		return
	}

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	defer rows.Close()

	var starredMap map[string]string
	if u != nil {
		starredMap, _ = h.store.StarredMap(u.ID, userdata.TypeAlbum)
	}
	list := &AlbumList2{}
	for rows.Next() {
		var al AlbumID3
		var date, genre string
		if err := rows.Scan(&al.ID, &al.Name, &al.ArtistID, &al.Artist, &date, &genre, &al.SongCount, &al.Duration); err != nil {
			continue
		}
		al.CoverArt = al.ID
		al.Year = yearFromDate(date)
		al.Genre = genre
		if ts, ok := starredMap[al.ID]; ok {
			al.Starred = ts
		}
		list.Album = append(list.Album, al)
	}
	writeResponse(w, r, &Response{AlbumList2: list})
}

// userID 安全取用户 id（u 可能为 nil，例如 auth.disable 且无管理员的极端情况）。
func userID(u *auth.User) string {
	if u == nil {
		return ""
	}
	return u.ID
}
```
> 需要 `internal/api/subsonic/browse.go` 增 import `"github.com/yxx-z/lyra/internal/auth"` 与 `"github.com/yxx-z/lyra/internal/userdata"`。若 `auth` 已被其它文件 import 但本文件未 import，则在本文件 import 块补上。

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/subsonic/...` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/subsonic && git commit -m "feat(subsonic): scrobble per-user + getAlbumList2 recent/frequent/starred"
```

---

## Task 7: Subsonic 浏览响应 starred 注解（getAlbum/getSong/search3）

**Files:**
- Modify: `internal/api/subsonic/browse.go`（getAlbum、getSong）、`internal/api/subsonic/search.go`（search3）
- Test: `internal/api/subsonic/favorites_test.go`（追加）

- [ ] **Step 1: 写失败测试** — 在 `favorites_test.go` 追加：
```go
func TestGetAlbum_StarredAnnotation(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	doReq(t, h, "/rest/star?u=admin&p=secret&id=t1&f=json")
	w := doReq(t, h, "/rest/getSong?u=admin&p=secret&id=t1&f=json")
	if !strings.Contains(w.Body.String(), `"starred"`) {
		t.Errorf("已收藏歌曲 getSong 应含 starred 属性: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: 运行确认失败** — `go test ./internal/api/subsonic/ -run GetAlbum_StarredAnnotation` → 失败。

- [ ] **Step 3: 实现** — 加一个公共助手（放 `favorites.go`）：
```go
// annotateSongs 用当前用户的歌曲收藏批量标注 Child.Starred。
func (h *Handler) annotateSongs(u *auth.User, songs []Child) {
	if u == nil || len(songs) == 0 {
		return
	}
	m, err := h.store.StarredMap(u.ID, userdata.TypeSong)
	if err != nil || len(m) == 0 {
		return
	}
	for i := range songs {
		if ts, ok := m[songs[i].ID]; ok {
			songs[i].Starred = ts
		}
	}
}
```
（`favorites.go` 需 import `"github.com/yxx-z/lyra/internal/auth"`；`userdata` 已 import。）

在 **getSong**（browse.go）`writeResponse` 前插入：
```go
	songs := []Child{c}
	h.annotateSongs(userFromCtx(r.Context()), songs)
	c = songs[0]
```
在 **getAlbum**（browse.go）构造完 `al.Song` 后、`writeResponse` 前插入：
```go
	h.annotateSongs(userFromCtx(r.Context()), al.Song)
```
在 **search3**（search.go）构造完歌曲结果切片后、写响应前，对其歌曲切片调用 `h.annotateSongs(userFromCtx(r.Context()), <歌曲切片>)`（先读 search.go 确认歌曲切片变量名，例如 `result.Song`）。

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/subsonic/...` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/subsonic && git commit -m "feat(subsonic): getAlbum/getSong/search3 标注 starred"
```

---

## Task 8: Web star/unstar/scrobble/favorites/recently/most-played

**Files:**
- Create: `internal/api/v1/favorites.go`, `internal/api/v1/favorites_test.go`

> 复用 v1 `writeJSON`/`writeJSONError`、`middleware.UserFromContext`。`router.go` 在 Task 10 接线；用 `go test ./internal/api/v1/ -run Fav` + `go vet` 验证。曲目/专辑 DTO（`TrackSummary`/`AlbumSummary`，snake_case）在 albums.go，本任务的 favorites/recently 返回复用它们——实现时先 `sed -n` 读 albums.go 的 DTO 与查询，照搬列与 `stream_url`/`cover_url` 构造方式。

- [ ] **Step 1: 写失败测试** — `internal/api/v1/favorites_test.go`:
```go
package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/userdata"
)

func favFixture(t *testing.T) (*StarHandler, *auth.UserStore, *auth.SessionStore, *auth.User, *userdata.Store, interface {
	Exec(string, ...any) (interface{ LastInsertId() (int64, error) }, error)
}) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	store := userdata.NewStore(d)
	u, _ := us.Create("admin", mustHashFav(t, "pw"), true)
	d.Exec(`INSERT INTO albums(id,title) VALUES('al1','专辑')`)
	d.Exec(`INSERT INTO tracks(id,title,album_id,file_path) VALUES('t1','歌一','al1','p1')`)
	return NewStarHandler(d, store), us, ss, u, store, nil
}

func mustHashFav(t *testing.T, pw string) string {
	t.Helper()
	h, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func favReq(t *testing.T, fn http.HandlerFunc, ss *auth.SessionStore, us *auth.UserStore, u *auth.User, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, _ := ss.Create(u.ID, time.Hour)
	handler := middleware.SessionAuth(ss, us, false)(fn)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestV1_StarAndFavorites(t *testing.T) {
	h, us, ss, u, _, _ := favFixture(t)
	w := favReq(t, h.Star, ss, us, u, "POST", "/star", `{"type":"song","id":"t1"}`)
	if w.Code != 200 {
		t.Fatalf("star 应成功: %d %s", w.Code, w.Body.String())
	}
	w = favReq(t, h.Favorites, ss, us, u, "GET", "/favorites", "")
	if !strings.Contains(w.Body.String(), "歌一") {
		t.Errorf("favorites 应含已收藏歌曲: %s", w.Body.String())
	}
}

func TestV1_ScrobbleAndRecent(t *testing.T) {
	h, us, ss, u, _, _ := favFixture(t)
	if favReq(t, h.Scrobble, ss, us, u, "POST", "/tracks/t1/scrobble", "").Code != 200 {
		// Scrobble 用 chi URLParam 取 id；测试需经 chi 路由，见下方说明
	}
}
```
> `TestV1_ScrobbleAndRecent` 里 `Scrobble` 依赖 `chi.URLParam(r,"id")`，必须经 chi 路由注入 URL 参数。请把该测试改为构造一个 chi 路由：
```go
func TestV1_ScrobbleAndRecent(t *testing.T) {
	h, us, ss, u, store, _ := favFixture(t)
	token, _ := ss.Create(u.ID, time.Hour)
	r := chi.NewRouter()
	r.Use(middleware.SessionAuth(ss, us, false))
	r.Post("/tracks/{id}/scrobble", h.Scrobble)
	r.Get("/recently-played", h.RecentlyPlayed)
	req := httptest.NewRequest("POST", "/tracks/t1/scrobble", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("scrobble 应成功: %d", w.Code)
	}
	ids, _ := store.RecentTrackIDs(u.ID, 10)
	if len(ids) != 1 || ids[0] != "t1" {
		t.Errorf("应记录最近播放 t1: %v", ids)
	}
	req = httptest.NewRequest("GET", "/recently-played", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "歌一") {
		t.Errorf("recently-played 应含 t1: %s", w.Body.String())
	}
}
```
并加 import `"github.com/go-chi/chi/v5"`。同时把 `favFixture` 第六返回值那个奇怪的 interface 删掉——最终 `favFixture` 返回 `(*StarHandler, *auth.UserStore, *auth.SessionStore, *auth.User, *userdata.Store)` 即可，相应调整调用处解构。

- [ ] **Step 2: 运行确认失败** — `go vet ./internal/api/v1/` → 失败（NewStarHandler 未定义）。

- [ ] **Step 3: 实现** — `internal/api/v1/favorites.go`:
```go
// internal/api/v1/favorites.go
package v1

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/userdata"
)

// StarHandler 处理 Web 端收藏与播放统计。
type StarHandler struct {
	db    *sql.DB
	store *userdata.Store
}

func NewStarHandler(db *sql.DB, store *userdata.Store) *StarHandler {
	return &StarHandler{db: db, store: store}
}

var validStarType = map[string]bool{
	userdata.TypeSong: true, userdata.TypeAlbum: true, userdata.TypeArtist: true,
}

func (h *StarHandler) Star(w http.ResponseWriter, r *http.Request)   { h.setStar(w, r, true) }
func (h *StarHandler) Unstar(w http.ResponseWriter, r *http.Request) { h.setStar(w, r, false) }

func (h *StarHandler) setStar(w http.ResponseWriter, r *http.Request, on bool) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if !validStarType[req.Type] || req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "type 或 id 非法")
		return
	}
	var err error
	if on {
		err = h.store.Star(u.ID, req.Type, req.ID)
	} else {
		err = h.store.Unstar(u.ID, req.Type, req.ID)
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "操作失败")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// Scrobble 处理 POST /api/v1/tracks/{id}/scrobble。
func (h *StarHandler) Scrobble(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "缺少 id")
		return
	}
	if err := h.store.RecordPlay(u.ID, id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "记录失败")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// favTrack 是收藏/最近播放列表里的曲目项。
type favTrack struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Album     string `json:"album"`
	AlbumID   string `json:"album_id"`
	Artist    string `json:"artist"`
	Duration  int    `json:"duration"`
	StreamURL string `json:"stream_url"`
	CoverURL  string `json:"cover_url"`
}

func (h *StarHandler) queryTracks(ids []string) []favTrack {
	out := []favTrack{}
	for _, id := range ids {
		var ft favTrack
		err := h.db.QueryRow(`
			SELECT tr.id, tr.title, COALESCE(al.title,''), COALESCE(tr.album_id,''),
			       COALESCE(ar.name,''), COALESCE(tr.duration,0)
			FROM tracks tr
			LEFT JOIN albums al ON al.id=tr.album_id
			LEFT JOIN artists ar ON ar.id=tr.artist_id
			WHERE tr.id=? AND tr.is_available=1`, id).
			Scan(&ft.ID, &ft.Title, &ft.Album, &ft.AlbumID, &ft.Artist, &ft.Duration)
		if err != nil {
			continue
		}
		ft.StreamURL = "/api/v1/tracks/" + ft.ID + "/stream"
		ft.CoverURL = "/api/v1/cover/" + ft.AlbumID
		out = append(out, ft)
	}
	return out
}

// Favorites 处理 GET /api/v1/favorites：当前用户收藏的歌曲与专辑。
func (h *StarHandler) Favorites(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	songIDs, _ := h.store.StarredIDs(u.ID, userdata.TypeSong)
	albumIDs, _ := h.store.StarredIDs(u.ID, userdata.TypeAlbum)
	albums := []favAlbum{}
	for _, id := range albumIDs {
		var fa favAlbum
		err := h.db.QueryRow(`
			SELECT al.id, al.title, COALESCE(ar.name,'')
			FROM albums al LEFT JOIN artists ar ON ar.id=al.artist_id WHERE al.id=?`, id).
			Scan(&fa.ID, &fa.Title, &fa.Artist)
		if err != nil {
			continue
		}
		fa.CoverURL = "/api/v1/cover/" + fa.ID
		albums = append(albums, fa)
	}
	writeJSON(w, map[string]any{"tracks": h.queryTracks(songIDs), "albums": albums})
}

type favAlbum struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	CoverURL string `json:"cover_url"`
}

// RecentlyPlayed 处理 GET /api/v1/recently-played。
func (h *StarHandler) RecentlyPlayed(w http.ResponseWriter, r *http.Request) {
	h.playList(w, r, false)
}

// MostPlayed 处理 GET /api/v1/most-played。
func (h *StarHandler) MostPlayed(w http.ResponseWriter, r *http.Request) {
	h.playList(w, r, true)
}

func (h *StarHandler) playList(w http.ResponseWriter, r *http.Request, frequent bool) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	var ids []string
	if frequent {
		ids, _ = h.store.FrequentTrackIDs(u.ID, 50)
	} else {
		ids, _ = h.store.RecentTrackIDs(u.ID, 50)
	}
	writeJSON(w, map[string]any{"tracks": h.queryTracks(ids)})
}
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/v1/ -run 'V1_Star|V1_Scrobble' -v && go vet ./internal/api/v1/` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/favorites.go internal/api/v1/favorites_test.go && git commit -m "feat(api): Web 收藏/播放统计端点（star/unstar/scrobble/favorites/recently/most-played）"
```

---

## Task 9: Web albums/search 响应补 starred 布尔

**Files:**
- Modify: `internal/api/v1/albums.go`、`internal/api/v1/search.go`、`internal/api/router.go`（仅 AlbumsHandler/SearchHandler 构造增 store —— 与 Task 10 协调；本任务先改 DTO + 标注逻辑，构造签名变更）
- Test: `internal/api/v1/albums_test.go`（追加）

> 本任务给 `AlbumsHandler`/`SearchHandler` 注入 `*userdata.Store` 以标注 `starred`。这会改其构造签名，router 暂不编译——Task 10 统一接线。用 `go test ./internal/api/v1/ -run Album` + `go vet` 验证（注意：vet 整个 v1 包，会因 router 不在本包而通过）。

- [ ] **Step 1: 读现状** — `sed -n '1,140p' internal/api/v1/albums.go` 与 `search.go`，确认 `AlbumSummary`/`TrackSummary`/搜索结果 DTO 与 `NewAlbumsHandler`/`NewSearchHandler` 签名、ListAlbums/GetAlbum/Search 的构造流程。

- [ ] **Step 2: 写失败测试** — 在 `internal/api/v1/albums_test.go` 追加（依据现有该文件的测试夹具风格；若无则参考下例自建夹具）：
```go
func TestAlbums_StarredFlag(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO albums(id,title) VALUES('al1','专辑')`)
	store := userdata.NewStore(d)
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	u, _ := us.Create("admin", mustHashFav(t, "pw"), true)
	store.Star(u.ID, userdata.TypeAlbum, "al1")

	h := NewAlbumsHandler(d, store)
	token, _ := ss.Create(u.ID, time.Hour)
	r := chi.NewRouter()
	r.Use(middleware.SessionAuth(ss, us, false))
	r.Get("/albums", h.ListAlbums)
	req := httptest.NewRequest("GET", "/albums", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `"starred":true`) {
		t.Errorf("已收藏专辑应含 starred:true: %s", w.Body.String())
	}
}
```
（import：net/http、net/http/httptest、strings、testing、time、chi、middleware、auth、db、userdata，按需补。）

- [ ] **Step 3: 实现**
  - `AlbumSummary` 增字段 `Starred bool \`json:"starred"\``；`TrackSummary` 增 `Starred bool \`json:"starred"\``。
  - `AlbumsHandler` 增字段 `store *userdata.Store`；`NewAlbumsHandler(db *sql.DB, store *userdata.Store)`。
  - `ListAlbums`/`GetAlbum`：取 `middleware.UserFromContext`，若有用户则 `albumMap,_ := h.store.StarredMap(u.ID, userdata.TypeAlbum)`、`songMap,_ := h.store.StarredMap(u.ID, userdata.TypeSong)`，对返回的专辑项 `a.Starred = albumMap 命中`、对曲目项 `t.Starred = songMap 命中`（`_, ok := m[id]`）。
  - `SearchHandler` 同样增 `store` 与构造参数；其曲目/专辑结果项标注 `starred`。
  - import `"github.com/yxx-z/lyra/internal/userdata"` 与（若未引入）`"github.com/yxx-z/lyra/internal/api/middleware"`。

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/v1/ -run 'Album|Search' -v && go vet ./internal/api/v1/` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/albums.go internal/api/v1/search.go internal/api/v1/albums_test.go && git commit -m "feat(api): Web albums/search 响应标注 starred"
```

---

## Task 10: router 装配 + 全量编译

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: 接线** — 在 `internal/api/router.go`：
  - import `"github.com/yxx-z/lyra/internal/userdata"`。
  - 在 stores 初始化区加 `udStore := userdata.NewStore(db)`。
  - `subsonic.NewHandler(...)` 调用末尾增参 `udStore`（与 Task 3 的新签名一致：`subsonic.NewHandler(db, cfg, streamH, subCover, users, key, udStore)`）。
  - `v1.NewAlbumsHandler(db)` → `v1.NewAlbumsHandler(db, udStore)`；`v1.NewSearchHandler(db)` → `v1.NewSearchHandler(db, udStore)`。
  - 新建 `starH := v1.NewStarHandler(db, udStore)`，并在 `/api/v1` 鉴权组内注册：
```go
		r.Post("/star", starH.Star)
		r.Post("/unstar", starH.Unstar)
		r.Post("/tracks/{id}/scrobble", starH.Scrobble)
		r.Get("/favorites", starH.Favorites)
		r.Get("/recently-played", starH.RecentlyPlayed)
		r.Get("/most-played", starH.MostPlayed)
```
  - 确认 `r.Get("/tracks/{id}/stream", ...)` 等既有路由不与 `/tracks/{id}/scrobble` 冲突（chi 允许不同方法/子路径，OK）。

- [ ] **Step 2: 全量编译 + 测试**
```bash
cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && gofmt -l internal/api/router.go && go build ./... && go test ./...
```
Expected: gofmt 无输出；build 成功；全部包 PASS。若 gofmt 报告则 `gofmt -w`。

- [ ] **Step 3: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/router.go && git commit -m "feat(api): router 装配 userdata.Store + 收藏/统计端点"
```

---

## Task 11: 前端 —— 红心 + 收藏/最近播放面板 + 播放 scrobble

**Files:**
- Modify: `web/src/api/client.ts`
- Create: `web/src/components/FavoritesPanel.vue`
- Modify: 曲目/专辑相关组件（红心）、`web/src/App.vue`、`web/src/components/LibraryShell.vue`、`web/src/stores/player.ts`（scrobble 接入）

> 无 FE 单测；以 `make build-frontend` + `go build ./...` 验证。先读 `web/src/api/client.ts`（request 风格、现有 AlbumSummary/TrackSummary/SearchResponse 类型）、`web/src/App.vue`（面板入口模式、showSettings/showUsers）、`web/src/components/LibraryShell.vue`（入口按钮）、`web/src/stores/player.ts`（播放起始点，用于 scrobble）、专辑/曲目展示组件（AlbumDetail.vue、SearchPanel.vue 等，红心落点）。

- [ ] **Step 1: client.ts 方法 + 类型** — 给 `AlbumSummary`/`TrackSummary`/搜索结果项类型补 `starred?: boolean`（与后端 snake/camel 对齐：后端是 `starred`）。新增方法（均需鉴权，沿用 request 风格）：
```ts
  star(type: 'song' | 'album' | 'artist', id: string): Promise<void> {
    return this.request<void>('/api/v1/star', { method: 'POST', body: JSON.stringify({ type, id }), headers: { 'Content-Type': 'application/json' } })
  }
  unstar(type: 'song' | 'album' | 'artist', id: string): Promise<void> {
    return this.request<void>('/api/v1/unstar', { method: 'POST', body: JSON.stringify({ type, id }), headers: { 'Content-Type': 'application/json' } })
  }
  scrobble(trackId: string): Promise<void> {
    return this.request<void>(`/api/v1/tracks/${trackId}/scrobble`, { method: 'POST' })
  }
  getFavorites(): Promise<{ tracks: FavTrack[]; albums: FavAlbum[] }> {
    return this.request('/api/v1/favorites', { method: 'GET' })
  }
  getRecentlyPlayed(): Promise<{ tracks: FavTrack[] }> {
    return this.request('/api/v1/recently-played', { method: 'GET' })
  }
  getMostPlayed(): Promise<{ tracks: FavTrack[] }> {
    return this.request('/api/v1/most-played', { method: 'GET' })
  }
```
并导出类型：
```ts
export type FavTrack = { id: string; title: string; album: string; album_id: string; artist: string; duration: number; stream_url: string; cover_url: string }
export type FavAlbum = { id: string; title: string; artist: string; cover_url: string }
```

- [ ] **Step 2: 红心按钮** — 在歌曲行（如 AlbumDetail.vue 的曲目列表、SearchPanel.vue 的曲目结果）与专辑卡片/详情加一个红心按钮：实心 `♥`=已收藏、空心 `♡`=未收藏（用内联 svg 或字符）。点击调用 `api.star/unstar('song'|'album', id)` 并就地翻转该项 `starred`。初始状态读列表项的 `starred`。保持与现有按钮样式一致。

- [ ] **Step 3: FavoritesPanel.vue** — 新组件，prop `{api: ApiClient}`，emit `close`，emit `play-track`（复用 App 既有播放入口）。三个标签：我的收藏（getFavorites → tracks/albums）、最近播放（getRecentlyPlayed）、最常听（getMostPlayed）。列表项点击触发播放。顶部「返回」。样式仿 UserManagement.vue。

- [ ] **Step 4: 入口 + scrobble 接入** —
  - `LibraryShell.vue`：在 `.logout-nav-container` 加一个「收藏」按钮 emit `open-favorites`（对所有登录用户可见）。
  - `App.vue`：import FavoritesPanel；新增 `showFavorites` ref；`@open-favorites="showFavorites=true; showSettings=false; showUsers=false"`；其它入口（settings/users）打开时也置 `showFavorites=false`；主内容与 SearchPanel 的 `v-if` 同步加 `&& !showFavorites`；渲染 `<FavoritesPanel v-if="showFavorites" :api="api" @close="showFavorites=false" @play-track="..." />`；logout 时 `showFavorites=false`。FavoritesPanel 的 `play-track` 复用 App 现有的播放函数（参考 playSearchTrack 把 FavTrack 映射为播放队列项）。
  - **scrobble 接入**：在 `web/src/stores/player.ts` 的「开始播放某曲」处（playTrack 设置当前曲目后）调用一次 `api.scrobble(trackId)`。player store 若无 api 实例，则在 App 监听当前曲目变化后调用 `api.scrobble`，或给 store 注入 api。择简：在 App.vue 里 watch `playerStore` 的当前曲目 id 变化，变化时 `void api.scrobble(newId)`（去重由「id 变化才触发」保证）。

- [ ] **Step 5: 构建验证**
```bash
cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && make build-frontend && go build ./...
```
Expected: 前端无 TS 错误、产物入 ui/dist；Go 构建通过。

- [ ] **Step 6: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web ui/dist && git commit -m "feat(web): 歌曲/专辑红心 + 收藏/最近播放面板 + 播放 scrobble"
```

---

## Self-Review（计划自检）

- **Spec 覆盖**：starred/play_stats 表(T1) ✓；userdata.Store(T2) ✓；Subsonic star/unstar(T3 桩→T4 真) ✓；getStarred2(T5) ✓；scrobble per-user + getAlbumList2 recent/frequent/starred(T6) ✓；starred 注解 getAlbum/getSong/search3(T7)，getAlbumList2 注解(T6) ✓；Web star/unstar/scrobble/favorites/recently/most-played(T8) ✓；albums/search starred 布尔(T9) ✓；router(T10) ✓；前端红心 + 面板 + scrobble(T11) ✓；per-user 隔离测试(T2 Store + T5 getStarred2) ✓；删用户级联(FK 既有，starred/play_stats 均 ON DELETE CASCADE) ✓。
- **占位符**：T8 测试夹具里标注需删除的多余 interface 返回值已在文字中要求精简并给出最终形态；无 TODO/TBD。
- **类型一致**：`NewHandler(...,store)`(T3) 与 router(T10) 一致；`userdata.Store` 方法（Star/Unstar/IsStarred/StarredIDs/StarredMap→map[string]string/RecordPlay/Recent/FrequentTrackIDs）跨 T2/T4/T5/T6/T7/T8 一致；`TypeSong/TypeAlbum/TypeArtist` 常量统一；`Child/AlbumID3/ArtistID3.Starred string`(T3) 被 T5/T6/T7 填充；v1 `NewAlbumsHandler(db,store)`/`NewSearchHandler(db,store)`/`NewStarHandler(db,store)`(T8/T9) 与 router(T10) 一致；前端 `starred` 字段与后端 JSON 名一致。
- **已知约束**：getAlbumList2 的 recent/frequent 按 `tracks.album_id` 聚合 play_stats；`orderClause` 不接受外部原文（防注入）；tracks 全局 play_count/last_played 退役（不再写）。
