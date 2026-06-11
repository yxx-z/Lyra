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
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/userdata"
)

// yearFromReleaseDate 取 release_date 前 4 位为年份；兼容 "2003" 与 "2003-07-31"。
func yearFromReleaseDate(releaseDate string) int {
	if len(releaseDate) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(releaseDate[:4])
	return y
}

// AlbumSummary is returned in album list responses.
type AlbumSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	ArtistID    string `json:"artist_id"`
	Year        int    `json:"year"`
	Genre       string `json:"genre"`
	ReleaseDate string `json:"release_date"`
	TrackCount  int    `json:"track_count"`
	CoverURL    string `json:"cover_url"`
	Starred     bool   `json:"starred"`
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
	Starred     bool   `json:"starred"`
}

// AlbumDetail is returned by the single album endpoint.
type AlbumDetail struct {
	AlbumSummary
	Tracks []TrackSummary `json:"tracks"`
}

// AlbumsHandler handles /api/v1/albums/* endpoints.
type AlbumsHandler struct {
	db    *sql.DB
	store *userdata.Store
}

// NewAlbumsHandler creates an AlbumsHandler backed by db and userdata store.
func NewAlbumsHandler(db *sql.DB, store *userdata.Store) *AlbumsHandler {
	return &AlbumsHandler{db: db, store: store}
}

// ListAlbums handles GET /api/v1/albums.
func (h *AlbumsHandler) ListAlbums(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`
		SELECT a.id, a.title, COALESCE(ar.name,''), COALESCE(a.artist_id,''),
		       COALESCE(a.release_date,''), COALESCE(a.genre,''), COUNT(t.id)
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
		if err := rows.Scan(&al.ID, &al.Title, &al.Artist, &al.ArtistID, &releaseDate, &al.Genre, &al.TrackCount); err != nil {
			slog.Error("扫描专辑失败", "err", err)
			continue
		}
		al.ReleaseDate = releaseDate
		al.Year = yearFromReleaseDate(releaseDate)
		al.CoverURL = "/api/v1/cover/" + al.ID
		albums = append(albums, al)
	}
	if err := rows.Err(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}

	// rows 已关闭后再查 starred，避免 modernc 单连接约束下的游标冲突。
	if h.store != nil {
		if u, ok := middleware.UserFromContext(r.Context()); ok {
			am, _ := h.store.StarredMap(u.ID, userdata.TypeAlbum)
			for i := range albums {
				_, albums[i].Starred = am[albums[i].ID]
			}
		}
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
		SELECT a.id, a.title, COALESCE(ar.name,''), COALESCE(a.artist_id,''), COALESCE(a.release_date,''), COALESCE(a.genre,'')
		FROM albums a
		LEFT JOIN artists ar ON a.artist_id = ar.id
		WHERE a.id = ?`, id).
		Scan(&al.ID, &al.Title, &al.Artist, &al.ArtistID, &releaseDate, &al.Genre)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	al.ReleaseDate = releaseDate
	al.Year = yearFromReleaseDate(releaseDate)
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
	// 显式关闭 rows，确保下方 StarredMap 可复用连接。
	rows.Close()

	// rows 已关闭后再查 starred，避免 modernc 单连接约束下的游标冲突。
	if h.store != nil {
		if u, ok := middleware.UserFromContext(r.Context()); ok {
			am, _ := h.store.StarredMap(u.ID, userdata.TypeAlbum)
			_, al.Starred = am[al.ID]

			sm, _ := h.store.StarredMap(u.ID, userdata.TypeSong)
			for i := range al.Tracks {
				_, al.Tracks[i].Starred = sm[al.Tracks[i].ID]
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(al); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
