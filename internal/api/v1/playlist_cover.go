// internal/api/v1/playlist_cover.go
package v1

import (
	"database/sql"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
)

// PlaylistCoverHandler 处理歌单封面：自定义优先，否则回退首曲专辑封面。
type PlaylistCoverHandler struct {
	db         *sql.DB
	artworkDir string
	cover      *CoverHandler
}

func NewPlaylistCoverHandler(db *sql.DB, artworkDir string, cover *CoverHandler) *PlaylistCoverHandler {
	return &PlaylistCoverHandler{db: db, artworkDir: artworkDir, cover: cover}
}

const maxCoverBytes = 5 << 20 // 5MB

// ownerCoverPath 校验属主并返回该歌单的自定义封面路径；非属主/不存在返回 ok=false。
func (h *PlaylistCoverHandler) ownerCoverPath(r *http.Request, id string) (uid, coverPath string, ok bool) {
	u, okUser := middleware.UserFromContext(r.Context())
	if !okUser {
		return "", "", false
	}
	err := h.db.QueryRow(`SELECT cover_path FROM playlists WHERE id=? AND user_id=?`, id, u.ID).Scan(&coverPath)
	if err != nil {
		return "", "", false
	}
	return u.ID, coverPath, true
}

func serveImageFile(w http.ResponseWriter, path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return false
	}
	ct := "image/jpeg"
	if strings.HasSuffix(strings.ToLower(path), ".png") {
		ct = "image/png"
	}
	w.Header().Set("Content-Type", ct)
	_, _ = w.Write(data)
	return true
}

// Get 处理 GET /api/v1/playlists/{id}/cover。
func (h *PlaylistCoverHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	_, coverPath, ok := h.ownerCoverPath(r, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if coverPath != "" && serveImageFile(w, coverPath) {
		return
	}
	var albumID sql.NullString
	err := h.db.QueryRow(
		`SELECT t.album_id FROM playlist_tracks pt JOIN tracks t ON t.id=pt.track_id
		 WHERE pt.playlist_id=? AND t.is_available=1 ORDER BY pt.position LIMIT 1`, id,
	).Scan(&albumID)
	if err != nil || !albumID.Valid || albumID.String == "" {
		http.NotFound(w, r)
		return
	}
	h.cover.ServeCover(w, r, albumID.String)
}

// Put 处理 PUT /api/v1/playlists/{id}/cover（multipart，字段名 cover）。
func (h *PlaylistCoverHandler) Put(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	uid, existing, ok := h.ownerCoverPath(r, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCoverBytes)
	file, _, err := r.FormFile("cover")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "未提供图片或图片过大（≤5MB）")
		return
	}
	defer file.Close()

	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	var ext string
	switch http.DetectContentType(head[:n]) {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	default:
		writeJSONError(w, http.StatusBadRequest, "仅支持 JPEG/PNG")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "读取图片失败")
		return
	}

	if err := os.MkdirAll(h.artworkDir, 0o755); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建封面目录失败")
		return
	}
	dst := filepath.Join(h.artworkDir, "playlist_"+id+ext)
	if existing != "" && existing != dst {
		_ = os.Remove(existing)
	}
	out, err := os.Create(dst)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "保存封面失败")
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		writeJSONError(w, http.StatusInternalServerError, "写入封面失败")
		return
	}
	out.Close()

	if _, err := h.db.Exec(
		`UPDATE playlists SET cover_path=?, updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`,
		dst, id, uid,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "更新封面失败")
		return
	}
	writeJSON(w, map[string]string{"cover_url": "/api/v1/playlists/" + id + "/cover"})
}

// Delete 处理 DELETE /api/v1/playlists/{id}/cover —— 删自定义图、恢复自动封面。
func (h *PlaylistCoverHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	uid, existing, ok := h.ownerCoverPath(r, id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if existing != "" {
		_ = os.Remove(existing)
	}
	if _, err := h.db.Exec(`UPDATE playlists SET cover_path='' WHERE id=? AND user_id=?`, id, uid); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "更新封面失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
