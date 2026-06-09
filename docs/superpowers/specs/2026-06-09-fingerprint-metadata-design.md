# 指纹联动元数据精配设计文档

> 版本：1.0 · 日期：2026-06-09 · 状态：已批准

---

## 目标

用 AcoustID 指纹得到的 recording MBID 精确定位专辑的权威 release，替代易受简繁/标点差异影响的文本搜索（文本搜索保留为兜底）。命中后填 `album.mbid/release_date/cover`，比文本匹配更准。

对应 PRD：US-09/10/11（精准识别，v0.3）；承接 AcoustID 指纹（US-07）。

**可行性已验证**：recording《以父之名》(9bbe2659) → MB 返回 10 个 release，含《叶惠美》正版 (faf326c3) 及各语言变体；语言中立，绕开简繁匹配问题。

---

## 范围

**本次做：**
- 扫描阶段顺序改为 歌词 → 指纹 → 元数据
- `EnrichAlbum` 改为指纹优先 + 文本兜底
- `MusicBrainzClient` 加 `RecordingReleases` + `ReleaseDate` + **自节流**（全局 ≥1.1s）
- 投票算法（覆盖度）定位 release

**本次不做（YAGNI）：**
- 自动改写曲目标题/艺术家
- 曲目级 recording→release 回写
- 纯文本→同步歌词升级（独立功能，已验证 LRCLIB 有数据，另开 spec）
- 前端改动

---

## 架构

### MusicBrainzClient 新增（`internal/metadata/musicbrainz.go`）

```go
// RecordingReleases 返回某 recording 所属的所有 release MBID。
func (c *MusicBrainzClient) RecordingReleases(ctx context.Context, recordingMBID string) ([]string, error)
// ReleaseDate 返回某 release 的发行日期（date 字段，可能空）。
func (c *MusicBrainzClient) ReleaseDate(ctx context.Context, releaseMBID string) (string, error)
```
- `RecordingReleases`：`GET {base}/ws/2/recording/{mbid}?inc=releases&fmt=json`，解析 `releases[].id`。
- `ReleaseDate`：`GET {base}/ws/2/release/{mbid}?fmt=json`，解析 `date`。
- 均带 `User-Agent`、`ctx` 绑定；非 2xx / 解码异常 → 普通 error。

### 自节流（关键，正确性必需）

MB 限速 1 req/s 是**全局**的；指纹路径每张专辑会发多次 MB 请求（≤5 recording + 1 release）。给 `MusicBrainzClient` 加自节流：

```go
type MusicBrainzClient struct {
    ...
    minInterval time.Duration // 默认 1100ms；测试设 0
    mu          sync.Mutex
    lastReqAt   time.Time
}
```
- 在每次发请求前（`Search`/`RecordingReleases`/`ReleaseDate` 共用的内部 `doGet`）：加锁，若距 `lastReqAt` 不足 `minInterval` 则 sleep 补足（sleep 用 `select{<-time.After: ; <-ctx.Done():}` 可中断），更新 `lastReqAt`，解锁后再发。
- `NewMusicBrainzClient` 默认 `minInterval = 1100ms`。包内测试可直接设 `c.minInterval = 0` 避免拖慢。
- 重构：把 `Search`/`RecordingReleases`/`ReleaseDate` 的 HTTP GET 收敛到一个内部 `doGet(ctx, url) ([]byte, error)`，节流逻辑在此一处。

### 投票算法（纯函数，可测）

```go
// pickByVote 统计 release 覆盖度，返回覆盖最多的 release MBID；并列取先出现者。
// releasesPerTrack: 每首曲目查到的 release MBID 列表。
func pickByVote(releasesPerTrack [][]string) (string, bool)
```
- 累计每个 release MBID 在多少首曲目中出现；取计数最大者；并列取首个达到该计数的（按遍历顺序，确定性）；无任何 release → false。

---

## EnrichAlbum 新流程（`internal/metadata/service.go`）

```
1. 载专辑 title/artist/曲目数；不存在 → ErrAlbumNotFound
2. 查本专辑 recording MBID：
     SELECT mbid FROM tracks WHERE album_id=? AND is_available=1
       AND mbid IS NOT NULL AND mbid<>'' LIMIT 5
3. 若有 ≥1 个 recording MBID（指纹路径）：
     对每个调 mb.RecordingReleases → releasesPerTrack
     pickByVote → releaseMBID
     命中 → date = mb.ReleaseDate(releaseMBID)；match = {MBID:releaseMBID, ReleaseDate:date}
4. 指纹路径未命中（无 recording mbid，或投票空，或 RecordingReleases 全失败）：
     退回 match, err := mb.Search(ctx, {AlbumTitle,ArtistName,TrackCount})  // 现有文本路径
     ErrNotFound → scrape_status='failed'，返回 {Status:"failed"}
     其它 error → 透传
5. 落库（复用现有逻辑）：
     UPDATE album release_date/genre；mbid 单独 best-effort（UNIQUE 容错）
     封面 CAA FetchFront(releaseMBID) → 写 cover_path（内嵌优先级不变）
     scrape_status='done'，返回 {Status:"done", MBID, HasCover}
```

**指纹路径的瞬时错误**：`RecordingReleases`/`ReleaseDate` 网络异常——若所有 recording 查询都失败，视为指纹路径无结果，退回文本搜索（不直接报错，保证有兜底）。`ReleaseDate` 失败则该 release 仍可用、date 留空（best-effort）。

**`genre`**：维持现状（MB release 无稳定 genre，留空）。

---

## 扫描器阶段顺序调整（`internal/scanner/scanner.go`）

`doScan` 当前：lyrics(scraping) → metadata → fingerprint。改为：
```
lyrics(scraping) → fingerprint → metadata
```
即把指纹阶段块移到元数据阶段块**之前**。这样元数据阶段运行时，本批曲目的 recording MBID 已写好。各阶段仍串行、受 `scrapeEnabled` 与对应 service 非 nil 门控、可中断；计数器不变。

> 注意：元数据阶段与指纹阶段都遍历各自的 pending 集合（albums.scrape_status='pending' / tracks.acoustid IS NULL），互不影响；仅顺序调整，无其它逻辑变更。

---

## 测试策略

| 测试 | 方式 |
|------|------|
| pickByVote：覆盖最多胜 / 并列取先出现 / 空 → false | 纯函数表驱动 |
| RecordingReleases：httptest 灌 recording JSON → 返回 release id 列表 | httptest |
| ReleaseDate：httptest → 返回 date；缺 date → 空串 | httptest |
| 自节流：minInterval>0 时两次请求间隔 ≥ 阈值；=0 时不延迟 | 计时断言（小阈值如 50ms 避免拖慢） |
| EnrichAlbum 指纹路径：预置带 mbid 的曲目 + httptest（recording→releases + release date + CAA）→ 落库正确 release、source 为指纹 | 内存 sqlite + httptest |
| EnrichAlbum 无 recording mbid → 走文本兜底（现有 Search 路径），既有测试保留通过 | 内存 sqlite + httptest |
| EnrichAlbum 指纹查询全失败 → 退回文本兜底 | httptest（recording 接口 500）|
| 扫描器阶段顺序：fingerprint 在 metadata 之前（既有阶段测试仍绿） | 既有 + 构建 |

**全部 httptest + fake，不打真网络。** 真实端到端合并后在 docker 验证（recording→release 已先验证可行）。

---

## 不在本次范围内

- 自动改写曲目标题/艺术家
- 曲目级精配回写
- 纯文本→同步歌词升级
- 前端展示
