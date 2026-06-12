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
	artDir := t.TempDir()
	musicFile := filepath.Join(artDir, "song.mp3")
	os.WriteFile(musicFile, []byte("not really mp3"), 0o644)
	coverFile := filepath.Join(filepath.Dir(musicFile), "cover.jpg")
	os.WriteFile(coverFile, jpegBytes(t), 0o644)
	d.Exec(`INSERT INTO albums(id,title) VALUES('al1','专辑')`)
	d.Exec(`INSERT INTO tracks(id,title,album_id,file_path,is_available) VALUES('t1','歌一','al1',?,1)`, musicFile)
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
	if _, err := os.Stat(filepath.Join(artDir, "playlist_p2.jpg")); err != nil {
		t.Fatalf("自定义图未落地: %v", err)
	}
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
	if pcDo(t, r, token, "GET", "/playlists/p1/cover", nil, "").Code != 200 {
		t.Error("删除自定义图后应回退首曲专辑封面")
	}
}
