# 删除专辑 / 歌手 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 管理员可在 Web 端删除专辑或歌手（默认仅删库，可勾选同时删硬盘文件）。

**Architecture:** v1 加管理员专属的 DELETE 端点（`r.With(middleware.RequireAdmin)` 按路由守卫）。删除在事务内按外键顺序做：lyrics + starred(song) → tracks（bookmarks/play_stats/playlist_tracks 经级联自动清）→ albums/artists；删歌手连带其全部专辑与曲目。可选删文件为尽力而为：DB 先删，文件删失败仅提示。前端在专辑详情/歌手页加管理员可见的删除按钮 + 确认勾选。

**Tech Stack:** Go 1.25 · modernc.org/sqlite（单连接、foreign_keys=ON）· chi v5 · Vue 3。

**关键约束：**
- 外键无级联的链：`tracks.album_id`、`tracks.artist_id`、`albums.artist_id`、`lyrics.track_id` → 必须手动按序删。有级联：`bookmarks/play_stats/playlist_tracks.track_id`（自动）。`starred.item_id` 无外键 → 手动清孤儿。
- modernc 单连接：先把要删的 id/file_path 用查询排空成切片，再开事务删。
- 音乐目录在 docker 里只读挂载（`/music:ro`）→ 删文件会失败；本设计让 DB 必删干净、文件失败仅在响应里报。
- Go 路径：`export PATH=$PATH:/home/yxx/go-local/go/bin`。后端测试用内存 sqlite + httptest + 临时文件。
- 端点路径用 `DELETE /api/v1/albums/{id}` 与 `DELETE /api/v1/artists/{id}`（与 GET 同路径、不同方法），经 `r.With(middleware.RequireAdmin)` 仅管理员可用。

---

## File Structure

```
internal/api/v1/library_delete.go        新：删除核心(deleteTracksByIDs/removeFiles/collect*) + DeleteAlbum/DeleteArtist
internal/api/v1/library_delete_test.go   新：删除测试
internal/api/router.go                    改：注册两个 admin 守卫的 DELETE 路由
web/src/api/client.ts                     改：deleteAlbum/deleteArtist
web/src/components/AlbumDetail.vue        改：管理员「删除专辑」按钮 + 确认勾选 + emit deleted
web/src/components/ArtistBrowser.vue      改：管理员「删除歌手」按钮 + 确认勾选 + emit deleted
web/src/App.vue                            改：传 is-admin、处理 @deleted 刷新清选中
```

---

## Task 1: 后端删除核心 + DeleteAlbum

**Files:** Create `internal/api/v1/library_delete.go`, `internal/api/v1/library_delete_test.go`

> 删除方法挂在已有的 `AlbumsHandler`（持 `*sql.DB`）。router 在 Task 3 接线；本任务用 `go test ./internal/api/v1/ -run DeleteAlbum` + `go vet ./internal/api/v1/` 验证。

- [ ] **Step 1: 写失败测试** `internal/api/v1/library_delete_test.go`：
```go
package v1

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/userdata"
)

// seedAlbumWithDeps 造 1 专辑 + 1 曲目 + 其 lyrics/bookmark/play_stats/playlist_tracks/starred，
// 返回 albumID、trackID、磁盘文件路径（真实存在的临时文件）。
func seedAlbumWithDeps(t *testing.T, d *interface {
	Exec(string, ...any) (interface{ RowsAffected() (int64, error) }, error)
}) {
	t.Helper()
}

func TestDeleteAlbum_RemovesAlbumAndDeps(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	dir := t.TempDir()
	fpath := filepath.Join(dir, "song.flac")
	os.WriteFile(fpath, []byte("x"), 0o644)

	d.Exec(`INSERT INTO users(id,username,password_hash) VALUES('u1','u1','h')`)
	d.Exec(`INSERT INTO artists(id,name) VALUES('ar1','歌手')`)
	d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','专辑','ar1')`)
	d.Exec(`INSERT INTO tracks(id,title,album_id,artist_id,file_path) VALUES('t1','歌','al1','ar1',?)`, fpath)
	d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source) VALUES('t1','[00:00]x','sidecar')`)
	d.Exec(`INSERT INTO bookmarks(user_id,track_id,position) VALUES('u1','t1',1)`)
	d.Exec(`INSERT INTO play_stats(user_id,track_id,play_count) VALUES('u1','t1',1)`)
	d.Exec(`INSERT INTO playlists(id,user_id,name) VALUES('p1','u1','PL')`)
	d.Exec(`INSERT INTO playlist_tracks(playlist_id,track_id,position) VALUES('p1','t1',0)`)
	d.Exec(`INSERT INTO starred(user_id,item_type,item_id) VALUES('u1','song','t1')`)
	d.Exec(`INSERT INTO starred(user_id,item_type,item_id) VALUES('u1','album','al1')`)

	h := NewAlbumsHandler(d, userdata.NewStore(d))
	r := chi.NewRouter()
	r.Delete("/albums/{id}", h.DeleteAlbum)

	req := httptest.NewRequest("DELETE", "/albums/al1?deleteFiles=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("删专辑应成功: %d %s", w.Code, w.Body.String())
	}

	count := func(q string) int {
		var n int
		d.QueryRow(q).Scan(&n)
		return n
	}
	if count(`SELECT COUNT(*) FROM albums WHERE id='al1'`) != 0 {
		t.Error("album 应删除")
	}
	if count(`SELECT COUNT(*) FROM tracks WHERE id='t1'`) != 0 {
		t.Error("track 应删除")
	}
	if count(`SELECT COUNT(*) FROM lyrics WHERE track_id='t1'`) != 0 {
		t.Error("lyrics 应删除")
	}
	if count(`SELECT COUNT(*) FROM bookmarks WHERE track_id='t1'`) != 0 {
		t.Error("bookmarks 应级联删除")
	}
	if count(`SELECT COUNT(*) FROM play_stats WHERE track_id='t1'`) != 0 {
		t.Error("play_stats 应级联删除")
	}
	if count(`SELECT COUNT(*) FROM playlist_tracks WHERE track_id='t1'`) != 0 {
		t.Error("playlist_tracks 应级联删除")
	}
	if count(`SELECT COUNT(*) FROM starred WHERE item_id IN ('t1','al1')`) != 0 {
		t.Error("starred 孤儿应清除")
	}
	if !strings.Contains(w.Body.String(), `"filesDeleted":1`) {
		t.Errorf("应删 1 个文件: %s", w.Body.String())
	}
	if _, err := os.Stat(fpath); !os.IsNotExist(err) {
		t.Error("磁盘文件应被删除")
	}
}

func TestDeleteAlbum_DBOnlyKeepsFile(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	dir := t.TempDir()
	fpath := filepath.Join(dir, "song.flac")
	os.WriteFile(fpath, []byte("x"), 0o644)
	d.Exec(`INSERT INTO albums(id,title) VALUES('al1','专辑')`)
	d.Exec(`INSERT INTO tracks(id,title,album_id,file_path) VALUES('t1','歌','al1',?)`, fpath)

	h := NewAlbumsHandler(d, userdata.NewStore(d))
	r := chi.NewRouter()
	r.Delete("/albums/{id}", h.DeleteAlbum)
	// 不带 deleteFiles → 仅删库，文件保留
	req := httptest.NewRequest("DELETE", "/albums/al1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("应成功: %d", w.Code)
	}
	if _, err := os.Stat(fpath); err != nil {
		t.Error("默认不应删文件")
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM albums WHERE id='al1'`).Scan(&n)
	if n != 0 {
		t.Error("库仍应删除")
	}
}

func TestDeleteAlbum_NotFound(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	h := NewAlbumsHandler(d, userdata.NewStore(d))
	r := chi.NewRouter()
	r.Delete("/albums/{id}", h.DeleteAlbum)
	req := httptest.NewRequest("DELETE", "/albums/nope", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("不存在应 404: %d", w.Code)
	}
}
```
> 删掉顶部那个空的 `seedAlbumWithDeps` 占位函数（它是误留的脚手架，未被任何用例使用）。最终文件只保留三个 `Test*` 函数 + import。

- [ ] **Step 2: 运行确认失败** — `go vet ./internal/api/v1/` → 失败（DeleteAlbum 未定义）。

- [ ] **Step 3: 实现** `internal/api/v1/library_delete.go`：
```go
// internal/api/v1/library_delete.go
package v1

import (
	"database/sql"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
)

// deleteTracksByIDs 在事务内删除给定曲目及其依赖（lyrics、starred(song)）；
// bookmarks/play_stats/playlist_tracks 经 FK ON DELETE CASCADE 自动清。
func deleteTracksByIDs(tx *sql.Tx, ids []string) error {
	for _, id := range ids {
		if _, err := tx.Exec(`DELETE FROM lyrics WHERE track_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM starred WHERE item_type='song' AND item_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM tracks WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

// collectTracks 跑给定查询，返回曲目 id 与 file_path 两个切片（已排空 rows）。
func collectTracks(db *sql.DB, query string, args ...any) (ids []string, paths []string, err error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		paths = append(paths, p)
	}
	return ids, paths, rows.Err()
}

// removeFiles 尽力删除磁盘文件，返回成功数与错误描述。
func removeFiles(paths []string) (int, []string) {
	deleted := 0
	errs := []string{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil {
			errs = append(errs, p+": "+err.Error())
		} else {
			deleted++
		}
	}
	return deleted, errs
}

// DeleteAlbum 处理 DELETE /api/v1/albums/{id}?deleteFiles=（管理员）。
func (h *AlbumsHandler) DeleteAlbum(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var exists string
	if err := h.db.QueryRow(`SELECT id FROM albums WHERE id=?`, id).Scan(&exists); err != nil {
		http.NotFound(w, r)
		return
	}
	ids, paths, err := collectTracks(h.db, `SELECT id, file_path FROM tracks WHERE album_id=?`, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	tx, err := h.db.Begin()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if err := deleteTracksByIDs(tx, ids); err != nil {
		tx.Rollback()
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if _, err := tx.Exec(`DELETE FROM starred WHERE item_type='album' AND item_id=?`, id); err != nil {
		tx.Rollback()
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if _, err := tx.Exec(`DELETE FROM albums WHERE id=?`, id); err != nil {
		tx.Rollback()
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeDeleteResult(w, r, paths)
}

// writeDeleteResult 按 deleteFiles 参数尽力删文件并写统一响应。
func writeDeleteResult(w http.ResponseWriter, r *http.Request, paths []string) {
	filesDeleted := 0
	fileErrors := []string{}
	if r.URL.Query().Get("deleteFiles") == "true" {
		filesDeleted, fileErrors = removeFiles(paths)
	}
	writeJSON(w, map[string]any{"ok": true, "filesDeleted": filesDeleted, "fileErrors": fileErrors})
}
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/v1/ -run DeleteAlbum -v && go vet ./internal/api/v1/` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/library_delete.go internal/api/v1/library_delete_test.go && git commit -m "feat(api): DeleteAlbum 端点（删库 + 可选删文件 + 级联清理）"
```

---

## Task 2: DeleteArtist

**Files:** Modify `internal/api/v1/library_delete.go`；Test `internal/api/v1/library_delete_test.go`（追加）

- [ ] **Step 1: 追加失败测试**：
```go
func TestDeleteArtist_RemovesArtistAlbumsTracks(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	d.Exec(`INSERT INTO artists(id,name) VALUES('ar1','歌手')`)
	d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','专辑A','ar1')`)
	d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al2','专辑B','ar1')`)
	d.Exec(`INSERT INTO tracks(id,title,album_id,artist_id,file_path) VALUES('t1','歌1','al1','ar1','/x/1.flac')`)
	d.Exec(`INSERT INTO tracks(id,title,album_id,artist_id,file_path) VALUES('t2','歌2','al2','ar1','/x/2.flac')`)
	d.Exec(`INSERT INTO starred(user_id,item_type,item_id) VALUES('u1','artist','ar1')`)

	h := NewArtistsHandler(d)
	r := chi.NewRouter()
	r.Delete("/artists/{id}", h.DeleteArtist)
	req := httptest.NewRequest("DELETE", "/artists/ar1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("删歌手应成功: %d %s", w.Code, w.Body.String())
	}
	c := func(q string) int { var n int; d.QueryRow(q).Scan(&n); return n }
	if c(`SELECT COUNT(*) FROM artists WHERE id='ar1'`) != 0 {
		t.Error("artist 应删除")
	}
	if c(`SELECT COUNT(*) FROM albums WHERE artist_id='ar1'`) != 0 {
		t.Error("该歌手专辑应全删")
	}
	if c(`SELECT COUNT(*) FROM tracks WHERE id IN ('t1','t2')`) != 0 {
		t.Error("该歌手曲目应全删")
	}
	if c(`SELECT COUNT(*) FROM starred WHERE item_type='artist' AND item_id='ar1'`) != 0 {
		t.Error("starred(artist) 应清")
	}
}
```

- [ ] **Step 2: 运行确认失败** — `go vet ./internal/api/v1/` → 失败（DeleteArtist 未定义）。

- [ ] **Step 3: 实现** — 在 `library_delete.go` 追加：
```go
// DeleteArtist 处理 DELETE /api/v1/artists/{id}?deleteFiles=（管理员）。
// 连带删除该歌手名下所有专辑与曲目。
func (h *ArtistsHandler) DeleteArtist(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var exists string
	if err := h.db.QueryRow(`SELECT id FROM artists WHERE id=?`, id).Scan(&exists); err != nil {
		http.NotFound(w, r)
		return
	}
	// 该歌手关联的曲目：artist_id 指向它，或其专辑下的曲目
	ids, paths, err := collectTracks(h.db,
		`SELECT id, file_path FROM tracks WHERE artist_id=? OR album_id IN (SELECT id FROM albums WHERE artist_id=?)`,
		id, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	tx, err := h.db.Begin()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if err := deleteTracksByIDs(tx, ids); err != nil {
		tx.Rollback()
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	stmts := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM starred WHERE item_type='album' AND item_id IN (SELECT id FROM albums WHERE artist_id=?)`, []any{id}},
		{`DELETE FROM albums WHERE artist_id=?`, []any{id}},
		{`DELETE FROM starred WHERE item_type='artist' AND item_id=?`, []any{id}},
		{`DELETE FROM artists WHERE id=?`, []any{id}},
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s.q, s.args...); err != nil {
			tx.Rollback()
			writeJSONError(w, http.StatusInternalServerError, "删除失败")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeDeleteResult(w, r, paths)
}
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/v1/ -run 'DeleteArtist|DeleteAlbum' -v && go vet ./internal/api/v1/` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/library_delete.go internal/api/v1/library_delete_test.go && git commit -m "feat(api): DeleteArtist 端点（连带删专辑/曲目）"
```

---

## Task 3: router 装配 + 全量编译

**Files:** Modify `internal/api/router.go`

- [ ] **Step 1: 接线** — 在 `/api/v1` 组内，已有 `albums := v1.NewAlbumsHandler(db, udStore)` 与其 GET 路由处，紧随其后加：
```go
		r.With(middleware.RequireAdmin).Delete("/albums/{id}", albums.DeleteAlbum)
```
在 `artists := v1.NewArtistsHandler(db)` 与其 GET 路由处，紧随其后加：
```go
		r.With(middleware.RequireAdmin).Delete("/artists/{id}", artists.DeleteArtist)
```
（`r.With(mw)` 给单条路由挂中间件，无需移动声明顺序；`middleware` 包已 import。）

- [ ] **Step 2: 全量编译 + 测试**
```bash
cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && gofmt -l internal/api/router.go && go build ./... && go test ./...
```
Expected: gofmt 无输出；build 成功；全部包 PASS。

- [ ] **Step 3: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/router.go && git commit -m "feat(api): 注册 admin 守卫的 DELETE albums/artists 路由"
```

---

## Task 4: 前端 —— client 方法 + 专辑删除 UI + App 接线

**Files:** Modify `web/src/api/client.ts`、`web/src/components/AlbumDetail.vue`、`web/src/App.vue`

> 先读 client.ts（request 风格）、AlbumDetail.vue（标题操作区、props、`heart-btn` 旁，已有 `:api`）、App.vue（`currentUser`、AlbumDetail 渲染处、`loadAlbums`/`selectedAlbum`）。

- [ ] **Step 1: client.ts** 新增（鉴权）：
```ts
  deleteAlbum(id: string, deleteFiles: boolean): Promise<{ ok: boolean; filesDeleted: number; fileErrors: string[] }> {
    return this.request(`/api/v1/albums/${encodeURIComponent(id)}?deleteFiles=${deleteFiles}`, { method: 'DELETE' })
  }
  deleteArtist(id: string, deleteFiles: boolean): Promise<{ ok: boolean; filesDeleted: number; fileErrors: string[] }> {
    return this.request(`/api/v1/artists/${encodeURIComponent(id)}?deleteFiles=${deleteFiles}`, { method: 'DELETE' })
  }
```

- [ ] **Step 2: AlbumDetail.vue** — 加 `isAdmin` prop 与删除 UI：
  - `defineProps` 增 `isAdmin?: boolean`（默认 false）；`defineEmits` 增 `(e:'deleted'): void`。
  - 在标题操作区（刮削/红心那一行）追加，仅管理员可见：
```vue
<button v-if="isAdmin" class="danger-btn" type="button" @click="confirmingDelete = true">删除专辑</button>
```
  - 一个内联确认区（`v-if="confirmingDelete"`）：文案「确认删除专辑「{{ album.title }}」？」+ 勾选 `<input type="checkbox" v-model="alsoDeleteFiles"> 同时删除硬盘文件`（勾选时追加红字「文件不可恢复；若音乐目录为只读挂载会删除失败」）+ 「确认删除」「取消」两个按钮。
  - 脚本：`const confirmingDelete = ref(false); const alsoDeleteFiles = ref(false)`；
```ts
async function doDelete() {
  if (!props.album) return
  try {
    const res = await props.api.deleteAlbum(props.album.id, alsoDeleteFiles.value)
    confirmingDelete.value = false
    alsoDeleteFiles.value = false
    if (res.fileErrors && res.fileErrors.length) {
      // 通过 emit 让上层提示；此处也可本地提示
    }
    emit('deleted', res.fileErrors || [])
  } catch (e) {
    /* 失败提示 */
  }
}
```
  把 emit 定义为 `(e:'deleted', fileErrors: string[]): void` 以便上层据 fileErrors 提示。加 `.danger-btn` scoped 样式（红色）。

- [ ] **Step 3: App.vue 接线** — 给 `<AlbumDetail>` 传 `:is-admin="currentUser?.isAdmin ?? false"` 与 `@deleted="onAlbumDeleted"`：
```ts
async function onAlbumDeleted(fileErrors: string[]) {
  selectedAlbum.value = null
  await loadAlbums()
  if (fileErrors && fileErrors.length) {
    globalError.value = `已从库删除，但 ${fileErrors.length} 个文件未能删除（音乐目录可能为只读挂载）`
  }
}
```

- [ ] **Step 4: 构建验证** — `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && make build-frontend && go build ./...` → 通过。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web/src && git commit -m "feat(web): 专辑删除入口（管理员,确认+可选删文件）"
```

---

## Task 5: 前端 —— 歌手删除 UI

**Files:** Modify `web/src/components/ArtistBrowser.vue`、`web/src/App.vue`

> 先读 ArtistBrowser.vue（`defineProps`、选中歌手 `selectedArtist`、布局）。

- [ ] **Step 1: ArtistBrowser.vue** — 加 `isAdmin` prop + 删除 UI：
  - `defineProps` 增 `isAdmin?: boolean`（默认 false）、确保有 `api: ApiClient`（若没有则加，并由 App 传入）；`defineEmits` 增 `(e:'deleted', fileErrors: string[]): void`。
  - 在选中歌手的标题区，仅管理员可见「删除歌手」按钮 → 内联确认区,文案**强调**「将删除该歌手的全部专辑与曲目，不可恢复」+ 勾选「同时删除硬盘文件」。
  - 脚本同 AlbumDetail：`confirmingDelete`/`alsoDeleteFiles`；`doDelete` 调 `props.api.deleteArtist(selectedArtist.id, alsoDeleteFiles)`，成功 `emit('deleted', res.fileErrors||[])`。
  - 若 ArtistBrowser 当前无 `api` prop：在 `defineProps` 增 `api: ApiClient`，并在 App 的 `<ArtistBrowser>` 上加 `:api="api"`。

- [ ] **Step 2: App.vue 接线** — `<ArtistBrowser>` 加 `:is-admin="currentUser?.isAdmin ?? false"`、（如需）`:api="api"`、`@deleted="onArtistDeleted"`：
```ts
async function onArtistDeleted(fileErrors: string[]) {
  selectedArtist.value = null
  await Promise.all([loadArtists(), loadAlbums()])
  if (fileErrors && fileErrors.length) {
    globalError.value = `已从库删除，但 ${fileErrors.length} 个文件未能删除（音乐目录可能为只读挂载）`
  }
}
```

- [ ] **Step 3: 构建验证** — `make build-frontend && go build ./...` → 通过。

- [ ] **Step 4: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web/src && git commit -m "feat(web): 歌手删除入口（管理员,强确认+可选删文件）"
```

---

## Self-Review（计划自检）

- **Spec 覆盖**：删专辑(T1)、删歌手连带专辑/曲目(T2)、外键顺序删 + starred 孤儿清(T1/T2 的 deleteTracksByIDs 及各 DELETE)、可选删文件尽力而为(removeFiles + writeDeleteResult)、管理员守卫(T3 r.With(RequireAdmin))、前端管理员可见按钮 + 确认勾选 + 删后刷新清选中(T4/T5) ✓；只读挂载导致文件删失败时 DB 仍清理且响应报错(removeFiles 收集 fileErrors，前端 globalError 提示) ✓。
- **占位符**：T1 测试里误留的空 `seedAlbumWithDeps` 已注明删除；无 TODO/TBD；所有步骤含完整代码。
- **类型一致**：`deleteTracksByIDs(tx,ids)`、`collectTracks(db,query,args...)→(ids,paths,err)`、`removeFiles(paths)→(int,[]string)`、`writeDeleteResult(w,r,paths)` 跨 T1/T2 一致；`AlbumsHandler.DeleteAlbum`/`ArtistsHandler.DeleteArtist`(T1/T2) 与 router(T3) 一致；端点路径 `/api/v1/albums|artists/{id}` + `?deleteFiles=` 与前端 client(T4) 一致；前端 `@deleted` 携带 `fileErrors: string[]`，App 的 onAlbumDeleted/onArtistDeleted 据此提示。
- **已知约束**：删文件在只读挂载下会进入 fileErrors（预期）；播放队列 `play_queue.track_ids` 字符串可能残留已删 track id，getPlayQueue 的 childByID 会跳过（无害，不处理）。
