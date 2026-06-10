// internal/api/v1/stream.go
package v1

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/transcode"
)

// StreamHandler 按 trackID 查库并委托 transcode.Service 输出音频。
type StreamHandler struct {
	db  *sql.DB
	svc *transcode.Service
}

// NewStreamHandler 创建 StreamHandler，复用传入的转码 Service。
func NewStreamHandler(db *sql.DB, svc *transcode.Service) *StreamHandler {
	return &StreamHandler{db: db, svc: svc}
}

// webNativeFormats 是浏览器 <audio> 普遍能直接解码的格式。其余（m4a/flac/alac/ape…）
// 在 Web 端点默认转 mp3，避免把浏览器放不出来的无损流（如 ALAC）直传过去。
var webNativeFormats = map[string]bool{"mp3": true, "ogg": true, "opus": true, "wav": true}

// Stream handles GET /api/v1/tracks/:id/stream（Web 浏览器播放器）。
// 客户端未显式指定格式/码率、且源不是浏览器原生格式时，默认转 mp3。
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	src, ok := h.lookup(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	p := transcode.ParseParams(r)
	if p.Format == "" && p.MaxBitRate == 0 && !webNativeFormats[src.Format] {
		p.Format = "mp3"
	}
	h.svc.Serve(w, r, src, p)
}

// StreamByID 给 Subsonic 等原生客户端用：按请求参数直传或转码（默认直传，
// 原生客户端能自行解码 ALAC/FLAC，省转码、保音质）。
func (h *StreamHandler) StreamByID(w http.ResponseWriter, r *http.Request, trackID string) {
	src, ok := h.lookup(w, r, trackID)
	if !ok {
		return
	}
	h.svc.Serve(w, r, src, transcode.ParseParams(r))
}

// lookup 按 trackID 查库构造 Source；不存在则写 404 并返回 ok=false。
func (h *StreamHandler) lookup(w http.ResponseWriter, r *http.Request, trackID string) (transcode.Source, bool) {
	var filePath, format string
	var bitrate int
	err := h.db.QueryRow(
		`SELECT file_path, COALESCE(format,''), COALESCE(bitrate,0) FROM tracks WHERE id=? AND is_available=1`,
		trackID,
	).Scan(&filePath, &format, &bitrate)
	if err != nil {
		http.NotFound(w, r)
		return transcode.Source{}, false
	}
	format = strings.ToLower(format)
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
	}
	return transcode.Source{ID: trackID, Path: filePath, Format: format, Bitrate: bitrate}, true
}
