package subsonic

import (
	"errors"
	"net/http"
	"strconv"

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

func (h *Handler) createPlaylist(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	songIDs := r.Form["songId"]
	id := r.Form.Get("playlistId")
	if id != "" {
		// playlistId 已存在：替换曲目
		if err := h.pl.ReplaceTracks(u.ID, id, songIDs); err != nil {
			if errors.Is(err, playlists.ErrNotFound) {
				writeError(w, r, 70, "歌单不存在")
			} else {
				writeError(w, r, 0, "保存失败")
			}
			return
		}
	} else {
		// 新建歌单
		newID, err := h.pl.Create(u.ID, r.Form.Get("name"))
		if err != nil {
			writeError(w, r, 0, "创建失败")
			return
		}
		if len(songIDs) > 0 {
			_ = h.pl.ReplaceTracks(u.ID, newID, songIDs)
		}
		id = newID
	}
	h.writePlaylistWithSongs(w, r, u, id)
}

func (h *Handler) updatePlaylist(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	id := r.Form.Get("playlistId")
	// 更新元信息（名称/备注）
	if name, comment := r.Form.Get("name"), r.Form.Get("comment"); name != "" || comment != "" {
		if err := h.pl.UpdateMeta(u.ID, id, name, comment); err != nil {
			if errors.Is(err, playlists.ErrNotFound) {
				writeError(w, r, 70, "歌单不存在")
			} else {
				writeError(w, r, 0, "更新失败")
			}
			return
		}
	}
	// 按下标删除曲目
	if idxStrs := r.Form["songIndexToRemove"]; len(idxStrs) > 0 {
		indices := make([]int, 0, len(idxStrs))
		for _, s := range idxStrs {
			if n, err := strconv.Atoi(s); err == nil {
				indices = append(indices, n)
			}
		}
		if err := h.pl.RemoveByIndices(u.ID, id, indices); err != nil {
			if errors.Is(err, playlists.ErrNotFound) {
				writeError(w, r, 70, "歌单不存在")
			} else {
				writeError(w, r, 0, "更新失败")
			}
			return
		}
	}
	// 追加曲目
	if add := r.Form["songIdToAdd"]; len(add) > 0 {
		if err := h.pl.AddTracks(u.ID, id, add); err != nil {
			if errors.Is(err, playlists.ErrNotFound) {
				writeError(w, r, 70, "歌单不存在")
			} else {
				writeError(w, r, 0, "更新失败")
			}
			return
		}
	}
	writeResponse(w, r, &Response{})
}

func (h *Handler) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if err := h.pl.Delete(u.ID, r.Form.Get("id")); err != nil {
		if errors.Is(err, playlists.ErrNotFound) {
			writeError(w, r, 70, "歌单不存在")
		} else {
			writeError(w, r, 0, "删除失败")
		}
		return
	}
	writeResponse(w, r, &Response{})
}
