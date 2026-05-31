// internal/api/v1/library.go
package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/yxx-z/lyra/internal/scanner"
)

// LibraryHandler handles /api/v1/library/* endpoints.
type LibraryHandler struct {
	scanner *scanner.Scanner
}

// NewLibraryHandler creates a handler backed by s.
func NewLibraryHandler(s *scanner.Scanner) *LibraryHandler {
	return &LibraryHandler{scanner: s}
}

// TriggerScan handles POST /api/v1/library/scan.
func (h *LibraryHandler) TriggerScan(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := h.scanner.TriggerScan(); err != nil {
		if errors.Is(err, scanner.ErrScanInProgress) {
			w.WriteHeader(http.StatusConflict)
			if err2 := json.NewEncoder(w).Encode(map[string]string{"error": "扫描正在进行中"}); err2 != nil {
				slog.Error("写响应失败", "err", err2)
			}
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}

// ScanStatus handles GET /api/v1/library/scan/status.
func (h *LibraryHandler) ScanStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(h.scanner.Status()); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
