// internal/api/v1/library_test.go
package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/scanner"
)

func newTestHandler(t *testing.T) *LibraryHandler {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := scanner.NewScanner(d, config.LibraryConfig{}, "", nil, false)
	return NewLibraryHandler(s)
}

func TestTriggerScan_Returns200(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/library/scan", nil)
	w := httptest.NewRecorder()
	h.TriggerScan(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body map[string]bool
	json.NewDecoder(w.Body).Decode(&body)
	if !body["ok"] {
		t.Errorf("want ok=true, got %v", body)
	}
}

func TestScanStatus_Returns200WithFields(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/scan/status", nil)
	w := httptest.NewRecorder()
	h.ScanStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if _, ok := body["running"]; !ok {
		t.Error("response missing 'running' field")
	}
}
