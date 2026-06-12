package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/lyrics"
)

func TestLyricsSearch_ReturnsCandidates(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"trackName":"罗刹海市","artistName":"刀郎","albumName":"山歌寥哉","duration":268,"instrumental":false,"plainLyrics":"p","syncedLyrics":"[00:01.00]那马户又鸟"},
			{"trackName":"伴奏","artistName":"刀郎","albumName":"山歌寥哉","duration":268,"instrumental":true,"plainLyrics":"","syncedLyrics":""}
		]`))
	}))
	defer upstream.Close()

	h := NewLyricsSearchHandler(lyrics.NewLRCLIBClient(upstream.URL, "", nil))
	r := chi.NewRouter()
	r.Get("/tracks/{id}/lyrics/search", h.Search)

	req := httptest.NewRequest("GET", "/tracks/t1/lyrics/search?trackName=罗刹海市&artistName=刀郎", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("应成功: %d %s", w.Code, w.Body.String())
	}
	b := w.Body.String()
	if !strings.Contains(b, `"candidates"`) || !strings.Contains(b, `"synced":true`) || !strings.Contains(b, "那马户又鸟") {
		t.Errorf("应返回候选(含同步标记+歌词): %s", b)
	}
	if strings.Count(b, `"trackName"`) != 1 {
		t.Errorf("器乐/空歌词候选应跳过，body: %s", b)
	}
}

func TestLyricsSearch_MissingParams(t *testing.T) {
	h := NewLyricsSearchHandler(lyrics.NewLRCLIBClient("http://example.invalid", "", nil))
	r := chi.NewRouter()
	r.Get("/tracks/{id}/lyrics/search", h.Search)
	req := httptest.NewRequest("GET", "/tracks/t1/lyrics/search", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("无参应 400: %d", w.Code)
	}
}
