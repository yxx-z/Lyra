# 歌词刮削接入设计文档

> 版本：1.0 · 日期：2026-06-01 · 状态：已批准

---

## 目标

1. **前端接入歌词刮削**：在歌词面板「暂无歌词」处提供「获取歌词」按钮，触发已有的 `POST /tracks/{id}/scrape`。
2. **扫描后自动刮削**：全量扫描完成后，后台串行为缺歌词的曲目刮削（受 `scraper.enabled` 控制）。
3. **多源 Provider 链**：将刮削源抽象为优先级链，本轮实现「内嵌标签 → LRCLIB」，为后续网易云预留扩展点。

对应 PRD：US-24（歌词同步显示）、4.3（歌词来源优先级：数据库缓存 → 内嵌标签 → LRCLIB → 网易云 → 纯文本）

---

## 范围

**本次做：**
- `internal/lyrics` 抽出 Provider 接口 + LyricsService 编排
- 内嵌标签歌词 provider（embedded）+ LRCLIB provider（改造现有）
- 扫描器后台刮削阶段（解耦、串行、间隔、可中断）
- 前端「获取歌词」按钮

**本次不做（留待后续独立 spec）：**
- 网易云 provider（非官方 API + YRC 逐字解析，PRD v0.3）
- 前端手动编辑/粘贴歌词 UI（`saveLyrics`，client.ts 已有方法但不接 UI）
- 前端删除歌词 UI（`deleteLyrics`，同上）

---

## 架构

### 文件结构

```
internal/lyrics/
├── provider.go        新建：Provider 接口 + Query/Result + 错误类型（迁移 ErrNotFound 等）
├── embedded.go        新建：embeddedProvider，读内嵌 LYRICS 标签（dhowden/tag）
├── lrclib.go          改造：LRCLIBClient 适配为 Provider
└── service.go         新建：LyricsService，编排缓存检查 + Provider 链 + 写库

internal/api/v1/
├── scrape.go          改造：ScrapeHandler 改为薄壳，调 LyricsService
└── lyrics.go          不变（GET/PUT/DELETE 歌词）

internal/scanner/
└── scanner.go         改造：注入 LyricsService，doScan 末尾追加刮削阶段；ScanStatus 加字段

internal/api/router.go 改造：构造 LyricsService 注入 ScrapeHandler 和 Scanner
cmd/server/main.go     改造：Scanner 构造传入刮削依赖

web/src/components/LyricsPanel.vue  改造：暂无歌词处加「获取歌词」按钮
```

---

## Provider 接口

```go
// provider.go
type Query struct {
    TrackName  string
    ArtistName string
    AlbumName  string
    Duration   int
    FilePath   string // 内嵌源需要读文件
}

type Result struct {
    LRCContent string
    YRCContent string // 预留网易云 YRC
    Source     string // "embedded" / "lrclib" / "netease"
}

type Provider interface {
    Name() string
    Fetch(ctx context.Context, q Query) (Result, error)
}
```

错误（沿用 lrclib.go 现有定义，迁移到 provider.go）：
- `ErrNotFound` — 该源未找到歌词
- `ErrInvalidQuery` — 查询信息不足（缺曲名/艺术家）

### embeddedProvider（embedded.go）

- 用 `dhowden/tag` 打开 `q.FilePath`，读内嵌歌词（`tag.Metadata.Lyrics()`）
- 非空 → `Result{LRCContent: 内容, Source: "embedded"}`（内嵌通常是纯文本，无时间轴，原样存 lrc_content）
- 空或读取失败 → `ErrNotFound`
- `Name() == "embedded"`，纯本地零网络

### lrclibProvider（lrclib.go 改造）

- 现有 `LRCLIBClient` 增加 `Name() string`（返回 "lrclib"），其 `Fetch` 已符合接口签名
- `Fetch` 命中返回 `Result{LRCContent, Source: "lrclib"}`

---

## LyricsService 编排

```go
// service.go
type ScrapeOutcome struct {
    Status string // "done" | "skipped" | "failed"
    Source string
}

type LyricsService struct {
    db        *sql.DB
    providers []Provider // 按优先级：embedded, lrclib
}

func NewLyricsService(db *sql.DB, providers ...Provider) *LyricsService

func (s *LyricsService) ScrapeTrack(ctx context.Context, trackID string) (ScrapeOutcome, error)
```

**ScrapeTrack 流程：**

```
1. 查 track（title/artist/album/duration/file_path），不存在 → 返回 error（ErrTrackNotFound）
2. 已有歌词（lyrics 表 lrc/yrc 非空）？
     → UPDATE tracks scrape_status='done'，返回 {Status:"skipped"}
3. 构造 Query，按顺序遍历 providers：
     provider.Fetch 返回成功（非 ErrNotFound）→ 采用，跳出
     返回 ErrNotFound/ErrInvalidQuery → 试下一个
4. 有命中：
     INSERT/UPSERT lyrics(track_id, lrc_content, yrc_content, source)
     UPDATE tracks scrape_status='done'
     返回 {Status:"done", Source: 命中源}
5. 全失败：
     UPDATE tracks scrape_status='failed'
     返回 {Status:"failed"}
```

**错误语义：**
- `ErrTrackNotFound`（service 定义）→ HTTP 层映射 404
- provider 全部 ErrNotFound → 不是 error，是 `{Status:"failed"}`（HTTP 层映射 404「未找到歌词」）
- provider 网络/IO 异常 → 返回 wrapped error（HTTP 层映射 502）

---

## scrape.go 改造（HTTP 薄壳）

`ScrapeHandler` 持有 `*LyricsService`，`scrapeTrack` 改为：

```
outcome, err := service.ScrapeTrack(r.Context(), trackID)
switch {
  err is ErrTrackNotFound       → 404
  err != nil（provider 异常）    → 502「歌词刮削失败」
  outcome.Status == "failed"    → 404「未找到歌词」
  其他（done/skipped）          → 200 ScrapeResponse{Status, Source}
}
```

`ScrapeResponse` 结构不变（track_id/status/source/message）。删除 scrape.go 中已迁移到 service 的 DB 逻辑（loadTrackForScrape/hasLyrics/updateScrapeStatus/写库）。

---

## 扫描器后台刮削阶段

### Scanner 结构体新增

```go
type Scanner struct {
    ... 现有字段 ...
    lyricsService *lyrics.LyricsService // 可为 nil（未启用刮削）
    scrapeEnabled bool
}
```

`NewScanner` 增加参数传入这两项（main.go 注入 `cfg.Scraper.Enabled` 和构造好的 service）。

### ScanStatus 新增字段

```go
type ScanStatus struct {
    Running       bool      `json:"running"`
    Total         int64     `json:"total"`
    Processed     int64     `json:"processed"`
    Errors        int64     `json:"errors"`
    StartedAt     time.Time `json:"started_at"`
    Phase         string    `json:"phase"`          // "scanning" | "scraping" | "idle"
    LyricsScraped int64     `json:"lyrics_scraped"` // 成功刮到的数量
}
```

`phase` 用一个 `atomic.Value`（或带锁的 string）维护；`lyricsScraped` 用 `atomic.Int64`。`idle` 为非运行时的默认值。

### doScan 末尾追加

```
（现有入库循环结束，phase 期间为 "scanning"）
    ↓
若 s.scrapeEnabled 且 s.lyricsService != nil：
    phase = "scraping"
    查询 SELECT id FROM tracks WHERE scrape_status='pending' AND is_available=1
    串行遍历每个 id：
        select { case <-ctx.Done(): return; default: }   // 可中断
        outcome, err := s.lyricsService.ScrapeTrack(ctx, id)
        if err != nil || outcome.Status == "failed" → errors++（不中断）
        if outcome.Status == "done" → lyricsScraped++
        // 间隔：仅当本次命中源是网络源（lrclib）或发生网络请求时 sleep 800ms；
        //       内嵌命中（embedded）或 skipped 不 sleep
        若 outcome.Source == "lrclib" 或 outcome.Status == "failed"：
            select { case <-time.After(800ms): case <-ctx.Done(): return }
    phase = "idle"（runScan 退出时统一复位）
```

**判断"是否施加间隔"的依据**：`outcome.Source != "embedded" && outcome.Status != "skipped"` 时 sleep（即只要可能打了 lrclib 网络就礼貌等待）。

### 关键约束

- 刮削阶段仍在 `running=true` 内（扫描+刮削是一次完整任务）
- `ctx`/`stopCh` 取消能立即中断刮削循环 → `Stop()` 不会被卡住
- 刮削失败/异常只累加 errors，绝不影响已入库曲目
- `TriggerScan`/`Start` 的整体结构不变，只是 doScan 内多一个阶段

---

## 前端歌词接入

### LyricsPanel.vue

当前 `error==='no_lyrics' || lrcLines.length===0` 分支只显示「暂无歌词」。改为额外渲染「获取歌词」按钮：

```
[暂无歌词]
[🔍 获取歌词]  ← 新按钮
    点击 handleScrape():
      scraping = true
      try:
        const res = await api.scrapeTrack(track.trackId)
        if res.status === 'done' || res.status === 'skipped':
            重新 await loadLyrics()   // 调 getLyrics → parseLrc → 渲染
        else:
            提示「未找到歌词」
      catch (404)： 提示「未找到歌词」
      catch (其他)：提示「刮削失败，请重试」
      finally: scraping = false
```

- 刮削中按钮 disabled + 显示 spinner（复用现有 `.loading-spinner`，注意配色用 currentColor）
- 成功后复用现有渲染逻辑（解析 + 滚动到当前行）
- `api.scrapeTrack` 已存在于 client.ts，无需新增

### 不做

- 手动编辑/粘贴/删除歌词 UI（本次范围外）

---

## 测试策略

| 测试 | 方式 |
|------|------|
| embeddedProvider 读内嵌歌词 | testdata 带 LYRICS 标签的样本文件；无标签返回 ErrNotFound |
| LyricsService 链：embedded 命中则不调 lrclib | mock 两个 provider，断言短路 |
| LyricsService：已有歌词跳过 | 内存 SQLite 预置歌词，断言 skipped |
| LyricsService：全部 ErrNotFound → failed | mock provider 均返回 ErrNotFound |
| LyricsService：provider 网络异常 → error 透传 | mock provider 返回普通 error |
| scrape.go HTTP 映射 | httptest，验证 200/404/502 各分支 |
| 扫描器刮削阶段 | 内存 SQLite + mock service，断言遍历 pending、ctx 取消可中断、计数正确 |
| 前端 | 构建通过（vue-tsc）；按钮触发 scrapeTrack → reload 逻辑 |

---

## 不在本次范围内

- 网易云 provider + YRC 逐字（v0.3，独立 spec）
- 前端手动编辑/删除歌词 UI
- 纯文本歌词回退的特殊渲染（embedded 无时间轴的纯文本已能作为单段显示）
