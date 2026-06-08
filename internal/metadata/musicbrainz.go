package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotFound 表示未匹配到合适的 release。
var ErrNotFound = errors.New("未匹配到专辑")

// mbRelease 是 MusicBrainz release 搜索结果中我们关心的字段。
type mbRelease struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Score      int    `json:"score"`
	Date       string `json:"date"`
	TrackCount int    `json:"track-count"`
}

// AlbumQuery 是元数据查询输入。
type AlbumQuery struct {
	AlbumTitle string
	ArtistName string
	TrackCount int // 本地该专辑曲目数，用于在多个 release 中择优
}

// ReleaseMatch 是选中的 release。
type ReleaseMatch struct {
	MBID        string
	Title       string
	ReleaseDate string
	Genre       string // 本轮恒为空（MB search 无可靠 genre 字段）
}

// pickRelease 过滤 score>=90，在剩余里选曲目数最接近 localTrackCount 的；
// 并列取靠前者；localTrackCount<=0 时取靠前者（MB 已按 score 降序）。
func pickRelease(releases []mbRelease, localTrackCount int) (mbRelease, bool) {
	bestIdx := -1
	bestDiff := 0
	for i, r := range releases {
		if r.Score < 90 {
			continue
		}
		if bestIdx == -1 {
			bestIdx = i
			bestDiff = absDiff(r.TrackCount, localTrackCount)
			if localTrackCount <= 0 {
				return r, true // 取首个满足阈值者
			}
			continue
		}
		if localTrackCount > 0 {
			if d := absDiff(r.TrackCount, localTrackCount); d < bestDiff {
				bestIdx = i
				bestDiff = d
			}
		}
	}
	if bestIdx == -1 {
		return mbRelease{}, false
	}
	return releases[bestIdx], true
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

const mbDefaultBaseURL = "https://musicbrainz.org"

// MusicBrainzClient 查询 MusicBrainz WS/2 release 搜索接口。
type MusicBrainzClient struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
}

// NewMusicBrainzClient 创建客户端；baseURL 空用默认，httpClient 空用 10s 超时。
func NewMusicBrainzClient(baseURL, userAgent string, httpClient *http.Client) *MusicBrainzClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = mbDefaultBaseURL
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "Lyra/0.1 (https://github.com/yxx-z/Lyra)"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &MusicBrainzClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		userAgent:  userAgent,
		httpClient: httpClient,
	}
}

// Search 按艺术家+专辑查询，返回择优后的 release；无匹配返回 ErrNotFound。
func (c *MusicBrainzClient) Search(ctx context.Context, q AlbumQuery) (ReleaseMatch, error) {
	lucene := fmt.Sprintf(`artist:"%s" AND release:"%s"`,
		sanitizeLucene(q.ArtistName), sanitizeLucene(q.AlbumTitle))
	endpoint := c.baseURL + "/ws/2/release/?query=" + url.QueryEscape(lucene) + "&fmt=json&limit=25"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ReleaseMatch{}, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ReleaseMatch{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ReleaseMatch{}, fmt.Errorf("musicbrainz status %d", resp.StatusCode)
	}

	var payload struct {
		Releases []mbRelease `json:"releases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ReleaseMatch{}, fmt.Errorf("musicbrainz 解码失败: %w", err)
	}

	r, ok := pickRelease(payload.Releases, q.TrackCount)
	if !ok {
		return ReleaseMatch{}, ErrNotFound
	}
	return ReleaseMatch{MBID: r.ID, Title: r.Title, ReleaseDate: r.Date}, nil
}

// sanitizeLucene 去除可能破坏 Lucene 查询的双引号。
func sanitizeLucene(s string) string {
	return strings.ReplaceAll(s, `"`, "")
}
