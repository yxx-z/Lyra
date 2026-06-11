// internal/api/v1/favorites.go
package v1

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/userdata"
)

// StarHandler 处理 Web 端收藏与播放统计。
type StarHandler struct {
	db    *sql.DB
	store *userdata.Store
}

func NewStarHandler(db *sql.DB, store *userdata.Store) *StarHandler {
	return &StarHandler{db: db, store: store}
}

var validStarType = map[string]bool{
	userdata.TypeSong: true, userdata.TypeAlbum: true, userdata.TypeArtist: true,
}

func (h *StarHandler) Star(w http.ResponseWriter, r *http.Request)   { h.setStar(w, r, true) }
func (h *StarHandler) Unstar(w http.ResponseWriter, r *http.Request) { h.setStar(w, r, false) }

func (h *StarHandler) setStar(w http.ResponseWriter, r *http.Request, on bool) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if !validStarType[req.Type] || req.ID == "" {
		writeJSONError(w, http.StatusBadRequest, "type 或 id 非法")
		return
	}
	var err error
	if on {
		err = h.store.Star(u.ID, req.Type, req.ID)
	} else {
		err = h.store.Unstar(u.ID, req.Type, req.ID)
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "操作失败")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// Scrobble 处理 POST /api/v1/tracks/{id}/scrobble。
func (h *StarHandler) Scrobble(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "缺少 id")
		return
	}
	if err := h.store.RecordPlay(u.ID, id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "记录失败")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

type favTrack struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Album     string `json:"album"`
	AlbumID   string `json:"album_id"`
	Artist    string `json:"artist"`
	Duration  int    `json:"duration"`
	StreamURL string `json:"stream_url"`
	CoverURL  string `json:"cover_url"`
}

func (h *StarHandler) queryTracks(ids []string) []favTrack {
	out := []favTrack{}
	for _, id := range ids {
		var ft favTrack
		err := h.db.QueryRow(`
			SELECT tr.id, tr.title, COALESCE(al.title,''), COALESCE(tr.album_id,''),
			       COALESCE(ar.name,''), COALESCE(tr.duration,0)
			FROM tracks tr
			LEFT JOIN albums al ON al.id=tr.album_id
			LEFT JOIN artists ar ON ar.id=tr.artist_id
			WHERE tr.id=? AND tr.is_available=1`, id).
			Scan(&ft.ID, &ft.Title, &ft.Album, &ft.AlbumID, &ft.Artist, &ft.Duration)
		if err != nil {
			continue
		}
		ft.StreamURL = "/api/v1/tracks/" + ft.ID + "/stream"
		ft.CoverURL = "/api/v1/cover/" + ft.AlbumID
		out = append(out, ft)
	}
	return out
}

type favAlbum struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	CoverURL string `json:"cover_url"`
}

// Favorites 处理 GET /api/v1/favorites：当前用户收藏的歌曲与专辑。
func (h *StarHandler) Favorites(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	songIDs, _ := h.store.StarredIDs(u.ID, userdata.TypeSong)
	albumIDs, _ := h.store.StarredIDs(u.ID, userdata.TypeAlbum)
	albums := []favAlbum{}
	for _, id := range albumIDs {
		var fa favAlbum
		err := h.db.QueryRow(`
			SELECT al.id, al.title, COALESCE(ar.name,'')
			FROM albums al LEFT JOIN artists ar ON ar.id=al.artist_id WHERE al.id=?`, id).
			Scan(&fa.ID, &fa.Title, &fa.Artist)
		if err != nil {
			continue
		}
		fa.CoverURL = "/api/v1/cover/" + fa.ID
		albums = append(albums, fa)
	}
	tracks := h.queryTracks(songIDs)
	writeJSON(w, map[string]any{"tracks": tracks, "albums": albums})
}

// RecentlyPlayed 处理 GET /api/v1/recently-played。
func (h *StarHandler) RecentlyPlayed(w http.ResponseWriter, r *http.Request) { h.playList(w, r, false) }

// MostPlayed 处理 GET /api/v1/most-played。
func (h *StarHandler) MostPlayed(w http.ResponseWriter, r *http.Request) { h.playList(w, r, true) }

func (h *StarHandler) playList(w http.ResponseWriter, r *http.Request, frequent bool) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	var ids []string
	if frequent {
		ids, _ = h.store.FrequentTrackIDs(u.ID, 50)
	} else {
		ids, _ = h.store.RecentTrackIDs(u.ID, 50)
	}
	writeJSON(w, map[string]any{"tracks": h.queryTracks(ids)})
}
