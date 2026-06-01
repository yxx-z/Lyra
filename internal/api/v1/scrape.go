package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/lyrics"
)

type lyricsProvider interface {
	Fetch(ctx context.Context, q lyrics.Query) (lyrics.Result, error)
}

type ScrapeResponse struct {
	TrackID string `json:"track_id"`
	Status  string `json:"status"`
	Source  string `json:"source,omitempty"`
	Message string `json:"message,omitempty"`
}

// ScrapeHandler handles track metadata scraping endpoints.
type ScrapeHandler struct {
	db             *sql.DB
	lyricsProvider lyricsProvider
}

func NewScrapeHandler(db *sql.DB, provider lyricsProvider) *ScrapeHandler {
	return &ScrapeHandler{db: db, lyricsProvider: provider}
}

// ScrapeTrack handles POST /api/v1/tracks/{id}/scrape.
func (h *ScrapeHandler) ScrapeTrack(w http.ResponseWriter, r *http.Request) {
	h.scrapeTrack(w, r, chi.URLParam(r, "id"))
}

func (h *ScrapeHandler) scrapeTrack(w http.ResponseWriter, r *http.Request, trackID string) {
	track, err := h.loadTrackForScrape(trackID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}

	hasLyrics, err := h.hasLyrics(trackID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询歌词失败")
		return
	}
	if hasLyrics {
		_ = h.updateScrapeStatus(trackID, "done")
		writeScrapeJSON(w, ScrapeResponse{TrackID: trackID, Status: "skipped", Message: "已有歌词"})
		return
	}
	if h.lyricsProvider == nil {
		_ = h.updateScrapeStatus(trackID, "failed")
		writeJSONError(w, http.StatusBadGateway, "歌词刮削源不可用")
		return
	}

	result, err := h.lyricsProvider.Fetch(r.Context(), lyrics.Query{
		TrackName:  track.Title,
		ArtistName: track.Artist,
		AlbumName:  track.Album,
		Duration:   track.Duration,
	})
	if err != nil {
		_ = h.updateScrapeStatus(trackID, "failed")
		if errors.Is(err, lyrics.ErrNotFound) || errors.Is(err, lyrics.ErrInvalidQuery) {
			writeJSONError(w, http.StatusNotFound, "未找到歌词")
			return
		}
		writeJSONError(w, http.StatusBadGateway, "歌词刮削失败")
		return
	}

	if strings.TrimSpace(result.Source) == "" {
		result.Source = "lrclib"
	}
	if _, err := h.db.Exec(`
		INSERT INTO lyrics(track_id,lrc_content,yrc_content,source,updated_at)
		VALUES(?,?,'',?,CURRENT_TIMESTAMP)
		ON CONFLICT(track_id) DO UPDATE SET
			lrc_content=excluded.lrc_content,
			source=excluded.source,
			updated_at=CURRENT_TIMESTAMP`,
		trackID, result.LRCContent, result.Source,
	); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "保存歌词失败")
		return
	}
	if err := h.updateScrapeStatus(trackID, "done"); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "更新刮削状态失败")
		return
	}

	writeScrapeJSON(w, ScrapeResponse{TrackID: trackID, Status: "done", Source: result.Source})
}

type scrapeTrackInfo struct {
	Title    string
	Artist   string
	Album    string
	Duration int
}

func (h *ScrapeHandler) loadTrackForScrape(trackID string) (scrapeTrackInfo, error) {
	var track scrapeTrackInfo
	err := h.db.QueryRow(`
		SELECT t.title, COALESCE(ar.name,''), COALESCE(al.title,''), COALESCE(t.duration,0)
		FROM tracks t
		LEFT JOIN artists ar ON ar.id = t.artist_id
		LEFT JOIN albums al ON al.id = t.album_id
		WHERE t.id=? AND t.is_available=1`, trackID).
		Scan(&track.Title, &track.Artist, &track.Album, &track.Duration)
	return track, err
}

func (h *ScrapeHandler) hasLyrics(trackID string) (bool, error) {
	var exists int
	err := h.db.QueryRow(`
		SELECT 1
		FROM lyrics
		WHERE track_id=?
		  AND (trim(COALESCE(lrc_content,'')) <> '' OR trim(COALESCE(yrc_content,'')) <> '')`,
		trackID,
	).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (h *ScrapeHandler) updateScrapeStatus(trackID, status string) error {
	_, err := h.db.Exec(`UPDATE tracks SET scrape_status=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, trackID)
	return err
}

func writeScrapeJSON(w http.ResponseWriter, resp ScrapeResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("写刮削响应失败", "err", err)
	}
}
