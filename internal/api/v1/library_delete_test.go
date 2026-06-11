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
