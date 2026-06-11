# 手动传参获取歌词 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 歌词面板可手动填歌名/歌手/专辑,用 LRCLIB 模糊搜索返回候选,选一条应用到本曲（默认自动获取不变）。

**Architecture:** `LRCLIBClient` 加 `Search`（调 `/api/search`）；v1 加 `LyricsSearchHandler` 暴露 `GET /tracks/{id}/lyrics/search` 返回候选；应用复用既有 `PUT /tracks/{id}/lyrics`（source='manual'）；前端 LyricsPanel 加手动表单 + 候选列表（仅元信息）。

**Tech Stack:** Go 1.25 · chi v5 · Vue 3 · LRCLIB `/api/search`。

**关键约束：**
- 复用既有件：`lyrics.LRCLIBClient`（baseURL/userAgent/httpClient）、v1 `writeJSON`/`writeJSONError`、既有 `PUT /tracks/{id}/lyrics`（`LyricsHandler.PutLyrics`，body `{lrc_content,yrc_content,source}`）、前端 `api.saveLyrics(trackId, payload)`、LyricsPanel 的 `props.api`/`playerStore.currentTrack`/`loadLyrics()`。
- LRCLIB `/api/search` 返回 JSON 数组,每项 `{trackName,artistName,albumName,duration,instrumental,plainLyrics,syncedLyrics}`。
- Go 路径：`export PATH=$PATH:/home/yxx/go-local/go/bin`。后端测试用 httptest 假 LRCLIB,不打真实网络。
- router 在 Task 3 接线；中途 v1 包独立编译，用 `go test ./internal/...`/`go vet` 验证。若出现 secret.key 勿 git add。

---

## File Structure

```
internal/lyrics/lrclib.go            改：加 SearchCandidate 类型 + LRCLIBClient.Search
internal/lyrics/lrclib_test.go       改：追加 Search 测试
internal/api/v1/lyrics_search.go     新：LyricsSearchHandler（GET .../lyrics/search）
internal/api/v1/lyrics_search_test.go 新
internal/api/router.go               改：提取 lrclib client 变量 + 注册搜索路由
web/src/api/client.ts                改：LyricCandidate 类型 + searchLyrics
web/src/components/LyricsPanel.vue   改：手动获取表单 + 候选列表 + 应用
```

---

## Task 1: LRCLIBClient.Search

**Files:** Modify `internal/lyrics/lrclib.go`, `internal/lyrics/lrclib_test.go`

- [ ] **Step 1: 追加失败测试** 到 `internal/lyrics/lrclib_test.go`：
```go
func TestLRCLIBClientSearch_ReturnsCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			t.Fatalf("path: want /api/search, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("track_name") != "罗刹海市" {
			t.Errorf("track_name: got %q", r.URL.Query().Get("track_name"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":1,"trackName":"罗刹海市","artistName":"刀郎","albumName":"山歌寥哉","duration":268,"instrumental":false,"plainLyrics":"那马户又鸟","syncedLyrics":"[00:01.00]那马户又鸟"},
			{"id":2,"trackName":"罗刹海市(伴奏)","artistName":"刀郎","albumName":"山歌寥哉","duration":268,"instrumental":true,"plainLyrics":"","syncedLyrics":""}
		]`))
	}))
	defer server.Close()

	c := NewLRCLIBClient(server.URL, "", nil)
	cands, err := c.Search(context.Background(), "罗刹海市", "刀郎", "山歌寥哉")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(cands))
	}
	if cands[0].TrackName != "罗刹海市" || cands[0].Duration != 268 || cands[0].SyncedLyrics == "" {
		t.Errorf("候选0 解析不符: %+v", cands[0])
	}
	if !cands[1].Instrumental {
		t.Errorf("候选1 应为器乐")
	}
}

func TestLRCLIBClientSearch_EmptyQuery(t *testing.T) {
	c := NewLRCLIBClient("http://example.invalid", "", nil)
	if _, err := c.Search(context.Background(), "", "", ""); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("空参应 ErrInvalidQuery, got %v", err)
	}
}
```
（文件已 import context/errors/net/http/httptest/testing；如缺再补。）

- [ ] **Step 2: 运行确认失败** — `go test ./internal/lyrics/ -run Search` → 编译失败（Search/SearchCandidate 未定义）。

- [ ] **Step 3: 实现** — 在 `internal/lyrics/lrclib.go` 末尾追加：
```go
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
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/lyrics/...` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/lyrics && git commit -m "feat(lyrics): LRCLIBClient.Search（/api/search 模糊搜索候选）"
```

---

## Task 2: LyricsSearchHandler 端点

**Files:** Create `internal/api/v1/lyrics_search.go`, `internal/api/v1/lyrics_search_test.go`

> router 在 Task 3 接线；用 `go test ./internal/api/v1/ -run LyricsSearch` + `go vet ./internal/api/v1/` 验证。

- [ ] **Step 1: 写失败测试** `internal/api/v1/lyrics_search_test.go`：
```go
package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/lyrics"
)

func TestLyricsSearch_ReturnsCandidates(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"trackName":"罗刹海市","artistName":"刀郎","albumName":"山歌寥哉","duration":268,"instrumental":false,"plainLyrics":"p","syncedLyrics":"[00:01.00]那马户又鸟"},
			{"trackName":"伴奏","artistName":"刀郎","albumName":"山歌寥哉","duration":268,"instrumental":true,"plainLyrics":"","syncedLyrics":""}
		]`))
	}))
	defer upstream.Close()

	h := NewLyricsSearchHandler(lyrics.NewLRCLIBClient(upstream.URL, "", nil))
	r := chi.NewRouter()
	r.Get("/tracks/{id}/lyrics/search", h.Search)

	req := httptest.NewRequest("GET", "/tracks/t1/lyrics/search?trackName=罗刹海市&artistName=刀郎", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("应成功: %d %s", w.Code, w.Body.String())
	}
	b := w.Body.String()
	if !strings.Contains(b, `"candidates"`) || !strings.Contains(b, `"synced":true`) || !strings.Contains(b, "那马户又鸟") {
		t.Errorf("应返回候选(含同步标记+歌词): %s", b)
	}
	// 器乐候选(lrc 为空)应被跳过 → 只剩 1 条
	if strings.Count(b, `"trackName"`) != 1 {
		t.Errorf("器乐/空歌词候选应跳过，body: %s", b)
	}
}

func TestLyricsSearch_MissingParams(t *testing.T) {
	h := NewLyricsSearchHandler(lyrics.NewLRCLIBClient("http://example.invalid", "", nil))
	r := chi.NewRouter()
	r.Get("/tracks/{id}/lyrics/search", h.Search)
	req := httptest.NewRequest("GET", "/tracks/t1/lyrics/search", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("无参应 400: %d", w.Code)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `go vet ./internal/api/v1/` → 失败（NewLyricsSearchHandler 未定义）。

- [ ] **Step 3: 实现** `internal/api/v1/lyrics_search.go`：
```go
// internal/api/v1/lyrics_search.go
package v1

import (
	"net/http"

	"github.com/yxx-z/lyra/internal/lyrics"
)

// LyricsSearchHandler 用 LRCLIB 模糊搜索返回歌词候选，供前端手动选取。
type LyricsSearchHandler struct {
	client *lyrics.LRCLIBClient
}

func NewLyricsSearchHandler(client *lyrics.LRCLIBClient) *LyricsSearchHandler {
	return &LyricsSearchHandler{client: client}
}

type lyricCandidate struct {
	TrackName  string `json:"trackName"`
	ArtistName string `json:"artistName"`
	AlbumName  string `json:"albumName"`
	Duration   int    `json:"duration"`
	Synced     bool   `json:"synced"`
	LRC        string `json:"lrc"`
}

// Search 处理 GET /api/v1/tracks/{id}/lyrics/search?trackName=&artistName=&albumName=。
// id 仅用于路由归属；搜索用 query 参数。
func (h *LyricsSearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	trackName := q.Get("trackName")
	artistName := q.Get("artistName")
	albumName := q.Get("albumName")
	if trackName == "" && artistName == "" {
		writeJSONError(w, http.StatusBadRequest, "请至少提供歌名或歌手")
		return
	}
	cands, err := h.client.Search(r.Context(), trackName, artistName, albumName)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "搜索失败")
		return
	}
	out := make([]lyricCandidate, 0, len(cands))
	for _, c := range cands {
		lrc := c.SyncedLyrics
		if lrc == "" {
			lrc = c.PlainLyrics
		}
		if c.Instrumental || lrc == "" {
			continue
		}
		out = append(out, lyricCandidate{
			TrackName:  c.TrackName,
			ArtistName: c.ArtistName,
			AlbumName:  c.AlbumName,
			Duration:   c.Duration,
			Synced:     c.SyncedLyrics != "",
			LRC:        lrc,
		})
	}
	writeJSON(w, map[string]any{"candidates": out})
}
```

- [ ] **Step 4: 运行确认通过** — `go test ./internal/api/v1/ -run LyricsSearch -v && go vet ./internal/api/v1/` → PASS。

- [ ] **Step 5: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/lyrics_search.go internal/api/v1/lyrics_search_test.go && git commit -m "feat(api): 歌词手动搜索端点（LRCLIB 候选）"
```

---

## Task 3: router 装配 + 全量编译

**Files:** Modify `internal/api/router.go`

- [ ] **Step 1: 接线** — 当前 `router.go` 在 `/api/v1` 组内有：
```go
		lyricsService := lyricspkg.NewLyricsService(
			db,
			lyricspkg.NewEmbeddedProvider(),
			lyricspkg.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil),
		)
		scrape := v1.NewScrapeHandler(lyricsService)
		r.Post("/tracks/{id}/scrape", scrape.ScrapeTrack)
		r.Post("/tracks/{id}/lyrics/upgrade", scrape.UpgradeLyrics)
```
改为先提取 LRCLIB client 变量，复用给 service 和新搜索 handler：
```go
		lrclib := lyricspkg.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil)
		lyricsService := lyricspkg.NewLyricsService(
			db,
			lyricspkg.NewEmbeddedProvider(),
			lrclib,
		)
		scrape := v1.NewScrapeHandler(lyricsService)
		r.Post("/tracks/{id}/scrape", scrape.ScrapeTrack)
		r.Post("/tracks/{id}/lyrics/upgrade", scrape.UpgradeLyrics)
		lyricsSearch := v1.NewLyricsSearchHandler(lrclib)
		r.Get("/tracks/{id}/lyrics/search", lyricsSearch.Search)
```

- [ ] **Step 2: 全量编译 + 测试**
```bash
cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && gofmt -l internal/api/router.go && go build ./... && go test ./...
```
Expected: gofmt 无输出；build 成功；全部包 PASS。

- [ ] **Step 3: 提交**
```bash
cd /home/yxx/develop/Lyra && git add internal/api/router.go && git commit -m "feat(api): router 装配歌词搜索路由（复用 LRCLIB client）"
```

---

## Task 4: 前端 —— 手动获取表单 + 候选列表

**Files:** Modify `web/src/api/client.ts`、`web/src/components/LyricsPanel.vue`

> 先读 client.ts（`saveLyrics`/`LyricsPayload`/request 风格、类型导出区）与 LyricsPanel.vue（`props.api`、`playerStore.currentTrack`、`loadLyrics()`、无歌词态/已有歌词态模板、scoped 样式）。

- [ ] **Step 1: client.ts** — 类型与方法：
```ts
export type LyricCandidate = { trackName: string; artistName: string; albumName: string; duration: number; synced: boolean; lrc: string }
```
方法（鉴权）：
```ts
  searchLyrics(trackId: string, params: { trackName?: string; artistName?: string; albumName?: string }): Promise<{ candidates: LyricCandidate[] }> {
    const qs = new URLSearchParams()
    if (params.trackName) qs.set('trackName', params.trackName)
    if (params.artistName) qs.set('artistName', params.artistName)
    if (params.albumName) qs.set('albumName', params.albumName)
    return this.request<{ candidates: LyricCandidate[] }>(`/api/v1/tracks/${encodeURIComponent(trackId)}/lyrics/search?${qs.toString()}`, { method: 'GET' })
  }
```
应用复用既有 `saveLyrics(trackId, { lrc_content: cand.lrc, source: 'manual' })`（`LyricsPayload` 已有 `lrc_content`/`source` 可选字段）。

- [ ] **Step 2: LyricsPanel.vue** — 加手动获取能力（不改动既有自动 `loadLyrics` 流程）：
  - script 加状态：
```ts
const showManual = ref(false)
const mTrack = ref('')
const mArtist = ref('')
const mAlbum = ref('')
const searching = ref(false)
const candidates = ref<LyricCandidate[]>([])
const manualMsg = ref('')

function openManual() {
  const t = playerStore.currentTrack
  mTrack.value = t?.title ?? ''
  mArtist.value = t?.artist ?? ''
  mAlbum.value = t?.album ?? ''
  candidates.value = []
  manualMsg.value = ''
  showManual.value = true
}

async function runManualSearch() {
  const track = playerStore.currentTrack
  if (!track) return
  searching.value = true
  manualMsg.value = ''
  candidates.value = []
  try {
    const res = await props.api.searchLyrics(track.trackId, { trackName: mTrack.value, artistName: mArtist.value, albumName: mAlbum.value })
    candidates.value = res.candidates
    if (res.candidates.length === 0) manualMsg.value = '未找到，试试调整歌名/歌手'
  } catch {
    manualMsg.value = '搜索失败，请重试'
  } finally {
    searching.value = false
  }
}

function fmtDur(s: number) {
  const m = Math.floor(s / 60); const r = String(s % 60).padStart(2, '0'); return `${m}:${r}`
}

async function applyCandidate(c: LyricCandidate) {
  const track = playerStore.currentTrack
  if (!track) return
  try {
    await props.api.saveLyrics(track.trackId, { lrc_content: c.lrc, source: 'manual' })
    showManual.value = false
    await loadLyrics()
  } catch {
    manualMsg.value = '应用失败，请重试'
  }
}
```
  import `LyricCandidate` 类型（与现有 `ApiClient` 同处 import）。
  - 模板：在歌词区放一个"手动获取歌词"按钮（无歌词态的兜底块里、以及标准歌词态都可触发，比如放在 `lyrics-list-col` 底部或封面信息下）。点击 `openManual()`。展开后是一个浮层/内联块：
```vue
<div v-if="showManual" class="manual-lyrics">
  <input class="custom-input" v-model="mTrack" placeholder="歌名" />
  <input class="custom-input" v-model="mArtist" placeholder="歌手" />
  <input class="custom-input" v-model="mAlbum" placeholder="专辑(可选)" />
  <div class="manual-actions">
    <button class="custom-btn-primary" :disabled="searching" @click="runManualSearch">{{ searching ? '搜索中…' : '查询' }}</button>
    <button class="link-btn" @click="showManual = false">关闭</button>
  </div>
  <p v-if="manualMsg" class="muted">{{ manualMsg }}</p>
  <ul class="candidate-list">
    <li v-for="(c, i) in candidates" :key="i">
      <button class="candidate-row" type="button" @click="applyCandidate(c)">
        <span class="cand-title">{{ c.trackName }} - {{ c.artistName }}</span>
        <span class="cand-meta muted">{{ c.albumName }} · {{ fmtDur(c.duration) }} · {{ c.synced ? '同步' : '纯文本' }}</span>
      </button>
    </li>
  </ul>
</div>
```
  - 加 scoped 样式（输入框/列表/行,沿用项目 `custom-input`/`muted` 等既有类 + 少量布局样式）。

- [ ] **Step 3: 构建验证** — `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && make build-frontend && go build ./...` → 通过（无 TS 错误）。

- [ ] **Step 4: 提交**
```bash
cd /home/yxx/develop/Lyra && git add web/src && git commit -m "feat(web): 歌词面板手动搜索+候选选择（LRCLIB）"
```

---

## Self-Review（计划自检）

- **Spec 覆盖**：LRCLIBClient.Search 调 /api/search(T1) ✓；搜索端点返回候选(synced 标记、跳过 instrumental/空)(T2) ✓；应用复用既有 PUT lyrics(T4 saveLyrics) ✓；router 复用同一 LRCLIB client(T3) ✓；前端手动表单(预填) + 候选列表(仅元信息) + 点选应用刷新(T4) ✓；默认自动获取不变(T4 不改 loadLyrics 触发) ✓。
- **占位符**：无 TODO/TBD；各步含完整代码（前端模板/脚本给到可直接落地）。
- **类型一致**：`SearchCandidate`(lyrics 包)→ 端点 `lyricCandidate`(synced/lrc 派生) → 前端 `LyricCandidate{trackName,artistName,albumName,duration,synced,lrc}`(T1/T2/T4 字段一致)；`NewLyricsSearchHandler(client)`(T2) 与 router(T3) 一致；应用用既有 `saveLyrics(trackId,{lrc_content,source})`。
- **已知约束**：搜索端点 trackName/artistName 全空 → 400；LRCLIB 无结果 → 空 candidates；器乐/空歌词候选在端点侧过滤。
