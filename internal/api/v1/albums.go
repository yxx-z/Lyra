// internal/api/v1/albums.go
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

// AlbumSummary is returned in album list responses.
type AlbumSummary struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	ArtistID   string `json:"artist_id"`
	Year       int    `json:"year"`
	TrackCount int    `json:"track_count"`
	CoverURL   string `json:"cover_url"`
}

// TrackSummary is returned inside an album detail.
type TrackSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	TrackNumber int    `json:"track_number"`
	DiscNumber  int    `json:"disc_number"`
	Duration    int    `json:"duration"`
	Format      string `json:"format"`
	Bitrate     int    `json:"bitrate"`
	StreamURL   string `json:"stream_url"`
}

// AlbumDetail is returned by the single album endpoint.
type AlbumDetail struct {
	AlbumSummary
	Tracks []TrackSummary `json:"tracks"`
}

// AlbumsHandler handles /api/v1/albums/* endpoints.
type AlbumsHandler struct {
	db *sql.DB
}

// NewAlbumsHandler creates an AlbumsHandler backed by db.
func NewAlbumsHandler(db *sql.DB) *AlbumsHandler {
	return &AlbumsHandler{db: db}
}

// ListAlbums handles GET /api/v1/albums.
func (h *AlbumsHandler) ListAlbums(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`
		SELECT a.id, a.title, COALESCE(ar.name,''), COALESCE(a.artist_id,''),
		       COALESCE(a.release_date,''), COUNT(t.id)
		FROM albums a
		LEFT JOIN artists ar ON a.artist_id = ar.id
		LEFT JOIN tracks t ON t.album_id = a.id AND t.is_available = 1
		GROUP BY a.id
		ORDER BY ar.name, a.title`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	albums := make([]AlbumSummary, 0)
	for rows.Next() {
		var al AlbumSummary
		var releaseDate string
		if err := rows.Scan(&al.ID, &al.Title, &al.Artist, &al.ArtistID, &releaseDate, &al.TrackCount); err != nil {
			slog.Error("扫描专辑失败", "err", err)
			continue
		}
		al.Year, _ = strconv.Atoi(releaseDate)
		al.CoverURL = "/api/v1/cover/" + al.ID
		albums = append(albums, al)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string][]AlbumSummary{"albums": albums}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}

// GetAlbum handles GET /api/v1/albums/:id.
func (h *AlbumsHandler) GetAlbum(w http.ResponseWriter, r *http.Request) {
	h.getAlbum(w, r, chi.URLParam(r, "id"))
}

func (h *AlbumsHandler) getAlbum(w http.ResponseWriter, r *http.Request, id string) {
	var al AlbumDetail
	var releaseDate string
	err := h.db.QueryRow(`
		SELECT a.id, a.title, COALESCE(ar.name,''), COALESCE(a.artist_id,''), COALESCE(a.release_date,'')
		FROM albums a
		LEFT JOIN artists ar ON a.artist_id = ar.id
		WHERE a.id = ?`, id).
		Scan(&al.ID, &al.Title, &al.Artist, &al.ArtistID, &releaseDate)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	al.Year, _ = strconv.Atoi(releaseDate)
	al.CoverURL = "/api/v1/cover/" + al.ID

	rows, err := h.db.Query(`
		SELECT id, title, COALESCE(track_number,0), COALESCE(disc_number,1),
		       COALESCE(duration,0), COALESCE(format,''), COALESCE(bitrate,0)
		FROM tracks
		WHERE album_id = ? AND is_available = 1
		ORDER BY disc_number, track_number, title`, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询曲目失败")
		return
	}
	defer rows.Close()

	al.Tracks = make([]TrackSummary, 0)
	for rows.Next() {
		var t TrackSummary
		if err := rows.Scan(&t.ID, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.Duration, &t.Format, &t.Bitrate); err != nil {
			slog.Error("扫描曲目失败", "err", err)
			continue
		}
		t.StreamURL = "/api/v1/tracks/" + t.ID + "/stream"
		al.Tracks = append(al.Tracks, t)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询曲目失败")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(al); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
