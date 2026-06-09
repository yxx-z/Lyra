# 指纹联动元数据精配 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 AcoustID 得到的 recording MBID 投票精确定位专辑权威 release（指纹优先、文本搜索兜底），并给 MusicBrainz 客户端加全局自节流。

**Architecture:** `MusicBrainzClient` 增 `RecordingReleases`/`ReleaseDate` 并把所有 GET 收敛到自节流的 `doGet`。`MetadataService.EnrichAlbum` 先走指纹投票（`resolveByFingerprint` + `pickByVote`），无结果退回现有文本 `Search`，落库逻辑抽成 `applyMatch` 复用。扫描阶段顺序改为 歌词→指纹→元数据。

**Tech Stack:** Go 1.25（net/http, httptest, modernc.org/sqlite, sync）、MusicBrainz WS/2。

**Go 环境：** 含 `go` 命令的步骤前 `export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## 设计参考

实现前读 `docs/superpowers/specs/2026-06-09-fingerprint-metadata-design.md`。

**关键既有代码：**
- `internal/metadata/musicbrainz.go`：`MusicBrainzClient{baseURL,userAgent,httpClient}`、`Search`（已用裸词查询）、`pickRelease`、`escapeLucene`、`ReleaseMatch{MBID,Title,ReleaseDate,Genre}`、`ErrNotFound`。`Search` 当前自己建 request 并 Do。
- `internal/metadata/service.go`：`EnrichAlbum` 现在直接 `mb.Search` → 落库（release_date/genre + mbid best-effort + `downloadCover` + `setStatus`）。`downloadCover`/`setStatus` 已有。
- `internal/metadata/musicbrainz_test.go`：`newTestMB(srv)` 构造指向 httptest 的 client（设 baseURL）。
- `internal/scanner/scanner.go` `doScan` 阶段顺序当前：lyrics → metadata → fingerprint（三个 `if s.scrapeEnabled && s.services.X != nil` 块）。
- `tracks` 表：`album_id`、`mbid`（recording MBID，指纹阶段写入）、`is_available`。

**MB 接口（已用真实接口确认）：**
- recording→releases：`GET /ws/2/recording/{mbid}?inc=releases&fmt=json` → `{"releases":[{"id":"..."}]}`
- release 详情：`GET /ws/2/release/{mbid}?fmt=json` → `{"date":"2003-07-31",...}`

---

## 文件结构

```
internal/metadata/musicbrainz.go        改：加 minInterval/throttle/doGet，Search 走 doGet，新增 RecordingReleases/ReleaseDate；加 pickByVote
internal/metadata/musicbrainz_test.go   改：newTestMB 设 minInterval=0；加 throttle/RecordingReleases/ReleaseDate/pickByVote 测试
internal/metadata/service.go            改：EnrichAlbum 指纹优先+文本兜底；抽 applyMatch + resolveByFingerprint
internal/metadata/service_test.go       改：加指纹路径测试（既有文本路径测试保留）
internal/scanner/scanner.go             改：doScan 阶段顺序 fingerprint 提到 metadata 之前
```

---

### Task 1: MusicBrainz 自节流 + doGet 重构

**Files:** Modify `internal/metadata/musicbrainz.go`、`internal/metadata/musicbrainz_test.go`

- [ ] **Step 1: 写失败测试** — 在 `musicbrainz_test.go` 把 `newTestMB` 改为设 minInterval=0，并追加节流测试。

把：
```go
func newTestMB(srv *httptest.Server) *MusicBrainzClient {
	c := NewMusicBrainzClient("", "Lyra-Test/0.1", srv.Client())
	c.baseURL = srv.URL
	return c
}
```
改为：
```go
func newTestMB(srv *httptest.Server) *MusicBrainzClient {
	c := NewMusicBrainzClient("", "Lyra-Test/0.1", srv.Client())
	c.baseURL = srv.URL
	c.minInterval = 0 // 测试不节流
	return c
}
```
并确保测试文件 import 含 `"time"`（节流测试要用）。追加：
```go
func TestMB_Throttle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[]}`))
	}))
	defer srv.Close()
	c := NewMusicBrainzClient(srv.URL, "T", srv.Client())
	c.minInterval = 80 * time.Millisecond

	start := time.Now()
	_, _ = c.Search(context.Background(), AlbumQuery{AlbumTitle: "a", ArtistName: "b"})
	_, _ = c.Search(context.Background(), AlbumQuery{AlbumTitle: "a", ArtistName: "b"})
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Errorf("两次请求应间隔≥80ms（节流），实际 %v", elapsed)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run TestMB_Throttle -v`
Expected: 编译失败（`c.minInterval` 未定义）。

- [ ] **Step 3: 实现** — 改 `internal/metadata/musicbrainz.go`：

a) import 加 `"io"` 和 `"sync"`：
```go
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
```

b) `MusicBrainzClient` 结构体加字段：
```go
type MusicBrainzClient struct {
	baseURL     string
	userAgent   string
	httpClient  *http.Client
	minInterval time.Duration
	mu          sync.Mutex
	lastReqAt   time.Time
}
```

c) `NewMusicBrainzClient` 末尾构造里加 `minInterval`：
```go
	return &MusicBrainzClient{
		baseURL:     strings.TrimRight(baseURL, "/"),
		userAgent:   userAgent,
		httpClient:  httpClient,
		minInterval: 1100 * time.Millisecond,
	}
```

d) 新增 `throttle` 与 `doGet`（放在 Search 之前）：
```go
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
	return io.ReadAll(resp.Body)
}
```

e) 把 `Search` 改为走 `doGet`（替换其 request/Do/状态校验/解码段）：
```go
func (c *MusicBrainzClient) Search(ctx context.Context, q AlbumQuery) (ReleaseMatch, error) {
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
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -v`
Expected: PASS（含 TestMB_Throttle；既有 Search 测试因 newTestMB 设 minInterval=0 仍快速通过）。

- [ ] **Step 5: 提交**
```bash
git add internal/metadata/musicbrainz.go internal/metadata/musicbrainz_test.go
git commit -m "refactor(metadata): MB 客户端加全局自节流 + doGet 统一 GET"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 2: pickByVote 投票纯函数

**Files:** Modify `internal/metadata/musicbrainz.go`、`internal/metadata/musicbrainz_test.go`

- [ ] **Step 1: 写失败测试** — 在 `musicbrainz_test.go` 追加：
```go
func TestPickByVote_MaxCoverage(t *testing.T) {
	in := [][]string{{"relA", "relB"}, {"relA", "relC"}, {"relA"}}
	got, ok := pickByVote(in)
	if !ok || got != "relA" {
		t.Fatalf("应选覆盖最多的 relA，得到 %q ok=%v", got, ok)
	}
}

func TestPickByVote_TieFirstSeen(t *testing.T) {
	in := [][]string{{"relX"}, {"relY"}} // 各 1 票，relX 先出现
	got, ok := pickByVote(in)
	if !ok || got != "relX" {
		t.Fatalf("并列应取先出现的 relX，得到 %q", got)
	}
}

func TestPickByVote_Empty(t *testing.T) {
	if _, ok := pickByVote(nil); ok {
		t.Error("空应 false")
	}
	if _, ok := pickByVote([][]string{{}}); ok {
		t.Error("全空应 false")
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run TestPickByVote -v`
Expected: 编译失败（`undefined: pickByVote`）。

- [ ] **Step 3: 实现** — 在 `musicbrainz.go` 末尾追加：
```go
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
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run TestPickByVote -v`
Expected: PASS

- [ ] **Step 5: 提交**
```bash
git add internal/metadata/musicbrainz.go internal/metadata/musicbrainz_test.go
git commit -m "feat(metadata): pickByVote release 覆盖度投票纯函数"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 3: RecordingReleases + ReleaseDate

**Files:** Modify `internal/metadata/musicbrainz.go`、`internal/metadata/musicbrainz_test.go`

- [ ] **Step 1: 写失败测试** — 在 `musicbrainz_test.go` 追加：
```go
func TestRecordingReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[{"id":"rel-1"},{"id":"rel-2"}]}`))
	}))
	defer srv.Close()
	ids, err := newTestMB(srv).RecordingReleases(context.Background(), "rec-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ids) != 2 || ids[0] != "rel-1" || ids[1] != "rel-2" {
		t.Errorf("ids = %v", ids)
	}
}

func TestRecordingReleases_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := newTestMB(srv).RecordingReleases(context.Background(), "rec-1"); err == nil {
		t.Error("500 应返回 error")
	}
}

func TestReleaseDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"rel-1","date":"2003-07-31"}`))
	}))
	defer srv.Close()
	d, err := newTestMB(srv).ReleaseDate(context.Background(), "rel-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d != "2003-07-31" {
		t.Errorf("date = %q", d)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run 'TestRecordingReleases|TestReleaseDate' -v`
Expected: 编译失败（`undefined: RecordingReleases`/`ReleaseDate`）。

- [ ] **Step 3: 实现** — 在 `musicbrainz.go` 末尾追加：
```go
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
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -v`
Expected: PASS（本包全部）。

- [ ] **Step 5: 提交**
```bash
git add internal/metadata/musicbrainz.go internal/metadata/musicbrainz_test.go
git commit -m "feat(metadata): MusicBrainz RecordingReleases + ReleaseDate"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 4: EnrichAlbum 指纹优先 + 文本兜底

**Files:** Modify `internal/metadata/service.go`、`internal/metadata/service_test.go`

- [ ] **Step 1: 写失败测试** — 在 `service_test.go` 追加指纹路径测试（既有 `mbAndCaaServers`/`setupAlbum`/`newSvc` 等 helper 复用；如签名不同按文件现状调整）：
```go
func TestEnrichAlbum_FingerprintPath(t *testing.T) {
	database, id := setupAlbum(t, 2)
	// 给该专辑两首曲目写 recording mbid（模拟已指纹）
	if _, err := database.Exec(`UPDATE tracks SET mbid='rec-a' WHERE id='tra'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE tracks SET mbid='rec-b' WHERE id='trb'`); err != nil {
		t.Fatal(err)
	}

	mb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/ws/2/recording/"):
			// 两首都覆盖 rel-album；rec-b 还出现在 rel-comp
			w.Write([]byte(`{"releases":[{"id":"rel-album"},{"id":"rel-comp"}]}`))
		case strings.Contains(r.URL.Path, "/ws/2/release/"):
			w.Write([]byte(`{"id":"rel-album","date":"2003-07-31"}`))
		default:
			w.Write([]byte(`{"releases":[]}`)) // 文本搜索兜底（不应走到）
		}
	}))
	defer mb.Close()
	caa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 无封面，简化
	}))
	defer caa.Close()
	svc := newSvc(t, database, mb.URL, caa.URL, t.TempDir())

	out, err := svc.EnrichAlbum(context.Background(), id)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "done" || out.MBID != "rel-album" {
		t.Fatalf("应走指纹路径选 rel-album，得到 %+v", out)
	}
	var mbid, date string
	database.QueryRow(`SELECT COALESCE(mbid,''),COALESCE(release_date,'') FROM albums WHERE id=?`, id).Scan(&mbid, &date)
	if mbid != "rel-album" || date != "2003-07-31" {
		t.Errorf("落库 mbid=%q date=%q", mbid, date)
	}
}
```
说明：service_test.go 现有 `setupAlbum(t, 2)` 会创建专辑 `al1`（返回的 id）+ 2 首曲目 `tra`/`trb`（`album_id='al1'`、`is_available=1`），已确认。所以上面对 `tra`/`trb` 的 `UPDATE ... SET mbid` 与 `id`（=`al1`）都对得上，直接用即可。

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -run TestEnrichAlbum_FingerprintPath -v`
Expected: 失败（当前 EnrichAlbum 只走文本搜索，命中 `{"releases":[]}` → failed，或 mbid 非 rel-album）。

- [ ] **Step 3: 实现** — 改 `internal/metadata/service.go`：

把现有 `EnrichAlbum`（载专辑后直接 `mb.Search` + 落库那段，约 38-81 行）整体替换为：先载专辑，再"指纹优先+文本兜底"解析 match，最后调 `applyMatch`：
```go
// EnrichAlbum 为单张专辑补元数据 + 封面：指纹优先，文本搜索兜底。
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

	// 指纹路径：本专辑曲目有 recording MBID 时，投票定位权威 release。
	match, ok := s.resolveByFingerprint(ctx, albumID)
	if !ok {
		// 文本兜底
		var ferr error
		match, ferr = s.mb.Search(ctx, AlbumQuery{AlbumTitle: title, ArtistName: artist, TrackCount: trackCount})
		if errors.Is(ferr, ErrNotFound) {
			s.setStatus(ctx, albumID, "failed")
			return EnrichOutcome{Status: "failed"}, nil
		}
		if ferr != nil {
			return EnrichOutcome{}, ferr
		}
	}
	return s.applyMatch(ctx, albumID, match)
}

// resolveByFingerprint 用本专辑曲目的 recording MBID 投票定位 release；无可用指纹/无结果 → false。
func (s *MetadataService) resolveByFingerprint(ctx context.Context, albumID string) (ReleaseMatch, bool) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mbid FROM tracks
		WHERE album_id=? AND is_available=1 AND mbid IS NOT NULL AND mbid<>''
		LIMIT 5`, albumID)
	if err != nil {
		return ReleaseMatch{}, false
	}
	var recMBIDs []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err == nil {
			recMBIDs = append(recMBIDs, m)
		}
	}
	rows.Close()
	if len(recMBIDs) == 0 {
		return ReleaseMatch{}, false
	}

	var releasesPerTrack [][]string
	for _, rm := range recMBIDs {
		rels, err := s.mb.RecordingReleases(ctx, rm)
		if err != nil {
			continue // 瞬时失败：跳过该曲，靠其余曲目投票
		}
		releasesPerTrack = append(releasesPerTrack, rels)
	}
	releaseMBID, ok := pickByVote(releasesPerTrack)
	if !ok {
		return ReleaseMatch{}, false
	}
	date, _ := s.mb.ReleaseDate(ctx, releaseMBID) // best-effort，失败则 date 空
	return ReleaseMatch{MBID: releaseMBID, ReleaseDate: date}, true
}

// applyMatch 把选中的 release 落库（元数据 + mbid + 封面 + 状态 done）。
func (s *MetadataService) applyMatch(ctx context.Context, albumID string, match ReleaseMatch) (EnrichOutcome, error) {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE albums SET
			release_date=COALESCE(NULLIF(?,''),release_date),
			genre=COALESCE(NULLIF(?,''),genre),
			updated_at=?
		WHERE id=?`,
		match.ReleaseDate, match.Genre, time.Now(), albumID); err != nil {
		return EnrichOutcome{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE albums SET mbid=? WHERE id=?`, match.MBID, albumID); err != nil {
		slog.Warn("设置专辑 mbid 失败（可能 UNIQUE 冲突）", "album", albumID, "mbid", match.MBID, "err", err)
	}
	hasCover := s.downloadCover(ctx, albumID, match.MBID)
	s.setStatus(ctx, albumID, "done")
	return EnrichOutcome{Status: "done", MBID: match.MBID, HasCover: hasCover}, nil
}
```
（`downloadCover`/`setStatus` 不变保留。删除被 `applyMatch` 取代的旧落库代码段。）

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/metadata/ -v`
Expected: PASS —— 新指纹测试通过；既有 EnrichAlbum 文本路径测试（曲目无 mbid → resolveByFingerprint 返回 false → 走 Search）仍全绿。

- [ ] **Step 5: 提交**
```bash
git add internal/metadata/service.go internal/metadata/service_test.go
git commit -m "feat(metadata): EnrichAlbum 指纹投票优先 + 文本兜底（applyMatch 复用）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 5: 扫描阶段顺序 指纹→元数据

**Files:** Modify `internal/scanner/scanner.go`

- [ ] **Step 1: 改 doScan 阶段顺序**

在 `doScan` 中找到（lyrics 阶段之后）：
```go
	if s.scrapeEnabled && s.services.Metadata != nil {
		s.phase.Store("metadata")
		s.scrapeAlbumsPending(ctx)
	}
	if s.scrapeEnabled && s.services.Fingerprint != nil {
		s.phase.Store("fingerprint")
		s.fingerprintPending(ctx)
	}
	s.phase.Store("idle")
```
调整为指纹在前（元数据要用指纹结果）：
```go
	if s.scrapeEnabled && s.services.Fingerprint != nil {
		s.phase.Store("fingerprint")
		s.fingerprintPending(ctx)
	}
	if s.scrapeEnabled && s.services.Metadata != nil {
		s.phase.Store("metadata")
		s.scrapeAlbumsPending(ctx)
	}
	s.phase.Store("idle")
```

- [ ] **Step 2: 构建 + 全量测试** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./...`
Expected: build 成功；所有包测试 PASS（仅顺序调整，既有 scanner 测试不受影响）。

- [ ] **Step 3: 提交**
```bash
git add internal/scanner/scanner.go
git commit -m "feat(scanner): 扫描阶段顺序调整为 歌词→指纹→元数据"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## 完成标准

- `go test ./...` 全绿；`go build ./...` 成功
- 有指纹结果的专辑：用 recording 投票定位 release，填 mbid/release_date/封面
- 无指纹结果：退回文本搜索（既有行为）
- MB 所有请求全局间隔 ≥1.1s（自节流）
- 扫描阶段顺序 歌词 → 指纹 → 元数据
- 全部测试 httptest + fake，不打真网络

## 验证（手动，docker）

1. `make docker-build && docker compose up -d`（config 已配 acoustid key）
2. 重置触发重扫（指纹会先跑，元数据再用其结果）：可清 `albums.scrape_status='pending'` + `tracks.acoustid=NULL` 后重扫
3. 查 DB：专辑 `mbid` 为投票选中的 release、`release_date` 来自该 release；日志阶段经历 …→指纹→元数据→idle
4. 真实链路已先验证：recording《以父之名》→ MB 含《叶惠美》release
