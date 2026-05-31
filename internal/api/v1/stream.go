// internal/api/v1/stream.go
package v1

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
)

var audioContentTypes = map[string]string{
	"mp3":  "audio/mpeg",
	"flac": "audio/flac",
	"m4a":  "audio/mp4",
	"ogg":  "audio/ogg",
	"opus": "audio/ogg",
	"wav":  "audio/wav",
	"aiff": "audio/aiff",
	"aif":  "audio/aiff",
	"wma":  "audio/x-ms-wma",
}

// StreamHandler handles GET /api/v1/tracks/:id/stream.
type StreamHandler struct {
	db *sql.DB
}

// NewStreamHandler creates a StreamHandler backed by db.
func NewStreamHandler(db *sql.DB) *StreamHandler {
	return &StreamHandler{db: db}
}

// Stream handles GET /api/v1/tracks/:id/stream.
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	h.stream(w, r, chi.URLParam(r, "id"))
}

func (h *StreamHandler) stream(w http.ResponseWriter, r *http.Request, trackID string) {
	var filePath, format string
	err := h.db.QueryRow(
		`SELECT file_path, format FROM tracks WHERE id=? AND is_available=1`,
		trackID,
	).Scan(&filePath, &format)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ct, ok := audioContentTypes[format]
	if !ok {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	http.ServeFile(w, r, filePath)
}
