// internal/api/v1/scrape.go
package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/lyrics"
)

// ScrapeResponse is returned by the scrape endpoint.
type ScrapeResponse struct {
	TrackID string `json:"track_id"`
	Status  string `json:"status"`
	Source  string `json:"source,omitempty"`
	Message string `json:"message,omitempty"`
}

// ScrapeHandler handles track scraping endpoints.
type ScrapeHandler struct {
	service *lyrics.LyricsService
}

// NewScrapeHandler creates a ScrapeHandler backed by a LyricsService.
func NewScrapeHandler(service *lyrics.LyricsService) *ScrapeHandler {
	return &ScrapeHandler{service: service}
}

// ScrapeTrack handles POST /api/v1/tracks/{id}/scrape.
func (h *ScrapeHandler) ScrapeTrack(w http.ResponseWriter, r *http.Request) {
	h.scrapeTrack(w, r, chi.URLParam(r, "id"))
}

func (h *ScrapeHandler) scrapeTrack(w http.ResponseWriter, r *http.Request, trackID string) {
	if h.service == nil {
		writeJSONError(w, http.StatusBadGateway, "歌词刮削源不可用")
		return
	}
	outcome, err := h.service.ScrapeTrack(r.Context(), trackID)
	if err != nil {
		if errors.Is(err, lyrics.ErrTrackNotFound) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "歌词刮削失败")
		return
	}
	if outcome.Status == "failed" {
		writeJSONError(w, http.StatusNotFound, "未找到歌词")
		return
	}
	writeScrapeJSON(w, ScrapeResponse{
		TrackID: trackID,
		Status:  outcome.Status,
		Source:  outcome.Source,
	})
}

// UpgradeLyrics handles POST /api/v1/tracks/{id}/lyrics/upgrade.
func (h *ScrapeHandler) UpgradeLyrics(w http.ResponseWriter, r *http.Request) {
	h.upgradeLyrics(w, r, chi.URLParam(r, "id"))
}

func (h *ScrapeHandler) upgradeLyrics(w http.ResponseWriter, r *http.Request, trackID string) {
	if h.service == nil {
		writeJSONError(w, http.StatusBadGateway, "歌词刮削源不可用")
		return
	}
	outcome, err := h.service.UpgradeToSynced(r.Context(), trackID)
	if err != nil {
		if errors.Is(err, lyrics.ErrTrackNotFound) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "同步歌词升级失败")
		return
	}
	writeScrapeJSON(w, ScrapeResponse{
		TrackID: trackID,
		Status:  outcome.Status,
		Source:  outcome.Source,
	})
}

func writeScrapeJSON(w http.ResponseWriter, resp ScrapeResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("写刮削响应失败", "err", err)
	}
}
