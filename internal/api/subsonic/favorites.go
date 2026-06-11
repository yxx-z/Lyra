package subsonic

import (
	"net/http"

	"github.com/yxx-z/lyra/internal/userdata"
)

func (h *Handler) star(w http.ResponseWriter, r *http.Request)   { h.setStar(w, r, true) }
func (h *Handler) unstar(w http.ResponseWriter, r *http.Request) { h.setStar(w, r, false) }

// setStar 按 id（歌曲）、albumId、artistId 三类多值参数加/取消收藏。
func (h *Handler) setStar(w http.ResponseWriter, r *http.Request, on bool) {
	u := userFromCtx(r.Context())
	apply := func(itemType string, ids []string) {
		for _, id := range ids {
			if id == "" {
				continue
			}
			if on {
				_ = h.store.Star(u.ID, itemType, id)
			} else {
				_ = h.store.Unstar(u.ID, itemType, id)
			}
		}
	}
	apply(userdata.TypeSong, r.Form["id"])
	apply(userdata.TypeAlbum, r.Form["albumId"])
	apply(userdata.TypeArtist, r.Form["artistId"])
	writeResponse(w, r, &Response{})
}
