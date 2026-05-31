// internal/api/v1/artists.go
package v1

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// ArtistSummary is returned in the artist list endpoint.
type ArtistSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AlbumCount int    `json:"album_count"`
}

// ArtistDetail is returned by the single artist endpoint.
type ArtistDetail struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Albums []AlbumSummary `json:"albums"`
}

// ArtistsHandler handles /api/v1/artists/* endpoints.
type ArtistsHandler struct {
	db *sql.DB
}

// NewArtistsHandler creates an ArtistsHandler backed by db.
func NewArtistsHandler(db *sql.DB) *ArtistsHandler {
	return &ArtistsHandler{db: db}
}

// ListArtists handles GET /api/v1/artists.
func (h *ArtistsHandler) ListArtists(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`
		SELECT a.id, a.name, COUNT(al.id)
		FROM artists a
		LEFT JOIN albums al ON al.artist_id = a.id
		GROUP BY a.id
		ORDER BY a.name`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	artists := make([]ArtistSummary, 0)
	for rows.Next() {
		var a ArtistSummary
		if err := rows.Scan(&a.ID, &a.Name, &a.AlbumCount); err != nil {
			slog.Error("扫描艺术家失败", "err", err)
			continue
		}
		artists = append(artists, a)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string][]ArtistSummary{"artists": artists}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}

// GetArtist handles GET /api/v1/artists/:id.
func (h *ArtistsHandler) GetArtist(w http.ResponseWriter, r *http.Request) {
	h.getArtist(w, r, chi.URLParam(r, "id"))
}

func (h *ArtistsHandler) getArtist(w http.ResponseWriter, r *http.Request, id string) {
	var ar ArtistDetail
	err := h.db.QueryRow(`SELECT id, name FROM artists WHERE id = ?`, id).
		Scan(&ar.ID, &ar.Name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}

	rows, err := h.db.Query(`
		SELECT al.id, al.title, COALESCE(ar2.name,''), COALESCE(al.artist_id,''),
		       COALESCE(al.release_date,''), COUNT(t.id)
		FROM albums al
		LEFT JOIN artists ar2 ON al.artist_id = ar2.id
		LEFT JOIN tracks t ON t.album_id = al.id AND t.is_available = 1
		WHERE al.artist_id = ?
		GROUP BY al.id
		ORDER BY al.release_date DESC, al.title`, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询专辑失败")
		return
	}
	defer rows.Close()

	ar.Albums = make([]AlbumSummary, 0)
	for rows.Next() {
		var al AlbumSummary
		var releaseDate string
		if err := rows.Scan(&al.ID, &al.Title, &al.Artist, &al.ArtistID, &releaseDate, &al.TrackCount); err != nil {
			slog.Error("扫描专辑失败", "err", err)
			continue
		}
		al.Year, _ = strconv.Atoi(releaseDate)
		al.CoverURL = "/api/v1/cover/" + al.ID
		ar.Albums = append(ar.Albums, al)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询专辑失败")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(ar); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
