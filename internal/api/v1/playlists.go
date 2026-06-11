// internal/api/v1/playlists.go
package v1

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/playlists"
)

type PlaylistHandler struct {
	db *sql.DB
	pl *playlists.Store
}

func NewPlaylistHandler(db *sql.DB, pl *playlists.Store) *PlaylistHandler {
	return &PlaylistHandler{db: db, pl: pl}
}

type playlistSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Comment   string `json:"comment"`
	SongCount int    `json:"song_count"`
	Duration  int    `json:"duration"`
	Created   string `json:"created"`
	Changed   string `json:"changed"`
}

func toSummary(p playlists.Playlist) playlistSummary {
	return playlistSummary{
		ID: p.ID, Name: p.Name, Comment: p.Comment,
		SongCount: p.SongCount, Duration: p.Duration,
		Created: p.Created, Changed: p.Changed,
	}
}

func (h *PlaylistHandler) user(w http.ResponseWriter, r *http.Request) (string, bool) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return "", false
	}
	return u.ID, true
}

func (h *PlaylistHandler) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, playlists.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "歌单不存在")
		return
	}
	writeJSONError(w, http.StatusInternalServerError, "操作失败")
}

func (h *PlaylistHandler) List(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	list, err := h.pl.List(uid)
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]playlistSummary, 0, len(list))
	for _, p := range list {
		out = append(out, toSummary(p))
	}
	writeJSON(w, map[string]any{"playlists": out})
}

func (h *PlaylistHandler) Create(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "歌单名不能为空")
		return
	}
	id, err := h.pl.Create(uid, req.Name)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, map[string]string{"id": id})
}

func (h *PlaylistHandler) Get(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	p, err := h.pl.Get(uid, id)
	if err != nil {
		h.fail(w, err)
		return
	}
	ids, err := h.pl.TrackIDs(uid, id)
	if err != nil {
		h.fail(w, err)
		return
	}
	sum := toSummary(p)
	writeJSON(w, map[string]any{
		"id": sum.ID, "name": sum.Name, "comment": sum.Comment,
		"song_count": sum.SongCount, "duration": sum.Duration,
		"created": sum.Created, "changed": sum.Changed,
		"tracks": tracksByIDs(h.db, ids),
	})
}

func (h *PlaylistHandler) Update(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    string `json:"name"`
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := h.pl.UpdateMeta(uid, chi.URLParam(r, "id"), req.Name, req.Comment); err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *PlaylistHandler) Delete(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	if err := h.pl.Delete(uid, chi.URLParam(r, "id")); err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *PlaylistHandler) AddTracks(w http.ResponseWriter, r *http.Request) {
	h.mutateTracks(w, r, false)
}

func (h *PlaylistHandler) ReplaceTracks(w http.ResponseWriter, r *http.Request) {
	h.mutateTracks(w, r, true)
}

func (h *PlaylistHandler) mutateTracks(w http.ResponseWriter, r *http.Request, replace bool) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	var req struct {
		TrackIds []string `json:"trackIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	id := chi.URLParam(r, "id")
	var err error
	if replace {
		err = h.pl.ReplaceTracks(uid, id, req.TrackIds)
	} else {
		err = h.pl.AddTracks(uid, id, req.TrackIds)
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
