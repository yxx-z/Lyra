# 歌单封面 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给自建歌单加封面——没上传时动态用首曲所属专辑封面，支持上传自定义图（jpeg/png ≤5MB）与「恢复自动封面」。

**Architecture:** 纯动态输出，不预生成。`playlists` 表加 `cover_path` 列只存自定义上传路径；请求歌单封面时自定义优先，否则回退到首曲专辑封面（复用现有 `CoverHandler.ServeCover`）。新 handler `PlaylistCoverHandler` 提供 GET/PUT/DELETE 三端点，全部按歌单 owner 鉴权（非 owner 一律 404）。

**Tech Stack:** Go (chi, modernc sqlite, net/http multipart)、Vue 3 + TS、现有 `artwork_dir` 文件存储。

**环境：** Go 在 `/home/yxx/go-local/go/bin`，使用前 `export PATH=$PATH:/home/yxx/go-local/go/bin`。所有 `git` 从仓库根执行（`cd /home/yxx/develop/Lyra && git ...`）。前端无 JS 测试框架，以 `make build-frontend` 为准。分支已是 `feat/playlist-cover`。

---

### Task 1: 迁移 010 + schema + 迁移测试

**Files:**
- Create: `internal/db/migrations/010_playlist_cover.up.sql`
- Modify: `internal/db/schema.sql:62-69`（playlists 建表）
- Test: `internal/db/migrations_test.go`（追加一个测试函数；文件中已有 `TestOpen_PlaylistsHaveUserIDAndCascade` 等，用 `grep -rn TestOpen_PlaylistsHaveUserIDAndCascade internal/db/` 确认确切文件名后在该文件追加）

- [ ] **Step 1: 写失败测试**

在含 `TestOpen_PlaylistsHaveUserIDAndCascade` 的测试文件末尾追加：

```go
func TestOpen_PlaylistsHaveCoverPath(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	db.Exec(`INSERT INTO users(id,username,password_hash) VALUES('u1','u1','h')`)
	if _, err := db.Exec(`INSERT INTO playlists(id,user_id,name,cover_path) VALUES('p1','u1','歌单','/x/y.jpg')`); err != nil {
		t.Fatalf("playlists 应有 cover_path 列: %v", err)
	}
	var cp string
	if err := db.QueryRow(`SELECT cover_path FROM playlists WHERE id='p1'`).Scan(&cp); err != nil {
		t.Fatalf("读 cover_path: %v", err)
	}
	if cp != "/x/y.jpg" {
		t.Errorf("cover_path = %q, 期望 /x/y.jpg", cp)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/ -run TestOpen_PlaylistsHaveCoverPath -v`
Expected: FAIL（`table playlists has no column named cover_path`）

- [ ] **Step 3: 写迁移文件**

`internal/db/migrations/010_playlist_cover.up.sql`：
```sql
ALTER TABLE playlists ADD COLUMN cover_path TEXT NOT NULL DEFAULT '';
```

- [ ] **Step 4: 同步 schema.sql**

把 `internal/db/schema.sql` 的 playlists 建表改为（在 `comment` 行后加 `cover_path` 行）：
```sql
CREATE TABLE playlists (
    id         TEXT PRIMARY KEY,
    user_id    TEXT REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    cover_path TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 5: 跑测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/ -v`
Expected: PASS（含新测试 + 原有迁移幂等测试全过）

- [ ] **Step 6: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/db/migrations/010_playlist_cover.up.sql internal/db/schema.sql internal/db/*_test.go && git commit -m "feat(db): playlists 加 cover_path 列（迁移 010）"
```

---

### Task 2: PlaylistCoverHandler（GET/PUT/DELETE）+ 路由

**Files:**
- Create: `internal/api/v1/playlist_cover.go`
- Create: `internal/api/v1/playlist_cover_test.go`
- Modify: `internal/api/router.go`（在 `/api/v1` 组的歌单路由处加三条）

**背景接口（已存在，直接用）：**
- `func (h *CoverHandler) ServeCover(w http.ResponseWriter, r *http.Request, albumID string)` —— 按 albumID 输出封面（`internal/api/v1/cover.go`）。
- `func middleware.UserFromContext(ctx context.Context) (*auth.User, bool)` —— 取当前登录用户。
- `func writeJSON(w http.ResponseWriter, v any)` / `func writeJSONError(w http.ResponseWriter, status int, message string)`（`internal/api/v1/auth.go`）。

- [ ] **Step 1: 写失败测试**

`internal/api/v1/playlist_cover.go`（先建空壳让包能编译，但测试需要的方法此步不写，故测试会 FAIL on undefined）—— **跳过空壳，直接写测试**：

`internal/api/v1/playlist_cover_test.go`：
```go
package v1

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

// jpegBytes 生成一张极小的合法 JPEG。
func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// multipartCover 把图片字节包成 multipart body，返回 body 与 content-type。
func multipartCover(t *testing.T, data []byte, filename string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("cover", filename)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(data)
	mw.Close()
	return &body, mw.FormDataContentType()
}

func pcFixture(t *testing.T) (http.Handler, *auth.User, string, string, string) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	u, _ := us.Create("alice", mustHashFav(t, "pw"), false)
	other, _ := us.Create("bob", mustHashFav(t, "pw"), false)
	// 专辑 al1 有内嵌封面来源：建一张带真实图片文件的曲目，使 ServeCover 能输出。
	artDir := t.TempDir()
	musicFile := filepath.Join(artDir, "song.mp3")
	os.WriteFile(musicFile, []byte("not really mp3"), 0o644)            // 无内嵌封面
	coverFile := filepath.Join(filepath.Dir(musicFile), "cover.jpg")    // 同目录 cover.jpg → ServeCover 命中
	os.WriteFile(coverFile, jpegBytes(t), 0o644)
	d.Exec(`INSERT INTO albums(id,title) VALUES('al1','专辑')`)
	d.Exec(`INSERT INTO tracks(id,title,album_id,file_path,is_available) VALUES('t1','歌一','al1',?,1)`, musicFile)
	// alice 的歌单 p1（含 t1）；bob 的歌单 p2（空）
	d.Exec(`INSERT INTO playlists(id,user_id,name) VALUES('p1',?,'我的')`, u.ID)
	d.Exec(`INSERT INTO playlist_tracks(playlist_id,track_id,position) VALUES('p1','t1',0)`)
	d.Exec(`INSERT INTO playlists(id,user_id,name) VALUES('p2',?,'空单')`, u.ID)

	cover := NewCoverHandler(d)
	h := NewPlaylistCoverHandler(d, artDir, cover)
	token, _ := ss.Create(u.ID, time.Hour)
	otherToken, _ := ss.Create(other.ID, time.Hour)

	r := chi.NewRouter()
	r.Use(middleware.SessionAuth(ss, us, false))
	r.Get("/playlists/{id}/cover", h.Get)
	r.Put("/playlists/{id}/cover", h.Put)
	r.Delete("/playlists/{id}/cover", h.Delete)
	return r, u, token, otherToken, artDir
}

func pcDo(t *testing.T, r http.Handler, token, method, target string, body *bytes.Buffer, ct string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Buffer
	if body == nil {
		rdr = &bytes.Buffer{}
	} else {
		rdr = body
	}
	req := httptest.NewRequest(method, target, rdr)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPlaylistCover_AutoFallbackToFirstTrackAlbum(t *testing.T) {
	r, _, token, _, _ := pcFixture(t)
	w := pcDo(t, r, token, "GET", "/playlists/p1/cover", nil, "")
	if w.Code != 200 {
		t.Fatalf("空自定义图应回退首曲专辑封面，得 %d", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "image/") {
		t.Errorf("应输出图片，Content-Type=%q", w.Header().Get("Content-Type"))
	}
}

func TestPlaylistCover_EmptyPlaylist404(t *testing.T) {
	r, _, token, _, _ := pcFixture(t)
	if pcDo(t, r, token, "GET", "/playlists/p2/cover", nil, "").Code != 404 {
		t.Error("空歌单无自定义图应 404")
	}
}

func TestPlaylistCover_NonOwner404(t *testing.T) {
	r, _, _, otherToken, _ := pcFixture(t)
	if pcDo(t, r, otherToken, "GET", "/playlists/p1/cover", nil, "").Code != 404 {
		t.Error("非属主应 404")
	}
}

func TestPlaylistCover_UploadThenServeCustom(t *testing.T) {
	r, _, token, _, artDir := pcFixture(t)
	body, ct := multipartCover(t, jpegBytes(t), "c.jpg")
	if w := pcDo(t, r, token, "PUT", "/playlists/p2/cover", body, ct); w.Code != 200 {
		t.Fatalf("上传失败 %d: %s", w.Code, w.Body.String())
	}
	// 文件落地
	if _, err := os.Stat(filepath.Join(artDir, "playlist_p2.jpg")); err != nil {
		t.Fatalf("自定义图未落地: %v", err)
	}
	// 即便 p2 是空歌单，自定义图也应能输出
	w := pcDo(t, r, token, "GET", "/playlists/p2/cover", nil, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("应输出自定义 jpeg，得 %d %q", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestPlaylistCover_UploadNonOwner404(t *testing.T) {
	r, _, _, otherToken, _ := pcFixture(t)
	body, ct := multipartCover(t, jpegBytes(t), "c.jpg")
	if pcDo(t, r, otherToken, "PUT", "/playlists/p1/cover", body, ct).Code != 404 {
		t.Error("非属主上传应 404")
	}
}

func TestPlaylistCover_RejectNonImage(t *testing.T) {
	r, _, token, _, _ := pcFixture(t)
	body, ct := multipartCover(t, []byte("plain text not an image"), "c.txt")
	if pcDo(t, r, token, "PUT", "/playlists/p2/cover", body, ct).Code != 400 {
		t.Error("非 jpeg/png 应 400")
	}
}

func TestPlaylistCover_JpgThenPngReplacesOld(t *testing.T) {
	r, _, token, _, artDir := pcFixture(t)
	b1, ct1 := multipartCover(t, jpegBytes(t), "c.jpg")
	pcDo(t, r, token, "PUT", "/playlists/p2/cover", b1, ct1)
	b2, ct2 := multipartCover(t, pngBytes(t), "c.png")
	if pcDo(t, r, token, "PUT", "/playlists/p2/cover", b2, ct2).Code != 200 {
		t.Fatal("重传 png 失败")
	}
	if _, err := os.Stat(filepath.Join(artDir, "playlist_p2.jpg")); !os.IsNotExist(err) {
		t.Error("重传 png 后旧 jpg 应被删除")
	}
	if _, err := os.Stat(filepath.Join(artDir, "playlist_p2.png")); err != nil {
		t.Errorf("png 应落地: %v", err)
	}
}

func TestPlaylistCover_RejectTooLarge(t *testing.T) {
	r, _, token, _, _ := pcFixture(t)
	big := append(jpegBytes(t), bytes.Repeat([]byte{0}, maxCoverBytes+1)...)
	body, ct := multipartCover(t, big, "big.jpg")
	if pcDo(t, r, token, "PUT", "/playlists/p2/cover", body, ct).Code != 400 {
		t.Error("超 5MB 应拒绝")
	}
}

func TestPlaylistCover_DeleteRevertsToAuto(t *testing.T) {
	r, _, token, _, artDir := pcFixture(t)
	body, ct := multipartCover(t, jpegBytes(t), "c.jpg")
	pcDo(t, r, token, "PUT", "/playlists/p1/cover", body, ct)
	if pcDo(t, r, token, "DELETE", "/playlists/p1/cover", nil, "").Code != 204 {
		t.Fatal("删除自定义图应 204")
	}
	if _, err := os.Stat(filepath.Join(artDir, "playlist_p1.jpg")); !os.IsNotExist(err) {
		t.Error("删除后自定义文件应不存在")
	}
	// p1 有曲目，删除后回退专辑封面仍 200
	if pcDo(t, r, token, "GET", "/playlists/p1/cover", nil, "").Code != 200 {
		t.Error("删除自定义图后应回退首曲专辑封面")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestPlaylistCover -v`
Expected: FAIL（`undefined: NewPlaylistCoverHandler`，编译错误）

- [ ] **Step 3: 写 handler**

`internal/api/v1/playlist_cover.go`：
```go
// internal/api/v1/playlist_cover.go
package v1

import (
	"database/sql"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
)

// PlaylistCoverHandler 处理歌单封面：自定义优先，否则回退首曲专辑封面。
type PlaylistCoverHandler struct {
	db         *sql.DB
	artworkDir string
	cover      *CoverHandler
}

func NewPlaylistCoverHandler(db *sql.DB, artworkDir string, cover *CoverHandler) *PlaylistCoverHandler {
	return &PlaylistCoverHandler{db: db, artworkDir: artworkDir, cover: cover}
}

const maxCoverBytes = 5 << 20 // 5MB

// ownerCoverPath 校验属主并返回该歌单的自定义封面路径；非属主/不存在返回 ok=false。
func (h *PlaylistCoverHandler) ownerCoverPath(r *http.Request, id string) (uid, coverPath string, ok bool) {
	u, okUser := middleware.UserFromContext(r.Context())
	if !okUser {
		return "", "", false
	}
	err := h.db.QueryRow(`SELECT cover_path FROM playlists WHERE id=? AND user_id=?`, id, u.ID).Scan(&coverPath)
	if err != nil {
		return "", "", false
	}
	return u.ID, coverPath, true
}

func serveImageFile(w http.ResponseWriter, path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return false
	}
	ct := "image/jpeg"
	if strings.HasSuffix(strings.ToLower(path), ".png") {
		ct = "image/png"
	}
	w.Header().Set("Content-Type", ct)
	_, _ = w.Write(data)
	return true
}

// Get 处理 GET /api/v1/playlists/{id}/cover。
func (h *PlaylistCoverHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, coverPath, ok := h.ownerCoverPath(r, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if coverPath != "" && serveImageFile(w, coverPath) {
		return
	}
	// 回退：首曲所属专辑封面
	var albumID sql.NullString
	err := h.db.QueryRow(
		`SELECT t.album_id FROM playlist_tracks pt JOIN tracks t ON t.id=pt.track_id
		 WHERE pt.playlist_id=? ORDER BY pt.position LIMIT 1`, id,
	).Scan(&albumID)
	if err != nil || !albumID.Valid || albumID.String == "" {
		http.NotFound(w, r)
		return
	}
	h.cover.ServeCover(w, r, albumID.String)
}

// Put 处理 PUT /api/v1/playlists/{id}/cover（multipart，字段名 cover）。
func (h *PlaylistCoverHandler) Put(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	uid, existing, ok := h.ownerCoverPath(r, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCoverBytes)
	file, _, err := r.FormFile("cover")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "未提供图片或图片过大（≤5MB）")
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	var ext string
	switch http.DetectContentType(head[:n]) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	default:
		writeJSONError(w, http.StatusBadRequest, "仅支持 JPEG/PNG")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "读取图片失败")
		return
	}

	if err := os.MkdirAll(h.artworkDir, 0o755); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建封面目录失败")
		return
	}
	dst := filepath.Join(h.artworkDir, "playlist_"+id+ext)
	if existing != "" && existing != dst {
		_ = os.Remove(existing) // 清理旧扩展名残留
	}
	out, err := os.Create(dst)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "保存封面失败")
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		writeJSONError(w, http.StatusInternalServerError, "写入封面失败")
		return
	}
	out.Close()

	if _, err := h.db.Exec(
		`UPDATE playlists SET cover_path=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`,
		dst, id, uid,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "更新封面失败")
		return
	}
	writeJSON(w, map[string]string{"cover_url": "/api/v1/playlists/" + id + "/cover"})
}

// Delete 处理 DELETE /api/v1/playlists/{id}/cover —— 删自定义图、恢复自动封面。
func (h *PlaylistCoverHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	uid, existing, ok := h.ownerCoverPath(r, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if existing != "" {
		_ = os.Remove(existing)
	}
	if _, err := h.db.Exec(`UPDATE playlists SET cover_path='' WHERE id=? AND user_id=?`, id, uid); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "更新封面失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestPlaylistCover -v`
Expected: PASS（9 个用例全过）

- [ ] **Step 5: 路由注册**

`internal/api/router.go`，在歌单路由块（`r.Get("/playlists", plH.List)` … `r.Put("/playlists/{id}/tracks", plH.ReplaceTracks)` 之后、该 `/api/v1` 闭包内）追加。注意 `cover` 变量已在同闭包前面 `cover := v1.NewCoverHandler(db)` 定义，可直接复用：
```go
		plCover := v1.NewPlaylistCoverHandler(db, cfg.Cache.ArtworkDir, cover)
		r.Get("/playlists/{id}/cover", plCover.Get)
		r.Put("/playlists/{id}/cover", plCover.Put)
		r.Delete("/playlists/{id}/cover", plCover.Delete)
```

- [ ] **Step 6: 全量构建确认无回归**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./internal/api/...`
Expected: build 无输出；测试全 ok

- [ ] **Step 7: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/playlist_cover.go internal/api/v1/playlist_cover_test.go internal/api/router.go && git commit -m "feat(api): 歌单封面端点（GET 回退首曲专辑/PUT 上传/DELETE 恢复）"
```

---

### Task 3: 歌单 DTO 加 cover_url

**Files:**
- Modify: `internal/api/v1/playlists.go`（`playlistSummary` 结构体 + `toSummary` + `Get` 详情 map）
- Test: `internal/api/v1/playlists_test.go`（在已有测试中加断言或新增一个）

- [ ] **Step 1: 写失败测试**

在 `internal/api/v1/playlists_test.go` 末尾追加：
```go
func TestV1Playlist_ListAndDetailHaveCoverURL(t *testing.T) {
	r, u, pl, token := plFixture(t)
	id, _ := pl.Create(u.ID, "封面单")
	_ = id
	listBody := plDo(t, r, token, "GET", "/playlists", "").Body.String()
	if !strings.Contains(listBody, `"cover_url"`) {
		t.Errorf("列表应含 cover_url: %s", listBody)
	}
	list, _ := pl.List(u.ID)
	detailBody := plDo(t, r, token, "GET", "/playlists/"+list[0].ID, "").Body.String()
	if !strings.Contains(detailBody, `/api/v1/playlists/`+list[0].ID+`/cover`) {
		t.Errorf("详情 cover_url 应指向该歌单封面端点: %s", detailBody)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestV1Playlist_ListAndDetailHaveCoverURL -v`
Expected: FAIL（响应里没有 cover_url）

- [ ] **Step 3: 改 DTO + toSummary**

`internal/api/v1/playlists.go`，`playlistSummary` 加字段：
```go
type playlistSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Comment   string `json:"comment"`
	SongCount int    `json:"song_count"`
	Duration  int    `json:"duration"`
	Created   string `json:"created"`
	Changed   string `json:"changed"`
	CoverURL  string `json:"cover_url"`
}
```
`toSummary` 填充（cover_url 固定指向封面端点，回退逻辑在后端）：
```go
func toSummary(p playlists.Playlist) playlistSummary {
	return playlistSummary{
		ID: p.ID, Name: p.Name, Comment: p.Comment,
		SongCount: p.SongCount, Duration: p.Duration,
		Created: p.Created, Changed: p.Changed,
		CoverURL: "/api/v1/playlists/" + p.ID + "/cover",
	}
}
```

- [ ] **Step 4: 详情 map 加 cover_url**

`Get` 方法里 `writeJSON(w, map[string]any{...})` 加一项 `"cover_url": sum.CoverURL`：
```go
	sum := toSummary(p)
	writeJSON(w, map[string]any{
		"id": sum.ID, "name": sum.Name, "comment": sum.Comment,
		"song_count": sum.SongCount, "duration": sum.Duration,
		"created": sum.Created, "changed": sum.Changed,
		"cover_url": sum.CoverURL,
		"tracks":    tracksByIDs(h.db, ids),
	})
```

- [ ] **Step 5: 跑测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestV1Playlist -v`
Expected: PASS（新测试 + 原有歌单测试全过）

- [ ] **Step 6: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/playlists.go internal/api/v1/playlists_test.go && git commit -m "feat(api): 歌单列表/详情 DTO 增 cover_url"
```

---

### Task 4: 前端显示 + 上传/恢复封面

**Files:**
- Modify: `web/src/api/client.ts`（`PlaylistSummary` 类型加 `cover_url`；新增 `uploadPlaylistCover` / `deletePlaylistCover`）
- Modify: `web/src/components/PlaylistsPage.vue`（卡片/详情头显示封面 + 上传/恢复入口）

**背景：** `client.ts` 的 `request<T>` 不强制设置 Content-Type（`new Headers(options.headers)` 仅在有 token 时加 Authorization），因此传 `FormData` 作为 body 时浏览器会自动带 multipart 边界，无需手动设 Content-Type。`PlaylistSummary`（client.ts:153）已有；`PlaylistDetail = PlaylistSummary & { tracks: FavTrack[] }`。

- [ ] **Step 1: client.ts 类型 + 方法**

`PlaylistSummary` 加 `cover_url`：
```ts
export type PlaylistSummary = { id: string; name: string; comment: string; song_count: number; duration: number; created: string; changed: string; cover_url: string }
```
在 `setPlaylistTracks` 方法之后、类内追加两个方法：
```ts
  uploadPlaylistCover(id: string, file: File): Promise<void> {
    const fd = new FormData()
    fd.append('cover', file)
    return this.request<void>(`/api/v1/playlists/${encodeURIComponent(id)}/cover`, {
      method: 'PUT',
      body: fd,
    })
  }

  deletePlaylistCover(id: string): Promise<void> {
    return this.request<void>(`/api/v1/playlists/${encodeURIComponent(id)}/cover`, {
      method: 'DELETE',
    })
  }
```

- [ ] **Step 2: PlaylistsPage 详情头加封面与操作**

`web/src/components/PlaylistsPage.vue`，把详情头部 `<div class="pl-detail-header">…</div>`（含 `pl-detail-name`/`pl-detail-count`/全部播放按钮）替换为：在头部前面加一个封面块，并把操作按钮放进去。具体：在 `<template v-if="selected">` 内、`<div class="pl-detail-header">` 之前插入封面，并在 header 内补上传/恢复按钮：
```vue
        <template v-if="selected">
          <div class="pl-detail-cover-row">
            <img
              class="pl-detail-cover"
              :src="coverSrc(selected)"
              alt="歌单封面"
              @error="onCoverError"
            />
            <div class="pl-cover-actions">
              <input
                ref="coverInput"
                type="file"
                accept="image/jpeg,image/png"
                style="display: none"
                @change="onCoverPicked"
              />
              <button class="pl-icon-btn" type="button" title="上传封面" @click="pickCover">上传封面</button>
              <button class="pl-icon-btn" type="button" title="恢复自动封面" @click="restoreCover">恢复自动封面</button>
            </div>
          </div>
          <div class="pl-detail-header">
            <span class="pl-detail-name">{{ selected.name }}</span>
            <span class="muted pl-detail-count">{{ selected.tracks.length }} 首</span>
            <button class="custom-btn-primary" v-if="selected && selected.tracks.length" type="button" @click="$emit('play-list', selected.tracks, 0)">▶ 全部播放</button>
          </div>
```

- [ ] **Step 3: PlaylistsPage 脚本逻辑**

在 `<script setup>` 内（已有 `selected`、`props.api`、`show(msg, isError)`、`errMsg(e)`、`open(id)` 等；用 `grep -n "function show\|function open\|const selected\|errMsg" web/src/components/PlaylistsPage.vue` 确认），追加封面相关状态与函数。`coverBust` 用于上传后破缓存：
```ts
import { ref } from 'vue'  // 若文件顶部已 import ref，则不要重复，只复用

const coverInput = ref<HTMLInputElement | null>(null)
const coverBust = ref(0)

// 封面 URL：附带 bust 参数破浏览器缓存（上传/恢复后变化）
function coverSrc(pl: PlaylistDetail): string {
  const base = pl.cover_url || `/api/v1/playlists/${pl.id}/cover`
  return coverBust.value ? `${base}?t=${coverBust.value}` : base
}

// 自动封面 404（空歌单且无自定义图）时隐藏破图
function onCoverError(e: Event) {
  ;(e.target as HTMLImageElement).style.visibility = 'hidden'
}

function pickCover() {
  coverInput.value?.click()
}

async function onCoverPicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = '' // 允许重复选同一文件
  if (!file || !selected.value) return
  try {
    await props.api.uploadPlaylistCover(selected.value.id, file)
    coverBust.value = Date.now()
    show('封面已更新', false)
  } catch (err) {
    show(errMsg(err), true)
  }
}

async function restoreCover() {
  if (!selected.value) return
  try {
    await props.api.deletePlaylistCover(selected.value.id)
    coverBust.value = Date.now()
    show('已恢复自动封面', false)
  } catch (err) {
    show(errMsg(err), true)
  }
}
```
注意：`onCoverError` 把 img 设为 hidden，切换歌单或上传后需复位可见性——在 `coverSrc` 被重新求值渲染新 `src` 时浏览器会重新触发加载；为稳妥，可在 `open()` 成功后重置：找到 `open` 函数中 `selected.value = await props.api.getPlaylist(id)` 成功处之后，不需特殊处理（新 `<img>` src 改变会重置）。若发现切歌单后封面仍隐藏，则在模板 img 上改用 `:key="selected.id + '-' + coverBust"` 强制重建节点——**本步先按上面写，构建后真机若有隐藏残留再加 `:key`。**

- [ ] **Step 4: 样式**

在 `<style scoped>` 末尾追加（与现有 `.pl-detail-header` 等风格一致）：
```css
.pl-detail-cover-row {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 12px;
}
.pl-detail-cover {
  width: 96px;
  height: 96px;
  border-radius: 12px;
  object-fit: cover;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid var(--border-glass, rgba(255, 255, 255, 0.08));
  flex-shrink: 0;
}
.pl-cover-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
```

- [ ] **Step 5: 构建**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && make build-frontend && go build ./...`
Expected: vue-tsc 0 错误；vite build 成功；Go build 无输出

- [ ] **Step 6: 提交**

```bash
cd /home/yxx/develop/Lyra && git add web/src/api/client.ts web/src/components/PlaylistsPage.vue && git commit -m "feat(web): 歌单封面显示 + 上传/恢复自动封面入口"
```

---

## 验证（真机，合并后）

`make docker-build && docker compose up -d` 后在浏览器：
- 进一个有歌曲的歌单 → 详情头显示首曲专辑封面。
- 「上传封面」选一张 jpg/png → 封面立即变为上传图；刷新后仍是它。
- 「恢复自动封面」→ 回到首曲专辑封面。
- 空歌单 → 封面位隐藏（无破图）。
- 换一个用户登录 → 看不到/改不了别人的歌单封面（私有）。
