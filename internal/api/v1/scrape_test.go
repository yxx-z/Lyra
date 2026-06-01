// internal/api/v1/scrape_test.go
package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/lyrics"
)

type stubProvider struct {
	res lyrics.Result
	err error
}

func (s stubProvider) Name() string { return "stub" }
func (s stubProvider) Fetch(_ context.Context, _ lyrics.Query) (lyrics.Result, error) {
	return s.res, s.err
}

func TestScrape_Success(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	svc := lyrics.NewLyricsService(d, stubProvider{res: lyrics.Result{LRCContent: "[00:01.00]hi", Source: "lrclib"}})
	h := NewScrapeHandler(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/scrape", nil)
	h.scrapeTrack(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp ScrapeResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "done" || resp.Source != "lrclib" {
		t.Errorf("got %+v", resp)
	}
}

func TestScrape_NotFoundLyrics(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	svc := lyrics.NewLyricsService(d, stubProvider{err: lyrics.ErrNotFound})
	h := NewScrapeHandler(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/scrape", nil)
	h.scrapeTrack(w, req, "t1")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestScrape_TrackNotFound(t *testing.T) {
	d := newTestDB(t)
	svc := lyrics.NewLyricsService(d)
	h := NewScrapeHandler(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/nope/scrape", nil)
	h.scrapeTrack(w, req, "nope")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
