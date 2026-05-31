// internal/api/v1/cover.go
package v1

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	"github.com/go-chi/chi/v5"
)

// CoverHandler handles GET /api/v1/cover/:id.
type CoverHandler struct {
	db *sql.DB
}

// NewCoverHandler creates a CoverHandler backed by db.
func NewCoverHandler(db *sql.DB) *CoverHandler {
	return &CoverHandler{db: db}
}

// GetCover handles GET /api/v1/cover/:id where id is an album ID.
func (h *CoverHandler) GetCover(w http.ResponseWriter, r *http.Request) {
	h.getCover(w, r, chi.URLParam(r, "id"))
}

func (h *CoverHandler) getCover(w http.ResponseWriter, r *http.Request, albumID string) {
	var filePath string
	err := h.db.QueryRow(
		`SELECT file_path FROM tracks WHERE album_id=? AND is_available=1 LIMIT 1`,
		albumID,
	).Scan(&filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if data, mimeType := extractEmbeddedCover(filePath); len(data) > 0 {
		w.Header().Set("Content-Type", mimeType)
		_, _ = w.Write(data)
		return
	}

	dir := filepath.Dir(filePath)
	for _, name := range []string{
		"cover.jpg", "Cover.jpg", "cover.jpeg", "Cover.jpeg",
		"folder.jpg", "Folder.jpg", "cover.png", "Cover.png",
	} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		mimeType := "image/jpeg"
		if strings.HasSuffix(strings.ToLower(name), ".png") {
			mimeType = "image/png"
		}
		w.Header().Set("Content-Type", mimeType)
		_, _ = w.Write(data)
		return
	}

	http.NotFound(w, r)
}

func extractEmbeddedCover(filePath string) ([]byte, string) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, ""
	}
	defer f.Close()

	tags, err := tag.ReadFrom(f)
	if err != nil {
		return nil, ""
	}
	pic := tags.Picture()
	if pic == nil || len(pic.Data) == 0 {
		return nil, ""
	}
	mimeType := pic.MIMEType
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return pic.Data, mimeType
}
