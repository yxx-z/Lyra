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
