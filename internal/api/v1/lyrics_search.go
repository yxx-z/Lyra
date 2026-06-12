// internal/api/v1/lyrics_search.go
package v1

import (
	"net/http"
	"strings"

	"github.com/yxx-z/lyra/internal/lyrics"
)

// LyricsSearchHandler 用 LRCLIB 模糊搜索返回歌词候选，供前端手动选取。
type LyricsSearchHandler struct {
	client *lyrics.LRCLIBClient
}

func NewLyricsSearchHandler(client *lyrics.LRCLIBClient) *LyricsSearchHandler {
	return &LyricsSearchHandler{client: client}
}

type lyricCandidate struct {
	TrackName  string `json:"trackName"`
	ArtistName string `json:"artistName"`
	AlbumName  string `json:"albumName"`
	Duration   int    `json:"duration"`
	Synced     bool   `json:"synced"`
	LRC        string `json:"lrc"`
}

// Search 处理 GET /api/v1/tracks/{id}/lyrics/search?trackName=&artistName=&albumName=。
// id 仅用于路由归属；搜索用 query 参数。
func (h *LyricsSearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	trackName := q.Get("trackName")
	artistName := q.Get("artistName")
	albumName := q.Get("albumName")
	if strings.TrimSpace(trackName) == "" && strings.TrimSpace(artistName) == "" {
		writeJSONError(w, http.StatusBadRequest, "请至少提供歌名或歌手")
		return
	}
	cands, err := h.client.Search(r.Context(), trackName, artistName, albumName)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "搜索失败")
		return
	}
	out := make([]lyricCandidate, 0, len(cands))
	for _, c := range cands {
		lrc := c.SyncedLyrics
		if lrc == "" {
			lrc = c.PlainLyrics
		}
		if c.Instrumental || lrc == "" {
			continue
		}
		out = append(out, lyricCandidate{
			TrackName:  c.TrackName,
			ArtistName: c.ArtistName,
			AlbumName:  c.AlbumName,
			Duration:   c.Duration,
			Synced:     c.SyncedLyrics != "",
			LRC:        lrc,
		})
	}
	writeJSON(w, map[string]any{"candidates": out})
}
