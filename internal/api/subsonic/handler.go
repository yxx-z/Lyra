package subsonic

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/userdata"
)

// Handler 实现 Subsonic /rest 端点。
type Handler struct {
	db      *sql.DB
	cfg     *config.Config
	streamH *v1.StreamHandler // 字段名 streamH 以避开端点方法 stream 的命名冲突
	cover   *v1.CoverHandler
	users   *auth.UserStore
	key     []byte
	store   *userdata.Store
}

// NewHandler 创建 Subsonic handler，复用 v1 的 stream/cover。
func NewHandler(db *sql.DB, cfg *config.Config, stream *v1.StreamHandler, cover *v1.CoverHandler, users *auth.UserStore, key []byte, store *userdata.Store) *Handler {
	return &Handler{db: db, cfg: cfg, streamH: stream, cover: cover, users: users, key: key, store: store}
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
	h.reg(r, "getArtists", h.getArtists)
	h.reg(r, "getArtist", h.getArtist)
	h.reg(r, "getAlbum", h.getAlbum)
	h.reg(r, "getAlbumList2", h.getAlbumList2)
	h.reg(r, "getSong", h.getSong)
	// Task 5: search3
	h.reg(r, "search3", h.search3)
	// Task 6: getCoverArt/stream/scrobble
	h.reg(r, "getCoverArt", h.getCoverArt)
	h.reg(r, "stream", h.stream)
	h.reg(r, "scrobble", h.scrobble)

	// 客户端启动探测的只读端点（第一期空实现，详见 stubs.go）
	h.reg(r, "getGenres", h.getGenres)
	h.reg(r, "getStarred2", h.getStarred2)
	h.reg(r, "star", h.star)
	h.reg(r, "unstar", h.unstar)
	h.reg(r, "getBookmarks", h.getBookmarks)
	h.reg(r, "createBookmark", h.createBookmark)
	h.reg(r, "deleteBookmark", h.deleteBookmark)
	h.reg(r, "savePlayQueue", h.savePlayQueue)
	h.reg(r, "getPlayQueue", h.getPlayQueue)

	// 兜底：任何未实现的 /rest 端点返回可解析的 Subsonic 错误封套，
	// 而非 chi 默认的纯文本 404（后者会让 Subsonic 客户端 JSON 解析失败）。
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, 0, "未实现的端点")
	})
}

// withAuth 在调用真正 handler 前校验 Subsonic 认证，并将用户注入 context。
func (h *Handler) withAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		u, e := h.authenticate(r.Form)
		if e != nil {
			writeError(w, r, e.Code, e.Message)
			return
		}
		fn(w, r.WithContext(withUser(r.Context(), u)))
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
