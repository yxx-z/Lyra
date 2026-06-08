# 专辑元数据 + 封面刮削 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `internal/metadata` 包，按"艺术家+专辑"查 MusicBrainz 补全专辑元数据、用 Cover Art Archive 下高清封面，接入扫描器专辑级刮削阶段 + 按需接口。

**Architecture:** `internal/metadata` 三文件：MusicBrainz 搜索/择优（纯函数 `pickRelease` + HTTP 客户端）、Cover Art Archive 取图、MetadataService 编排（查 MB → 填字段 → 下封面 → 写状态）。扫描器在歌词阶段后追加专辑级元数据阶段（串行、≥1s 间隔、可中断、受 `scraper.enabled` 控）。封面服务在内嵌/本地都缺时用刮削的 `cover_path` 兜底。

**Tech Stack:** Go 1.25（net/http、httptest、modernc.org/sqlite）、MusicBrainz WS/2 + Cover Art Archive。

**Go 环境：** 每个含 `go` 命令的步骤前先 `export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## 设计参考

实现前阅读 `docs/superpowers/specs/2026-06-08-metadata-cover-scraping-design.md`。

**既有可复用模式（勿改）：**
- lyrics 的 Provider/Service + scanner 刮削阶段（`internal/scanner/scanner.go` 的 `scrapePending`）是本次的镜像范本。
- `writeJSONError(w, status, msg)` 定义在 `internal/api/v1/auth.go`，包内可直接用。
- 迁移：`internal/db/migrations/*.up.sql` 按字母序执行；`internal/db/db_test.go` 有迁移测试范例。

**MusicBrainz release 搜索响应结构**（已用真实接口确认）：
```json
{"count":19,"releases":[
  {"id":"faf326c3-...","score":100,"title":"叶惠美","date":"2003-07-31","track-count":11},
  ...
]}
```

**关键约束：**
- `albums.mbid` 是 `UNIQUE` —— 两张本地专辑匹配到同一 release 时设 mbid 会冲突，EnrichAlbum 必须容错（mbid 单独 best-effort 设置，冲突则跳过不致命）。
- genre：MB search 响应不含可靠 genre 字段，本轮 `ReleaseMatch.Genre` 恒为 `""`（COALESCE 保留原值）；不为 genre 单独发请求。字段保留供将来 release-group 查询扩展。
- MusicBrainz 限速 1 req/s，请求必须带 `User-Agent`（否则 403）。

---

## 文件结构

```
internal/metadata/
├── musicbrainz.go        新建：mbRelease/AlbumQuery/ReleaseMatch 类型、pickRelease 纯函数、MusicBrainzClient.Search、ErrNotFound
├── musicbrainz_test.go   新建
├── coverart.go           新建：CoverArtClient.FetchFront、ErrNoCover
├── coverart_test.go      新建
├── service.go            新建：MetadataService.EnrichAlbum、EnrichOutcome、ErrAlbumNotFound
└── service_test.go       新建

internal/db/migrations/003_albums_scrape_status.up.sql   新建
internal/db/schema.sql                                   改：albums 加 scrape_status
internal/db/db_test.go                                   改：加 scrape_status 列存在性测试
internal/api/v1/cover.go                                 改：serving 末尾加 cover_path 兜底
internal/api/v1/cover_test.go                            改：加 cover_path 兜底测试
internal/api/v1/album_scrape.go                          新建：AlbumScrapeHandler
internal/api/v1/album_scrape_test.go                     新建
internal/scanner/scanner.go                              改：NewScanner 增参、ScanStatus 加字段、doScan 加元数据阶段、scrapeAlbumsPending
internal/api/router.go                                   改：构造 MetadataService、注册路由、更新 NewScanner 调用
cmd/server/main.go                                       改：构造 MetadataService、更新 NewScanner 调用
internal/scanner/scanner_test.go / router_test.go / router_scrape_test.go / v1/library_test.go  改：更新 NewScanner 调用（加 nil 参数）
```

**注意：** `ingester.go` 的 `findOrCreateAlbum` INSERT 不写 `scrape_status`，新专辑由列默认值 `'pending'` 兜底，**无需改动**。

---

### Task 1: 迁移 — albums.scrape_status

**Files:**
- Create: `internal/db/migrations/003_albums_scrape_status.up.sql`
- Modify: `internal/db/schema.sql`
- Modify: `internal/db/db_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/db/db_test.go` 末尾追加：
```go
func TestOpen_AlbumsHasScrapeStatusColumn(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT name FROM pragma_table_info('albums') WHERE name='scrape_status'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("albums 表应有 scrape_status 列")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/ -run TestOpen_AlbumsHasScrapeStatusColumn -v`
Expected: FAIL（列不存在）

- [ ] **Step 3: 写迁移 + 同步 schema**

创建 `internal/db/migrations/003_albums_scrape_status.up.sql`：
```sql
ALTER TABLE albums ADD COLUMN scrape_status TEXT DEFAULT 'pending';
CREATE INDEX idx_albums_scrape_status ON albums(scrape_status);
```

在 `internal/db/schema.sql` 的 albums 表定义中，`mbid` 行之后、`created_at` 之前加一行（保持与迁移一致）：
```sql
    mbid         TEXT UNIQUE,
    scrape_status TEXT DEFAULT 'pending',
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
```

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/ -v`
Expected: PASS（全部 db 测试，含迁移幂等）

- [ ] **Step 5: 提交**

```bash
git add internal/db/migrations/003_albums_scrape_status.up.sql internal/db/schema.sql internal/db/db_test.go
git commit -m "feat(db): albums 加 scrape_status 列（迁移 003）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 2: pickRelease 纯函数 + 类型

**Files:**
- Create: `internal/metadata/musicbrainz.go`（本任务先放类型 + pickRelease）
- Create: `internal/metadata/musicbrainz_test.go`

- [ ] **Step 1: 写失败测试**

`internal/metadata/musicbrainz_test.go`：
```go
package metadata

import "testing"

func TestPickRelease_ClosestTrackCount(t *testing.T) {
	rs := []mbRelease{
		{ID: "a", Score: 100, TrackCount: 22}, // 差 11
		{ID: "b", Score: 100, TrackCount: 11}, // 差 0 ← 选这个
		{ID: "c", Score: 100, TrackCount: 14}, // 差 3
	}
	got, ok := pickRelease(rs, 11)
	if !ok || got.ID != "b" {
		t.Fatalf("应选 b，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickRelease_FiltersLowScore(t *testing.T) {
	rs := []mbRelease{
		{ID: "x", Score: 39, TrackCount: 11},
		{ID: "y", Score: 91, TrackCount: 99},
	}
	got, ok := pickRelease(rs, 11)
	if !ok || got.ID != "y" {
		t.Fatalf("应只在 score>=90 里选，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickRelease_AllBelowThreshold(t *testing.T) {
	rs := []mbRelease{{ID: "x", Score: 50, TrackCount: 11}}
	if _, ok := pickRelease(rs, 11); ok {
		t.Error("全部 score<90 应不命中")
	}
}

func TestPickRelease_UnknownLocalCountTakesFirst(t *testing.T) {
	rs := []mbRelease{
		{ID: "first", Score: 100, TrackCount: 20},
		{ID: "second", Score: 95, TrackCount: 11},
	}
	got, ok := pickRelease(rs, 0)
	if !ok || got.ID != "first" {
		t.Fatalf("localCount=0 应取靠前者，得到 %q", got.ID)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run TestPickRelease -v`
Expected: 编译失败（`undefined: mbRelease` / `pickRelease`）

- [ ] **Step 3: 写最小实现**

`internal/metadata/musicbrainz.go`：
```go
package metadata

import "errors"

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
```

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run TestPickRelease -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/metadata/musicbrainz.go internal/metadata/musicbrainz_test.go
git commit -m "feat(metadata): release 择优纯函数 pickRelease"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 3: MusicBrainzClient.Search

**Files:**
- Modify: `internal/metadata/musicbrainz.go`（追加 client）
- Modify: `internal/metadata/musicbrainz_test.go`（追加 httptest）

- [ ] **Step 1: 写失败测试**

在 `internal/metadata/musicbrainz_test.go` 顶部把 `import "testing"` 替换为：
```go
import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)
```
并在文件末尾追加：
```go
func newTestMB(srv *httptest.Server) *MusicBrainzClient {
	c := NewMusicBrainzClient("", "Lyra-Test/0.1", srv.Client())
	c.baseURL = srv.URL
	return c
}

func TestMBSearch_Hit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[
			{"id":"mbid-11","score":100,"title":"叶惠美","date":"2003-07-31","track-count":11},
			{"id":"mbid-22","score":100,"title":"叶惠美","date":"2008-01-23","track-count":22}
		]}`))
	}))
	defer srv.Close()

	m, err := newTestMB(srv).Search(context.Background(), AlbumQuery{AlbumTitle: "叶惠美", ArtistName: "周杰伦", TrackCount: 11})
	if err != nil {
		t.Fatalf("Search err: %v", err)
	}
	if m.MBID != "mbid-11" {
		t.Errorf("应选 11 首的 release，得到 %q", m.MBID)
	}
	if m.ReleaseDate != "2003-07-31" {
		t.Errorf("ReleaseDate = %q", m.ReleaseDate)
	}
}

func TestMBSearch_NoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[{"id":"x","score":40,"title":"别的","track-count":5}]}`))
	}))
	defer srv.Close()
	_, err := newTestMB(srv).Search(context.Background(), AlbumQuery{AlbumTitle: "叶惠美", ArtistName: "周杰伦", TrackCount: 11})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("低 score 应返回 ErrNotFound，得到 %v", err)
	}
}

func TestMBSearch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := newTestMB(srv).Search(context.Background(), AlbumQuery{AlbumTitle: "x", ArtistName: "y", TrackCount: 1})
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("500 应返回普通 error，得到 %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run TestMBSearch -v`
Expected: 编译失败（`undefined: NewMusicBrainzClient`）

- [ ] **Step 3: 写最小实现**

在 `internal/metadata/musicbrainz.go` 顶部 import 改为：
```go
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
```
追加到文件末尾：
```go
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
```

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -v`
Expected: PASS（含 Task 2 的 pickRelease 测试）

- [ ] **Step 5: 提交**

```bash
git add internal/metadata/musicbrainz.go internal/metadata/musicbrainz_test.go
git commit -m "feat(metadata): MusicBrainzClient.Search（release 搜索）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 4: CoverArtClient.FetchFront

**Files:**
- Create: `internal/metadata/coverart.go`
- Create: `internal/metadata/coverart_test.go`

- [ ] **Step 1: 写失败测试**

`internal/metadata/coverart_test.go`：
```go
package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCoverFetch_HitWithRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/release/mbid-1/front" {
			http.Redirect(w, r, "/img.jpg", http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("\xff\xd8\xff JPEGDATA"))
	}))
	defer srv.Close()

	c := NewCoverArtClient(srv.URL, srv.Client())
	data, mime, err := c.FetchFront(context.Background(), "mbid-1")
	if err != nil {
		t.Fatalf("FetchFront err: %v", err)
	}
	if len(data) == 0 {
		t.Error("应返回图片字节")
	}
	if mime != "image/jpeg" {
		t.Errorf("mime = %q", mime)
	}
}

func TestCoverFetch_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := NewCoverArtClient(srv.URL, srv.Client())
	_, _, err := c.FetchFront(context.Background(), "mbid-x")
	if !errors.Is(err, ErrNoCover) {
		t.Errorf("404 应返回 ErrNoCover，得到 %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run TestCoverFetch -v`
Expected: 编译失败（`undefined: NewCoverArtClient` / `ErrNoCover`）

- [ ] **Step 3: 写最小实现**

`internal/metadata/coverart.go`：
```go
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

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return data, mimeType, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run TestCoverFetch -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/metadata/coverart.go internal/metadata/coverart_test.go
git commit -m "feat(metadata): Cover Art Archive 取封面客户端"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 5: MetadataService.EnrichAlbum

**Files:**
- Create: `internal/metadata/service.go`
- Create: `internal/metadata/service_test.go`

- [ ] **Step 1: 写失败测试**

`internal/metadata/service_test.go`：
```go
package metadata

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

// setupAlbum 建库并插入 1 个艺术家 + 1 张专辑 + n 首曲目，返回 db 与 albumID。
func setupAlbum(t *testing.T, trackCount int) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir + "/t.db")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	_, _ = database.Exec(`INSERT INTO artists(id,name) VALUES('ar1','周杰伦')`)
	_, _ = database.Exec(`INSERT INTO albums(id,title,artist_id,scrape_status) VALUES('al1','叶惠美','ar1','pending')`)
	for i := 0; i < trackCount; i++ {
		_, _ = database.Exec(`INSERT INTO tracks(id,title,album_id,artist_id,file_path,is_available) VALUES(?,?,?,?,?,1)`,
			"tr"+string(rune('a'+i)), "曲", "al1", "ar1", "/m/"+string(rune('a'+i))+".flac")
	}
	return database, "al1"
}

func mbAndCaaServers(t *testing.T, mbBody string, caaStatus int) (mbURL, caaURL string) {
	t.Helper()
	mb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(mbBody))
	}))
	t.Cleanup(mb.Close)
	caa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if caaStatus == http.StatusOK {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("\xff\xd8\xffJPEG"))
			return
		}
		w.WriteHeader(caaStatus)
	}))
	t.Cleanup(caa.Close)
	return mb.URL, caa.URL
}

func newSvc(t *testing.T, database *sql.DB, mbURL, caaURL, artDir string) *MetadataService {
	mb := NewMusicBrainzClient(mbURL, "Lyra-Test/0.1", nil)
	mb.baseURL = mbURL
	cover := NewCoverArtClient(caaURL, nil)
	return NewMetadataService(database, mb, cover, artDir)
}

func TestEnrichAlbum_HitWithCover(t *testing.T) {
	database, id := setupAlbum(t, 11)
	mbURL, caaURL := mbAndCaaServers(t,
		`{"releases":[{"id":"mbid-11","score":100,"title":"叶惠美","date":"2003-07-31","track-count":11}]}`,
		http.StatusOK)
	artDir := t.TempDir()

	out, err := newSvc(t, database, mbURL, caaURL, artDir).EnrichAlbum(context.Background(), id)
	if err != nil {
		t.Fatalf("EnrichAlbum err: %v", err)
	}
	if out.Status != "done" || !out.HasCover || out.MBID != "mbid-11" {
		t.Fatalf("outcome = %+v", out)
	}

	var mbid, date, cover, status string
	database.QueryRow(`SELECT COALESCE(mbid,''),COALESCE(release_date,''),COALESCE(cover_path,''),scrape_status FROM albums WHERE id=?`, id).
		Scan(&mbid, &date, &cover, &status)
	if mbid != "mbid-11" || date != "2003-07-31" || cover == "" || status != "done" {
		t.Errorf("db 落库错误: mbid=%q date=%q cover=%q status=%q", mbid, date, cover, status)
	}
	if _, statErr := os.Stat(cover); statErr != nil {
		t.Errorf("封面文件应存在: %v", statErr)
	}
}

func TestEnrichAlbum_NoMatch(t *testing.T) {
	database, id := setupAlbum(t, 11)
	mbURL, caaURL := mbAndCaaServers(t, `{"releases":[{"id":"x","score":30,"track-count":5}]}`, http.StatusNotFound)
	out, err := newSvc(t, database, mbURL, caaURL, t.TempDir()).EnrichAlbum(context.Background(), id)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "failed" {
		t.Errorf("无匹配应 failed，得到 %q", out.Status)
	}
	var status string
	database.QueryRow(`SELECT scrape_status FROM albums WHERE id=?`, id).Scan(&status)
	if status != "failed" {
		t.Errorf("db status 应 failed，得到 %q", status)
	}
}

func TestEnrichAlbum_HitNoCover(t *testing.T) {
	database, id := setupAlbum(t, 11)
	mbURL, caaURL := mbAndCaaServers(t,
		`{"releases":[{"id":"mbid-11","score":100,"date":"2003","track-count":11}]}`,
		http.StatusNotFound) // CAA 404
	out, err := newSvc(t, database, mbURL, caaURL, t.TempDir()).EnrichAlbum(context.Background(), id)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "done" || out.HasCover {
		t.Errorf("无封面应 done 且 HasCover=false，得到 %+v", out)
	}
	var cover, status string
	database.QueryRow(`SELECT COALESCE(cover_path,''),scrape_status FROM albums WHERE id=?`, id).Scan(&cover, &status)
	if cover != "" || status != "done" {
		t.Errorf("cover 应空 status 应 done，得到 cover=%q status=%q", cover, status)
	}
}

func TestEnrichAlbum_AlbumNotFound(t *testing.T) {
	database, _ := setupAlbum(t, 1)
	mbURL, caaURL := mbAndCaaServers(t, `{"releases":[]}`, http.StatusNotFound)
	_, err := newSvc(t, database, mbURL, caaURL, t.TempDir()).EnrichAlbum(context.Background(), "nonexistent")
	if !errors.Is(err, ErrAlbumNotFound) {
		t.Errorf("不存在的专辑应 ErrAlbumNotFound，得到 %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run TestEnrichAlbum -v`
Expected: 编译失败（`undefined: NewMetadataService` / `ErrAlbumNotFound`）

- [ ] **Step 3: 写最小实现**

`internal/metadata/service.go`：
```go
package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// ErrAlbumNotFound 表示数据库中无此专辑。
var ErrAlbumNotFound = errors.New("专辑不存在")

// EnrichOutcome 是 EnrichAlbum 的结果。
type EnrichOutcome struct {
	Status   string // "done" | "failed"
	MBID     string
	HasCover bool
}

// MetadataService 编排专辑元数据 + 封面刮削。
type MetadataService struct {
	db         *sql.DB
	mb         *MusicBrainzClient
	cover      *CoverArtClient
	artworkDir string
}

// NewMetadataService 创建服务。
func NewMetadataService(db *sql.DB, mb *MusicBrainzClient, cover *CoverArtClient, artworkDir string) *MetadataService {
	return &MetadataService{db: db, mb: mb, cover: cover, artworkDir: artworkDir}
}

// EnrichAlbum 为单张专辑查 MB 补元数据并下封面。
func (s *MetadataService) EnrichAlbum(ctx context.Context, albumID string) (EnrichOutcome, error) {
	var title, artist string
	var trackCount int
	err := s.db.QueryRowContext(ctx, `
		SELECT a.title, COALESCE(ar.name,''),
		       (SELECT COUNT(*) FROM tracks WHERE album_id=a.id AND is_available=1)
		FROM albums a LEFT JOIN artists ar ON ar.id=a.artist_id
		WHERE a.id=?`, albumID).Scan(&title, &artist, &trackCount)
	if errors.Is(err, sql.ErrNoRows) {
		return EnrichOutcome{}, ErrAlbumNotFound
	}
	if err != nil {
		return EnrichOutcome{}, err
	}

	match, err := s.mb.Search(ctx, AlbumQuery{AlbumTitle: title, ArtistName: artist, TrackCount: trackCount})
	if errors.Is(err, ErrNotFound) {
		s.setStatus(ctx, albumID, "failed")
		return EnrichOutcome{Status: "failed"}, nil
	}
	if err != nil {
		return EnrichOutcome{}, err
	}

	// 元数据落库：release_date/genre 用 COALESCE(NULLIF) 仅在非空时覆盖。
	if _, err := s.db.ExecContext(ctx, `
		UPDATE albums SET
			release_date=COALESCE(NULLIF(?,''),release_date),
			genre=COALESCE(NULLIF(?,''),genre),
			updated_at=?
		WHERE id=?`,
		match.ReleaseDate, match.Genre, time.Now(), albumID); err != nil {
		return EnrichOutcome{}, err
	}
	// mbid 单独 best-effort 设置：UNIQUE 冲突（别的专辑已占）时跳过，不致命。
	if _, err := s.db.ExecContext(ctx, `UPDATE albums SET mbid=? WHERE id=?`, match.MBID, albumID); err != nil {
		slog.Warn("设置专辑 mbid 失败（可能 UNIQUE 冲突）", "album", albumID, "mbid", match.MBID, "err", err)
	}

	hasCover := s.downloadCover(ctx, albumID, match.MBID)

	s.setStatus(ctx, albumID, "done")
	return EnrichOutcome{Status: "done", MBID: match.MBID, HasCover: hasCover}, nil
}

// downloadCover 下封面到 artworkDir 并写 cover_path；成功返回 true。
func (s *MetadataService) downloadCover(ctx context.Context, albumID, mbid string) bool {
	data, mime, err := s.cover.FetchFront(ctx, mbid)
	if err != nil {
		if !errors.Is(err, ErrNoCover) {
			slog.Warn("下载封面失败", "album", albumID, "err", err)
		}
		return false
	}
	ext := ".jpg"
	if mime == "image/png" {
		ext = ".png"
	}
	if err := os.MkdirAll(s.artworkDir, 0o755); err != nil {
		slog.Warn("创建封面目录失败", "dir", s.artworkDir, "err", err)
		return false
	}
	path := filepath.Join(s.artworkDir, albumID+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Warn("写封面文件失败", "path", path, "err", err)
		return false
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE albums SET cover_path=? WHERE id=?`, path, albumID); err != nil {
		slog.Warn("写 cover_path 失败", "album", albumID, "err", err)
		return false
	}
	return true
}

func (s *MetadataService) setStatus(ctx context.Context, albumID, status string) {
	if _, err := s.db.ExecContext(ctx, `UPDATE albums SET scrape_status=? WHERE id=?`, status, albumID); err != nil {
		slog.Warn("更新专辑 scrape_status 失败", "album", albumID, "err", err)
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -v`
Expected: PASS（本包全部测试）

- [ ] **Step 5: 提交**

```bash
git add internal/metadata/service.go internal/metadata/service_test.go
git commit -m "feat(metadata): MetadataService.EnrichAlbum 编排"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 6: 封面服务 cover_path 兜底

**Files:**
- Modify: `internal/api/v1/cover.go`
- Modify: `internal/api/v1/cover_test.go`

- [ ] **Step 1: 写失败测试**

`cover_test.go` 已有 `newTestDB(t) *sql.DB` 辅助、并直接调用 `h.getCover(w, req, albumID)`（见 `TestGetCover_CoverJpg`）。仿其风格在文件末尾追加：
```go
func TestGetCover_CoverPathFallback(t *testing.T) {
	d := newTestDB(t)

	dir := t.TempDir()
	coverFile := filepath.Join(dir, "scraped.jpg")
	if err := os.WriteFile(coverFile, []byte("\xff\xd8\xffJPEG"), 0644); err != nil {
		t.Fatal(err)
	}

	// 专辑无内嵌/本地封面，但 cover_path 指向缓存文件；track 的 file_path 指向不含封面的临时目录
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('ar','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id,cover_path) VALUES('al','T','ar',?)`, coverFile); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(
		`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('tr','t','ar','al',?,'',1,'pending')`,
		filepath.Join(dir, "song.flac"),
	); err != nil {
		t.Fatal(err)
	}

	h := NewCoverHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cover/al", nil)
	h.getCover(w, req, "al")

	if w.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("应返回封面字节")
	}
}
```
（`cover_test.go` 顶部 import 已有 `net/http`、`net/http/httptest`、`os`、`path/filepath`，无需新增。）

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestGetCover_CoverPathFallback -v`
Expected: FAIL（当前无 cover_path 兜底，返回 404）

- [ ] **Step 3: 写最小实现**

在 `internal/api/v1/cover.go` 的 `getCover` 中，把结尾的 `http.NotFound(w, r)`（本地封面循环之后）替换为 cover_path 兜底逻辑：

找到（本地封面 for 循环之后）：
```go
	http.NotFound(w, r)
}
```
替换为：
```go
	// 刮削封面兜底：内嵌与本地都没有时，用 albums.cover_path 指向的缓存文件。
	var coverPath sql.NullString
	if err := h.db.QueryRow(`SELECT cover_path FROM albums WHERE id=?`, albumID).Scan(&coverPath); err == nil && coverPath.Valid && coverPath.String != "" {
		if data, rerr := os.ReadFile(coverPath.String); rerr == nil && len(data) > 0 {
			mimeType := "image/jpeg"
			if strings.HasSuffix(strings.ToLower(coverPath.String), ".png") {
				mimeType = "image/png"
			}
			w.Header().Set("Content-Type", mimeType)
			_, _ = w.Write(data)
			return
		}
	}

	http.NotFound(w, r)
}
```
（`cover.go` 已 import `database/sql`、`os`、`strings`，无需新增。）

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestGetCover -v`
Expected: PASS（含现有 cover 测试与新兜底测试）

- [ ] **Step 5: 提交**

```bash
git add internal/api/v1/cover.go internal/api/v1/cover_test.go
git commit -m "feat(api): 封面服务追加 cover_path 兜底（内嵌/本地缺失时）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 7: AlbumScrapeHandler（按需接口）

**Files:**
- Create: `internal/api/v1/album_scrape.go`
- Create: `internal/api/v1/album_scrape_test.go`

- [ ] **Step 1: 写失败测试**

`internal/api/v1/album_scrape_test.go`：
```go
package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/metadata"
)

func mkMetaSvc(t *testing.T, mbBody string, caaStatus int) (*metadata.MetadataService, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	mb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(mbBody)) }))
	t.Cleanup(mb.Close)
	caa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if caaStatus == http.StatusOK {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("\xff\xd8\xffJPEG"))
			return
		}
		w.WriteHeader(caaStatus)
	}))
	t.Cleanup(caa.Close)
	svc := metadata.NewMetadataService(database, metadata.NewMusicBrainzClient(mb.URL, "T/0.1", nil), metadata.NewCoverArtClient(caa.URL, nil), dir)
	return svc, database
}

func doScrapeReq(h *AlbumScrapeHandler, albumID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/albums/"+albumID+"/scrape", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", albumID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.ScrapeAlbum(rec, req)
	return rec
}

func TestAlbumScrape_Done(t *testing.T) {
	svc, d := mkMetaSvc(t, `{"releases":[{"id":"mbid-1","score":100,"date":"2003","track-count":0}]}`, http.StatusOK)
	d.Exec(`INSERT INTO artists(id,name) VALUES('ar','A')`)
	d.Exec(`INSERT INTO albums(id,title,artist_id,scrape_status) VALUES('al','T','ar','pending')`)

	rec := doScrapeReq(NewAlbumScrapeHandler(svc), "al")
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d", rec.Code)
	}
	var resp AlbumScrapeResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Status != "done" || resp.MBID != "mbid-1" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestAlbumScrape_NotFound(t *testing.T) {
	svc, _ := mkMetaSvc(t, `{"releases":[]}`, http.StatusNotFound)
	rec := doScrapeReq(NewAlbumScrapeHandler(svc), "nonexistent")
	if rec.Code != http.StatusNotFound {
		t.Errorf("不存在专辑应 404，得到 %d", rec.Code)
	}
}

func TestAlbumScrape_NoMatch(t *testing.T) {
	svc, d := mkMetaSvc(t, `{"releases":[{"id":"x","score":10,"track-count":1}]}`, http.StatusNotFound)
	d.Exec(`INSERT INTO artists(id,name) VALUES('ar','A')`)
	d.Exec(`INSERT INTO albums(id,title,artist_id,scrape_status) VALUES('al','T','ar','pending')`)
	rec := doScrapeReq(NewAlbumScrapeHandler(svc), "al")
	if rec.Code != http.StatusNotFound {
		t.Errorf("无匹配应 404，得到 %d", rec.Code)
	}
}
```
注意：测试顶部 import 需加 `"database/sql"`（`mkMetaSvc` 返回 `*sql.DB`）。

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestAlbumScrape -v`
Expected: 编译失败（`undefined: AlbumScrapeHandler` / `NewAlbumScrapeHandler` / `AlbumScrapeResponse`）

- [ ] **Step 3: 写最小实现**

`internal/api/v1/album_scrape.go`：
```go
package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/metadata"
)

// AlbumScrapeResponse 是专辑元数据刮削接口的响应。
type AlbumScrapeResponse struct {
	AlbumID  string `json:"album_id"`
	Status   string `json:"status"`
	MBID     string `json:"mbid,omitempty"`
	HasCover bool   `json:"has_cover"`
}

// AlbumScrapeHandler 处理专辑元数据刮削端点。
type AlbumScrapeHandler struct {
	service *metadata.MetadataService
}

// NewAlbumScrapeHandler 创建 handler。
func NewAlbumScrapeHandler(service *metadata.MetadataService) *AlbumScrapeHandler {
	return &AlbumScrapeHandler{service: service}
}

// ScrapeAlbum 处理 POST /api/v1/albums/{id}/scrape。
func (h *AlbumScrapeHandler) ScrapeAlbum(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "id")
	if h.service == nil {
		writeJSONError(w, http.StatusBadGateway, "元数据刮削源不可用")
		return
	}
	outcome, err := h.service.EnrichAlbum(r.Context(), albumID)
	if err != nil {
		if errors.Is(err, metadata.ErrAlbumNotFound) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "元数据刮削失败")
		return
	}
	if outcome.Status == "failed" {
		writeJSONError(w, http.StatusNotFound, "未匹配到专辑")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(AlbumScrapeResponse{
		AlbumID:  albumID,
		Status:   outcome.Status,
		MBID:     outcome.MBID,
		HasCover: outcome.HasCover,
	}); err != nil {
		slog.Error("写专辑刮削响应失败", "err", err)
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestAlbumScrape -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/api/v1/album_scrape.go internal/api/v1/album_scrape_test.go
git commit -m "feat(api): 专辑元数据按需刮削接口 ScrapeAlbum"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 8: 扫描器专辑元数据阶段

**Files:**
- Modify: `internal/scanner/scanner.go`
- Modify: `internal/scanner/scanner_test.go`、`internal/api/router_test.go`、`internal/api/router_scrape_test.go`、`internal/api/v1/library_test.go`（更新 NewScanner 调用）

- [ ] **Step 1: 写失败测试**

在 `internal/scanner/scanner_test.go` 追加（验证 `ScanStatus` 有新字段且 metadataService 参数存在）：
```go
func TestScanStatus_HasAlbumsScraped(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewScanner(d, config.LibraryConfig{}, "", nil, nil, false)
	st := s.Status()
	if st.AlbumsScraped != 0 {
		t.Errorf("初始 AlbumsScraped 应为 0，得到 %d", st.AlbumsScraped)
	}
}
```
（`scanner_test.go` 已 import `internal/db` 与 `config`。此测试同时锁定 `NewScanner` 新签名：6 参，metadataService 为第 5 个、此处传 nil。）

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/scanner/ -run TestScanStatus_HasAlbumsScraped -v`
Expected: 编译失败（`NewScanner` 参数数量不符 / `AlbumsScraped` 未定义）

- [ ] **Step 3: 改 scanner.go**

a) 顶部 import 加 `"github.com/yxx-z/lyra/internal/metadata"`：
```go
import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/lyrics"
	"github.com/yxx-z/lyra/internal/metadata"
)
```

b) `ScanStatus` 加字段：
```go
type ScanStatus struct {
	Running       bool      `json:"running"`
	Total         int64     `json:"total"`
	Processed     int64     `json:"processed"`
	Errors        int64     `json:"errors"`
	StartedAt     time.Time `json:"started_at"`
	Phase         string    `json:"phase"`
	LyricsScraped int64     `json:"lyrics_scraped"`
	AlbumsScraped int64     `json:"albums_scraped"`
}
```

c) `Scanner` 结构体加字段（在 `lyricsService`/`scrapeEnabled` 旁、`lyricsScraped` 旁）：
```go
	lyricsService   *lyrics.LyricsService
	metadataService *metadata.MetadataService
	scrapeEnabled   bool
```
```go
	lyricsScraped atomic.Int64
	albumsScraped atomic.Int64
	phase         atomic.Value // string
```

d) `NewScanner` 增参 `metadataService`（放在 lyricsService 之后、scrapeEnabled 之前）：
```go
func NewScanner(db *sql.DB, cfg config.LibraryConfig, ffprobePath string, lyricsService *lyrics.LyricsService, metadataService *metadata.MetadataService, scrapeEnabled bool) *Scanner {
	s := &Scanner{
		db:              db,
		cfg:             cfg,
		ing:             NewIngester(db),
		ffprobePath:     ffprobePath,
		lyricsService:   lyricsService,
		metadataService: metadataService,
		scrapeEnabled:   scrapeEnabled,
		stopCh:          make(chan struct{}),
	}
	s.phase.Store("idle")
	return s
}
```

e) `Status()` 返回里加 `AlbumsScraped: s.albumsScraped.Load(),`：
```go
	return ScanStatus{
		Running:       s.running.Load(),
		Total:         s.total.Load(),
		Processed:     s.processed.Load(),
		Errors:        s.errors.Load(),
		StartedAt:     startedAt,
		Phase:         phase,
		LyricsScraped: s.lyricsScraped.Load(),
		AlbumsScraped: s.albumsScraped.Load(),
	}
```

f) `doScan` 里：重置计数加 `s.albumsScraped.Store(0)`（在 `s.lyricsScraped.Store(0)` 旁）；并在歌词刮削阶段之后、`s.phase.Store("idle")` 之前插入元数据阶段：
```go
	// 刮削阶段：为缺歌词的曲目串行刮削（受 scraper.enabled 控制，可被 ctx 中断）
	if s.scrapeEnabled && s.lyricsService != nil {
		s.phase.Store("scraping")
		s.scrapePending(ctx)
	}
	// 专辑元数据阶段：为待刮专辑查 MB + 下封面（同样受控、可中断）
	if s.scrapeEnabled && s.metadataService != nil {
		s.phase.Store("metadata")
		s.scrapeAlbumsPending(ctx)
	}
	s.phase.Store("idle")
```

g) 文件末尾追加 `scrapeAlbumsPending`：
```go
func (s *Scanner) scrapeAlbumsPending(ctx context.Context) {
	rows, err := s.db.Query(`SELECT id FROM albums WHERE scrape_status='pending'`)
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
		slog.Warn("元数据阶段遍历待处理专辑出错", "err", err)
	}
	if len(ids) == 0 {
		return
	}
	slog.Info("开始后台专辑元数据刮削", "待处理", len(ids))

	for _, id := range ids {
		select {
		case <-ctx.Done():
			return
		default:
		}
		outcome, err := s.metadataService.EnrichAlbum(ctx, id)
		if err != nil || outcome.Status == "failed" {
			s.errors.Add(1)
		} else if outcome.Status == "done" {
			s.albumsScraped.Add(1)
		}
		// MusicBrainz 限速 1 req/s：每张专辑后固定等待 ≥1s（可被 ctx 中断）。
		select {
		case <-time.After(1100 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
	slog.Info("后台专辑元数据刮削结束", "成功", s.albumsScraped.Load())
}
```

- [ ] **Step 4: 更新所有 NewScanner 调用（加 nil metadataService）**

逐个修改（在第 4 个参数 lyricsService 之后插入一个 `nil`）：
- `internal/scanner/scanner_test.go:25` → `NewScanner(d, config.LibraryConfig{Paths: paths}, "", nil, nil, false)`
- `internal/scanner/scanner_test.go:108` → `NewScanner(d, config.LibraryConfig{Paths: []string{dir}}, "", svc, nil, true)`
- `internal/api/router_test.go:27` → `scanner.NewScanner(d, config.LibraryConfig{}, "", nil, nil, false)`
- `internal/api/router_scrape_test.go:23` → `scanner.NewScanner(d, config.LibraryConfig{}, "", nil, nil, false)`
- `internal/api/v1/library_test.go:22` → `scanner.NewScanner(d, config.LibraryConfig{}, "", nil, nil, false)`

- [ ] **Step 5: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./internal/scanner/ ./internal/api/... -v 2>&1 | tail -20`
Expected: build 成功；scanner 与 api 测试 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/scanner/ internal/api/router_test.go internal/api/router_scrape_test.go internal/api/v1/library_test.go
git commit -m "feat(scanner): 专辑元数据刮削阶段（phase/albums_scraped + 1s 间隔 + 可中断）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 9: 接线 — 构造 MetadataService + 注册路由

**Files:**
- Modify: `internal/api/router.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: 改 router.go**

a) import 加 `metadatapkg "github.com/yxx-z/lyra/internal/metadata"`：
```go
	lyricspkg "github.com/yxx-z/lyra/internal/lyrics"
	metadatapkg "github.com/yxx-z/lyra/internal/metadata"
	"github.com/yxx-z/lyra/internal/scanner"
```

b) 在 lyrics 的 scrape 注册之后追加专辑刮削服务与路由：
```go
		scrape := v1.NewScrapeHandler(lyricsService)
		r.Post("/tracks/{id}/scrape", scrape.ScrapeTrack)

		metadataService := metadatapkg.NewMetadataService(
			db,
			metadatapkg.NewMusicBrainzClient("https://musicbrainz.org", cfg.Scraper.MusicBrainz.UserAgent, nil),
			metadatapkg.NewCoverArtClient("https://coverartarchive.org", nil),
			cfg.Cache.ArtworkDir,
		)
		albumScrape := v1.NewAlbumScrapeHandler(metadataService)
		r.Post("/albums/{id}/scrape", albumScrape.ScrapeAlbum)
```

- [ ] **Step 2: 改 main.go**

在 `lyricsService := ...` 构造之后、`NewScanner` 之前，构造 metadataService，并把它传入 NewScanner：
```go
	lyricsService := lyrics.NewLyricsService(
		database,
		lyrics.NewEmbeddedProvider(),
		lyrics.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil),
	)
	metadataService := metadata.NewMetadataService(
		database,
		metadata.NewMusicBrainzClient("https://musicbrainz.org", cfg.Scraper.MusicBrainz.UserAgent, nil),
		metadata.NewCoverArtClient("https://coverartarchive.org", nil),
		cfg.Cache.ArtworkDir,
	)
	sc := scanner.NewScanner(database, cfg.Library, cfg.Transcode.FfprobePath, lyricsService, metadataService, cfg.Scraper.Enabled)
```
并在 `cmd/server/main.go` 顶部 import 加 `"github.com/yxx-z/lyra/internal/metadata"`（lyrics import 旁）。

- [ ] **Step 3: 构建 + 全量测试**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./...`
Expected: build 成功；所有包测试 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/api/router.go cmd/server/main.go
git commit -m "feat(metadata): 构造 MetadataService，注入扫描器 + 注册按需接口"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## 完成标准

- `go test ./...` 全绿；`go build ./...` 成功
- 迁移 003 可执行，`albums` 有 `scrape_status`
- 扫描完成后自动跑专辑元数据阶段（受 `scraper.enabled` 控、≥1s 间隔、可中断）
- `POST /api/v1/albums/{id}/scrape` 可手动触发单张专辑
- 封面服务：内嵌 → 本地 → `cover_path` → 404
- 所有测试用 httptest 桩，不打真实网络

## 验证（手动，需真实网络）

1. `make build` 启动；触发扫描，观察 `library/scan/status` 的 `phase` 经历 scanning → scraping → metadata → idle
2. 查 DB：某张专辑的 `mbid`/`release_date` 被填、`scrape_status='done'`，`./data/artwork/` 下出现封面文件
3. `POST /api/v1/albums/{id}/scrape` 对单张专辑返回 `{status:"done", mbid, has_cover}`
4. 因你的库文件多带内嵌封面，cover 接口仍优先返回内嵌封面（符合设计 A）；元数据字段则已补全
