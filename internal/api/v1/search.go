// internal/api/v1/search.go
package v1

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
)

// TrackResult is a search result for a track.
type TrackResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	AlbumID   string `json:"album_id"`
	Duration  int    `json:"duration"`
	StreamURL string `json:"stream_url"`
}

// AlbumResult is a search result for an album.
type AlbumResult struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	CoverURL string `json:"cover_url"`
}

// ArtistResult is a search result for an artist.
type ArtistResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SearchResponse is the full search response.
type SearchResponse struct {
	Tracks  []TrackResult  `json:"tracks"`
	Albums  []AlbumResult  `json:"albums"`
	Artists []ArtistResult `json:"artists"`
}

// SearchHandler handles GET /api/v1/search.
type SearchHandler struct {
	db *sql.DB
}

// NewSearchHandler creates a SearchHandler backed by db.
func NewSearchHandler(db *sql.DB) *SearchHandler {
	return &SearchHandler{db: db}
}

// Search handles GET /api/v1/search?q=keyword.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSONError(w, http.StatusBadRequest, "参数 q 不能为空")
		return
	}

	like := "%" + q + "%"
	resp := SearchResponse{
		Tracks:  make([]TrackResult, 0),
		Albums:  make([]AlbumResult, 0),
		Artists: make([]ArtistResult, 0),
	}

	resp.Tracks = h.searchTracks(like)
	resp.Albums = h.searchAlbums(like)
	resp.Artists = h.searchArtists(like)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}

func (h *SearchHandler) searchTracks(like string) []TrackResult {
	rows, err := h.db.Query(`
		SELECT t.id, t.title, COALESCE(ar.name,''), COALESCE(al.title,''),
		       COALESCE(t.album_id,''), COALESCE(t.duration,0)
		FROM tracks t
		LEFT JOIN artists ar ON t.artist_id = ar.id
		LEFT JOIN albums al ON t.album_id = al.id
		WHERE t.is_available = 1
		  AND (t.title LIKE ? OR ar.name LIKE ? OR al.title LIKE ?)
		LIMIT 20`, like, like, like)
	if err != nil {
		slog.Error("搜索曲目失败", "err", err)
		return []TrackResult{}
	}
	defer rows.Close()

	tracks := make([]TrackResult, 0)
	for rows.Next() {
		var tr TrackResult
		if err := rows.Scan(&tr.ID, &tr.Title, &tr.Artist, &tr.Album, &tr.AlbumID, &tr.Duration); err != nil {
			slog.Error("扫描曲目搜索结果失败", "err", err)
			continue
		}
		tr.StreamURL = "/api/v1/tracks/" + tr.ID + "/stream"
		tracks = append(tracks, tr)
	}
	return tracks
}

func (h *SearchHandler) searchAlbums(like string) []AlbumResult {
	rows, err := h.db.Query(`
		SELECT a.id, a.title, COALESCE(ar.name,'')
		FROM albums a
		LEFT JOIN artists ar ON a.artist_id = ar.id
		WHERE a.title LIKE ? OR ar.name LIKE ?
		LIMIT 20`, like, like)
	if err != nil {
		slog.Error("搜索专辑失败", "err", err)
		return []AlbumResult{}
	}
	defer rows.Close()

	albums := make([]AlbumResult, 0)
	for rows.Next() {
		var al AlbumResult
		if err := rows.Scan(&al.ID, &al.Title, &al.Artist); err != nil {
			slog.Error("扫描专辑搜索结果失败", "err", err)
			continue
		}
		al.CoverURL = "/api/v1/cover/" + al.ID
		albums = append(albums, al)
	}
	return albums
}

func (h *SearchHandler) searchArtists(like string) []ArtistResult {
	rows, err := h.db.Query(`SELECT id, name FROM artists WHERE name LIKE ? LIMIT 20`, like)
	if err != nil {
		slog.Error("搜索艺术家失败", "err", err)
		return []ArtistResult{}
	}
	defer rows.Close()

	artists := make([]ArtistResult, 0)
	for rows.Next() {
		var ar ArtistResult
		if err := rows.Scan(&ar.ID, &ar.Name); err != nil {
			slog.Error("扫描艺术家搜索结果失败", "err", err)
			continue
		}
		artists = append(artists, ar)
	}
	return artists
}
