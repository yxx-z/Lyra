package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// neteaseSong 是搜索候选中我们关心的字段。
type neteaseSong struct {
	ID         int64
	Name       string
	DurationMS int
}

// normalizeText 归一化文本：去首尾空格、转小写、全角转半角、折叠空白。
func normalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '　': // 全角空格
			b.WriteRune(' ')
		case r >= '！' && r <= '～': // 全角 ASCII 区
			b.WriteRune(r - 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// titleMatches 判断两个标题归一化后是否互相包含。
func titleMatches(a, b string) bool {
	na, nb := normalizeText(a), normalizeText(b)
	if na == "" || nb == "" {
		return false
	}
	return strings.Contains(na, nb) || strings.Contains(nb, na)
}

// pickMatch 从候选中选时长差 ≤3 秒且标题互含的第一首。
func pickMatch(songs []neteaseSong, wantTitle string, wantDurationSec int) (neteaseSong, bool) {
	for _, s := range songs {
		diff := wantDurationSec - s.DurationMS/1000
		if diff < 0 {
			diff = -diff
		}
		if diff <= 3 && titleMatches(s.Name, wantTitle) {
			return s, true
		}
	}
	return neteaseSong{}, false
}

const neteaseDefaultBaseURL = "https://interface.music.163.com"

// NeteaseProvider 通过网易云 eapi 接口获取歌词（含 YRC 逐字）。
type NeteaseProvider struct {
	httpClient *http.Client
	baseURL    string
}

// NewNeteaseProvider 创建 provider；httpClient 为 nil 时用 10s 超时默认客户端。
func NewNeteaseProvider(httpClient *http.Client) *NeteaseProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &NeteaseProvider{httpClient: httpClient, baseURL: neteaseDefaultBaseURL}
}

// Name 实现 Provider。
func (p *NeteaseProvider) Name() string { return "netease" }

// Fetch 实现 Provider：搜索 → 匹配 → 取词 → 解析 YRC。
func (p *NeteaseProvider) Fetch(ctx context.Context, q Query) (Result, error) {
	if strings.TrimSpace(q.TrackName) == "" || strings.TrimSpace(q.ArtistName) == "" {
		return Result{}, ErrInvalidQuery
	}

	songs, err := p.search(ctx, q.TrackName+" "+q.ArtistName)
	if err != nil {
		return Result{}, err
	}
	song, ok := pickMatch(songs, q.TrackName, q.Duration)
	if !ok {
		return Result{}, ErrNotFound
	}

	lrc, yrcRaw, err := p.lyric(ctx, song.ID)
	if err != nil {
		return Result{}, err
	}

	yrcJSON := ""
	if strings.TrimSpace(yrcRaw) != "" {
		yrcJSON, err = parseYRC(yrcRaw)
		if err != nil {
			return Result{}, err
		}
	}
	if strings.TrimSpace(lrc) == "" && strings.TrimSpace(yrcJSON) == "" {
		return Result{}, ErrNotFound
	}

	return Result{
		LRCContent: strings.TrimSpace(lrc),
		YRCContent: yrcJSON,
		Source:     "netease",
	}, nil
}

// search 调 eapi cloudsearch，返回候选歌曲。
func (p *NeteaseProvider) search(ctx context.Context, keyword string) ([]neteaseSong, error) {
	payload := map[string]string{
		"s": keyword, "type": "1", "limit": "10", "offset": "0", "total": "true",
	}
	body, err := p.eapiPost(ctx, "/api/cloudsearch/pc", payload)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Result struct {
			Songs []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
				Dt   int    `json:"dt"`
			} `json:"songs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("netease search 解码失败: %w", err)
	}
	songs := make([]neteaseSong, 0, len(parsed.Result.Songs))
	for _, s := range parsed.Result.Songs {
		songs = append(songs, neteaseSong{ID: s.ID, Name: s.Name, DurationMS: s.Dt})
	}
	return songs, nil
}

// lyric 调 eapi song/lyric/v1，返回 (普通lrc, 原始yrc)。
func (p *NeteaseProvider) lyric(ctx context.Context, songID int64) (string, string, error) {
	// lv/yv/yrc 等为各类歌词的“版本号”参数（0 = 取当前版本，并非禁用）；
	// 携带 yrc/yv 即请求逐字歌词，与通用网易云接口实现一致。
	payload := map[string]string{
		"id": strconv.FormatInt(songID, 10),
		"cp": "false", "lv": "0", "kv": "0", "tv": "0", "rv": "0", "yv": "0", "ytv": "0", "yrc": "0",
	}
	body, err := p.eapiPost(ctx, "/api/song/lyric/v1", payload)
	if err != nil {
		return "", "", err
	}
	var parsed struct {
		Lrc struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
		Yrc struct {
			Lyric string `json:"lyric"`
		} `json:"yrc"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("netease lyric 解码失败: %w", err)
	}
	return parsed.Lrc.Lyric, parsed.Yrc.Lyric, nil
}

// eapiPost 对 apiPath 做 eapi 加密并 POST，返回响应体（明文 JSON）。
// 请求 URL 由 baseURL + (apiPath 中 /api 换成 /eapi) 构成。
func (p *NeteaseProvider) eapiPost(ctx context.Context, apiPath string, payload map[string]string) ([]byte, error) {
	text, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	params := eapiEncryptParams(apiPath, string(text))
	form := url.Values{}
	form.Set("params", params)

	reqURL := p.baseURL + strings.Replace(apiPath, "/api", "/eapi", 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) NeteaseMusicDesktop/2.10.2")
	req.Header.Set("Referer", "https://music.163.com")
	req.Header.Set("Cookie", "os=pc; appver=8.9.70")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("netease 接口状态 %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
