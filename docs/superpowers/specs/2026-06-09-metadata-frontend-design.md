# 元数据/封面刮削前端接入设计文档

> 版本：1.0 · 日期：2026-06-09 · 状态：已批准

---

## 目标

把已合并的"专辑元数据 + 封面刮削"后端在前端露出来，使其可见可用：

1. **按需触发**：专辑详情页提供「刮削元数据」按钮，调用 `POST /api/v1/albums/{id}/scrape`，完成后重载。
2. **展示刮到的元数据**：专辑详情页显示完整发行日期 + 流派。
3. **进度可见**：扫描面板显示当前阶段（扫描/歌词/元数据）与歌词数、专辑数进度。

对应 PRD：US-05/US-06 的前端呈现（v0.2 收尾）。

---

## 范围

**本次做：**
- 后端：`AlbumSummary` 暴露 `genre` + `release_date`（完整日期）
- 前端 `client.ts`：类型补字段 + 新增 `scrapeAlbum` 方法
- 前端 `AlbumDetail.vue`：刮削按钮 + 元数据展示 + 封面缓存击穿
- 前端 `ScanPanel.vue`：阶段 + 刮削计数展示

**本次不做（YAGNI）：**
- 刮削状态徽章（pending/done/failed）
- 批量刮削按钮
- 艺术家级元数据（biography/image）展示

---

## 设计前提

封面服务优先级为**内嵌 > 本地 cover.jpg > 刮削 cover_path**（见 `2026-06-08-metadata-cover-scraping-design.md` 决策 A）。带内嵌封面的专辑点「刮削」后封面通常不变（内嵌仍优先），但 `genre`/`release_date` 会更新。按钮的主要可见收益是元数据；封面刷新仅对无内嵌/本地封面的专辑生效。

---

## 后端改动

### `internal/api/v1/albums.go`

`AlbumSummary` 增两字段：
```go
type AlbumSummary struct {
    ID          string `json:"id"`
    Title       string `json:"title"`
    Artist      string `json:"artist"`
    ArtistID    string `json:"artist_id"`
    Year        int    `json:"year"`
    Genre       string `json:"genre"`        // 新增
    ReleaseDate string `json:"release_date"` // 新增，完整日期如 "2003-07-31"
    TrackCount  int    `json:"track_count"`
    CoverURL    string `json:"cover_url"`
}
```

- `ListAlbums` 查询：在现有 SELECT 中追加 `COALESCE(a.genre,'')`；`release_date` 已在查询中（用于派生 `year`），同时填入 `ReleaseDate`。
- `GetAlbum` 查询：同样追加 `COALESCE(a.genre,'')`，填 `Genre` 与 `ReleaseDate`。
- `Year` 保留（继续从 release_date 前 4 位派生）。
- 扫描时空值用 `COALESCE` 兜底为空串。

### 接口契约不变

`POST /api/v1/albums/{id}/scrape` 已存在（上轮实现），返回 `{album_id, status, mbid, has_cover}`。本次不改后端刮削逻辑。

---

## 前端改动

### `web/src/api/client.ts`

`AlbumSummary` 类型加：
```ts
genre: string
release_date: string
```

`ScanStatus` 类型补齐（缺哪个补哪个，与后端 `ScanStatus` JSON 对齐）：
```ts
phase: string
lyrics_scraped: number
albums_scraped: number
```

新增类型与方法：
```ts
export type AlbumScrapeResponse = {
  album_id: string
  status: string      // "done" | "failed"
  mbid?: string
  has_cover: boolean
}

async scrapeAlbum(albumId: string): Promise<AlbumScrapeResponse> {
  return this.request<AlbumScrapeResponse>(
    `/api/v1/albums/${encodeURIComponent(albumId)}/scrape`,
    { method: 'POST' },
  )
}
```
（与现有 `scrapeTrack` 同风格；`request` 的错误处理沿用，404/502 抛 `ApiError`。）

### `web/src/components/AlbumDetail.vue`

**按钮**：在现有播放按钮旁新增「🔍 刮削元数据」按钮：
```
scraping (ref) / scrapeMessage (ref)
handleScrape():
  scraping = true; scrapeMessage = ''
  try:
    res = await api.scrapeAlbum(album.id)
    if res.status === 'done':
        await reloadAlbum()          // 重新 getAlbum，刷新 genre/release_date/cover
        scrapeMessage = '已更新'
    else:
        scrapeMessage = '未匹配到专辑'   // status === 'failed'
  catch (ApiError 404): scrapeMessage = '未匹配到专辑'
  catch (其它):          scrapeMessage = '刮削失败，请重试'
  finally: scraping = false
```
- 刮削中按钮 disabled + spinner（复用现有 `.loading-spinner`，配色用 currentColor）。
- 始终可点（无状态门控）。

**元数据展示**：eyebrow 行 `{{ album.year || '未知年份' }} · ALBUM` 改为展示完整发行日期 + 流派：
```
{{ album.release_date || album.year || '未知年份' }}<span v-if="album.genre"> · {{ album.genre }}</span> · ALBUM
```

**封面缓存击穿**：模板中封面 `:src` 改为带版本参数，重载后 bump 版本以强制刷新：
```
coverVersion (ref = 0)
:src="`${album.cover_url}?v=${coverVersion}`"
reloadAlbum(): album = await api.getAlbum(id); coverVersion++ ; coverBroken = false
```
（`cover_url` 形如 `/api/v1/cover/{id}`，同 URL 浏览器会缓存，必须靠变化的 `?v=` 强制重取。）

### `web/src/components/ScanPanel.vue`

**阶段标签**：依据 `status.phase` 显示：
```
scanning → '正在扫描'
scraping → '刮削歌词中'
metadata → '刮削专辑元数据中'
idle / 其它 → '空闲'
```
用一个 computed 映射；在现有标题/状态文案处展示。

**刮削计数**：在现有统计区（total/processed/errors）追加两项：
```
已刮歌词：{{ status.lyrics_scraped }}
已刮专辑：{{ status.albums_scraped }}
```

---

## 错误处理

- `scrapeAlbum` 404（专辑不存在 / 未匹配）→ 提示「未匹配到专辑」
- `scrapeAlbum` 502（MB 异常）→ 提示「刮削失败，请重试」
- `getAlbum` 重载失败 → 沿用现有错误处理；按钮提示「刮削失败，请重试」
- JSON/网络异常不崩页面

---

## 测试策略

| 测试 | 方式 |
|------|------|
| 后端：GetAlbum 返回 genre/release_date | `albums_test.go` 预置含 genre/release_date 的专辑，断言响应字段 |
| 后端：ListAlbums 返回 genre/release_date | 同上（或在现有列表测试中补断言） |
| 前端构建 | `make build-frontend`（vue-tsc 类型检查 + vite 构建）通过 |

不做 JS 单测（项目无前端测试 runner，沿用既有约定）。

---

## 不在本次范围内

- 刮削状态徽章
- 批量/全库刮削触发
- 艺术家简介/头像展示
- 封面优先级配置切换（保持内嵌优先）
