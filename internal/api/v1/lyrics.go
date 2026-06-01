package v1

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// LyricsResponse is returned by track lyrics endpoints.
type LyricsResponse struct {
	TrackID    string `json:"track_id"`
	LRCContent string `json:"lrc_content"`
	YRCContent string `json:"yrc_content"`
	Source     string `json:"source"`
	UpdatedAt  string `json:"updated_at"`
	HasLRC     bool   `json:"has_lrc"`
	HasYRC     bool   `json:"has_yrc"`
}

// LyricsRequest is accepted by PUT /api/v1/tracks/{id}/lyrics.
type LyricsRequest struct {
	LRCContent string `json:"lrc_content"`
	YRCContent string `json:"yrc_content"`
	Source     string `json:"source"`
}

// LyricsHandler handles /api/v1/tracks/{id}/lyrics endpoints.
type LyricsHandler struct {
	db *sql.DB
}

// NewLyricsHandler creates a LyricsHandler backed by db.
func NewLyricsHandler(db *sql.DB) *LyricsHandler {
	return &LyricsHandler{db: db}
}

// GetLyrics handles GET /api/v1/tracks/{id}/lyrics.
func (h *LyricsHandler) GetLyrics(w http.ResponseWriter, r *http.Request) {
	h.getLyrics(w, r, chi.URLParam(r, "id"))
}

// PutLyrics handles PUT /api/v1/tracks/{id}/lyrics.
func (h *LyricsHandler) PutLyrics(w http.ResponseWriter, r *http.Request) {
	h.putLyrics(w, r, chi.URLParam(r, "id"))
}

// DeleteLyrics handles DELETE /api/v1/tracks/{id}/lyrics.
func (h *LyricsHandler) DeleteLyrics(w http.ResponseWriter, r *http.Request) {
	h.deleteLyrics(w, r, chi.URLParam(r, "id"))
}

func (h *LyricsHandler) getLyrics(w http.ResponseWriter, r *http.Request, trackID string) {
	exists, err := h.trackAvailable(trackID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}

	resp, err := h.fetchLyrics(trackID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeLyricsJSON(w, resp)
}

func (h *LyricsHandler) putLyrics(w http.ResponseWriter, r *http.Request, trackID string) {
	exists, err := h.trackAvailable(trackID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}

	var req LyricsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if strings.TrimSpace(req.LRCContent) == "" && strings.TrimSpace(req.YRCContent) == "" {
		writeJSONError(w, http.StatusBadRequest, "歌词内容不能为空")
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "manual"
	}

	_, err = h.db.Exec(`
		INSERT INTO lyrics(track_id,lrc_content,yrc_content,source,updated_at)
		VALUES(?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(track_id) DO UPDATE SET
			lrc_content=excluded.lrc_content,
			yrc_content=excluded.yrc_content,
			source=excluded.source,
			updated_at=CURRENT_TIMESTAMP`,
		trackID, req.LRCContent, req.YRCContent, source,
	)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "保存失败")
		return
	}

	resp, err := h.fetchLyrics(trackID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeLyricsJSON(w, resp)
}

func (h *LyricsHandler) deleteLyrics(w http.ResponseWriter, r *http.Request, trackID string) {
	exists, err := h.trackAvailable(trackID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}

	if _, err := h.db.Exec(`DELETE FROM lyrics WHERE track_id=?`, trackID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *LyricsHandler) trackAvailable(trackID string) (bool, error) {
	var exists int
	err := h.db.QueryRow(`SELECT 1 FROM tracks WHERE id=? AND is_available=1`, trackID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (h *LyricsHandler) fetchLyrics(trackID string) (LyricsResponse, error) {
	var resp LyricsResponse
	err := h.db.QueryRow(`
		SELECT track_id, COALESCE(lrc_content,''), COALESCE(yrc_content,''),
		       COALESCE(source,''), COALESCE(updated_at,'')
		FROM lyrics
		WHERE track_id=?`, trackID).
		Scan(&resp.TrackID, &resp.LRCContent, &resp.YRCContent, &resp.Source, &resp.UpdatedAt)
	if err != nil {
		return LyricsResponse{}, err
	}
	resp.HasLRC = strings.TrimSpace(resp.LRCContent) != ""
	resp.HasYRC = strings.TrimSpace(resp.YRCContent) != ""
	return resp, nil
}

func writeLyricsJSON(w http.ResponseWriter, resp LyricsResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("写歌词响应失败", "err", err)
	}
}
