// internal/api/v1/stream.go
package v1

import (
	"database/sql"
	"net/http"
	"os"
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
	cache     *TranscodeCache
}

// NewStreamHandler creates a StreamHandler backed by db, using transcode config
// and a disk cache rooted at cacheDir.
func NewStreamHandler(db *sql.DB, transcode config.TranscodeConfig, cacheDir string) *StreamHandler {
	if transcode.FFmpegPath == "" {
		transcode.FFmpegPath = "ffmpeg"
	}
	if transcode.DefaultFormat == "" {
		transcode.DefaultFormat = "mp3"
	}
	if transcode.DefaultBitrate == 0 {
		transcode.DefaultBitrate = 192
	}
	return &StreamHandler{
		db:        db,
		transcode: transcode,
		cache:     NewTranscodeCache(cacheDir),
	}
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

	h.serveTranscoded(w, r, trackID, filePath)
}

// serveTranscoded serves an mp3 transcode of filePath, caching the result to
// disk so subsequent requests (and Range/seek) are served via http.ServeFile.
func (h *StreamHandler) serveTranscoded(w http.ResponseWriter, r *http.Request, trackID, filePath string) {
	bitrate := h.transcode.DefaultBitrate
	cachePath := h.cache.Path(trackID, "mp3", bitrate)
	key := h.cache.key(trackID, "mp3", bitrate)

	// 命中缓存：直接 ServeFile（自带 Range）
	if _, err := os.Stat(cachePath); err == nil {
		w.Header().Set("Content-Type", "audio/mpeg")
		http.ServeFile(w, r, cachePath)
		return
	}

	// 未命中：加锁转码（同一曲目并发请求只转一次）
	lock := h.cache.lockFor(key)
	lock.Lock()
	// double-check：可能其他请求已转好
	if _, err := os.Stat(cachePath); err != nil {
		if terr := h.transcodeToFile(r, filePath, cachePath, bitrate); terr != nil {
			lock.Unlock()
			if r.Context().Err() != nil {
				return // 客户端断开
			}
			writeJSONError(w, http.StatusInternalServerError, "转码失败")
			return
		}
	}
	lock.Unlock()

	w.Header().Set("Content-Type", "audio/mpeg")
	http.ServeFile(w, r, cachePath)
}

// transcodeToFile transcodes filePath to mp3 at the given bitrate, writing to a
// temp file then atomically renaming to dst. A failed run cleans up the temp file.
func (h *StreamHandler) transcodeToFile(r *http.Request, filePath, dst string, bitrate int) error {
	if err := os.MkdirAll(h.cache.dir, 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"

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
		"-y",
		tmp,
	)
	if err := cmd.Run(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
