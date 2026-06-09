// internal/api/v1/cover_test.go
package v1

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGetCover_CoverJpg(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	coverPath := filepath.Join(dir, "cover.jpg")
	if err := os.WriteFile(coverPath, []byte("FAKEJPEG"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','Album','a1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(
		`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t1','T','a1','al1',?,'' ,1,'pending')`,
		filepath.Join(dir, "song.flac"),
	); err != nil {
		t.Fatal(err)
	}

	h := NewCoverHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cover/al1", nil)
	h.ServeCover(w, req, "al1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("want Content-Type image/jpeg, got %q", ct)
	}
}

func TestGetCover_NotFound(t *testing.T) {
	d := newTestDB(t)
	h := NewCoverHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cover/noalbum", nil)
	h.ServeCover(w, req, "noalbum")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestGetCover_CoverPathFallback(t *testing.T) {
	d := newTestDB(t)

	dir := t.TempDir()
	coverFile := filepath.Join(dir, "scraped.jpg")
	if err := os.WriteFile(coverFile, []byte("\xff\xd8\xffJPEG"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('ar','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id,cover_path) VALUES('al','T','ar',?)`, coverFile); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(
		`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('tr','t','ar','al',?,'',1,'pending')`,
		filepath.Join(dir, "song.flac"),
	); err != nil {
		t.Fatal(err)
	}

	h := NewCoverHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cover/al", nil)
	h.ServeCover(w, req, "al")

	if w.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("应返回封面字节")
	}
}
