// internal/api/v1/search_test.go
package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearch_FindsTrack(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewSearchHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=渡口", nil)
	w := httptest.NewRecorder()
	h.Search(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	tracks := resp["tracks"].([]interface{})
	if len(tracks) != 1 {
		t.Fatalf("want 1 track, got %d", len(tracks))
	}
}

func TestSearch_EmptyQuery_Returns400(t *testing.T) {
	d := newTestDB(t)
	h := NewSearchHandler(d)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	w := httptest.NewRecorder()
	h.Search(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestSearch_FindsArtist(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewSearchHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=蔡琴", nil)
	w := httptest.NewRecorder()
	h.Search(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	artists := resp["artists"].([]interface{})
	if len(artists) != 1 {
		t.Fatalf("want 1 artist, got %d", len(artists))
	}
}
