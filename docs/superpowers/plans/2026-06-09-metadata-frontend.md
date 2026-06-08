# 元数据/封面刮削前端接入 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在前端露出已合并的专辑元数据/封面刮削：专辑页加刮削按钮 + 展示流派/发行日期，扫描面板显示阶段与刮削计数。

**Architecture:** 后端 `AlbumSummary` 暴露 `genre`/`release_date`（并修复全日期下 `Year` 派生 bug）。前端 `client.ts` 补类型 + `scrapeAlbum` 方法；`AlbumDetail.vue` 加按钮（收 `api` prop、触发刮削、emit `refresh` 让父组件 `App.vue` 重载）+ 封面 `?v=` 缓存击穿；`ScanPanel.vue` 显示阶段与计数。

**Tech Stack:** Go 1.25（net/http, httptest）、Vue 3 + TypeScript + Vite。

**Go 环境：** 含 `go` 命令的步骤前先 `export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## 设计参考

实现前阅读 `docs/superpowers/specs/2026-06-09-metadata-frontend-design.md`。

**既有结构（已确认）：**
- `AlbumDetail.vue` 的 `album` 是父组件 `App.vue` 传入的 prop；组件内无 api。`App.vue` 有 `api`、`selectAlbum(id)`（`selectedAlbum.value = await api.getAlbum(id)`）、`AlbumDetail` 用法在 `:album="selectedAlbum" @play="playAlbumTrack"`。
- `LyricsPanel.vue` 已是"收 `:api` prop + 始终可点按钮 + 即时反馈"的范本。
- `App.vue:156` 有 `reactive<ScanStatus>({running,total,processed,errors,started_at})` 字面量初始化器 —— 改 `ScanStatus` 类型必须同步补字段。
- `client.ts` 的 `request<T>(path, options)` 支持 `{method:'POST'}`（见 `scrapeTrack`）。
- 后端 `albums.go`：`Year` 由 `strconv.Atoi(releaseDate)` 派生 —— 刮削后 release_date 变 `"2003-07-31"`，`Atoi` 失败使 Year=0，需修为取前 4 位。

---

## 文件结构

```
internal/api/v1/albums.go        改：AlbumSummary 加 genre/release_date；两查询补 genre；Year 取前4位
internal/api/v1/albums_test.go   改：断言 genre/release_date/Year
web/src/api/client.ts            改：AlbumSummary/ScanStatus 补字段；加 AlbumScrapeResponse + scrapeAlbum
web/src/App.vue                  改：scanStatus 初始化器补字段；AlbumDetail 传 :api + @refresh + refreshSelectedAlbum
web/src/components/AlbumDetail.vue  改：api prop、刮削按钮、元数据展示、封面 ?v= 缓存击穿
web/src/components/ScanPanel.vue    改：阶段标签 + 刮削计数
```

---

### Task 1: 后端暴露 genre/release_date + 修 Year 派生

**Files:**
- Modify: `internal/api/v1/albums.go`
- Modify: `internal/api/v1/albums_test.go`

- [ ] **Step 1: 写失败测试** — 在 `internal/api/v1/albums_test.go` 末尾追加：
```go
func TestGetAlbum_ReturnsGenreAndReleaseDate(t *testing.T) {
	d := newTestDB(t)

	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('ar','周杰伦')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id,release_date,genre) VALUES('al','叶惠美','ar','2003-07-31','Mandopop')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t','晴天','ar','al','/m/a.flac','',1,'done')`); err != nil {
		t.Fatal(err)
	}

	h := NewAlbumsHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums/al", nil)
	h.getAlbum(w, req, "al")

	if w.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d", w.Code)
	}
	var resp AlbumDetail
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Genre != "Mandopop" {
		t.Errorf("Genre = %q, want Mandopop", resp.Genre)
	}
	if resp.ReleaseDate != "2003-07-31" {
		t.Errorf("ReleaseDate = %q, want 2003-07-31", resp.ReleaseDate)
	}
	if resp.Year != 2003 {
		t.Errorf("Year = %d, want 2003（应能从完整日期派生）", resp.Year)
	}
}
```
（`albums_test.go` 已 import `net/http`、`net/http/httptest`、`encoding/json`、`testing`；`newTestDB` 已有。确认无需新增 import。）

- [ ] **Step 2: 运行测试确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestGetAlbum_ReturnsGenreAndReleaseDate -v`
Expected: 编译失败（`resp.Genre`/`resp.ReleaseDate` 未定义）或断言失败。

- [ ] **Step 3: 改 albums.go**

a) `AlbumSummary` 加两字段：
```go
type AlbumSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	ArtistID    string `json:"artist_id"`
	Year        int    `json:"year"`
	Genre       string `json:"genre"`
	ReleaseDate string `json:"release_date"`
	TrackCount  int    `json:"track_count"`
	CoverURL    string `json:"cover_url"`
}
```

b) 在文件顶部 import 之后、`AlbumSummary` 之前（或任意包级位置）加一个年份派生辅助：
```go
// yearFromReleaseDate 取 release_date 前 4 位为年份；兼容 "2003" 与 "2003-07-31"。
func yearFromReleaseDate(releaseDate string) int {
	if len(releaseDate) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(releaseDate[:4])
	return y
}
```

c) `ListAlbums` 查询补 `genre`，scan 它，填字段，并用辅助派生 Year。把查询和扫描循环改为：
```go
	rows, err := h.db.Query(`
		SELECT a.id, a.title, COALESCE(ar.name,''), COALESCE(a.artist_id,''),
		       COALESCE(a.release_date,''), COALESCE(a.genre,''), COUNT(t.id)
		FROM albums a
		LEFT JOIN artists ar ON a.artist_id = ar.id
		LEFT JOIN tracks t ON t.album_id = a.id AND t.is_available = 1
		GROUP BY a.id
		ORDER BY ar.name, a.title`)
```
扫描循环：
```go
	for rows.Next() {
		var al AlbumSummary
		var releaseDate string
		if err := rows.Scan(&al.ID, &al.Title, &al.Artist, &al.ArtistID, &releaseDate, &al.Genre, &al.TrackCount); err != nil {
			slog.Error("扫描专辑失败", "err", err)
			continue
		}
		al.ReleaseDate = releaseDate
		al.Year = yearFromReleaseDate(releaseDate)
		al.CoverURL = "/api/v1/cover/" + al.ID
		albums = append(albums, al)
	}
```

d) `getAlbum` 查询补 `genre`，填字段，派生 Year：
```go
	err := h.db.QueryRow(`
		SELECT a.id, a.title, COALESCE(ar.name,''), COALESCE(a.artist_id,''), COALESCE(a.release_date,''), COALESCE(a.genre,'')
		FROM albums a
		LEFT JOIN artists ar ON a.artist_id = ar.id
		WHERE a.id = ?`, id).
		Scan(&al.ID, &al.Title, &al.Artist, &al.ArtistID, &releaseDate, &al.Genre)
```
紧接着（替换原 `al.Year, _ = strconv.Atoi(releaseDate)` 行）：
```go
	al.ReleaseDate = releaseDate
	al.Year = yearFromReleaseDate(releaseDate)
	al.CoverURL = "/api/v1/cover/" + al.ID
```

- [ ] **Step 4: 运行测试确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run TestGetAlbum -v && go test ./internal/api/v1/`
Expected: 新测试 + 既有 album 测试全 PASS。

- [ ] **Step 5: 提交**
```bash
git add internal/api/v1/albums.go internal/api/v1/albums_test.go
git commit -m "feat(api): AlbumSummary 暴露 genre/release_date 并修复 Year 派生"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 2: client.ts 类型 + scrapeAlbum + 修 scanStatus 初始化器

**Files:**
- Modify: `web/src/api/client.ts`
- Modify: `web/src/App.vue`（仅 scanStatus 初始化器）

- [ ] **Step 1: 改 `client.ts` 类型**

`AlbumSummary` 加两字段：
```ts
export type AlbumSummary = {
  id: string
  title: string
  artist: string
  artist_id: string
  year: number
  genre: string
  release_date: string
  track_count: number
  cover_url: string
}
```

`ScanStatus` 补三字段：
```ts
export type ScanStatus = {
  running: boolean
  total: number
  processed: number
  errors: number
  started_at: string
  phase: string
  lyrics_scraped: number
  albums_scraped: number
}
```

- [ ] **Step 2: 加 AlbumScrapeResponse 类型 + scrapeAlbum 方法**

在 `ScrapeResponse` 类型附近加：
```ts
export type AlbumScrapeResponse = {
  album_id: string
  status: string
  mbid?: string
  has_cover: boolean
}
```
在 `scrapeTrack` 方法附近加：
```ts
  scrapeAlbum(albumId: string) {
    return this.request<AlbumScrapeResponse>(`/api/v1/albums/${encodeURIComponent(albumId)}/scrape`, {
      method: 'POST',
    })
  }
```

- [ ] **Step 3: 修 `App.vue` 的 scanStatus 初始化器**（line ~156）

把：
```ts
const scanStatus = reactive<ScanStatus>({
  running: false,
  total: 0,
  processed: 0,
  errors: 0,
  started_at: '',
})
```
改为：
```ts
const scanStatus = reactive<ScanStatus>({
  running: false,
  total: 0,
  processed: 0,
  errors: 0,
  started_at: '',
  phase: 'idle',
  lyrics_scraped: 0,
  albums_scraped: 0,
})
```

- [ ] **Step 4: 构建确认通过** — `make build-frontend`
Expected: vue-tsc 类型检查通过（新增必填字段未破坏其它字面量）；vite 构建成功。

- [ ] **Step 5: 提交**
```bash
git add web/src/api/client.ts web/src/App.vue
git commit -m "feat(web): client 补 genre/release_date/scan 字段 + scrapeAlbum 方法"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 3: AlbumDetail 刮削按钮 + 元数据展示 + 封面缓存击穿（含 App.vue 接线）

**Files:**
- Modify: `web/src/components/AlbumDetail.vue`
- Modify: `web/src/App.vue`（AlbumDetail 用法 + refreshSelectedAlbum）

- [ ] **Step 1: 改 AlbumDetail.vue `<script setup>`**

把现有 script 顶部的 import 与 props/emits 段：
```ts
import { ref, watch } from 'vue'
import { usePlayerStore } from '../stores/player'
import type { AlbumDetail, TrackSummary } from '../api/client'

const props = defineProps<{
  album: AlbumDetail | null
}>()

defineEmits<{
  play: [track: TrackSummary]
}>()
```
改为：
```ts
import { ref, computed, watch } from 'vue'
import { usePlayerStore } from '../stores/player'
import { ApiError, type ApiClient, type AlbumDetail, type TrackSummary } from '../api/client'

const props = defineProps<{
  album: AlbumDetail | null
  api: ApiClient
}>()

const emit = defineEmits<{
  play: [track: TrackSummary]
  refresh: []
}>()
```
并在 `const coverBroken = ref(false)` 附近追加状态 + 刮削逻辑 + 封面源：
```ts
const scraping = ref(false)
const scrapeMessage = ref('')
const coverVersion = ref(0)

// 带版本号的封面 URL：刮削后 bump 版本强制浏览器重取（同 URL 否则命中缓存）
const coverSrc = computed(() =>
  props.album ? `${props.album.cover_url}?v=${coverVersion.value}` : '',
)

async function handleScrape() {
  if (!props.album || scraping.value) return
  scraping.value = true
  scrapeMessage.value = ''
  try {
    const res = await props.api.scrapeAlbum(props.album.id)
    if (res.status === 'done') {
      emit('refresh') // 父组件重载 selectedAlbum，刷新 genre/release_date
      coverVersion.value++
      scrapeMessage.value = '已更新'
    } else {
      scrapeMessage.value = '未匹配到专辑'
    }
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      scrapeMessage.value = '未匹配到专辑'
    } else {
      scrapeMessage.value = '刮削失败，请重试'
    }
  } finally {
    scraping.value = false
  }
}
```
并把 `watch` 块改为切换专辑时一并重置刮削提示：
```ts
watch(
  () => props.album?.id,
  () => {
    coverBroken.value = false
    scrapeMessage.value = ''
  },
)
```

- [ ] **Step 2: 改 AlbumDetail.vue 模板**

a) 背景层（line 4-8）与封面 img（line 23-29）的 `:src`/`backgroundImage` 改用 `coverSrc`：
背景层：
```html
    <div
      v-if="album && album.cover_url && !coverBroken"
      class="detail-backdrop"
      :style="{ backgroundImage: `url(${coverSrc})` }"
    ></div>
```
封面 img：
```html
        <img
          v-if="album.cover_url && !coverBroken"
          :src="coverSrc"
          alt="Album cover artwork"
          class="detail-cover"
          @error="coverBroken = true"
        />
```

b) eyebrow 行（line 35）改为展示发行日期 + 流派：
```html
          <p class="eyebrow" style="color: var(--accent);">
            {{ album.release_date || album.year || '未知年份' }}<span v-if="album.genre"> · {{ album.genre }}</span> · ALBUM
          </p>
```

c) 在 `detail-title-info` 的曲目数 `<p>`（line 40-42）之后、`</div>`（line 43）之前，加刮削按钮 + 提示：
```html
          <button
            class="custom-btn-primary"
            style="width: auto; padding: 8px 18px; font-size: 13px; margin-top: 12px; display: inline-flex; align-items: center; gap: 8px;"
            type="button"
            :disabled="scraping"
            @click="handleScrape"
          >
            <span v-if="scraping" class="loading-spinner" aria-label="刮削中"></span>
            <span>{{ scraping ? '刮削中…' : '🔍 刮削元数据' }}</span>
          </button>
          <p v-if="scrapeMessage" class="muted" style="font-size: 12px; margin-top: 8px;">{{ scrapeMessage }}</p>
```

- [ ] **Step 3: 改 App.vue —— 传 api + @refresh + refreshSelectedAlbum**

a) AlbumDetail 用法（line 63-66）：
```html
        <AlbumDetail
          :album="selectedAlbum"
          :api="api"
          @play="playAlbumTrack"
          @refresh="refreshSelectedAlbum"
        />
```

b) 在 `selectAlbum` 函数（line 248-256）之后追加：
```ts
async function refreshSelectedAlbum() {
  if (!selectedAlbum.value) return
  try {
    selectedAlbum.value = await api.getAlbum(selectedAlbum.value.id)
  } catch (error) {
    handleApiError(error)
  }
}
```

- [ ] **Step 4: 构建确认通过** — `make build-frontend`
Expected: vue-tsc + vite 通过，无类型错误。

- [ ] **Step 5: 提交**
```bash
git add web/src/components/AlbumDetail.vue web/src/App.vue
git commit -m "feat(web): 专辑页刮削元数据按钮 + 流派/发行日期展示 + 封面缓存击穿"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 4: ScanPanel 阶段标签 + 刮削计数

**Files:**
- Modify: `web/src/components/ScanPanel.vue`

- [ ] **Step 1: 加阶段标签 computed**

在 `<script setup>` 的 `startedAt` computed 之后追加：
```ts
const phaseLabel = computed(() => {
  switch (props.status.phase) {
    case 'scanning':
      return '正在扫描'
    case 'scraping':
      return '刮削歌词中'
    case 'metadata':
      return '刮削专辑元数据中'
    default:
      return '空闲'
  }
})
```

- [ ] **Step 2: 模板展示阶段 + 计数**

a) 头部副标题（line 10-12）追加阶段：
```html
        <p class="muted" style="font-size: 13px;">
          已处理 {{ status.processed }} 个音频文件 &middot; 累计发现 {{ status.total }} 个曲目资源 &middot; 阶段：{{ phaseLabel }}
        </p>
```

b) 统计网格（`<dl class="scan-stats">` 内，错误文件数卡片之后、扫描启动时间卡片之前）加两张卡：
```html
      <div class="scan-stats-card">
        <dt>已刮歌词</dt>
        <dd style="color: var(--accent);">{{ status.lyrics_scraped }}</dd>
      </div>
      <div class="scan-stats-card">
        <dt>已刮专辑</dt>
        <dd style="color: var(--accent);">{{ status.albums_scraped }}</dd>
      </div>
```

- [ ] **Step 3: 构建确认通过** — `make build-frontend`
Expected: vue-tsc + vite 通过。

- [ ] **Step 4: 提交**
```bash
git add web/src/components/ScanPanel.vue
git commit -m "feat(web): 扫描面板显示阶段与歌词/专辑刮削计数"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## 完成标准

- `go test ./...` 全绿；`go build ./...` 成功；`make build-frontend` 成功
- 专辑页有「🔍 刮削元数据」按钮，点击触发刮削、完成后流派/发行日期刷新、有反馈提示
- 专辑页展示完整发行日期 + 流派（有则显示）
- 扫描面板显示当前阶段（扫描/歌词/元数据/空闲）+ 已刮歌词数 + 已刮专辑数
- 刮削后完整日期（如 2003-07-31）下 Year 仍正确（取前 4 位）

## 验证（手动）

1. `make build` 启动；打开一张专辑，点「刮削元数据」→ 观察 spinner→「已更新」，发行日期/流派刷新（封面因内嵌优先通常不变，符合设计）
2. 触发扫描，扫描面板阶段经历 正在扫描 → 刮削歌词中 → 刮削专辑元数据中 → 空闲；已刮专辑计数增长
