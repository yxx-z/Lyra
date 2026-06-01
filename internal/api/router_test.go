// internal/api/router_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/scanner"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	return newTestRouterWithAuth(t, config.AuthConfig{Disable: true})
}

func newTestRouterWithAuth(t *testing.T, auth config.AuthConfig) http.Handler {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := scanner.NewScanner(d, config.LibraryConfig{}, "", nil, false)
	cfg := &config.Config{Auth: auth}
	return NewRouter(s, d, cfg)
}

func TestHealth_Returns200WithStatusOK(t *testing.T) {
	r := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("解析响应体失败: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("期望 status=ok，实际 %q", body["status"])
	}
	if body["version"] == "" {
		t.Errorf("version 字段不应为空")
	}
}

func TestProtectedRoute_RequiresBearerToken(t *testing.T) {
	r := newTestRouterWithAuth(t, config.AuthConfig{Token: "secret"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("期望状态码 401，实际 %d", w.Code)
	}
}

func TestProtectedRoute_AcceptsBearerToken(t *testing.T) {
	r := newTestRouterWithAuth(t, config.AuthConfig{Token: "secret"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", w.Code)
	}
}
