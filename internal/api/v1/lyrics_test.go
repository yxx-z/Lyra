package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLyricsGet_ReturnsSavedLyrics(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	if _, err := d.Exec(
		`INSERT INTO lyrics(track_id,lrc_content,yrc_content,source,updated_at) VALUES('t1','[00:01.00]渡口','','manual',CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("insert lyrics: %v", err)
	}
	h := NewLyricsHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/lyrics", nil)
	w := httptest.NewRecorder()
	h.getLyrics(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp LyricsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TrackID != "t1" {
		t.Errorf("track_id: want t1, got %q", resp.TrackID)
	}
	if resp.LRCContent != "[00:01.00]渡口" {
		t.Errorf("lrc_content mismatch: %q", resp.LRCContent)
	}
	if resp.Source != "manual" {
		t.Errorf("source: want manual, got %q", resp.Source)
	}
	if !resp.HasLRC {
		t.Error("has_lrc: want true")
	}
	if resp.HasYRC {
		t.Error("has_yrc: want false")
	}
	if strings.TrimSpace(resp.UpdatedAt) == "" {
		t.Error("updated_at should be present")
	}
}

func TestLyricsGet_Returns404WhenLyricsMissing(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewLyricsHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/lyrics", nil)
	w := httptest.NewRecorder()
	h.getLyrics(w, req, "t1")

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestLyricsPut_UpsertsManualLyrics(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewLyricsHandler(d)
	body := bytes.NewBufferString(`{"lrc_content":"[00:01.00]渡口\n[00:03.00]让我与你握别","source":" manual "}`)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tracks/t1/lyrics", body)
	w := httptest.NewRecorder()
	h.putLyrics(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp LyricsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TrackID != "t1" {
		t.Errorf("track_id: want t1, got %q", resp.TrackID)
	}
	if resp.Source != "manual" {
		t.Errorf("source: want trimmed manual, got %q", resp.Source)
	}
	if !resp.HasLRC {
		t.Error("has_lrc: want true")
	}

	var savedContent, savedSource string
	if err := d.QueryRow(`SELECT lrc_content, source FROM lyrics WHERE track_id='t1'`).Scan(&savedContent, &savedSource); err != nil {
		t.Fatalf("query saved lyrics: %v", err)
	}
	if savedContent != "[00:01.00]渡口\n[00:03.00]让我与你握别" {
		t.Errorf("saved lrc_content mismatch: %q", savedContent)
	}
	if savedSource != "manual" {
		t.Errorf("saved source: want manual, got %q", savedSource)
	}
}

func TestLyricsPut_DefaultsSourceToManual(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewLyricsHandler(d)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tracks/t1/lyrics", bytes.NewBufferString(`{"yrc_content":"[1,100]渡口"}`))
	w := httptest.NewRecorder()
	h.putLyrics(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp LyricsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Source != "manual" {
		t.Errorf("source: want manual, got %q", resp.Source)
	}
	if !resp.HasYRC {
		t.Error("has_yrc: want true")
	}
}

func TestLyricsPut_RejectsEmptyPayload(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewLyricsHandler(d)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tracks/t1/lyrics", bytes.NewBufferString(`{"lrc_content":"   ","yrc_content":""}`))
	w := httptest.NewRecorder()
	h.putLyrics(w, req, "t1")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestLyricsPut_Returns404WhenTrackMissing(t *testing.T) {
	d := newTestDB(t)
	h := NewLyricsHandler(d)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/tracks/missing/lyrics", bytes.NewBufferString(`{"lrc_content":"[00:01.00]x"}`))
	w := httptest.NewRecorder()
	h.putLyrics(w, req, "missing")

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestLyricsDelete_RemovesLyrics(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	if _, err := d.Exec(
		`INSERT INTO lyrics(track_id,lrc_content,yrc_content,source,updated_at) VALUES('t1','[00:01.00]渡口','','manual',CURRENT_TIMESTAMP)`,
	); err != nil {
		t.Fatalf("insert lyrics: %v", err)
	}
	h := NewLyricsHandler(d)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tracks/t1/lyrics", nil)
	w := httptest.NewRecorder()
	h.deleteLyrics(w, req, "t1")

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", w.Code)
	}
	var count int
	if err := d.QueryRow(`SELECT count(*) FROM lyrics WHERE track_id='t1'`).Scan(&count); err != nil {
		t.Fatalf("count lyrics: %v", err)
	}
	if count != 0 {
		t.Fatalf("want deleted lyrics, got count %d", count)
	}
}
