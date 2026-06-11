package subsonic

import "net/http"

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	// 复用 v1 转码/直传管线（按 trackID）；不存在曲目时 v1 写 404。
	h.streamH.StreamByID(w, r, r.Form.Get("id"))
}

func (h *Handler) getCoverArt(w http.ResponseWriter, r *http.Request) {
	// 复用 v1 封面优先级（内嵌→本地→cover_path）；找不到写 404。
	h.cover.ServeCover(w, r, r.Form.Get("id"))
}

func (h *Handler) scrobble(w http.ResponseWriter, r *http.Request) {
	if id := r.Form.Get("id"); id != "" {
		if u := userFromCtx(r.Context()); u != nil {
			_ = h.store.RecordPlay(u.ID, id)
		}
	}
	writeResponse(w, r, &Response{})
}
