package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/scanner"
)

func TestRouterTrackScrapeRouteRequiresAuth(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	cfg := config.Default()
	cfg.Auth.Token = "test-token"
	s := scanner.NewScanner(d, config.LibraryConfig{}, "")
	router := NewRouter(s, d, cfg)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/scrape", strings.NewReader(""))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 from authenticated scrape route, got %d", w.Code)
	}
}
