package metadata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNoCover 表示该 release 在 Cover Art Archive 没有封面。
var ErrNoCover = errors.New("无封面")

const caaDefaultBaseURL = "https://coverartarchive.org"

// CoverArtClient 从 Cover Art Archive 取专辑封面。
type CoverArtClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewCoverArtClient 创建客户端；baseURL 空用默认，httpClient 空用 15s 超时。
func NewCoverArtClient(baseURL string, httpClient *http.Client) *CoverArtClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = caaDefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &CoverArtClient{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

// FetchFront 取 release 的正面封面；CAA 返回 307 跳转，http.Client 默认跟随。
// 404 → ErrNoCover；其它非 2xx / 网络异常 → 普通 error。
func (c *CoverArtClient) FetchFront(ctx context.Context, releaseMBID string) ([]byte, string, error) {
	endpoint := c.baseURL + "/release/" + releaseMBID + "/front"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Lyra/0.1 (https://github.com/yxx-z/Lyra)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, "", ErrNoCover
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("coverartarchive status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 上限 10MB，防止超大响应耗尽内存
	if err != nil {
		return nil, "", err
	}
	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return data, mimeType, nil
}
