// internal/api/v1/stream_test.go
package v1

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	h := NewStreamHandler(d)
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
	ffmpeg := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nprintf MP3DATA\n"), 0755); err != nil {
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
	})
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
	h := NewStreamHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/nope/stream", nil)
	h.stream(w, req, "nope")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
