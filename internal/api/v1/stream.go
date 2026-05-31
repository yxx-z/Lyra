// internal/api/v1/stream.go
package v1

import (
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/config"
)

var audioContentTypes = map[string]string{
	"mp3":  "audio/mpeg",
	"ogg":  "audio/ogg",
	"opus": "audio/ogg",
	"wav":  "audio/wav",
}

// StreamHandler handles GET /api/v1/tracks/:id/stream.
type StreamHandler struct {
	db        *sql.DB
	transcode config.TranscodeConfig
}

// NewStreamHandler creates a StreamHandler backed by db.
func NewStreamHandler(db *sql.DB, transcode ...config.TranscodeConfig) *StreamHandler {
	cfg := config.Default().Transcode
	if len(transcode) > 0 {
		cfg = transcode[0]
	}
	if cfg.FFmpegPath == "" {
		cfg.FFmpegPath = "ffmpeg"
	}
	if cfg.DefaultFormat == "" {
		cfg.DefaultFormat = "mp3"
	}
	if cfg.DefaultBitrate == 0 {
		cfg.DefaultBitrate = 192
	}
	return &StreamHandler{db: db, transcode: cfg}
}

// Stream handles GET /api/v1/tracks/:id/stream.
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	h.stream(w, r, chi.URLParam(r, "id"))
}

func (h *StreamHandler) stream(w http.ResponseWriter, r *http.Request, trackID string) {
	var filePath, format string
	err := h.db.QueryRow(
		`SELECT file_path, format FROM tracks WHERE id=? AND is_available=1`,
		trackID,
	).Scan(&filePath, &format)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	format = strings.ToLower(format)
	if ct, ok := audioContentTypes[format]; ok {
		w.Header().Set("Content-Type", ct)
		http.ServeFile(w, r, filePath)
		return
	}

	h.transcodeToMP3(w, r, filePath)
}

func (h *StreamHandler) transcodeToMP3(w http.ResponseWriter, r *http.Request, filePath string) {
	bitrate := h.transcode.DefaultBitrate
	if bitrate <= 0 {
		bitrate = 192
	}

	cmd := exec.CommandContext(
		r.Context(),
		h.transcode.FFmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-i", filePath,
		"-vn",
		"-codec:a", "libmp3lame",
		"-b:a", strconv.Itoa(bitrate)+"k",
		"-f", "mp3",
		"pipe:1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "转码启动失败")
		return
	}
	if err := cmd.Start(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "转码启动失败")
		return
	}

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := io.Copy(w, stdout); err != nil {
		slog.Warn("转码流写入失败", "err", err)
	}
	if err := cmd.Wait(); err != nil {
		if r.Context().Err() != nil {
			slog.Debug("客户端已停止读取转码流", "err", err)
			return
		}
		slog.Warn("ffmpeg 转码失败", "err", err)
	}
}
