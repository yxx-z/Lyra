package subsonic

import (
	"errors"
	"net/http"

	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/playlists"
)

func (h *Handler) getPlaylists(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	list, err := h.pl.List(u.ID)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	res := &Playlists{Playlist: []Playlist{}}
	for _, p := range list {
		res.Playlist = append(res.Playlist, toPlaylistDTO(p, u.Username, nil))
	}
	writeResponse(w, r, &Response{Playlists: res})
}

func (h *Handler) getPlaylist(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	h.writePlaylistWithSongs(w, r, u, r.Form.Get("id"))
}

// writePlaylistWithSongs 输出单个歌单（含 entry 曲目）；非属主/不存在 → 70。
func (h *Handler) writePlaylistWithSongs(w http.ResponseWriter, r *http.Request, u *auth.User, id string) {
	p, err := h.pl.Get(u.ID, id)
	if errors.Is(err, playlists.ErrNotFound) {
		writeError(w, r, 70, "歌单不存在")
		return
	}
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	ids, _ := h.pl.TrackIDs(u.ID, id)
	var entries []Child
	for _, tid := range ids {
		if c, ok := h.childByID(tid); ok {
			entries = append(entries, c)
		}
	}
	dto := toPlaylistDTO(p, u.Username, entries)
	writeResponse(w, r, &Response{Playlist: &dto})
}

func toPlaylistDTO(p playlists.Playlist, owner string, entries []Child) Playlist {
	return Playlist{
		ID: p.ID, Name: p.Name, Comment: p.Comment,
		Owner: owner, Public: false,
		SongCount: p.SongCount, Duration: p.Duration,
		Created: p.Created, Changed: p.Changed,
		Entry: entries,
	}
}
