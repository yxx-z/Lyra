// internal/api/v1/stream_test.go
package v1

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/transcode"
)

// fakeMP3FFmpeg 写一个假 ffmpeg：pipe:1 输出到 stdout，否则写文件，内容恒为 MP3DATA。
func fakeMP3FFmpeg(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\nout=\"\"\nfor a in \"$@\"; do out=\"$a\"; done\nif [ \"$out\" = \"pipe:1\" ]; then printf MP3DATA; else printf MP3DATA > \"$out\"; fi\n"
	if err := os.WriteFile(p, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

// seedM4A 插入一首 m4a（模拟浏览器放不出的 ALAC/无损）。
func seedM4A(t *testing.T, d *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.m4a")
	if err := os.WriteFile(audioFile, []byte("RAWALAC"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','B','a1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t1','T','a1','al1',?,'m4a',1,'pending')`, audioFile); err != nil {
		t.Fatal(err)
	}
}

func newStreamHandler(t *testing.T, ffmpegPath string) (*StreamHandler, *sql.DB) {
	t.Helper()
	d := newTestDB(t)
	cache := transcode.NewCache(t.TempDir(), 0)
	svc := transcode.NewService(ffmpegPath, 192, cache)
	return NewStreamHandler(d, svc), d
}

func TestStream_PassthroughMP3(t *testing.T) {
	h, d := newStreamHandler(t, "ffmpeg")
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.mp3")
	if err := os.WriteFile(audioFile, []byte("ORIGINALMP3"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','B','a1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t1','T','a1','al1',?,'mp3',1,'pending')`, audioFile); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream", nil)
	h.StreamByID(w, req, "t1")
	if w.Code != http.StatusOK || w.Body.String() != "ORIGINALMP3" {
		t.Fatalf("直传应原样返回: %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type=%q", ct)
	}
}

func TestStream_TranscodeOnFormatParam(t *testing.T) {
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\nout=\"\"\nfor a in \"$@\"; do out=\"$a\"; done\nif [ \"$out\" = \"pipe:1\" ]; then printf MP3DATA; else printf MP3DATA > \"$out\"; fi\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	h, d := newStreamHandler(t, ffmpeg)
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.flac")
	if err := os.WriteFile(audioFile, []byte("FLACDATA"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','B','a1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t1','T','a1','al1',?,'flac',1,'pending')`, audioFile); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream?format=mp3", nil)
	h.StreamByID(w, req, "t1")
	if w.Code != http.StatusOK || w.Body.String() != "MP3DATA" {
		t.Fatalf("转码应返回 ffmpeg 输出: %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type=%q", ct)
	}
}

func TestStream_NotFound(t *testing.T) {
	h, _ := newStreamHandler(t, "ffmpeg")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/nope/stream", nil)
	h.StreamByID(w, req, "nope")
	if w.Code != http.StatusNotFound {
		t.Errorf("不存在曲目应 404，得到 %d", w.Code)
	}
}

// Web 端点（浏览器）：非原生格式（m4a/ALAC）无参数时应默认转 mp3，而非直传。
func TestStream_WebTranscodesNonNativeFormat(t *testing.T) {
	h, d := newStreamHandler(t, fakeMP3FFmpeg(t))
	seedM4A(t, d)
	r := chi.NewRouter()
	r.Get("/api/v1/tracks/{id}/stream", h.Stream)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "MP3DATA" {
		t.Fatalf("Web 端点应把 m4a 转 mp3: %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type 应为 audio/mpeg，得到 %q", ct)
	}
}

// Subsonic 端点（StreamByID）：非原生格式无参数时仍默认直传（原生客户端能解码）。
func TestStreamByID_PassthroughNonNativeFormat(t *testing.T) {
	h, d := newStreamHandler(t, fakeMP3FFmpeg(t))
	seedM4A(t, d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/rest/stream?id=t1", nil)
	h.StreamByID(w, req, "t1")
	if w.Code != http.StatusOK || w.Body.String() != "RAWALAC" {
		t.Fatalf("Subsonic 端点应直传原文件: %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mp4" {
		t.Errorf("Content-Type 应为 audio/mp4，得到 %q", ct)
	}
}
