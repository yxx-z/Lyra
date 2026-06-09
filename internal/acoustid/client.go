package acoustid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNoMatch 表示指纹未匹配到（无结果或低于置信阈值）。
var ErrNoMatch = errors.New("指纹未匹配")

const scoreThreshold = 0.9

type recordingRef struct {
	ID string `json:"id"`
}

type acoustResult struct {
	ID         string         `json:"id"`
	Score      float64        `json:"score"`
	Recordings []recordingRef `json:"recordings"`
}

// IdentifyResult 是识别命中后的权威标识。
type IdentifyResult struct {
	AcoustID string
	MBID     string // recording MBID，可能为空
	Score    float64
}

// pickResult 取 results[0]（AcoustID 已按 score 降序）；score≥0.9 才命中。
func pickResult(results []acoustResult) (IdentifyResult, bool) {
	if len(results) == 0 {
		return IdentifyResult{}, false
	}
	r := results[0]
	if r.Score < scoreThreshold {
		return IdentifyResult{}, false
	}
	res := IdentifyResult{AcoustID: r.ID, Score: r.Score}
	if len(r.Recordings) > 0 {
		res.MBID = r.Recordings[0].ID
	}
	return res, true
}

const acoustIDDefaultBaseURL = "https://api.acoustid.org"

// AcoustIDClient 查询 AcoustID v2 lookup 接口。
type AcoustIDClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewAcoustIDClient 创建客户端；baseURL 空用默认，httpClient 空用 15s 超时。
func NewAcoustIDClient(baseURL, apiKey string, httpClient *http.Client) *AcoustIDClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = acoustIDDefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &AcoustIDClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: httpClient,
	}
}

// Lookup 用指纹+时长查询；无匹配返回 ErrNoMatch，其它异常返回普通 error。
func (c *AcoustIDClient) Lookup(ctx context.Context, durationSec int, fingerprint string) (IdentifyResult, error) {
	form := url.Values{}
	form.Set("client", c.apiKey)
	form.Set("duration", strconv.Itoa(durationSec))
	form.Set("fingerprint", fingerprint)
	form.Set("meta", "recordings")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v2/lookup", strings.NewReader(form.Encode()))
	if err != nil {
		return IdentifyResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return IdentifyResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return IdentifyResult{}, fmt.Errorf("acoustid status %d", resp.StatusCode)
	}

	var payload struct {
		Status  string         `json:"status"`
		Results []acoustResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return IdentifyResult{}, fmt.Errorf("acoustid 解码失败: %w", err)
	}
	if payload.Status != "ok" {
		return IdentifyResult{}, fmt.Errorf("acoustid 返回状态 %q", payload.Status)
	}

	res, ok := pickResult(payload.Results)
	if !ok {
		return IdentifyResult{}, ErrNoMatch
	}
	return res, nil
}
