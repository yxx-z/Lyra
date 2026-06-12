package lyrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LRCLIBClient queries https://lrclib.net/api/get.
type LRCLIBClient struct {
	baseURL    string
	userAgent  string
	httpClient *http.Client
}

func NewLRCLIBClient(baseURL, userAgent string, httpClient *http.Client) *LRCLIBClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://lrclib.net"
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = "Lyra/0.1"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &LRCLIBClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		userAgent:  userAgent,
		httpClient: httpClient,
	}
}

// Name implements Provider.
func (c *LRCLIBClient) Name() string { return "lrclib" }

func (c *LRCLIBClient) Fetch(ctx context.Context, q Query) (Result, error) {
	if strings.TrimSpace(q.TrackName) == "" || strings.TrimSpace(q.ArtistName) == "" || q.Duration <= 0 {
		return Result{}, ErrInvalidQuery
	}

	endpoint, err := url.Parse(c.baseURL + "/api/get")
	if err != nil {
		return Result{}, err
	}
	params := endpoint.Query()
	params.Set("track_name", q.TrackName)
	params.Set("artist_name", q.ArtistName)
	params.Set("album_name", q.AlbumName)
	params.Set("duration", strconv.Itoa(q.Duration))
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Result{}, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("lrclib status %d", resp.StatusCode)
	}

	var payload struct {
		PlainLyrics  string `json:"plainLyrics"`
		SyncedLyrics string `json:"syncedLyrics"`
		Instrumental bool   `json:"instrumental"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Result{}, err
	}
	if payload.Instrumental {
		return Result{}, ErrNotFound
	}

	content := strings.TrimSpace(payload.SyncedLyrics)
	if content == "" {
		content = strings.TrimSpace(payload.PlainLyrics)
	}
	if content == "" {
		return Result{}, ErrNotFound
	}
	return Result{
		LRCContent:   content,
		PlainContent: strings.TrimSpace(payload.PlainLyrics),
		Source:       "lrclib",
	}, nil
}

// SearchCandidate 是 LRCLIB /api/search 返回的一条候选。
type SearchCandidate struct {
	TrackName    string
	ArtistName   string
	AlbumName    string
	Duration     int
	SyncedLyrics string
	PlainLyrics  string
	Instrumental bool
}

// Search 用 LRCLIB /api/search 模糊搜索歌词候选（不要求精确时长）。
// 歌名与歌手至少一个非空，否则 ErrInvalidQuery。
func (c *LRCLIBClient) Search(ctx context.Context, trackName, artistName, albumName string) ([]SearchCandidate, error) {
	if strings.TrimSpace(trackName) == "" && strings.TrimSpace(artistName) == "" {
		return nil, ErrInvalidQuery
	}
	endpoint, err := url.Parse(c.baseURL + "/api/search")
	if err != nil {
		return nil, err
	}
	params := endpoint.Query()
	if strings.TrimSpace(trackName) != "" {
		params.Set("track_name", trackName)
	}
	if strings.TrimSpace(artistName) != "" {
		params.Set("artist_name", artistName)
	}
	if strings.TrimSpace(albumName) != "" {
		params.Set("album_name", albumName)
	}
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
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

	if resp.StatusCode == http.StatusNotFound {
		return []SearchCandidate{}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("lrclib search status %d", resp.StatusCode)
	}

	var payload []struct {
		TrackName    string  `json:"trackName"`
		ArtistName   string  `json:"artistName"`
		AlbumName    string  `json:"albumName"`
		Duration     float64 `json:"duration"`
		Instrumental bool    `json:"instrumental"`
		PlainLyrics  string  `json:"plainLyrics"`
		SyncedLyrics string  `json:"syncedLyrics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	out := make([]SearchCandidate, 0, len(payload))
	for _, p := range payload {
		out = append(out, SearchCandidate{
			TrackName:    p.TrackName,
			ArtistName:   p.ArtistName,
			AlbumName:    p.AlbumName,
			Duration:     int(p.Duration),
			SyncedLyrics: strings.TrimSpace(p.SyncedLyrics),
			PlainLyrics:  strings.TrimSpace(p.PlainLyrics),
			Instrumental: p.Instrumental,
		})
	}
	return out, nil
}
