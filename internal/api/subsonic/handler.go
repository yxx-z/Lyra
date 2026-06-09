package subsonic

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/config"
)

// Handler 实现 Subsonic /rest 端点。
type Handler struct {
	db      *sql.DB
	cfg     *config.Config
	streamH *v1.StreamHandler // 字段名 streamH 以避开端点方法 stream 的命名冲突
	cover   *v1.CoverHandler
}

// NewHandler 创建 Subsonic handler，复用 v1 的 stream/cover。
func NewHandler(db *sql.DB, cfg *config.Config, stream *v1.StreamHandler, cover *v1.CoverHandler) *Handler {
	return &Handler{db: db, cfg: cfg, streamH: stream, cover: cover}
}

// reg 在 /rest 子路由上注册某端点的 /name 与 /name.view（GET+POST），套认证。
func (h *Handler) reg(r chi.Router, name string, fn http.HandlerFunc) {
	wrapped := h.withAuth(fn)
	r.Get("/"+name, wrapped)
	r.Get("/"+name+".view", wrapped)
	r.Post("/"+name, wrapped)
	r.Post("/"+name+".view", wrapped)
}

// RegisterRoutes 注册本期全部 Subsonic 端点。
// 注意：后续 Task（4/5/6）会往这里增量添加各自端点的 h.reg(...) 行。
func (h *Handler) RegisterRoutes(r chi.Router) {
	h.reg(r, "ping", h.ping)
	h.reg(r, "getLicense", h.getLicense)
	h.reg(r, "getMusicFolders", h.getMusicFolders)
	// Task 4: getArtists/getArtist/getAlbum/getAlbumList2/getSong
	// Task 5: search3
	// Task 6: getCoverArt/stream/scrobble
}

// withAuth 在调用真正 handler 前校验 Subsonic 认证。
func (h *Handler) withAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if e := authenticate(r.Form, h.cfg); e != nil {
			writeError(w, r, e.Code, e.Message)
			return
		}
		fn(w, r)
	}
}

func (h *Handler) ping(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, r, &Response{})
}

func (h *Handler) getLicense(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, r, &Response{License: &License{Valid: true}})
}

func (h *Handler) getMusicFolders(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, r, &Response{MusicFolders: &MusicFolders{
		Folder: []MusicFolder{{ID: 0, Name: "Music"}},
	}})
}
