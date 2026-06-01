// internal/api/v1/stream_test.go
package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yxx-z/lyra/internal/config"
)

func TestStream_ServesFile(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.mp3")
	if err := os.WriteFile(audioFile, []byte("FAKEMP3DATA"), 0644); err != nil {
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

	h := NewStreamHandler(d, config.TranscodeConfig{}, t.TempDir())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream", nil)
	h.stream(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("want Content-Type audio/mpeg, got %q", ct)
	}
}

func TestStream_TranscodesM4AToMP3(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.m4a")
	if err := os.WriteFile(audioFile, []byte("FAKEM4ADATA"), 0644); err != nil {
		t.Fatal(err)
	}
	// mock ffmpeg：把 MP3DATA 写到最后一个参数（输出文件路径），而非 stdout
	ffmpeg := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\neval \"out=\\${$#}\"\nprintf MP3DATA > \"$out\"\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0755); err != nil {
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

	h := NewStreamHandler(d, config.TranscodeConfig{
		FFmpegPath:     ffmpeg,
		DefaultFormat:  "mp3",
		DefaultBitrate: 192,
	}, t.TempDir())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream", nil)
	h.stream(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("want Content-Type audio/mpeg, got %q", ct)
	}
	if body := w.Body.String(); body != "MP3DATA" {
		t.Errorf("want transcoded body MP3DATA, got %q", body)
	}
}

func TestStream_NotFound(t *testing.T) {
	d := newTestDB(t)
	h := NewStreamHandler(d, config.TranscodeConfig{}, t.TempDir())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/nope/stream", nil)
	h.stream(w, req, "nope")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestStream_TranscodeCacheHit(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.m4a")
	if err := os.WriteFile(audioFile, []byte("FAKEM4ADATA"), 0644); err != nil {
		t.Fatal(err)
	}
	// mock ffmpeg：调用计数器 —— 第二次请求应命中缓存，不再调用
	ffmpeg := filepath.Join(dir, "ffmpeg")
	marker := filepath.Join(dir, "called")
	script := "#!/bin/sh\neval \"out=\\${$#}\"\nprintf MP3DATA > \"$out\"\necho x >> " + marker + "\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0755); err != nil {
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

	cacheDir := t.TempDir()
	h := NewStreamHandler(d, config.TranscodeConfig{FFmpegPath: ffmpeg, DefaultBitrate: 192}, cacheDir)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream", nil)
		h.stream(w, req, "t1")
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i, w.Code)
		}
	}

	// ffmpeg 只应被调用一次（第二次命中缓存）
	data, _ := os.ReadFile(marker)
	if got := strings.Count(string(data), "x"); got != 1 {
		t.Errorf("ffmpeg called %d times, want 1 (second request should hit cache)", got)
	}
}

func TestStream_TranscodeIgnoresCanceledProbeRequest(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.m4a")
	if err := os.WriteFile(audioFile, []byte("FAKEM4ADATA"), 0644); err != nil {
		t.Fatal(err)
	}
	ffmpeg := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\neval \"out=\\${$#}\"\nprintf MP3DATA > \"$out\"\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0755); err != nil {
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

	cacheDir := t.TempDir()
	h := NewStreamHandler(d, config.TranscodeConfig{FFmpegPath: ffmpeg, DefaultBitrate: 192}, cacheDir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	h.stream(w, req, "t1")

	cachePath := h.cache.Path("t1", "mp3", 192)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("want cache file after canceled request, got %v", err)
	}
}
