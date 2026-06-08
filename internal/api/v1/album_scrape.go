package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/metadata"
)

// AlbumScrapeResponse 是专辑元数据刮削接口的响应。
type AlbumScrapeResponse struct {
	AlbumID  string `json:"album_id"`
	Status   string `json:"status"`
	MBID     string `json:"mbid,omitempty"`
	HasCover bool   `json:"has_cover"`
}

// AlbumScrapeHandler 处理专辑元数据刮削端点。
type AlbumScrapeHandler struct {
	service *metadata.MetadataService
}

// NewAlbumScrapeHandler 创建 handler。
func NewAlbumScrapeHandler(service *metadata.MetadataService) *AlbumScrapeHandler {
	return &AlbumScrapeHandler{service: service}
}

// ScrapeAlbum 处理 POST /api/v1/albums/{id}/scrape。
func (h *AlbumScrapeHandler) ScrapeAlbum(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "id")
	if h.service == nil {
		writeJSONError(w, http.StatusBadGateway, "元数据刮削源不可用")
		return
	}
	outcome, err := h.service.EnrichAlbum(r.Context(), albumID)
	if err != nil {
		if errors.Is(err, metadata.ErrAlbumNotFound) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "元数据刮削失败")
		return
	}
	if outcome.Status == "failed" {
		writeJSONError(w, http.StatusNotFound, "未匹配到专辑")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(AlbumScrapeResponse{
		AlbumID:  albumID,
		Status:   outcome.Status,
		MBID:     outcome.MBID,
		HasCover: outcome.HasCover,
	}); err != nil {
		slog.Error("写专辑刮削响应失败", "err", err)
	}
}
