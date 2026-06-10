// internal/api/v1/stream.go
package v1

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/transcode"
)

// StreamHandler 按 trackID 查库并委托 transcode.Service 输出音频。
type StreamHandler struct {
	db  *sql.DB
	svc *transcode.Service
}

// NewStreamHandler 创建 StreamHandler，复用传入的转码 Service。
func NewStreamHandler(db *sql.DB, svc *transcode.Service) *StreamHandler {
	return &StreamHandler{db: db, svc: svc}
}

// Stream handles GET /api/v1/tracks/:id/stream.
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	h.StreamByID(w, r, chi.URLParam(r, "id"))
}

// StreamByID 按 trackID 查库后委托 Service 直传或转码。
func (h *StreamHandler) StreamByID(w http.ResponseWriter, r *http.Request, trackID string) {
	var filePath, format string
	var bitrate int
	err := h.db.QueryRow(
		`SELECT file_path, COALESCE(format,''), COALESCE(bitrate,0) FROM tracks WHERE id=? AND is_available=1`,
		trackID,
	).Scan(&filePath, &format, &bitrate)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	format = strings.ToLower(format)
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
	}
	h.svc.Serve(w, r, transcode.Source{ID: trackID, Path: filePath, Format: format, Bitrate: bitrate})
}
