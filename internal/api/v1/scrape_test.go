package v1

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/lyrics"
)

type fakeLyricsProvider struct {
	queries []lyrics.Query
	result  lyrics.Result
	err     error
}

func (p *fakeLyricsProvider) Fetch(ctx context.Context, q lyrics.Query) (lyrics.Result, error) {
	p.queries = append(p.queries, q)
	return p.result, p.err
}

func TestScrapeTrack_FetchesLyricsFromProvider(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	provider := &fakeLyricsProvider{
		result: lyrics.Result{
			LRCContent: "[00:01.00]让我与你握别",
			Source:     "lrclib",
		},
	}
	h := NewScrapeHandler(d, provider)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/scrape", nil)
	w := httptest.NewRecorder()
	h.scrapeTrack(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if len(provider.queries) != 1 {
		t.Fatalf("want one lyrics query, got %d", len(provider.queries))
	}
	q := provider.queries[0]
	if q.TrackName != "渡口" {
		t.Errorf("TrackName: want 渡口, got %q", q.TrackName)
	}
	if q.ArtistName != "蔡琴" {
		t.Errorf("ArtistName: want 蔡琴, got %q", q.ArtistName)
	}
	if q.AlbumName != "金片子" {
		t.Errorf("AlbumName: want 金片子, got %q", q.AlbumName)
	}
	if q.Duration != 245 {
		t.Errorf("Duration: want 245, got %d", q.Duration)
	}

	var content, source, status string
	if err := d.QueryRow(`SELECT lrc_content, source FROM lyrics WHERE track_id='t1'`).Scan(&content, &source); err != nil {
		t.Fatalf("query lyrics: %v", err)
	}
	if content != "[00:01.00]让我与你握别" {
		t.Errorf("content mismatch: %q", content)
	}
	if source != "lrclib" {
		t.Errorf("source: want lrclib, got %q", source)
	}
	if err := d.QueryRow(`SELECT scrape_status FROM tracks WHERE id='t1'`).Scan(&status); err != nil {
		t.Fatalf("query scrape_status: %v", err)
	}
	if status != "done" {
		t.Errorf("scrape_status: want done, got %q", status)
	}
}

func TestScrapeTrack_SkipsExistingLyrics(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	if _, err := d.Exec(
		`INSERT INTO lyrics(track_id,lrc_content,source,updated_at) VALUES('t1','[00:01.00]手工歌词','manual',CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("insert lyrics: %v", err)
	}
	provider := &fakeLyricsProvider{
		result: lyrics.Result{LRCContent: "[00:01.00]网络歌词", Source: "lrclib"},
	}
	h := NewScrapeHandler(d, provider)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/scrape", nil)
	w := httptest.NewRecorder()
	h.scrapeTrack(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if len(provider.queries) != 0 {
		t.Fatalf("provider should not be called when lyrics already exist")
	}
	var content, source string
	if err := d.QueryRow(`SELECT lrc_content, source FROM lyrics WHERE track_id='t1'`).Scan(&content, &source); err != nil {
		t.Fatalf("query lyrics: %v", err)
	}
	if content != "[00:01.00]手工歌词" {
		t.Errorf("manual lyrics should be preserved, got %q", content)
	}
	if source != "manual" {
		t.Errorf("source should stay manual, got %q", source)
	}
}

func TestScrapeTrack_MarksFailedWhenLyricsNotFound(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	provider := &fakeLyricsProvider{err: lyrics.ErrNotFound}
	h := NewScrapeHandler(d, provider)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/scrape", nil)
	w := httptest.NewRecorder()
	h.scrapeTrack(w, req, "t1")

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
	var status string
	if err := d.QueryRow(`SELECT scrape_status FROM tracks WHERE id='t1'`).Scan(&status); err != nil {
		t.Fatalf("query scrape_status: %v", err)
	}
	if status != "failed" {
		t.Errorf("scrape_status: want failed, got %q", status)
	}
}

func TestScrapeTrack_Returns404WhenTrackMissing(t *testing.T) {
	d := newTestDB(t)
	h := NewScrapeHandler(d, &fakeLyricsProvider{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/missing/scrape", nil)
	w := httptest.NewRecorder()
	h.scrapeTrack(w, req, "missing")

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestScrapeTrack_Returns502WhenProviderFails(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewScrapeHandler(d, &fakeLyricsProvider{err: errors.New("provider down")})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/scrape", nil)
	w := httptest.NewRecorder()
	h.scrapeTrack(w, req, "t1")

	if w.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", w.Code)
	}
}
