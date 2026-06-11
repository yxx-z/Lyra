# 手动传参获取歌词（LRCLIB 搜索）设计文档

> 版本：1.0 · 日期：2026-06-12 · 状态：已批准

---

## 背景

当前歌词自动刮削用 LRCLIB 的 `/api/get`（精确匹配 track_name+artist_name+album_name+duration），参数全取自曲目标签。痛点：标签里的歌手名/歌名稍有出入、或时长差几秒，就匹配不到。需要让用户在歌词面板手动纠正参数后查询。

LRCLIB 另有 `/api/search`（模糊搜索，不要求精确时长，返回多个候选，每条自带歌词），适合"手动纠正后选版本"。

---

## 范围

**做**：

- 歌词面板增加"手动获取歌词"入口：默认自动获取流程不变；用户可展开表单填歌名/歌手/(专辑)，用 LRCLIB `/api/search` 查询，得到候选列表，选一条应用到本曲。
- 候选列表只显示元信息（歌名/歌手/专辑/时长/同步标记），点选才应用并看全文。
- 仅 LRCLIB（项目唯一网络歌词源）。

**不做**（YAGNI）：

- 候选歌词预览片段（只显示元信息）。
- 网易/其它源的手动搜索。
- 改动自动刮削流程或"升级为同步歌词"流程。

---

## 后端

### LRCLIBClient.Search（`internal/lyrics/lrclib.go`）

新增方法（与现有 `Fetch` 同一 client，复用 baseURL/userAgent/httpClient）：

```go
type SearchCandidate struct {
    TrackName    string
    ArtistName   string
    AlbumName    string
    Duration     int
    SyncedLyrics string
    PlainLyrics  string
    Instrumental bool
}

func (c *LRCLIBClient) Search(ctx context.Context, trackName, artistName, albumName string) ([]SearchCandidate, error)
```

- 校验：`trackName` 与 `artistName` 至少一个非空，否则 `ErrInvalidQuery`。
- 请求：GET `{baseURL}/api/search`，query 参数 `track_name`、`artist_name`、`album_name`（非空才带）；头 `User-Agent`、`Accept: application/json`。
- 响应是 JSON 数组，逐项解析为 `SearchCandidate`（字段 `trackName`/`artistName`/`albumName`/`duration`/`syncedLyrics`/`plainLyrics`/`instrumental`）。非 2xx → error；空数组 → 返回空切片（非错误）。

### 搜索端点（`internal/api/v1/lyrics.go` 或新 handler）

`GET /api/v1/tracks/{id}/lyrics/search?trackName=&artistName=&albumName=`（在 `/api/v1` 鉴权组内）。

- `id` 仅用于路由归属/鉴权上下文；实际搜索用 query 参数。
- 调 `LRCLIBClient.Search`，把结果映射为：
  ```json
  { "candidates": [
    { "trackName": "...", "artistName": "...", "albumName": "...",
      "duration": 218, "synced": true, "lrc": "<同步优先,否则纯文本>" }
  ] }
  ```
  - `synced` = `syncedLyrics` 非空；`lrc` = `syncedLyrics` 优先、否则 `plainLyrics`。
  - 跳过 `instrumental` 或 lrc 为空的候选。
- LRCLIB 出错 → 500 + 简短错误；参数都空 → 400。

落点：给持有 `*lyrics.LyricsService` 的 `ScrapeHandler` 增持一个 `*lyrics.LRCLIBClient` 并加 `SearchLyrics` 方法；或新建一个轻量 `LyricsSearchHandler{client *lyrics.LRCLIBClient}`。router 现在在 `NewLyricsService(...)` 内部 inline 构造了 `NewLRCLIBClient(...)`——把它提取成一个局部变量 `lrclib := lyrics.NewLRCLIBClient(...)`，同时传给 `NewLyricsService` 与新搜索 handler，避免重复构造。

### 应用候选

复用既有 `PUT /api/v1/tracks/{id}/lyrics`（`LyricsRequest{lrc_content, yrc_content, source}`）：前端把选中候选的 `lrc` 作为 `lrc_content`、`source:"manual"` PUT 上去。无需新增保存端点。

---

## 前端（LyricsPanel.vue）

- 在歌词区加"手动获取歌词"入口（无歌词/纯器乐态、以及已有歌词态都能用，例如底部一个小按钮 `🔍 手动获取歌词`）。
- 点开内联表单：
  - 歌名（预填 `playerStore.currentTrack.title`）
  - 歌手（预填 `playerStore.currentTrack.artist`）
  - 专辑（预填 `playerStore.currentTrack.album`）
  - 「查询」按钮（loading 态）
- 查询 → 调 `api.searchLyrics(trackId, {trackName, artistName, albumName})` → 渲染候选列表，每行**仅元信息**：`歌名 - 歌手 · 专辑 · mm:ss · [同步]/[纯文本]`。
- 点某条候选 → 调既有 `putLyrics(trackId, {lrc_content: candidate.lrc, source:'manual'})` → 成功后 `loadLyrics()` 重新拉取渲染（即"应用并看全文"）。
- 无结果 → 提示"未找到，试试调整歌名/歌手"。
- 默认打开面板/切歌的自动 `loadLyrics()` 流程不变；手动表单是叠加能力。
- `web/src/api/client.ts` 新增 `searchLyrics(trackId, params)`；应用复用现有 putLyrics 方法（`web/src/api/client.ts` 已有 PUT lyrics 方法）。

候选 DTO（client.ts）：
```ts
export type LyricCandidate = { trackName: string; artistName: string; albumName: string; duration: number; synced: boolean; lrc: string }
```

---

## 测试

| 测试 | 方式 |
|------|------|
| `LRCLIBClient.Search` 解析候选数组（synced/plain/instrumental/duration）| httptest 假 LRCLIB `/api/search` 返回固定 JSON 数组 |
| Search 参数都空 → ErrInvalidQuery；无结果 → 空切片非错误 | 单测 |
| 搜索端点：返回 `{candidates:[...]}`，synced 标记正确、跳过 instrumental/空 lrc | httptest（注入假 LRCLIB baseURL）|
| 应用候选：走既有 `PUT /tracks/{id}/lyrics`（已有测试覆盖）| —— |

后端全部 httptest + 假 HTTP 服务（不打真实 LRCLIB）。前端构建 + 真机验证（搜索→选→应用→面板刷新）。

---

## 不在本次范围内

- 候选歌词预览片段。
- 网易/其它源手动搜索、逐字 YRC。
- 自动刮削/升级流程改动。
