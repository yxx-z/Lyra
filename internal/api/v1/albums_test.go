// internal/api/v1/albums_test.go
package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAlbums_ReturnsAlbums(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewAlbumsHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums", nil)
	w := httptest.NewRecorder()
	h.ListAlbums(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Albums []map[string]interface{} `json:"albums"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Albums) != 1 {
		t.Fatalf("want 1 album, got %d", len(resp.Albums))
	}
	if resp.Albums[0]["title"] != "金片子" {
		t.Errorf("want title=金片子, got %v", resp.Albums[0]["title"])
	}
	if resp.Albums[0]["artist"] != "蔡琴" {
		t.Errorf("want artist=蔡琴, got %v", resp.Albums[0]["artist"])
	}
	if resp.Albums[0]["track_count"].(float64) != 2 {
		t.Errorf("want track_count=2, got %v", resp.Albums[0]["track_count"])
	}
}

func TestGetAlbum_ReturnsTracks(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewAlbumsHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums/al1", nil)
	w := httptest.NewRecorder()
	h.getAlbum(w, req, "al1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["title"] != "金片子" {
		t.Errorf("want title=金片子, got %v", resp["title"])
	}
	tracks := resp["tracks"].([]interface{})
	if len(tracks) != 2 {
		t.Fatalf("want 2 tracks, got %d", len(tracks))
	}
}

func TestGetAlbum_NotFound(t *testing.T) {
	d := newTestDB(t)
	h := NewAlbumsHandler(d)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums/nonexistent", nil)
	h.getAlbum(w, req, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
