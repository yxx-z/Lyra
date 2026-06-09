package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	baseURL     string
	userAgent   string
	httpClient  *http.Client
	minInterval time.Duration
	mu          sync.Mutex
	lastReqAt   time.Time
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
		baseURL:     strings.TrimRight(baseURL, "/"),
		userAgent:   userAgent,
		httpClient:  httpClient,
		minInterval: 1100 * time.Millisecond,
	}
}

// throttle 保证任意两次 MB 请求间隔 ≥ minInterval（全局 1 req/s 限速合规）。
func (c *MusicBrainzClient) throttle(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := c.minInterval - time.Since(c.lastReqAt); d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.lastReqAt = time.Now()
	return nil
}

// doGet 节流后发 GET，校验状态码，返回响应体。
func (c *MusicBrainzClient) doGet(ctx context.Context, endpoint string) ([]byte, error) {
	if err := c.throttle(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("musicbrainz status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 上限 4MiB，防止超大响应耗尽内存
}

// Search 按艺术家+专辑查询，返回择优后的 release；无匹配返回 ErrNotFound。
func (c *MusicBrainzClient) Search(ctx context.Context, q AlbumQuery) (ReleaseMatch, error) {
	// 不用 release:"..." 精确短语：简体/繁体、标点（如「贰・」vs「貳·」）差异会让短语 0 命中。
	// 改用裸词，让 MB 按分词打分（简繁差异下正确专辑仍能 score 100）；score≥90 + 曲目数兜底防误配。
	lucene := fmt.Sprintf(`artist:%s AND release:%s`,
		escapeLucene(q.ArtistName), escapeLucene(q.AlbumTitle))
	endpoint := c.baseURL + "/ws/2/release/?query=" + url.QueryEscape(lucene) + "&fmt=json&limit=25"

	body, err := c.doGet(ctx, endpoint)
	if err != nil {
		return ReleaseMatch{}, err
	}
	var payload struct {
		Releases []mbRelease `json:"releases"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ReleaseMatch{}, fmt.Errorf("musicbrainz 解码失败: %w", err)
	}
	r, ok := pickRelease(payload.Releases, q.TrackCount)
	if !ok {
		return ReleaseMatch{}, ErrNotFound
	}
	return ReleaseMatch{MBID: r.ID, Title: r.Title, ReleaseDate: r.Date}, nil
}

// escapeLucene 转义 Lucene 保留字符，避免标题/艺术家中的特殊符号破坏裸词查询。
func escapeLucene(s string) string {
	const special = `+-!(){}[]^"~*?:\/`
	var b strings.Builder
	for _, r := range s {
		if r < 128 && strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// pickByVote 统计各 release 被多少首曲目覆盖，返回覆盖最多者；并列取先出现者；无 → false。
func pickByVote(releasesPerTrack [][]string) (string, bool) {
	counts := map[string]int{}
	order := make([]string, 0)
	for _, rels := range releasesPerTrack {
		for _, id := range rels {
			if counts[id] == 0 {
				order = append(order, id)
			}
			counts[id]++
		}
	}
	best := ""
	bestN := 0
	for _, id := range order {
		if counts[id] > bestN {
			bestN = counts[id]
			best = id
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}

// RecordingReleases 返回某 recording 所属的所有 release MBID。
func (c *MusicBrainzClient) RecordingReleases(ctx context.Context, recordingMBID string) ([]string, error) {
	endpoint := c.baseURL + "/ws/2/recording/" + url.PathEscape(recordingMBID) + "?inc=releases&fmt=json"
	body, err := c.doGet(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Releases []struct {
			ID string `json:"id"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("musicbrainz recording 解码失败: %w", err)
	}
	ids := make([]string, 0, len(payload.Releases))
	for _, r := range payload.Releases {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// ReleaseDate 返回某 release 的发行日期（date 字段，可能空）。
func (c *MusicBrainzClient) ReleaseDate(ctx context.Context, releaseMBID string) (string, error) {
	endpoint := c.baseURL + "/ws/2/release/" + url.PathEscape(releaseMBID) + "?fmt=json"
	body, err := c.doGet(ctx, endpoint)
	if err != nil {
		return "", err
	}
	var payload struct {
		Date string `json:"date"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("musicbrainz release 解码失败: %w", err)
	}
	return payload.Date, nil
}
