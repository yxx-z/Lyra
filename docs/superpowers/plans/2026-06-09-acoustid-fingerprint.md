# AcoustID 指纹识别 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `internal/acoustid` 包，用 fpcalc 算音频指纹 + AcoustID 识别 recording MBID，存 `tracks.acoustid`/`tracks.mbid`；接入扫描器曲目级指纹阶段（key 门控）。

**Architecture:** `internal/acoustid` 三文件：fpcalc 运行器（`Fingerprinter` 接口便于测试）、AcoustID 查询客户端（`pickResult` 纯函数 score≥0.9）、`FingerprintService` 编排。扫描器 `NewScanner` 重构为接收 `ScrapeServices{Lyrics,Metadata,Fingerprint}` 结构体，新增曲目级指纹阶段（串行、350ms 节流、可中断、`scraper.enabled && acoustid.api_key!=""` 门控）。

**Tech Stack:** Go 1.25（os/exec, net/http, httptest, modernc.org/sqlite）、chromaprint(fpcalc) + AcoustID API。

**Go 环境：** 含 `go` 命令的步骤前 `export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## 设计参考

实现前读 `docs/superpowers/specs/2026-06-09-acoustid-fingerprint-design.md`。

**关键既有事实：**
- fpcalc 仅 docker 内有；**单测一律用 fake `Fingerprinter` + httptest，不依赖真二进制/真网络**。
- `tracks` 有 `acoustid TEXT` 与 `mbid TEXT UNIQUE` 列。`db.Open(":memory:")` 跑迁移建全表。
- `internal/scanner/probe.go` 的外部进程模式：`exec.CommandContext` + `cmd.WaitDelay = 5*time.Second` + `cmd.Output()`。
- 当前 `NewScanner(db, cfg, ffprobePath, lyricsService, metadataService, scrapeEnabled)`（6 参）。**7 个调用点**：`router_scrape_test.go:23`、`router_test.go:27`、`v1/library_test.go:22`、`scanner_test.go:28/111/151/197`、`cmd/server/main.go:80`。
- 扫描器现有 `scrapePending`（歌词）/`scrapeAlbumsPending`（元数据）是镜像范本：收集 id→关 rows→`rows.Err()`→ctx 可中断循环→计数→节流。
- `lyrics`/`metadata` service 在 router.go 也各自构造（给 HTTP handler 用），**与 scanner 无关**；本轮不动 router.go。

**AcoustID lookup 响应结构（已用真实接口确认）：**
```json
{"status":"ok","results":[{"id":"<acoustid-uuid>","score":0.97,"recordings":[{"id":"<recording-mbid>","title":"晴天"}]}]}
```

---

## 文件结构

```
internal/config/config.go            改：AcoustIDConfig 加 FpcalcPath；Default 设 "fpcalc"
internal/acoustid/fpcalc.go          新建：Fingerprinter 接口 + ExecFingerprinter + parseFpcalcJSON
internal/acoustid/fpcalc_test.go     新建
internal/acoustid/client.go          新建：acoustResult/IdentifyResult、pickResult、AcoustIDClient.Lookup、ErrNoMatch
internal/acoustid/client_test.go     新建
internal/acoustid/service.go         新建：FingerprintService.IdentifyTrack、IdentifyOutcome、ErrTrackNotFound
internal/acoustid/service_test.go    新建
internal/scanner/scanner.go          改：ScrapeServices 结构体 + NewScanner 重构 + 指纹阶段 + ScanStatus.Fingerprinted
internal/scanner/scanner_test.go     改：更新调用点 + 指纹阶段测试
internal/api/router_test.go / router_scrape_test.go / v1/library_test.go  改：更新 NewScanner 调用
cmd/server/main.go                   改：key 门控构造 fpSvc + 传 ScrapeServices
```

---

### Task 1: 配置 — AcoustIDConfig.FpcalcPath

**Files:** Modify `internal/config/config.go`

- [ ] **Step 1: 改结构体 + 默认值**

`AcoustIDConfig` 改为：
```go
type AcoustIDConfig struct {
	APIKey     string `yaml:"api_key"`
	FpcalcPath string `yaml:"fpcalc_path"`
}
```
在 `Default()` 的 `Scraper` 构造中为 AcoustID 设默认 fpcalc 路径。找到 `Scraper: ScraperConfig{Enabled: true, Netease: NeteaseConfig{Enabled: true}},` 改为：
```go
		Scraper:  ScraperConfig{Enabled: true, Netease: NeteaseConfig{Enabled: true}, AcoustID: AcoustIDConfig{FpcalcPath: "fpcalc"}},
```

- [ ] **Step 2: 构建 + 既有配置测试**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./internal/config/ && go test ./internal/config/`
Expected: 通过（结构体加字段不破坏既有测试）。

- [ ] **Step 3: 提交**
```bash
git add internal/config/config.go
git commit -m "feat(config): AcoustIDConfig 加 fpcalc_path"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 2: fpcalc 运行器 + 解析

**Files:** Create `internal/acoustid/fpcalc.go`, `internal/acoustid/fpcalc_test.go`

- [ ] **Step 1: 写失败测试** — `internal/acoustid/fpcalc_test.go`:
```go
package acoustid

import (
	"context"
	"os/exec"
	"testing"
)

func TestParseFpcalcJSON(t *testing.T) {
	dur, fp, err := parseFpcalcJSON([]byte(`{"duration": 269.00, "fingerprint": "AQADtABC"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if dur != 269 {
		t.Errorf("duration = %d, want 269", dur)
	}
	if fp != "AQADtABC" {
		t.Errorf("fingerprint = %q", fp)
	}
}

func TestParseFpcalcJSON_Bad(t *testing.T) {
	if _, _, err := parseFpcalcJSON([]byte(`not json`)); err == nil {
		t.Error("坏 JSON 应返回 error")
	}
}

// ExecFingerprinter 的真实跑：仅当环境有 fpcalc 时执行，否则跳过。
func TestExecFingerprinter_SkipsWithoutBinary(t *testing.T) {
	if _, err := exec.LookPath("fpcalc"); err != nil {
		t.Skip("无 fpcalc，跳过真实指纹测试")
	}
	// 有 fpcalc 但无测试音频文件，仅验证构造不 panic、对不存在文件返回 error
	f := NewExecFingerprinter("fpcalc")
	if _, _, err := f.Calc(context.Background(), "/nonexistent/file.flac"); err == nil {
		t.Error("对不存在文件应返回 error")
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/acoustid/ -run TestParseFpcalcJSON -v`
Expected: 编译失败（`undefined: parseFpcalcJSON` / `NewExecFingerprinter`）。

- [ ] **Step 3: 实现** — `internal/acoustid/fpcalc.go`:
```go
package acoustid

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// Fingerprinter 计算音频指纹（duration 秒 + chromaprint 指纹）。
type Fingerprinter interface {
	Calc(ctx context.Context, filePath string) (durationSec int, fingerprint string, err error)
}

// ExecFingerprinter 调用 fpcalc 二进制实现 Fingerprinter。
type ExecFingerprinter struct {
	fpcalcPath string
}

// NewExecFingerprinter 创建运行器；path 为空用 "fpcalc"。
func NewExecFingerprinter(fpcalcPath string) *ExecFingerprinter {
	if strings.TrimSpace(fpcalcPath) == "" {
		fpcalcPath = "fpcalc"
	}
	return &ExecFingerprinter{fpcalcPath: fpcalcPath}
}

// Calc 跑 `fpcalc -json <file>` 并解析。
func (f *ExecFingerprinter) Calc(ctx context.Context, filePath string) (int, string, error) {
	cmd := exec.CommandContext(ctx, f.fpcalcPath, "-json", filePath)
	cmd.WaitDelay = 5 * time.Second // 防止子进程持有管道导致 Output 永久阻塞
	out, err := cmd.Output()
	if err != nil {
		return 0, "", err
	}
	return parseFpcalcJSON(out)
}

// parseFpcalcJSON 解析 fpcalc -json 输出。
func parseFpcalcJSON(data []byte) (int, string, error) {
	var payload struct {
		Duration    float64 `json:"duration"`
		Fingerprint string  `json:"fingerprint"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, "", err
	}
	return int(payload.Duration), payload.Fingerprint, nil
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/acoustid/ -v`
Expected: PASS（parseFpcalcJSON 测试通过；ExecFingerprinter 测试在无 fpcalc 时 Skip）。

- [ ] **Step 5: 提交**
```bash
git add internal/acoustid/fpcalc.go internal/acoustid/fpcalc_test.go
git commit -m "feat(acoustid): fpcalc 运行器 + 指纹 JSON 解析"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 3: pickResult 纯函数 + 类型

**Files:** Create `internal/acoustid/client.go`（先放类型 + pickResult）, `internal/acoustid/client_test.go`

- [ ] **Step 1: 写失败测试** — `internal/acoustid/client_test.go`:
```go
package acoustid

import "testing"

func TestPickResult_Hit(t *testing.T) {
	rs := []acoustResult{
		{ID: "aid-1", Score: 0.97, Recordings: []recordingRef{{ID: "mbid-1"}, {ID: "mbid-2"}}},
	}
	got, ok := pickResult(rs)
	if !ok {
		t.Fatal("0.97 应命中")
	}
	if got.AcoustID != "aid-1" || got.MBID != "mbid-1" {
		t.Errorf("got %+v", got)
	}
}

func TestPickResult_BelowThreshold(t *testing.T) {
	rs := []acoustResult{{ID: "x", Score: 0.85, Recordings: []recordingRef{{ID: "m"}}}}
	if _, ok := pickResult(rs); ok {
		t.Error("score<0.9 不应命中")
	}
}

func TestPickResult_Empty(t *testing.T) {
	if _, ok := pickResult(nil); ok {
		t.Error("无结果不应命中")
	}
}

func TestPickResult_HitNoRecordings(t *testing.T) {
	rs := []acoustResult{{ID: "aid-2", Score: 0.95}}
	got, ok := pickResult(rs)
	if !ok || got.AcoustID != "aid-2" || got.MBID != "" {
		t.Errorf("命中但无 recordings 应只 AcoustID、MBID 空，得到 %+v ok=%v", got, ok)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/acoustid/ -run TestPickResult -v`
Expected: 编译失败（`undefined: acoustResult` / `pickResult`）。

- [ ] **Step 3: 实现** — `internal/acoustid/client.go`:
```go
package acoustid

import "errors"

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
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/acoustid/ -run TestPickResult -v`
Expected: PASS

- [ ] **Step 5: 提交**
```bash
git add internal/acoustid/client.go internal/acoustid/client_test.go
git commit -m "feat(acoustid): pickResult 择优纯函数（score≥0.9）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 4: AcoustIDClient.Lookup

**Files:** Modify `internal/acoustid/client.go`（追加 client）, `internal/acoustid/client_test.go`（追加 httptest）

- [ ] **Step 1: 写失败测试** — 在 `client_test.go` 顶部把 `import "testing"` 替换为：
```go
import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)
```
并追加：
```go
func newTestClient(srv *httptest.Server) *AcoustIDClient {
	return NewAcoustIDClient(srv.URL, "testkey", srv.Client())
}

func TestLookup_Hit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","results":[{"id":"aid-1","score":0.97,"recordings":[{"id":"mbid-1","title":"晴天"}]}]}`))
	}))
	defer srv.Close()
	res, err := newTestClient(srv).Lookup(context.Background(), 269, "FP")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.AcoustID != "aid-1" || res.MBID != "mbid-1" {
		t.Errorf("got %+v", res)
	}
}

func TestLookup_NoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()
	_, err := newTestClient(srv).Lookup(context.Background(), 269, "FP")
	if !errors.Is(err, ErrNoMatch) {
		t.Errorf("空结果应 ErrNoMatch，得到 %v", err)
	}
}

func TestLookup_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"error","error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()
	_, err := newTestClient(srv).Lookup(context.Background(), 269, "FP")
	if err == nil || errors.Is(err, ErrNoMatch) {
		t.Errorf("status!=ok 应普通 error，得到 %v", err)
	}
}

func TestLookup_HTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := newTestClient(srv).Lookup(context.Background(), 269, "FP")
	if err == nil || errors.Is(err, ErrNoMatch) {
		t.Errorf("500 应普通 error，得到 %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/acoustid/ -run TestLookup -v`
Expected: 编译失败（`undefined: NewAcoustIDClient`）。

- [ ] **Step 3: 实现** — 在 `internal/acoustid/client.go` 顶部 import 改为：
```go
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
```
追加到文件末尾：
```go
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
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/acoustid/ -v`
Expected: PASS（本包全部）。

- [ ] **Step 5: 提交**
```bash
git add internal/acoustid/client.go internal/acoustid/client_test.go
git commit -m "feat(acoustid): AcoustIDClient.Lookup（v2 lookup）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 5: FingerprintService.IdentifyTrack

**Files:** Create `internal/acoustid/service.go`, `internal/acoustid/service_test.go`

- [ ] **Step 1: 写失败测试** — `internal/acoustid/service_test.go`:
```go
package acoustid

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

// fakeFP 是测试用指纹器。
type fakeFP struct {
	dur int
	fp  string
	err error
}

func (f fakeFP) Calc(ctx context.Context, path string) (int, string, error) {
	return f.dur, f.fp, f.err
}

// openTrackDB 建内存库并插入一首曲目，返回 *sql.DB。
func openTrackDB(t *testing.T, trackID, filePath string) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO tracks(id,title,file_path,is_available) VALUES(?,?,?,1)`, trackID, "曲", filePath); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestIdentifyTrack_Hit(t *testing.T) {
	database := openTrackDB(t, "tr1", "/m/a.flac")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","results":[{"id":"aid-1","score":0.97,"recordings":[{"id":"mbid-1"}]}]}`))
	}))
	defer srv.Close()
	svc := NewFingerprintService(database, fakeFP{dur: 269, fp: "FP"}, NewAcoustIDClient(srv.URL, "k", srv.Client()))

	out, err := svc.IdentifyTrack(context.Background(), "tr1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "identified" || out.AcoustID != "aid-1" || out.MBID != "mbid-1" {
		t.Fatalf("out=%+v", out)
	}
	var aid, mbid string
	database.QueryRow(`SELECT COALESCE(acoustid,''),COALESCE(mbid,'') FROM tracks WHERE id='tr1'`).Scan(&aid, &mbid)
	if aid != "aid-1" || mbid != "mbid-1" {
		t.Errorf("落库 acoustid=%q mbid=%q", aid, mbid)
	}
}

func TestIdentifyTrack_NoMatch(t *testing.T) {
	database := openTrackDB(t, "tr1", "/m/a.flac")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()
	svc := NewFingerprintService(database, fakeFP{dur: 269, fp: "FP"}, NewAcoustIDClient(srv.URL, "k", srv.Client()))

	out, err := svc.IdentifyTrack(context.Background(), "tr1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "nomatch" {
		t.Errorf("应 nomatch，得到 %q", out.Status)
	}
	var aid sql.NullString
	database.QueryRow(`SELECT acoustid FROM tracks WHERE id='tr1'`).Scan(&aid)
	if !aid.Valid || aid.String != "" {
		t.Errorf("nomatch 应置 acoustid=''（已尝试），得到 valid=%v %q", aid.Valid, aid.String)
	}
}

func TestIdentifyTrack_FpcalcError(t *testing.T) {
	database := openTrackDB(t, "tr1", "/m/a.flac")
	svc := NewFingerprintService(database, fakeFP{err: errors.New("fpcalc boom")}, NewAcoustIDClient("http://unused", "k", nil))
	_, err := svc.IdentifyTrack(context.Background(), "tr1")
	if err == nil {
		t.Fatal("fpcalc 错误应返回 error")
	}
	var aid sql.NullString
	database.QueryRow(`SELECT acoustid FROM tracks WHERE id='tr1'`).Scan(&aid)
	if aid.Valid {
		t.Errorf("瞬时错误应保持 acoustid NULL，得到 %q", aid.String)
	}
}

func TestIdentifyTrack_NotFound(t *testing.T) {
	database := openTrackDB(t, "tr1", "/m/a.flac")
	svc := NewFingerprintService(database, fakeFP{dur: 1, fp: "x"}, NewAcoustIDClient("http://unused", "k", nil))
	if _, err := svc.IdentifyTrack(context.Background(), "missing"); !errors.Is(err, ErrTrackNotFound) {
		t.Errorf("不存在曲目应 ErrTrackNotFound，得到 %v", err)
	}
}
```
（`NewFingerprintService` 第一个参数类型为 `*sql.DB`；`openTrackDB` 已在上方测试代码中定义。）

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/acoustid/ -run TestIdentifyTrack -v`
Expected: 编译失败（`undefined: NewFingerprintService` / `ErrTrackNotFound`）。

- [ ] **Step 3: 实现** — `internal/acoustid/service.go`:
```go
package acoustid

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
)

// ErrTrackNotFound 表示数据库无此曲目。
var ErrTrackNotFound = errors.New("曲目不存在")

// IdentifyOutcome 是 IdentifyTrack 的结果。
type IdentifyOutcome struct {
	Status   string // "identified" | "nomatch"
	AcoustID string
	MBID     string
}

// FingerprintService 编排单曲指纹识别。
type FingerprintService struct {
	db     *sql.DB
	fp     Fingerprinter
	client *AcoustIDClient
}

// NewFingerprintService 创建服务。
func NewFingerprintService(db *sql.DB, fp Fingerprinter, client *AcoustIDClient) *FingerprintService {
	return &FingerprintService{db: db, fp: fp, client: client}
}

// IdentifyTrack 为单曲算指纹并经 AcoustID 识别，落库 acoustid/mbid。
func (s *FingerprintService) IdentifyTrack(ctx context.Context, trackID string) (IdentifyOutcome, error) {
	var filePath string
	err := s.db.QueryRowContext(ctx, `SELECT file_path FROM tracks WHERE id=?`, trackID).Scan(&filePath)
	if errors.Is(err, sql.ErrNoRows) {
		return IdentifyOutcome{}, ErrTrackNotFound
	}
	if err != nil {
		return IdentifyOutcome{}, err
	}

	dur, fingerprint, err := s.fp.Calc(ctx, filePath)
	if err != nil {
		return IdentifyOutcome{}, err // 瞬时：acoustid 保持 NULL，下次重试
	}

	res, err := s.client.Lookup(ctx, dur, fingerprint)
	if errors.Is(err, ErrNoMatch) {
		if _, e := s.db.ExecContext(ctx, `UPDATE tracks SET acoustid='' WHERE id=?`, trackID); e != nil {
			slog.Warn("标记 acoustid 空失败", "track", trackID, "err", e)
		}
		return IdentifyOutcome{Status: "nomatch"}, nil
	}
	if err != nil {
		return IdentifyOutcome{}, err // 瞬时
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE tracks SET acoustid=? WHERE id=?`, res.AcoustID, trackID); err != nil {
		return IdentifyOutcome{}, err
	}
	if res.MBID != "" {
		// mbid UNIQUE：冲突仅 warn 不致命
		if _, err := s.db.ExecContext(ctx, `UPDATE tracks SET mbid=? WHERE id=?`, res.MBID, trackID); err != nil {
			slog.Warn("设置曲目 mbid 失败（可能 UNIQUE 冲突）", "track", trackID, "mbid", res.MBID, "err", err)
		}
	}
	return IdentifyOutcome{Status: "identified", AcoustID: res.AcoustID, MBID: res.MBID}, nil
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/acoustid/ -v`
Expected: PASS（本包全部）。

- [ ] **Step 5: 提交**
```bash
git add internal/acoustid/service.go internal/acoustid/service_test.go
git commit -m "feat(acoustid): FingerprintService.IdentifyTrack 编排"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 6: 扫描器重构为 ScrapeServices（无新行为）

**Files:** Modify `internal/scanner/scanner.go`、`internal/scanner/scanner_test.go`、`internal/api/router_test.go`、`internal/api/router_scrape_test.go`、`internal/api/v1/library_test.go`

本任务**纯重构**：把 `lyricsService`/`metadataService` 两字段收进 `ScrapeServices` 结构体，不加新功能。完成后全部测试仍绿。

- [ ] **Step 1: 改 scanner.go**

a) import 加 acoustid（结构体字段类型需要）：
```go
	"github.com/yxx-z/lyra/internal/acoustid"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/lyrics"
	"github.com/yxx-z/lyra/internal/metadata"
```

b) 在 `Scanner` 结构体定义前新增：
```go
// ScrapeServices 聚合后台刮削/识别服务，任一可为 nil。
type ScrapeServices struct {
	Lyrics      *lyrics.LyricsService
	Metadata    *metadata.MetadataService
	Fingerprint *acoustid.FingerprintService
}
```

c) `Scanner` 结构体把：
```go
	lyricsService   *lyrics.LyricsService
	metadataService *metadata.MetadataService
	scrapeEnabled   bool
```
替换为：
```go
	services      ScrapeServices
	scrapeEnabled bool
```

d) `NewScanner` 改签名与赋值：
```go
func NewScanner(db *sql.DB, cfg config.LibraryConfig, ffprobePath string, services ScrapeServices, scrapeEnabled bool) *Scanner {
	s := &Scanner{
		db:            db,
		cfg:           cfg,
		ing:           NewIngester(db),
		ffprobePath:   ffprobePath,
		services:      services,
		scrapeEnabled: scrapeEnabled,
		stopCh:        make(chan struct{}),
	}
	s.phase.Store("idle")
	return s
}
```

e) `doScan` 内两处条件改用 services：
```go
	if s.scrapeEnabled && s.services.Lyrics != nil {
		s.phase.Store("scraping")
		s.scrapePending(ctx)
	}
	if s.scrapeEnabled && s.services.Metadata != nil {
		s.phase.Store("metadata")
		s.scrapeAlbumsPending(ctx)
	}
	s.phase.Store("idle")
```

f) `scrapePending` 内 `s.lyricsService.ScrapeTrack(...)` → `s.services.Lyrics.ScrapeTrack(...)`；`scrapeAlbumsPending` 内 `s.metadataService.EnrichAlbum(...)` → `s.services.Metadata.EnrichAlbum(...)`。

- [ ] **Step 2: 更新 7 个 NewScanner 调用点**

- `internal/api/router_scrape_test.go:23` → `scanner.NewScanner(d, config.LibraryConfig{}, "", scanner.ScrapeServices{}, false)`
- `internal/api/router_test.go:27` → `scanner.NewScanner(d, config.LibraryConfig{}, "", scanner.ScrapeServices{}, false)`
- `internal/api/v1/library_test.go:22` → `scanner.NewScanner(d, config.LibraryConfig{}, "", scanner.ScrapeServices{}, false)`
- `internal/scanner/scanner_test.go:28` → `NewScanner(d, config.LibraryConfig{Paths: paths}, "", ScrapeServices{}, false)`
- `internal/scanner/scanner_test.go:111` → `NewScanner(d, config.LibraryConfig{Paths: []string{dir}}, "", ScrapeServices{Lyrics: svc}, true)`
- `internal/scanner/scanner_test.go:151` → `NewScanner(d, config.LibraryConfig{}, "", ScrapeServices{}, false)`
- `internal/scanner/scanner_test.go:197` → `NewScanner(d, config.LibraryConfig{}, "", ScrapeServices{Metadata: metaSvc}, true)`

- [ ] **Step 3: 构建 + 测试** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./internal/... && go test ./internal/scanner/ ./internal/api/... 2>&1 | tail -15`
Expected: build 成功；scanner + api 测试全 PASS（纯重构，行为不变）。`cmd/server/main.go` 仍是旧签名会导致 `go build ./...` 失败 —— 本任务用 `./internal/...`，main.go 在 Task 8 修。

- [ ] **Step 4: 提交**
```bash
git add internal/scanner/ internal/api/router_test.go internal/api/router_scrape_test.go internal/api/v1/library_test.go
git commit -m "refactor(scanner): NewScanner 改用 ScrapeServices 结构体"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 7: 扫描器指纹阶段

**Files:** Modify `internal/scanner/scanner.go`、`internal/scanner/scanner_test.go`

- [ ] **Step 1: 写失败测试** — 在 `internal/scanner/scanner_test.go` 追加（顶部 import 需含 `net/http`、`net/http/httptest`、`github.com/yxx-z/lyra/internal/acoustid`；`context`/`db`/`config` 已有）：
```go
type fakeFP struct{}

func (fakeFP) Calc(ctx context.Context, path string) (int, string, error) { return 269, "FP", nil }

func TestFingerprintPending_CountsIdentified(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO tracks(id,title,file_path,is_available) VALUES('tr','曲','/m/a.flac',1)`); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","results":[{"id":"aid-1","score":0.97,"recordings":[{"id":"mbid-1"}]}]}`))
	}))
	defer srv.Close()
	fpSvc := acoustid.NewFingerprintService(d, fakeFP{}, acoustid.NewAcoustIDClient(srv.URL, "k", srv.Client()))
	s := NewScanner(d, config.LibraryConfig{}, "", ScrapeServices{Fingerprint: fpSvc}, true)

	s.fingerprintPending(context.Background())

	if got := s.Status().Fingerprinted; got != 1 {
		t.Errorf("Fingerprinted = %d, want 1", got)
	}
	var aid string
	d.QueryRow(`SELECT COALESCE(acoustid,'') FROM tracks WHERE id='tr'`).Scan(&aid)
	if aid != "aid-1" {
		t.Errorf("acoustid 落库 = %q", aid)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/scanner/ -run TestFingerprintPending -v`
Expected: 编译失败（`s.fingerprintPending` / `Status().Fingerprinted` 未定义）。

- [ ] **Step 3: 改 scanner.go**

a) `ScanStatus` 加字段（AlbumsScraped 之后）：
```go
	AlbumsScraped int64 `json:"albums_scraped"`
	Fingerprinted int64 `json:"fingerprinted"`
```

b) `Scanner` 结构体计数区加：
```go
	lyricsScraped atomic.Int64
	albumsScraped atomic.Int64
	fingerprinted atomic.Int64
```

c) `Status()` 返回加 `Fingerprinted: s.fingerprinted.Load(),`（AlbumsScraped 之后）。

d) `doScan` 重置区加 `s.fingerprinted.Store(0)`（`s.albumsScraped.Store(0)` 旁）；并在元数据阶段之后、最终 `s.phase.Store("idle")` 之前插入：
```go
	if s.scrapeEnabled && s.services.Fingerprint != nil {
		s.phase.Store("fingerprint")
		s.fingerprintPending(ctx)
	}
```

e) 文件末尾追加：
```go
func (s *Scanner) fingerprintPending(ctx context.Context) {
	rows, err := s.db.Query(`SELECT id FROM tracks WHERE acoustid IS NULL AND is_available=1`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		slog.Warn("指纹阶段遍历待识别曲目出错", "err", err)
	}
	if len(ids) == 0 {
		return
	}
	slog.Info("开始后台指纹识别", "待处理", len(ids))

	for _, id := range ids {
		select {
		case <-ctx.Done():
			return
		default:
		}
		outcome, err := s.services.Fingerprint.IdentifyTrack(ctx, id)
		if err != nil {
			// 瞬时错误（fpcalc/网络）：acoustid 留 NULL，下次扫描重试
			s.errors.Add(1)
		} else if outcome.Status == "identified" {
			s.fingerprinted.Add(1)
		}
		// AcoustID 限速：每曲后等待 ~350ms（可被 ctx 中断）
		select {
		case <-time.After(350 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
	slog.Info("后台指纹识别结束", "成功", s.fingerprinted.Load())
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/scanner/ -v 2>&1 | tail -15`
Expected: PASS（含新指纹测试，约 0.35s 节流一次）。

- [ ] **Step 5: 提交**
```bash
git add internal/scanner/scanner.go internal/scanner/scanner_test.go
git commit -m "feat(scanner): 曲目级指纹阶段（phase/fingerprinted + 350ms 节流 + 可中断）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 8: 接线 main.go（key 门控构造 + ScrapeServices）

**Files:** Modify `cmd/server/main.go`

- [ ] **Step 1: 改 main.go**

a) import 加 acoustid（lyrics/metadata 旁）：
```go
	"github.com/yxx-z/lyra/internal/acoustid"
	"github.com/yxx-z/lyra/internal/lyrics"
	"github.com/yxx-z/lyra/internal/metadata"
```

b) 找到现有 `lyricsService := ...` 与 `metadataService := ...` 构造之后、`sc := scanner.NewScanner(...)` 之前，插入 fingerprint 服务的 key 门控构造，并把 NewScanner 改为传 ScrapeServices：
```go
	var fingerprintService *acoustid.FingerprintService
	if cfg.Scraper.AcoustID.APIKey != "" {
		fingerprintService = acoustid.NewFingerprintService(
			database,
			acoustid.NewExecFingerprinter(cfg.Scraper.AcoustID.FpcalcPath),
			acoustid.NewAcoustIDClient("https://api.acoustid.org", cfg.Scraper.AcoustID.APIKey, nil),
		)
	}
	sc := scanner.NewScanner(database, cfg.Library, cfg.Transcode.FfprobePath, scanner.ScrapeServices{
		Lyrics:      lyricsService,
		Metadata:    metadataService,
		Fingerprint: fingerprintService,
	}, cfg.Scraper.Enabled)
```

- [ ] **Step 2: 构建 + 全量测试** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./...`
Expected: build 成功（cmd/server 现编译）；所有包测试 PASS。

- [ ] **Step 3: 提交**
```bash
git add cmd/server/main.go
git commit -m "feat(acoustid): main 构造 FingerprintService（key 门控）注入扫描器"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## 完成标准

- `go test ./...` 全绿；`go build ./...` 成功
- 配了 `acoustid.api_key` 时，扫描完成后跑指纹阶段：未识别曲目 → fpcalc → AcoustID → 存 acoustid/mbid；未配 key 则整段跳过
- 阶段经历 scanning → scraping → metadata → fingerprint → idle；`fingerprinted` 计数
- 所有测试用 fake Fingerprinter + httptest，不依赖真 fpcalc/真网络

## 验证（手动，docker）

1. 在挂载的 config.yaml 配 `scraper.acoustid.api_key: <你的key>`；`make docker-build && docker compose up -d`
2. 触发扫描，观察 `library/scan/status` 的 `phase` 出现 `fingerprint`、`fingerprinted` 增长
3. 查 DB：曲目 `acoustid` 被填（命中）或 `''`（无匹配）；命中的 `mbid` 为 recording MBID
4. 真实端到端已先验证：《晴天》→ AcoustID score 0.97 → recording MBID
