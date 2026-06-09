# 纯文本歌词升级为同步歌词 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按需把曲目的纯文本歌词升级为 LRCLIB 的同步(带时间轴)歌词，仅在找到同步版时替换。

**Architecture:** `LyricsService.UpgradeToSynced` 跳过 embedded provider、向网络源(lrclib)查同步版，命中(YRC 非空或 LRC 带时间轴)才替换。HTTP `POST /tracks/{id}/lyrics/upgrade`（ScrapeHandler 加方法）。前端歌词面板纯文本模式显示「升级为同步歌词」按钮。

**Tech Stack:** Go 1.25（regexp, httptest, modernc.org/sqlite）、Vue 3 + TypeScript。

**Go 环境：** 含 `go` 命令的步骤前 `export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## 设计参考

实现前读 `docs/superpowers/specs/2026-06-09-lyrics-sync-upgrade-design.md`。

**关键既有代码：**
- `internal/lyrics/service.go`：`LyricsService{db, providers}`，私有 `loadTrack`/`saveLyrics`/`updateStatus`，`Query`/`Result`/`ScrapeOutcome`，`ErrTrackNotFound`/`ErrNotFound`/`ErrInvalidQuery`。`Provider` 接口有 `Name() string` + `Fetch(ctx, Query) (Result, error)`。embedded provider 的 `Name()=="embedded"`。
- `internal/lyrics/service_test.go`（同包）：`fakeProvider{name, result, err, calls *int}`（实现 Provider）、`newServiceTestDB(t) *sql.DB`（建内存库 + 曲目 `t1`，初始无歌词）。**升级测试可直接复用这两个。**
- `internal/api/v1/scrape.go`：`ScrapeHandler{service *lyrics.LyricsService}`、`ScrapeResponse{TrackID,Status,Source,Message}`、`writeScrapeJSON`、`writeJSONError`（auth.go）。
- `internal/api/v1/scrape_test.go`（同包）：`stubProvider{res, err}`（`Name()=="stub"`）、`newTestDB(t)`、`insertTestData(t, d)`（曲目 `t1`）。
- `internal/api/router.go`：`scrape := v1.NewScrapeHandler(lyricsService)` + `r.Post("/tracks/{id}/scrape", scrape.ScrapeTrack)`（约 69-70 行）。
- `web/src/api/client.ts`：已有 `ScrapeResponse` 类型 + `scrapeTrack` 方法（`request<ScrapeResponse>(..., {method:'POST'})`）。
- `web/src/components/LyricsPanel.vue`：`synced` ref（纯文本时为 false）、`lrcLines`、`yrcLines`、`scraping`/`scrapeMessage`、`handleScrape`、`loadLyrics`，分支 C（`v-else` 的 `.lyrics-scroller`，渲染 lrcLines，纯文本时 `is-static`）。切歌 watch 在 `props.album?.id`... 实为 `playerStore.currentTrack?.trackId`（见文件）。

---

## 文件结构

```
internal/lyrics/upgrade.go            新建：hasTimestamps + UpgradeOutcome + UpgradeToSynced
internal/lyrics/upgrade_test.go       新建（复用 fakeProvider/newServiceTestDB）
internal/api/v1/scrape.go             改：加 UpgradeLyrics 方法
internal/api/v1/scrape_test.go        改：加 upgrade HTTP 测试
internal/api/router.go                改：注册 POST /tracks/{id}/lyrics/upgrade
web/src/api/client.ts                 改：加 upgradeLyrics 方法
web/src/components/LyricsPanel.vue     改：纯文本模式加升级按钮 + handleUpgrade
```

---

### Task 1: UpgradeToSynced + hasTimestamps

**Files:** Create `internal/lyrics/upgrade.go`, `internal/lyrics/upgrade_test.go`

- [ ] **Step 1: 写失败测试** — `internal/lyrics/upgrade_test.go`（`fakeProvider`/`newServiceTestDB` 来自同包 service_test.go）：
```go
package lyrics

import (
	"context"
	"errors"
	"testing"
)

func TestHasTimestamps(t *testing.T) {
	if !hasTimestamps("[00:01.00]hi") {
		t.Error("带时间轴应 true")
	}
	if hasTimestamps("纯文本\n第二行") {
		t.Error("纯文本应 false")
	}
}

func TestUpgradeToSynced_ReplacesWithSynced(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source) VALUES('t1','纯文本一行','embedded')`); err != nil {
		t.Fatal(err)
	}
	embCalls := 0
	emb := &fakeProvider{name: "embedded", result: Result{LRCContent: "纯文本一行", Source: "embedded"}, calls: &embCalls}
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "[00:01.00]同步", Source: "lrclib"}}
	svc := NewLyricsService(d, emb, lrc)

	out, err := svc.UpgradeToSynced(context.Background(), "t1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "upgraded" || out.Source != "lrclib" {
		t.Fatalf("out=%+v", out)
	}
	if embCalls != 0 {
		t.Errorf("embedded 应被跳过，实际调用 %d 次", embCalls)
	}
	var got string
	d.QueryRow(`SELECT lrc_content FROM lyrics WHERE track_id='t1'`).Scan(&got)
	if got != "[00:01.00]同步" {
		t.Errorf("应替换为同步歌词，得到 %q", got)
	}
}

func TestUpgradeToSynced_NoSyncedKeepsExisting(t *testing.T) {
	d := newServiceTestDB(t)
	if _, err := d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source) VALUES('t1','纯文本一行','embedded')`); err != nil {
		t.Fatal(err)
	}
	emb := &fakeProvider{name: "embedded", result: Result{LRCContent: "纯文本一行", Source: "embedded"}}
	lrc := &fakeProvider{name: "lrclib", result: Result{LRCContent: "还是纯文本无时间轴", Source: "lrclib"}}
	svc := NewLyricsService(d, emb, lrc)

	out, err := svc.UpgradeToSynced(context.Background(), "t1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "no_synced" {
		t.Errorf("应 no_synced，得到 %q", out.Status)
	}
	var got string
	d.QueryRow(`SELECT lrc_content FROM lyrics WHERE track_id='t1'`).Scan(&got)
	if got != "纯文本一行" {
		t.Errorf("原歌词不应被改，得到 %q", got)
	}
}

func TestUpgradeToSynced_YRCCountsSynced(t *testing.T) {
	d := newServiceTestDB(t)
	lrc := &fakeProvider{name: "lrclib", result: Result{YRCContent: `{"lines":[{"start":1}]}`, Source: "netease"}}
	svc := NewLyricsService(d, lrc)

	out, err := svc.UpgradeToSynced(context.Background(), "t1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "upgraded" {
		t.Errorf("YRC 命中应算同步 upgraded，得到 %q", out.Status)
	}
}

func TestUpgradeToSynced_TrackNotFound(t *testing.T) {
	d := newServiceTestDB(t)
	svc := NewLyricsService(d)
	if _, err := svc.UpgradeToSynced(context.Background(), "nope"); !errors.Is(err, ErrTrackNotFound) {
		t.Errorf("want ErrTrackNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -run 'TestHasTimestamps|TestUpgradeToSynced' -v`
Expected: 编译失败（`undefined: hasTimestamps` / `UpgradeToSynced`）。

- [ ] **Step 3: 实现** — `internal/lyrics/upgrade.go`：
```go
package lyrics

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

var lrcTimestampRe = regexp.MustCompile(`\[\d+:\d+`)

// hasTimestamps 判断 LRC 文本是否含 [mm:ss] 时间轴（即同步歌词）。
func hasTimestamps(lrc string) bool {
	return lrcTimestampRe.MatchString(lrc)
}

// UpgradeOutcome 报告同步歌词升级结果。
type UpgradeOutcome struct {
	Status string // "upgraded" | "no_synced"
	Source string
}

// UpgradeToSynced 跳过 embedded，向网络源查同步版歌词；仅在找到同步版时替换。
func (s *LyricsService) UpgradeToSynced(ctx context.Context, trackID string) (UpgradeOutcome, error) {
	track, err := s.loadTrack(trackID)
	if err != nil {
		return UpgradeOutcome{}, err
	}
	q := Query{
		TrackName:  track.Title,
		ArtistName: track.Artist,
		AlbumName:  track.Album,
		Duration:   track.Duration,
		FilePath:   track.FilePath,
	}

	for _, p := range s.providers {
		if p.Name() == "embedded" {
			continue // embedded 给的正是要替换的纯文本，跳过
		}
		res, ferr := p.Fetch(ctx, q)
		if ferr != nil {
			if errors.Is(ferr, ErrNotFound) || errors.Is(ferr, ErrInvalidQuery) {
				continue
			}
			return UpgradeOutcome{}, ferr
		}
		// 仅接受同步结果（YRC 或带时间轴的 LRC）
		if strings.TrimSpace(res.YRCContent) != "" || hasTimestamps(res.LRCContent) {
			if err := s.saveLyrics(trackID, res); err != nil {
				return UpgradeOutcome{}, err
			}
			if err := s.updateStatus(trackID, "done"); err != nil {
				return UpgradeOutcome{}, err
			}
			return UpgradeOutcome{Status: "upgraded", Source: res.Source}, nil
		}
		// 命中但仍是纯文本 → 试下一个
	}
	return UpgradeOutcome{Status: "no_synced"}, nil
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -v`
Expected: PASS（本包全部，含既有 scrape 测试）。

- [ ] **Step 5: 提交**
```bash
git add internal/lyrics/upgrade.go internal/lyrics/upgrade_test.go
git commit -m "feat(lyrics): UpgradeToSynced 纯文本升级同步歌词（跳过 embedded）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 2: HTTP 接口 UpgradeLyrics

**Files:** Modify `internal/api/v1/scrape.go`、`internal/api/v1/scrape_test.go`、`internal/api/router.go`

- [ ] **Step 1: 写失败测试** — 在 `internal/api/v1/scrape_test.go` 追加（复用 `stubProvider`/`newTestDB`/`insertTestData`）：
```go
func TestUpgradeLyrics_Upgraded(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	// stubProvider 的 Name() 是 "stub"（非 embedded），返回同步 LRC
	svc := lyrics.NewLyricsService(d, stubProvider{res: lyrics.Result{LRCContent: "[00:01.00]hi", Source: "lrclib"}})
	h := NewScrapeHandler(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/lyrics/upgrade", nil)
	h.upgradeLyrics(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp ScrapeResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "upgraded" || resp.Source != "lrclib" {
		t.Errorf("got %+v", resp)
	}
}

func TestUpgradeLyrics_NoSynced(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	svc := lyrics.NewLyricsService(d, stubProvider{res: lyrics.Result{LRCContent: "纯文本无时间轴", Source: "lrclib"}})
	h := NewScrapeHandler(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/t1/lyrics/upgrade", nil)
	h.upgradeLyrics(w, req, "t1")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp ScrapeResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "no_synced" {
		t.Errorf("got %+v", resp)
	}
}

func TestUpgradeLyrics_TrackNotFound(t *testing.T) {
	d := newTestDB(t)
	svc := lyrics.NewLyricsService(d)
	h := NewScrapeHandler(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tracks/nope/lyrics/upgrade", nil)
	h.upgradeLyrics(w, req, "nope")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestUpgradeLyrics -v`
Expected: 编译失败（`h.upgradeLyrics` 未定义）。

- [ ] **Step 3: 实现** — 在 `internal/api/v1/scrape.go` 追加（`errors`/`chi`/`lyrics`/`http` 已 import）：
```go
// UpgradeLyrics handles POST /api/v1/tracks/{id}/lyrics/upgrade.
func (h *ScrapeHandler) UpgradeLyrics(w http.ResponseWriter, r *http.Request) {
	h.upgradeLyrics(w, r, chi.URLParam(r, "id"))
}

func (h *ScrapeHandler) upgradeLyrics(w http.ResponseWriter, r *http.Request, trackID string) {
	if h.service == nil {
		writeJSONError(w, http.StatusBadGateway, "歌词刮削源不可用")
		return
	}
	outcome, err := h.service.UpgradeToSynced(r.Context(), trackID)
	if err != nil {
		if errors.Is(err, lyrics.ErrTrackNotFound) {
			http.NotFound(w, r)
			return
		}
		writeJSONError(w, http.StatusBadGateway, "同步歌词升级失败")
		return
	}
	writeScrapeJSON(w, ScrapeResponse{
		TrackID: trackID,
		Status:  outcome.Status,
		Source:  outcome.Source,
	})
}
```

- [ ] **Step 4: 注册路由** — 在 `internal/api/router.go` 的 `r.Post("/tracks/{id}/scrape", scrape.ScrapeTrack)` 之后加：
```go
		r.Post("/tracks/{id}/scrape", scrape.ScrapeTrack)
		r.Post("/tracks/{id}/lyrics/upgrade", scrape.UpgradeLyrics)
```

- [ ] **Step 5: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./internal/api/... -v 2>&1 | tail -15`
Expected: build 成功；upgrade 测试 + 既有 api 测试 PASS。

- [ ] **Step 6: 提交**
```bash
git add internal/api/v1/scrape.go internal/api/v1/scrape_test.go internal/api/router.go
git commit -m "feat(api): POST /tracks/{id}/lyrics/upgrade 同步歌词升级接口"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 3: 前端升级按钮

**Files:** Modify `web/src/api/client.ts`、`web/src/components/LyricsPanel.vue`

- [ ] **Step 1: client.ts 加方法** — 在 `scrapeTrack` 方法附近追加（复用既有 `ScrapeResponse` 类型）：
```ts
  upgradeLyrics(trackId: string) {
    return this.request<ScrapeResponse>(`/api/v1/tracks/${encodeURIComponent(trackId)}/lyrics/upgrade`, {
      method: 'POST',
    })
  }
```

- [ ] **Step 2: LyricsPanel.vue 脚本加状态 + handleUpgrade**

在 `const scraping = ref(false)` / `const scrapeMessage = ref('')` 附近追加：
```ts
const upgrading = ref(false)
const upgradeMessage = ref('')
```
在 `handleScrape` 函数附近追加：
```ts
// 纯文本歌词升级为同步歌词
async function handleUpgrade() {
  const track = playerStore.currentTrack
  if (!track || upgrading.value) return
  upgrading.value = true
  upgradeMessage.value = ''
  try {
    const res = await props.api.upgradeLyrics(track.trackId)
    if (res.status === 'upgraded') {
      await loadLyrics() // 重新拉取 → 变同步 → 滚动
      upgradeMessage.value = '已升级为同步歌词'
    } else {
      upgradeMessage.value = '未找到同步版本'
    }
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      upgradeMessage.value = '未找到同步版本'
    } else {
      upgradeMessage.value = '升级失败，请重试'
    }
  } finally {
    upgrading.value = false
  }
}
```
（`ApiError` 已 import。）在切歌的 `watch(() => playerStore.currentTrack?.trackId, ...)` 回调里，`loadLyrics()` 之外补一行重置：`upgradeMessage.value = ''`（若该 watch 直接调用 loadLyrics，可在 `loadLyrics` 顶部统一重置区加 `upgradeMessage.value = ''`，与 `scrapeMessage.value = ''` 并列；优先放进 loadLyrics 的重置区）。

- [ ] **Step 3: LyricsPanel.vue 模板加升级按钮**

把标准滚动歌词面板分支（`<div v-else ref="scrollerRef" class="lyrics-scroller">`，渲染 lrcLines 的那个）改为在顶部插入"纯文本时的升级条"：
```html
        <!-- C. 标准滚动歌词面板（纯文本时顶部可升级为同步歌词） -->
        <div v-else ref="scrollerRef" class="lyrics-scroller">
          <div v-if="!synced" style="display: flex; flex-direction: column; align-items: center; gap: 6px; padding: 8px 0 16px;">
            <button
              class="custom-btn-primary"
              style="width: auto; padding: 8px 18px; font-size: 13px; display: inline-flex; align-items: center; gap: 8px;"
              type="button"
              :disabled="upgrading"
              @click="handleUpgrade"
            >
              <span v-if="upgrading" class="loading-spinner" aria-label="升级中"></span>
              <span>{{ upgrading ? '升级中…' : '⏱ 升级为同步歌词' }}</span>
            </button>
            <span v-if="upgradeMessage" class="muted" style="font-size: 12px;">{{ upgradeMessage }}</span>
          </div>
          <button
            v-for="(line, idx) in lrcLines"
            :key="idx"
            :class="{ active: idx === currentLineIndex, 'is-static': !synced }"
            class="lyric-line"
            type="button"
            @click="seekToLine(line.time)"
          >
            {{ line.text || '• • •' }}
          </button>
        </div>
```
（仅在原 `<div v-else ...>` 内、`v-for` 按钮之前插入那个 `v-if="!synced"` 升级条；`v-for` 按钮块保持原样不动。）

- [ ] **Step 4: 构建确认通过** — `make build-frontend`
Expected: vue-tsc + vite 通过，无类型错误。

- [ ] **Step 5: 提交**
```bash
git add web/src/api/client.ts web/src/components/LyricsPanel.vue
git commit -m "feat(web): 纯文本歌词面板加「升级为同步歌词」按钮"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## 完成标准

- `go test ./...` 全绿；`go build ./...` 成功；`make build-frontend` 成功
- 纯文本歌词的曲目：点「升级为同步歌词」→ 有同步版则替换并开始滚动、提示「已升级」；无同步版提示「未找到同步版本」、原歌词不变
- 升级跳过 embedded、只接受带时间轴/YRC 的结果
- 全部测试 mock/桩，不打真网络

## 验证（手动，docker）

1. `make docker-build && docker compose up -d`
2. 打开一首内嵌纯文本歌词的周杰伦曲目（如《晴天》《东风破》），歌词面板应是纯文本静态模式 + 顶部「升级为同步歌词」按钮
3. 点按钮 → 应替换为带时间轴的滚动歌词（LRCLIB 有同步版）
4. 蔡琴某些无同步版的曲目 → 点后提示「未找到同步版本」，原纯文本保留
