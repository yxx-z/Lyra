// internal/api/v1/artists_test.go
package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListArtists_ReturnsArtists(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewArtistsHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artists", nil)
	w := httptest.NewRecorder()
	h.ListArtists(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Artists []map[string]interface{} `json:"artists"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Artists) != 1 {
		t.Fatalf("want 1 artist, got %d", len(resp.Artists))
	}
	if resp.Artists[0]["name"] != "蔡琴" {
		t.Errorf("want name=蔡琴, got %v", resp.Artists[0]["name"])
	}
	if resp.Artists[0]["album_count"].(float64) != 1 {
		t.Errorf("want album_count=1, got %v", resp.Artists[0]["album_count"])
	}
}

func TestGetArtist_ReturnsAlbums(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewArtistsHandler(d)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artists/a1", nil)
	h.getArtist(w, req, "a1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["name"] != "蔡琴" {
		t.Errorf("want name=蔡琴, got %v", resp["name"])
	}
	albums := resp["albums"].([]interface{})
	if len(albums) != 1 {
		t.Fatalf("want 1 album, got %d", len(albums))
	}
}

func TestGetArtist_NotFound(t *testing.T) {
	d := newTestDB(t)
	h := NewArtistsHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artists/nonexistent", nil)
	h.getArtist(w, req, "nonexistent")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
