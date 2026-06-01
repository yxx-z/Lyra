# 歌词刮削接入实现计划

> **给 AI 工作者：** 必须使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务执行本计划。步骤使用复选框（`- [ ]`）语法追踪进度。

**目标：** 把歌词刮削抽象为 Provider 链（内嵌标签 → LRCLIB）+ LyricsService 编排，让 HTTP 端点与扫描器后台阶段复用；扫描完成后自动为缺歌词的曲目刮削；前端「暂无歌词」处加「获取歌词」按钮。

**架构：** `internal/lyrics` 新增 `Provider` 接口、`embeddedProvider`、`LyricsService`。`LyricsService.ScrapeTrack` 编排「查缓存 → 按链尝试 → 写库」。HTTP `ScrapeHandler` 和 `Scanner` 后台阶段都调它。扫描器在入库循环后追加串行刮削阶段（间隔、可中断），进度经 `ScanStatus` 暴露。

**技术栈：** Go 1.25、dhowden/tag（内嵌歌词）、modernc SQLite、Vue 3 + Pinia

---

## 前置条件

```bash
export PATH=$PATH:/home/yxx/go-local/go/bin
cd /home/yxx/develop/Lyra
```

---

## 文件结构

```
internal/lyrics/
├── provider.go     新建：Provider 接口；Query/Result/错误从 lrclib.go 迁入；Query 加 FilePath，Result 加 YRCContent
├── embedded.go     新建：embeddedProvider（dhowden/tag 读内嵌歌词）
├── embedded_test.go
├── lrclib.go       改造：删除已迁走的 Query/Result/错误定义；LRCLIBClient 加 Name()
├── service.go      新建：LyricsService + ScrapeTrack + ScrapeOutcome + ErrTrackNotFound
└── service_test.go

internal/api/v1/
├── scrape.go       改造：ScrapeHandler 持有 *LyricsService，scrapeTrack 改为薄壳
└── scrape_test.go  改造：适配新构造签名

internal/scanner/
├── scanner.go      改造：Scanner 注入 lyricsService+scrapeEnabled；ScanStatus 加 Phase/LyricsScraped；doScan 追加刮削阶段
└── scanner_test.go 改造：NewScanner 新签名

internal/api/router.go   改造：构造 providers+LyricsService，注入 ScrapeHandler 和 Scanner
internal/api/router_test.go     改造：NewScanner 新签名
internal/api/v1/library_test.go 改造：NewScanner 新签名
cmd/server/main.go       改造：构造 service 传给 NewScanner

web/src/components/LyricsPanel.vue  改造：暂无歌词处加「获取歌词」按钮
```

---

## 任务 1：Provider 接口 + 迁移共享类型

**文件：**
- 创建：`internal/lyrics/provider.go`
- 修改：`internal/lyrics/lrclib.go`

- [ ] **步骤 1：创建 provider.go**

把 `Query`、`Result`、`ErrInvalidQuery`、`ErrNotFound` 从 lrclib.go 迁来，并给 Query 加 `FilePath`、Result 加 `YRCContent`，定义 Provider 接口：

```go
// internal/lyrics/provider.go
package lyrics

import (
	"context"
	"errors"
)

var (
	ErrInvalidQuery = errors.New("歌词查询参数不足")
	ErrNotFound     = errors.New("歌词不存在")
)

// Query contains track metadata used by lyrics providers.
type Query struct {
	TrackName  string
	ArtistName string
	AlbumName  string
	Duration   int
	FilePath   string // 内嵌源读取文件用
}

// Result contains provider-normalized lyrics content.
type Result struct {
	LRCContent   string
	PlainContent string
	YRCContent   string // 预留网易云逐字歌词
	Source       string // "embedded" / "lrclib" / "netease"
}

// Provider is a single lyrics source.
type Provider interface {
	Name() string
	Fetch(ctx context.Context, q Query) (Result, error)
}
```

- [ ] **步骤 2：从 lrclib.go 删除已迁走的定义**

编辑 `internal/lyrics/lrclib.go`：
- 删除 `var ( ErrInvalidQuery ... ErrNotFound ... )` 整块
- 删除 `type Query struct {...}` 整块
- 删除 `type Result struct {...}` 整块
- import 块中删除不再使用的 `"errors"`（其它 import 仍用到则保留；删 errors 后若编译报未使用再调整）

注意：`Fetch` 内 `return Result{ LRCContent:..., PlainContent:..., Source:"lrclib" }` 不变（Result 现在来自 provider.go，新增的 YRCContent 字段留零值即可）。

- [ ] **步骤 3：给 LRCLIBClient 加 Name() 实现 Provider**

在 `internal/lyrics/lrclib.go` 的 `NewLRCLIBClient` 之后加：

```go
// Name implements Provider.
func (c *LRCLIBClient) Name() string { return "lrclib" }
```

- [ ] **步骤 4：编译验证**

```bash
go build ./internal/lyrics/
```

预期：编译成功（此时 scrape.go 仍用旧的 `lyricsProvider` 接口 + Fetch，类型兼容，不受影响）。再跑全项目编译：

```bash
go build ./...
```

预期：成功。

- [ ] **步骤 5：提交**

```bash
git add internal/lyrics/provider.go internal/lyrics/lrclib.go
git commit -m "feat(lyrics): 抽出 Provider 接口，Query 加 FilePath、Result 加 YRCContent"
```

---

## 任务 2：内嵌标签歌词 Provider

**文件：**
- 创建：`internal/lyrics/embedded.go`
- 创建：`internal/lyrics/embedded_test.go`

- [ ] **步骤 1：写失败测试**

```go
// internal/lyrics/embedded_test.go
package lyrics

import (
	"context"
	"errors"
	"testing"
)

func TestEmbeddedProvider_Name(t *testing.T) {
	p := NewEmbeddedProvider()
	if p.Name() != "embedded" {
		t.Errorf("Name = %q, want embedded", p.Name())
	}
}

func TestEmbeddedProvider_MissingFile_NotFound(t *testing.T) {
	p := NewEmbeddedProvider()
	_, err := p.Fetch(context.Background(), Query{FilePath: "/nonexistent/file.mp3"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestEmbeddedProvider_EmptyPath_NotFound(t *testing.T) {
	p := NewEmbeddedProvider()
	_, err := p.Fetch(context.Background(), Query{FilePath: ""})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}
```

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/lyrics/... -run TestEmbeddedProvider -v
```

预期：FAIL（`NewEmbeddedProvider` 未定义）

- [ ] **步骤 3：实现 embedded.go**

```go
// internal/lyrics/embedded.go
package lyrics

import (
	"context"
	"os"
	"strings"

	"github.com/dhowden/tag"
)

// EmbeddedProvider reads lyrics embedded in the audio file's tags (USLT/LYRICS).
// It is purely local and makes no network calls.
type EmbeddedProvider struct{}

// NewEmbeddedProvider creates an EmbeddedProvider.
func NewEmbeddedProvider() *EmbeddedProvider { return &EmbeddedProvider{} }

// Name implements Provider.
func (p *EmbeddedProvider) Name() string { return "embedded" }

// Fetch reads embedded lyrics from q.FilePath. Returns ErrNotFound when the file
// is missing/unreadable or carries no embedded lyrics.
func (p *EmbeddedProvider) Fetch(_ context.Context, q Query) (Result, error) {
	if strings.TrimSpace(q.FilePath) == "" {
		return Result{}, ErrNotFound
	}
	f, err := os.Open(q.FilePath)
	if err != nil {
		return Result{}, ErrNotFound
	}
	defer f.Close()

	meta, err := tag.ReadFrom(f)
	if err != nil {
		return Result{}, ErrNotFound
	}
	content := strings.TrimSpace(meta.Lyrics())
	if content == "" {
		return Result{}, ErrNotFound
	}
	// 内嵌歌词通常为纯文本（无时间轴）。若本身是 LRC 时间轴格式则前端解析器同样能处理。
	return Result{
		LRCContent:   content,
		PlainContent: content,
		Source:       "embedded",
	}, nil
}
```

- [ ] **步骤 4：运行测试，确认通过**

```bash
go test ./internal/lyrics/... -run TestEmbeddedProvider -v
```

预期：3 个测试全部 PASS

- [ ] **步骤 5：提交**

```bash
git add internal/lyrics/embedded.go internal/lyrics/embedded_test.go
git commit -m "feat(lyrics): 内嵌标签歌词 Provider"
```

---

## 任务 3：LyricsService 编排

**文件：**
- 创建：`internal/lyrics/service.go`
- 创建：`internal/lyrics/service_test.go`

- [ ] **步骤 1：写失败测试**

```go
// internal/lyrics/service_test.go
package lyrics

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

// fakeProvider 用于测试链行为
type fakeProvider struct {
	name   string
	result Result
	err    error
	calls  *int
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Fetch(_ context.Context, _ Query) (Result, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.result, f.err
}

func newServiceTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	// 预置一首曲目
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','艺术家')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','专辑','a1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,duration,is_available,scrape_status) VALUES('t1','歌名','a1','al1','/tmp/x.mp3','mp3',200,1,'pending')`); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestScrapeTrack_TrackNotFound(t *testing.T) {
	d := newServiceTestDB(t)
	svc := NewLyricsService(d)
	_, err := svc.ScrapeTrack(context.Background(), "nope")
	if !errors.Is(err, ErrTrackNotFound) {
		t.Errorf("want ErrTrackNotFound, got %v", err)
	}
}

func TestScrapeTrack_FirstProviderWins_ShortCircuits(t *testing.T) {
	d := newServiceTestDB(t)
	lrclibCalls := 0
	embedded := &fakeProvider{name: "embedded", result: Result{LRCContent: "[00:01.00]hi", Source: "embedded"}}
	lrclib := &fakeProvider{name: "lrclib", result: Result{}, err: ErrNotFound, calls: &lrclibCalls}
	svc := NewLyricsService(d, embedded, lrclib)

	out, err := svc.ScrapeTrack(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ScrapeTrack: %v", err)
	}
	if out.Status != "done" || out.Source != "embedded" {
		t.Errorf("got %+v, want done/embedded", out)
	}
	if lrclibCalls != 0 {
		t.Errorf("lrclib 不应被调用（embedded 已命中），实际调用 %d 次", lrclibCalls)
	}
	// 歌词应写入库
	var lrc string
	d.QueryRow(`SELECT lrc_content FROM lyrics WHERE track_id='t1'`).Scan(&lrc)
	if lrc != "[00:01.00]hi" {
		t.Errorf("lrc_content = %q", lrc)
	}
	// scrape_status 应为 done
	var st string
	d.QueryRow(`SELECT scrape_status FROM tracks WHERE id='t1'`).Scan(&st)
	if st != "done" {
		t.Errorf("scrape_status = %q, want done", st)
	}
}

func TestScrapeTrack_FallsThroughToSecond(t *testing.T) {
	d := newServiceTestDB(t)
	embedded := &fakeProvider{name: "embedded", err: ErrNotFound}
	lrclib := &fakeProvider{name: "lrclib", result: Result{LRCContent: "[00:02.00]yo", Source: "lrclib"}}
	svc := NewLyricsService(d, embedded, lrclib)

	out, err := svc.ScrapeTrack(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ScrapeTrack: %v", err)
	}
	if out.Status != "done" || out.Source != "lrclib" {
		t.Errorf("got %+v, want done/lrclib", out)
	}
}

func TestScrapeTrack_AllNotFound_Failed(t *testing.T) {
	d := newServiceTestDB(t)
	embedded := &fakeProvider{name: "embedded", err: ErrNotFound}
	lrclib := &fakeProvider{name: "lrclib", err: ErrNotFound}
	svc := NewLyricsService(d, embedded, lrclib)

	out, err := svc.ScrapeTrack(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ScrapeTrack should not error on all-not-found: %v", err)
	}
	if out.Status != "failed" {
		t.Errorf("Status = %q, want failed", out.Status)
	}
	var st string
	d.QueryRow(`SELECT scrape_status FROM tracks WHERE id='t1'`).Scan(&st)
	if st != "failed" {
		t.Errorf("scrape_status = %q, want failed", st)
	}
}

func TestScrapeTrack_AlreadyHasLyrics_Skipped(t *testing.T) {
	d := newServiceTestDB(t)
	d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source) VALUES('t1','[00:00.00]exist','manual')`)
	called := 0
	embedded := &fakeProvider{name: "embedded", err: ErrNotFound, calls: &called}
	svc := NewLyricsService(d, embedded)

	out, err := svc.ScrapeTrack(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ScrapeTrack: %v", err)
	}
	if out.Status != "skipped" {
		t.Errorf("Status = %q, want skipped", out.Status)
	}
	if called != 0 {
		t.Errorf("已有歌词不应调用 provider，实际 %d 次", called)
	}
}

func TestScrapeTrack_ProviderError_Propagates(t *testing.T) {
	d := newServiceTestDB(t)
	boom := errors.New("network boom")
	lrclib := &fakeProvider{name: "lrclib", err: boom}
	svc := NewLyricsService(d, lrclib)

	_, err := svc.ScrapeTrack(context.Background(), "t1")
	if err == nil {
		t.Error("provider 非 ErrNotFound 错误应透传")
	}
}
```

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/lyrics/... -run TestScrapeTrack -v
```

预期：FAIL（`NewLyricsService`、`ErrTrackNotFound` 未定义）

- [ ] **步骤 3：实现 service.go**

```go
// internal/lyrics/service.go
package lyrics

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrTrackNotFound is returned when the track id does not exist / is unavailable.
var ErrTrackNotFound = errors.New("曲目不存在")

// ScrapeOutcome reports the result of scraping a single track.
type ScrapeOutcome struct {
	Status string // "done" | "skipped" | "failed"
	Source string
}

// LyricsService orchestrates lyrics lookup across providers and persistence.
type LyricsService struct {
	db        *sql.DB
	providers []Provider // 按优先级排序
}

// NewLyricsService creates a service with providers tried in the given order.
func NewLyricsService(db *sql.DB, providers ...Provider) *LyricsService {
	return &LyricsService{db: db, providers: providers}
}

type trackInfo struct {
	Title    string
	Artist   string
	Album    string
	Duration int
	FilePath string
}

// ScrapeTrack runs the full lyrics scrape pipeline for one track.
func (s *LyricsService) ScrapeTrack(ctx context.Context, trackID string) (ScrapeOutcome, error) {
	track, err := s.loadTrack(trackID)
	if err != nil {
		return ScrapeOutcome{}, err
	}

	has, err := s.hasLyrics(trackID)
	if err != nil {
		return ScrapeOutcome{}, err
	}
	if has {
		_ = s.updateStatus(trackID, "done")
		return ScrapeOutcome{Status: "skipped"}, nil
	}

	q := Query{
		TrackName:  track.Title,
		ArtistName: track.Artist,
		AlbumName:  track.Album,
		Duration:   track.Duration,
		FilePath:   track.FilePath,
	}

	for _, p := range s.providers {
		res, ferr := p.Fetch(ctx, q)
		if ferr == nil {
			if err := s.saveLyrics(trackID, res); err != nil {
				return ScrapeOutcome{}, err
			}
			if err := s.updateStatus(trackID, "done"); err != nil {
				return ScrapeOutcome{}, err
			}
			return ScrapeOutcome{Status: "done", Source: res.Source}, nil
		}
		if errors.Is(ferr, ErrNotFound) || errors.Is(ferr, ErrInvalidQuery) {
			continue // 该源无结果，试下一个
		}
		return ScrapeOutcome{}, ferr // 网络/IO 等真实错误透传
	}

	_ = s.updateStatus(trackID, "failed")
	return ScrapeOutcome{Status: "failed"}, nil
}

func (s *LyricsService) loadTrack(trackID string) (trackInfo, error) {
	var t trackInfo
	err := s.db.QueryRow(`
		SELECT tr.title, COALESCE(ar.name,''), COALESCE(al.title,''),
		       COALESCE(tr.duration,0), tr.file_path
		FROM tracks tr
		LEFT JOIN artists ar ON ar.id = tr.artist_id
		LEFT JOIN albums al ON al.id = tr.album_id
		WHERE tr.id=? AND tr.is_available=1`, trackID).
		Scan(&t.Title, &t.Artist, &t.Album, &t.Duration, &t.FilePath)
	if errors.Is(err, sql.ErrNoRows) {
		return trackInfo{}, ErrTrackNotFound
	}
	return t, err
}

func (s *LyricsService) hasLyrics(trackID string) (bool, error) {
	var one int
	err := s.db.QueryRow(`
		SELECT 1 FROM lyrics
		WHERE track_id=?
		  AND (trim(COALESCE(lrc_content,'')) <> '' OR trim(COALESCE(yrc_content,'')) <> '')`,
		trackID).Scan(&one)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (s *LyricsService) saveLyrics(trackID string, res Result) error {
	source := strings.TrimSpace(res.Source)
	if source == "" {
		source = "unknown"
	}
	_, err := s.db.Exec(`
		INSERT INTO lyrics(track_id,lrc_content,yrc_content,source,updated_at)
		VALUES(?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(track_id) DO UPDATE SET
			lrc_content=excluded.lrc_content,
			yrc_content=excluded.yrc_content,
			source=excluded.source,
			updated_at=CURRENT_TIMESTAMP`,
		trackID, res.LRCContent, res.YRCContent, source)
	return err
}

func (s *LyricsService) updateStatus(trackID, status string) error {
	_, err := s.db.Exec(`UPDATE tracks SET scrape_status=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, trackID)
	return err
}
```

- [ ] **步骤 4：运行测试，确认通过**

```bash
go test ./internal/lyrics/... -v
```

预期：所有 lyrics 测试 PASS（含任务 2 的 embedded 测试）

- [ ] **步骤 5：提交**

```bash
git add internal/lyrics/service.go internal/lyrics/service_test.go
git commit -m "feat(lyrics): LyricsService 编排（缓存检查 + Provider 链 + 写库）"
```

---

## 任务 4：scrape.go 改造为薄壳

**文件：**
- 修改：`internal/api/v1/scrape.go`
- 修改：`internal/api/v1/scrape_test.go`

- [ ] **步骤 1：完整重写 scrape.go**

```go
// internal/api/v1/scrape.go
package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/lyrics"
)

// ScrapeResponse is returned by the scrape endpoint.
type ScrapeResponse struct {
	TrackID string `json:"track_id"`
	Status  string `json:"status"`
	Source  string `json:"source,omitempty"`
	Message string `json:"message,omitempty"`
}

// ScrapeHandler handles track scraping endpoints.
type ScrapeHandler struct {
	service *lyrics.LyricsService
}

// NewScrapeHandler creates a ScrapeHandler backed by a LyricsService.
func NewScrapeHandler(service *lyrics.LyricsService) *ScrapeHandler {
	return &ScrapeHandler{service: service}
}

// ScrapeTrack handles POST /api/v1/tracks/{id}/scrape.
func (h *ScrapeHandler) ScrapeTrack(w http.ResponseWriter, r *http.Request) {
	h.scrapeTrack(w, r, chi.URLParam(r, "id"))
}

func (h *ScrapeHandler) scrapeTrack(w http.ResponseWriter, r *http.Request, trackID string) {
	if h.service == nil {
		writeJSONError(w, http.StatusBadGateway, "歌词刮削源不可用")
		return
	}
	outcome, err := h.service.ScrapeTrack(r.Context(), trackID)
	if err != nil {
		if errors.Is(err, lyrics.ErrTrackNotFound) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "歌词刮削失败")
		return
	}
	if outcome.Status == "failed" {
		writeJSONError(w, http.StatusNotFound, "未找到歌词")
		return
	}
	writeScrapeJSON(w, ScrapeResponse{
		TrackID: trackID,
		Status:  outcome.Status,
		Source:  outcome.Source,
	})
}

func writeScrapeJSON(w http.ResponseWriter, resp ScrapeResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("写刮削响应失败", "err", err)
	}
}
```

- [ ] **步骤 2：重写 scrape_test.go**

```go
// internal/api/v1/scrape_test.go
package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/lyrics"
)

// stubProvider 让 LyricsService 走可控路径
type stubProvider struct {
	res lyrics.Result
	err error
}

func (s stubProvider) Name() string { return "stub" }
func (s stubProvider) Fetch(_ context.Context, _ lyrics.Query) (lyrics.Result, error) {
	return s.res, s.err
}

func TestScrape_Success(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d) // 来自 testhelpers_test.go：含曲目 t1
	svc := lyrics.NewLyricsService(d, stubProvider{res: lyrics.Result{LRCContent: "[00:01.00]hi", Source: "lrclib"}})
	h := NewScrapeHandler(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/scrape", nil)
	h.scrapeTrack(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp ScrapeResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "done" || resp.Source != "lrclib" {
		t.Errorf("got %+v", resp)
	}
}

func TestScrape_NotFoundLyrics(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	svc := lyrics.NewLyricsService(d, stubProvider{err: lyrics.ErrNotFound})
	h := NewScrapeHandler(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/scrape", nil)
	h.scrapeTrack(w, req, "t1")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestScrape_TrackNotFound(t *testing.T) {
	d := newTestDB(t)
	svc := lyrics.NewLyricsService(d)
	h := NewScrapeHandler(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/nope/scrape", nil)
	h.scrapeTrack(w, req, "nope")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
```

注意：`newTestDB` 和 `insertTestData` 已存在于 `internal/api/v1/testhelpers_test.go`（任务来自 albums 测试）。确认 `insertTestData` 插入的曲目 id 是 `t1`：

```bash
grep -n "INSERT INTO tracks" internal/api/v1/testhelpers_test.go
```

若插入的 track id 不是 `t1`，把测试里的 `"t1"` 改成实际 id。

- [ ] **步骤 3：运行测试 + 编译**

注意此时 router.go 仍以旧签名 `NewScrapeHandler(db, provider)` 调用，会编译失败——任务 5 修复。先单独验证 v1 包测试无法跑（因 router 在 api 包，不影响 v1 包自身编译）：

```bash
go test ./internal/api/v1/... -run TestScrape -v
```

预期：v1 包测试 PASS（v1 包自身不依赖 router.go）。

- [ ] **步骤 4：提交**

```bash
git add internal/api/v1/scrape.go internal/api/v1/scrape_test.go
git commit -m "feat(lyrics): ScrapeHandler 改为调用 LyricsService 的薄壳"
```

---

## 任务 5：Router + main.go 接线

**文件：**
- 修改：`internal/api/router.go`
- 修改：`cmd/server/main.go`

- [ ] **步骤 1：更新 router.go 构造 service**

`internal/api/router.go` 中找到 scrape 那行：

```go
		scrape := v1.NewScrapeHandler(db, lyricspkg.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil))
		r.Post("/tracks/{id}/scrape", scrape.ScrapeTrack)
```

替换为：

```go
		lyricsService := lyricspkg.NewLyricsService(
			db,
			lyricspkg.NewEmbeddedProvider(),
			lyricspkg.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil),
		)
		scrape := v1.NewScrapeHandler(lyricsService)
		r.Post("/tracks/{id}/scrape", scrape.ScrapeTrack)
```

- [ ] **步骤 2：编译验证**

```bash
go build ./internal/api/...
```

预期：成功（注意：main.go 里 NewScanner 还是旧签名，任务 6 会改 NewScanner 加 service 参数；本任务先不动 scanner，仅修复 scrape 接线）。

- [ ] **步骤 3：跑全部 api 测试**

```bash
go test ./internal/api/... -v 2>&1 | tail -20
```

预期：全部 PASS。

- [ ] **步骤 4：提交**

```bash
git add internal/api/router.go
git commit -m "feat(lyrics): router 构造 LyricsService（embedded + lrclib）注入 ScrapeHandler"
```

---

## 任务 6：扫描器后台刮削阶段

**文件：**
- 修改：`internal/scanner/scanner.go`
- 修改：`internal/scanner/scanner_test.go`
- 修改：`internal/api/router.go`、`internal/api/router_test.go`、`internal/api/v1/library_test.go`、`cmd/server/main.go`（NewScanner 新签名）

- [ ] **步骤 1：写失败测试**

在 `internal/scanner/scanner_test.go` 末尾追加（验证刮削阶段遍历 pending 并计数；用一个本包内的假 service 不现实——改为验证：未启用刮削时 phase 最终为 idle，且带一个真实文件 + stub provider 的端到端小测）。

由于 `LyricsService` 在 `internal/lyrics` 包，scanner 测试可直接构造它 + 一个假 Provider。追加：

```go
func TestDoScan_ScrapePhase_MarksDone(t *testing.T) {
	dir := t.TempDir()
	// 造一个能被 walker 发现、tag 读不出但能入库的文件
	f := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(f, []byte("notreal"), 0644); err != nil {
		t.Fatal(err)
	}

	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	svc := lyrics.NewLyricsService(d, scanStubProvider{res: lyrics.Result{LRCContent: "[00:01.00]hi", Source: "lrclib"}})
	s := NewScanner(d, config.LibraryConfig{Paths: []string{dir}}, "", svc, true)
	defer s.Stop()

	s.TriggerScan()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !s.Status().Running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	st := s.Status()
	if st.Running {
		t.Fatal("扫描应已结束")
	}
	if st.LyricsScraped < 1 {
		t.Errorf("LyricsScraped = %d, want >=1", st.LyricsScraped)
	}
	if st.Phase != "idle" {
		t.Errorf("Phase = %q, want idle", st.Phase)
	}
}

type scanStubProvider struct {
	res lyrics.Result
	err error
}

func (p scanStubProvider) Name() string { return "stub" }
func (p scanStubProvider) Fetch(_ context.Context, _ lyrics.Query) (lyrics.Result, error) {
	return p.res, p.err
}
```

在 scanner_test.go 顶部 import 补 `"context"`、`"github.com/yxx-z/lyra/internal/lyrics"`（`os`/`filepath`/`time`/`db`/`config` 应已有，缺则补）。

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/scanner/... -run TestDoScan_ScrapePhase -v
```

预期：FAIL（NewScanner 签名不符 + Phase/LyricsScraped 字段不存在）

- [ ] **步骤 3：改造 scanner.go —— 结构体、ScanStatus、NewScanner**

在 import 块加入 `"github.com/yxx-z/lyra/internal/lyrics"`。

`ScanStatus` 替换为：

```go
// ScanStatus is a point-in-time snapshot of scanner progress.
type ScanStatus struct {
	Running       bool      `json:"running"`
	Total         int64     `json:"total"`
	Processed     int64     `json:"processed"`
	Errors        int64     `json:"errors"`
	StartedAt     time.Time `json:"started_at"`
	Phase         string    `json:"phase"`          // "scanning" | "scraping" | "idle"
	LyricsScraped int64     `json:"lyrics_scraped"`
}
```

`Scanner` 结构体新增字段（在 `ffprobePath string` 后）：

```go
	lyricsService *lyrics.LyricsService
	scrapeEnabled bool

	lyricsScraped atomic.Int64
	phase         atomic.Value // string
```

`NewScanner` 改为：

```go
func NewScanner(db *sql.DB, cfg config.LibraryConfig, ffprobePath string, lyricsService *lyrics.LyricsService, scrapeEnabled bool) *Scanner {
	s := &Scanner{
		db:            db,
		cfg:           cfg,
		ing:           NewIngester(db),
		ffprobePath:   ffprobePath,
		lyricsService: lyricsService,
		scrapeEnabled: scrapeEnabled,
		stopCh:        make(chan struct{}),
	}
	s.phase.Store("idle")
	return s
}
```

- [ ] **步骤 4：改造 Status() 暴露新字段**

找到 `Status()` 方法，改为：

```go
func (s *Scanner) Status() ScanStatus {
	s.mu.RLock()
	startedAt := s.startedAt
	s.mu.RUnlock()
	phase, _ := s.phase.Load().(string)
	if phase == "" {
		phase = "idle"
	}
	return ScanStatus{
		Running:       s.running.Load(),
		Total:         s.total.Load(),
		Processed:     s.processed.Load(),
		Errors:        s.errors.Load(),
		StartedAt:     startedAt,
		Phase:         phase,
		LyricsScraped: s.lyricsScraped.Load(),
	}
}
```

- [ ] **步骤 5：doScan 设置 phase + 追加刮削阶段**

在 `doScan` 开头（重置计数处）加入：

```go
	s.lyricsScraped.Store(0)
	s.phase.Store("scanning")
```

并确保 `doScan` 返回前（或 runScan 里 `running.Store(false)` 附近）把 phase 复位为 idle。最稳妥：在 `doScan` 末尾（入库循环结束之后）继续追加刮削阶段，最后 `defer` 或显式置 idle。

在入库消费循环 `for r := range results { ... }` 结束之后、`doScan` 函数结尾之前，追加：

```go
	// 刮削阶段：为缺歌词的曲目串行刮削（受 scraper.enabled 控制，可被 ctx 中断）
	if s.scrapeEnabled && s.lyricsService != nil {
		s.phase.Store("scraping")
		s.scrapePending(ctx)
	}
	s.phase.Store("idle")
```

并新增方法 `scrapePending`：

```go
func (s *Scanner) scrapePending(ctx context.Context) {
	rows, err := s.db.Query(`SELECT id FROM tracks WHERE scrape_status='pending' AND is_available=1`)
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

	for _, id := range ids {
		select {
		case <-ctx.Done():
			return
		default:
		}
		outcome, err := s.lyricsService.ScrapeTrack(ctx, id)
		if err != nil || outcome.Status == "failed" {
			s.errors.Add(1)
		} else if outcome.Status == "done" {
			s.lyricsScraped.Add(1)
		}
		// 间隔：仅当可能发起了网络请求时礼貌等待（内嵌命中/已有歌词不等）
		if outcome.Status == "failed" || (outcome.Status == "done" && outcome.Source != "embedded") {
			select {
			case <-time.After(800 * time.Millisecond):
			case <-ctx.Done():
				return
			}
		}
	}
}
```

注意：`ctx` 是 `doScan` 里已有的 `context.WithCancel(...)` 派生、随 stopCh 取消的 context（确认 doScan 内变量名为 `ctx`；若不同则相应调整）。

- [ ] **步骤 6：更新所有 NewScanner 调用方**

```bash
grep -rn "NewScanner(" --include="*.go" . | grep -v "func NewScanner"
```

逐处补两个参数：
- `cmd/server/main.go`：构造 service 后传入（见步骤 7）
- `internal/scanner/scanner_test.go`：已有调用补 `, nil, false`（除步骤 1 新测试外）
- `internal/api/router_test.go`：`NewScanner(d, config.LibraryConfig{}, "")` → `NewScanner(d, config.LibraryConfig{}, "", nil, false)`
- `internal/api/v1/library_test.go`：同理补 `, nil, false`

- [ ] **步骤 7：main.go 构造 service 传给 NewScanner**

`cmd/server/main.go` 中找到：

```go
	sc := scanner.NewScanner(database, cfg.Library, cfg.Transcode.FfprobePath)
```

替换为：

```go
	lyricsService := lyrics.NewLyricsService(
		database,
		lyrics.NewEmbeddedProvider(),
		lyrics.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil),
	)
	sc := scanner.NewScanner(database, cfg.Library, cfg.Transcode.FfprobePath, lyricsService, cfg.Scraper.Enabled)
```

并在 main.go import 块加入 `"github.com/yxx-z/lyra/internal/lyrics"`。

- [ ] **步骤 8：全量测试 + 编译**

```bash
go build ./...
go test ./... -race 2>&1 | grep -E "^(ok|FAIL)"
```

预期：编译成功，全部测试 PASS，无 race。

- [ ] **步骤 9：提交**

```bash
git add internal/scanner/ internal/api/router.go internal/api/router_test.go internal/api/v1/library_test.go cmd/server/main.go
git commit -m "feat(lyrics): 扫描器后台刮削阶段（phase/lyrics_scraped 进度 + 串行间隔 + 可中断）"
```

---

## 任务 7：前端「获取歌词」按钮

**文件：**
- 修改：`web/src/components/LyricsPanel.vue`

- [ ] **步骤 1：在无歌词分支加按钮 + 状态**

`web/src/components/LyricsPanel.vue` 的「B. 无歌词」分支（`<div v-else-if="error === 'no_lyrics' || lrcLines.length === 0" ...>`）内，在两个 `<p>` 之后、`</div>` 之前加入按钮与提示：

```vue
          <button
            class="custom-btn-primary"
            style="width: auto; padding: 10px 22px; font-size: 14px; margin-top: 18px; display: inline-flex; align-items: center; gap: 8px;"
            type="button"
            :disabled="scraping"
            @click="handleScrape"
          >
            <span v-if="scraping" class="loading-spinner" aria-label="刮削中"></span>
            <span>{{ scraping ? '正在获取歌词…' : '🔍 获取歌词' }}</span>
          </button>
          <p v-if="scrapeMessage" class="muted" style="font-size: 12px; margin-top: 10px;">{{ scrapeMessage }}</p>
```

- [ ] **步骤 2：script 中新增状态与 handleScrape**

在 `<script setup>` 的 ref 声明区（`const currentLineIndex = ...` 附近）加：

```ts
const scraping = ref(false)
const scrapeMessage = ref('')
```

在 `loadLyrics` 函数之后新增：

```ts
async function handleScrape() {
  const track = playerStore.currentTrack
  if (!track || scraping.value) return
  scraping.value = true
  scrapeMessage.value = ''
  try {
    const res = await props.api.scrapeTrack(track.trackId)
    if (res.status === 'done' || res.status === 'skipped') {
      await loadLyrics()
      if (lrcLines.value.length === 0) {
        scrapeMessage.value = '已获取，但该曲目无可显示的同步歌词'
      }
    } else {
      scrapeMessage.value = '未找到歌词'
    }
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      scrapeMessage.value = '未找到歌词'
    } else {
      scrapeMessage.value = '获取失败，请稍后重试'
    }
  } finally {
    scraping.value = false
  }
}
```

（`ApiError` 已在该文件 import；`scrapeTrack` 已在 client.ts 提供。）

- [ ] **步骤 3：切歌时清掉刮削提示**

`loadLyrics` 开头已有重置区（`error.value = null` 那段），在其中加一行 `scrapeMessage.value = ''`，避免切歌后残留上一首的提示。

- [ ] **步骤 4：构建验证**

```bash
cd web && npm run build 2>&1 | tail -6 && cd ..
```

预期：vue-tsc 类型检查 + vite 构建成功。

- [ ] **步骤 5：提交**

```bash
git add web/src/components/LyricsPanel.vue
git commit -m "feat(web): 歌词面板「获取歌词」按钮，触发刮削并重载"
```

---

## 任务 8：端到端验证 + 推送

- [ ] **步骤 1：全量测试 + 构建**

```bash
go test ./... 2>&1 | grep -E "^(ok|FAIL)"
go build ./...
cd web && npm run build 2>&1 | tail -3 && cd ..
```

预期：全部 PASS，编译/构建成功。

- [ ] **步骤 2：冒烟测试刮削端点（无音乐库也可验证 404/契约）**

```bash
go build -o lyra ./cmd/server
cat > /tmp/lyra-smoke.yaml <<'EOF'
server:
  port: 4601
auth:
  disable: true
database:
  path: ./data/music.db
scraper:
  enabled: true
EOF
./lyra --config /tmp/lyra-smoke.yaml > /tmp/lyra-lyrics.log 2>&1 &
sleep 2
echo "=== 对已有曲目触发刮削 ==="
AID=$(curl -s http://localhost:4601/api/v1/albums | grep -o '"id":"[^"]*"' | head -1 | sed 's/"id":"//;s/"//')
TID=$(curl -s "http://localhost:4601/api/v1/albums/$AID" | grep -o '"stream_url":"[^"]*"' | head -1 | sed 's#.*tracks/##;s#/stream"##')
curl -s -X POST "http://localhost:4601/api/v1/tracks/$TID/scrape"
echo ""
echo "=== 读回歌词 ==="
curl -s "http://localhost:4601/api/v1/tracks/$TID/lyrics" | head -c 200
kill %1 2>/dev/null; rm -f lyra /tmp/lyra-smoke.yaml
```

预期：scrape 返回 `{"track_id":...,"status":"done"|"failed",...}`；若 lrclib 命中则 lyrics 有内容。（依赖网络访问 lrclib.net；无网络时返回 failed/404 属正常。）

- [ ] **步骤 3：推送**

```bash
git push origin master
```

---

## 自检清单

**规格覆盖：**
- [x] Provider 接口 + Query.FilePath + Result.YRCContent → 任务 1
- [x] embeddedProvider（内嵌标签）→ 任务 2
- [x] lrclibProvider（Name() 适配）→ 任务 1
- [x] LyricsService 编排（缓存检查 + 链 + 写库 + ScrapeOutcome + ErrTrackNotFound）→ 任务 3
- [x] scrape.go 薄壳（404/502/200 映射）→ 任务 4
- [x] router/main 接线（embedded + lrclib 注入两个消费方）→ 任务 5、6
- [x] 扫描器刮削阶段（phase/lyrics_scraped、串行、800ms 仅网络源后等待、ctx 可中断）→ 任务 6
- [x] 前端「获取歌词」按钮（done/skipped 重载、404 提示、loading）→ 任务 7
- [x] 不做：网易云、手动编辑/删除 UI（spec 已界定）

**类型一致性：**
- `Query{TrackName,ArtistName,AlbumName,Duration,FilePath}`、`Result{LRCContent,PlainContent,YRCContent,Source}`、`Provider{Name,Fetch}`、`ScrapeOutcome{Status,Source}`、`ErrTrackNotFound` —— 全计划统一
- `NewScanner(db, cfg, ffprobePath, lyricsService, scrapeEnabled)` —— 所有调用方任务 6 统一更新
