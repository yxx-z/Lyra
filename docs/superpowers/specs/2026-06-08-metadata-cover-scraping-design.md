# 专辑元数据 + 封面刮削设计文档

> 版本：1.0 · 日期：2026-06-08 · 状态：已批准

---

## 目标

1. **专辑元数据刮削**：按"艺术家 + 专辑"查 MusicBrainz，补全 `albums` 的 `mbid / release_date / genre`。
2. **高清封面刮削**：用 release mbid 从 Cover Art Archive 下载封面到本地 artwork 缓存，写 `albums.cover_path`。
3. **融入既有架构**：扫描完成后追加专辑级刮削阶段（受 `scraper.enabled` 控、串行、可中断），并提供按需接口。

对应 PRD：US-05（自动识别曲名/艺术家/专辑/年份/流派）、US-06（自动下载高质量专辑封面）、v0.2。

**数据源选型说明**：PRD 167 行列的封面源是「网易云 > Last.fm > Spotify」。但网易云匿名访问受版权门控（见 `2026-06-01` 网易云冻结记录），Last.fm/Spotify 需 API key。本设计改用 **MusicBrainz + Cover Art Archive**——开放数据、无门控、无需 key，且封面与 MB release 直接关联。已用真实接口验证：周杰伦《叶惠美》MB score=100 命中、CAA 返回真实封面。

---

## 范围

**本次做：**
- 新包 `internal/metadata`：MusicBrainz 搜索 + 选择、Cover Art Archive 取图、MetadataService 编排
- `albums` 加 `scrape_status` 列（迁移）
- 扫描器专辑元数据刮削阶段（解耦、串行、≥1s 间隔、可中断）
- `cover.go` serving 追加 `cover_path` 兜底
- 按需接口 `POST /api/v1/albums/{id}/scrape`

**本次不做（YAGNI / 后续独立 spec）：**
- AcoustID 指纹识别（US-07，v0.3，需 chromaprint + AcoustID key）
- 艺术家简介/头像（`artists.biography / image_url`）
- Last.fm / Spotify 封面源
- track 级 recording mbid 精确识别
- 前端专辑刮削按钮（后端接口先就位，前端下轮加）

---

## 架构

### 文件结构

```
internal/metadata/
├── musicbrainz.go    新建：MusicBrainzClient（release 搜索）+ pickRelease（纯函数选择）
├── coverart.go       新建：CoverArtClient（按 release mbid 取封面）
└── service.go        新建：MetadataService（编排：查 MB → 填字段 → 下封面 → 写状态）

internal/db/migrations/003_albums_scrape_status.up.sql   新建：albums 加 scrape_status
internal/db/schema.sql                                   同步更新
internal/scanner/scanner.go    改：doScan 末尾追加专辑元数据阶段；ScanStatus 加字段
internal/scanner/ingester.go   改：findOrCreateAlbum 建新专辑时 scrape_status='pending'
internal/api/v1/cover.go       改：serving 末尾追加 cover_path 兜底
internal/api/v1/album_scrape.go 新建：AlbumScrapeHandler（薄壳，调 MetadataService）
internal/api/router.go         改：构造 MetadataService，注册 POST /albums/{id}/scrape
cmd/server/main.go             改：构造 MetadataService 注入 Scanner
```

---

## MusicBrainz（musicbrainz.go）

### 类型

```go
// AlbumQuery 是查询输入。
type AlbumQuery struct {
    AlbumTitle  string
    ArtistName  string
    TrackCount  int // 本地该专辑曲目数，用于在多个 release 中择优
}

// ReleaseMatch 是从 MB 选中的 release。
type ReleaseMatch struct {
    MBID        string
    Title       string
    ReleaseDate string // MB 的 date 字段，可能 "2003-07-31" 或 "2003" 或空
    Genre       string // best-effort，可能空
}
```

### MusicBrainzClient

```go
func NewMusicBrainzClient(baseURL, userAgent string, httpClient *http.Client) *MusicBrainzClient
func (c *MusicBrainzClient) Search(ctx context.Context, q AlbumQuery) (ReleaseMatch, error)
```

- baseURL 默认 `https://musicbrainz.org`，userAgent 必填（MB 无 UA 返回 403）
- 请求：`GET {baseURL}/ws/2/release/?query=artist:"{ArtistName}" AND release:"{AlbumTitle}"&fmt=json`，header `User-Agent`，`http.NewRequestWithContext` 绑定 ctx
- 解析 `releases[]`：每个含 `id`、`title`、`score`、`date`、`track-count`
- 调 `pickRelease` 择优；无满足 → `ErrNotFound`
- 网络/解码异常 → 普通 error
- genre：MB release 顶层无稳定 genre 字段，本次 **best-effort**（若响应含可用 genre/tag 则取首个，否则留空）；不为 genre 单独再发请求

### pickRelease（纯函数，可测）

```go
func pickRelease(releases []mbRelease, localTrackCount int) (mbRelease, bool)
```

- 过滤 `score >= 90`
- 在过滤后的集合里，选 `|release.TrackCount - localTrackCount|` 最小的；并列时取 score 更高者，再并列取靠前者
- `localTrackCount <= 0`（未知）时退化为取 score 最高/靠前者
- 过滤后为空 → 返回 `(_, false)`

错误（包内定义，与 lyrics 风格一致）：
- `ErrNotFound` — 无 score≥90 的匹配

---

## Cover Art Archive（coverart.go）

```go
func NewCoverArtClient(baseURL string, httpClient *http.Client) *CoverArtClient
func (c *CoverArtClient) FetchFront(ctx context.Context, releaseMBID string) (data []byte, mimeType string, err error)
```

- baseURL 默认 `https://coverartarchive.org`
- 请求：`GET {baseURL}/release/{mbid}/front`（CAA 返回 307 跳转到 archive.org 实际图片；`http.Client` 默认跟随重定向）
- 200 → 读 body + Content-Type（默认 `image/jpeg`）
- 404 → 返回 `(nil, "", ErrNoCover)`（该 release 无封面，非异常）
- 其它非 2xx / 网络异常 → 普通 error

错误：`ErrNoCover` — 该 release 无封面

---

## MetadataService 编排（service.go）

```go
var ErrAlbumNotFound = errors.New("专辑不存在")

type EnrichOutcome struct {
    Status   string // "done" | "failed"
    MBID     string
    HasCover bool
}

type MetadataService struct {
    db         *sql.DB
    mb         *MusicBrainzClient
    cover      *CoverArtClient
    artworkDir string
}

func NewMetadataService(db *sql.DB, mb *MusicBrainzClient, cover *CoverArtClient, artworkDir string) *MetadataService
func (s *MetadataService) EnrichAlbum(ctx context.Context, albumID string) (EnrichOutcome, error)
```

**EnrichAlbum 流程：**

```
1. 载入专辑：title、artist 名（JOIN artists）、本地曲目数（COUNT tracks WHERE album_id=? AND is_available=1）
     专辑不存在 → ErrAlbumNotFound
2. mb.Search(ctx, {AlbumTitle, ArtistName, TrackCount})
     ErrNotFound → UPDATE albums scrape_status='failed'，返回 {Status:"failed"}
     其它 error → 透传（HTTP 502 / 扫描计数）
3. UPDATE albums SET mbid=?, release_date=?, genre=COALESCE(NULLIF(?,''),genre) WHERE id=?
4. 封面：cover.FetchFront(ctx, match.MBID)
     成功 → 写 artworkDir/{albumID}.{ext}（ext 由 mime 定，默认 jpg）；UPDATE albums SET cover_path=?；HasCover=true
     ErrNoCover → 跳过封面
     其它 error → 记录但不致命（元数据已入库，仍判 done）
5. UPDATE albums scrape_status='done'，返回 {Status:"done", MBID, HasCover}
```

**错误语义：**
- `ErrAlbumNotFound` → HTTP 404
- MB `ErrNotFound` → `{Status:"failed"}`（HTTP 404「未匹配到专辑」）
- MB 网络/解码异常 → wrapped error（HTTP 502；扫描阶段只计数）
- CAA 异常（非 404）→ 不影响元数据入库与 done 判定

---

## 数据库迁移

`internal/db/migrations/003_albums_scrape_status.up.sql`：
```sql
ALTER TABLE albums ADD COLUMN scrape_status TEXT DEFAULT 'pending';
```
- 现有专辑迁移后变 `pending` → 下次扫描自动回填刮削
- `internal/db/schema.sql` 同步加该列
- `go test ./internal/db/...` 验证迁移可执行

---

## 扫描器集成

### ingester.go

`findOrCreateAlbum` 新建专辑的 INSERT 不显式写 `scrape_status`，由列默认值 `'pending'` 兜底（与迁移一致）。确认 INSERT 语句不覆盖该列即可。

### scanner.go

- `Scanner` 新增 `metadataService *metadata.MetadataService`（可为 nil）。`NewScanner` 增参注入（main.go 传入）。
- `ScanStatus` 复用现有 `Phase`，新增 `AlbumsScraped int64 json:"albums_scraped"`（与 `LyricsScraped` 对称）。
- `doScan`：在歌词刮削阶段之后，若 `scrapeEnabled && metadataService != nil`：
  ```
  phase = "metadata"
  查询 SELECT id FROM albums WHERE scrape_status='pending'
  收集 ids，关闭 rows，检查 rows.Err()
  串行遍历每个 id：
      select { case <-ctx.Done(): return; default: }
      outcome, err := metadataService.EnrichAlbum(ctx, id)
      err != nil || outcome.Status=="failed" → errors++（不中断）
      outcome.Status=="done" → albumsScraped++
      间隔（MB 限速 1 req/s）：select { case <-time.After(1100ms): case <-ctx.Done(): return }
  phase = "idle"（runScan 退出统一复位）
  ```
- 间隔固定 1100ms（略超 1s，留余量），无论命中与否都等待（每次 EnrichAlbum 至少发一次 MB 请求）。

### 关键约束（与歌词刮削一致）

- 仍在 `running=true` 内；`ctx`/`Stop()` 可立即中断
- 失败/异常只累加 errors，绝不影响已入库数据
- 受 `scraper.enabled` 控

---

## 封面服务（cover.go）

现有优先级：内嵌 → 同目录 `cover.jpg/folder.jpg`。**追加第三级兜底**：

```
内嵌封面 → 同目录封面文件 → albums.cover_path（刮削缓存文件）→ 404
```

实现：在现有两级都未命中后，`SELECT cover_path FROM albums WHERE id=?`，非空则读该文件返回（mime 由扩展名判定）。读失败 → 404。

---

## 配置

- `cfg.Scraper.Enabled` —— 控制扫描器元数据阶段是否运行（与歌词共用同一开关）
- `cfg.Scraper.MusicBrainz.UserAgent` —— MB 请求 User-Agent（必填，已存在）
- `cfg.Cache.ArtworkDir` —— 封面缓存目录（默认 `./data/artwork`，已存在）

---

## 按需接口

`POST /api/v1/albums/{id}/scrape`（`album_scrape.go`，薄壳，仿 lyrics 的 ScrapeHandler）：
```
outcome, err := metadataService.EnrichAlbum(r.Context(), albumID)
switch {
  err is ErrAlbumNotFound      → 404
  err != nil（MB 异常）         → 502「元数据刮削失败」
  outcome.Status == "failed"   → 404「未匹配到专辑」
  done                         → 200 {album_id, status, mbid, has_cover}
}
```

---

## 测试策略

| 测试 | 方式 |
|------|------|
| pickRelease：score 过滤 + 曲目数最接近 | 纯函数表驱动（多 release、并列、localTrackCount=0） |
| pickRelease：全部 score<90 → 不命中 | 表驱动 |
| MB Search 解析 + 择优 | httptest 灌伪造 MB JSON，验证选中正确 release |
| MB Search 无匹配 → ErrNotFound | httptest |
| MB 网络异常 → 普通 error | httptest 500 |
| CAA FetchFront：200 返回图 | httptest（含 307→200 跳转链） |
| CAA FetchFront：404 → ErrNoCover | httptest |
| EnrichAlbum：命中 → 填 mbid/date + 下封面 + cover_path + done | 内存 sqlite + httptest MB/CAA |
| EnrichAlbum：MB 无匹配 → failed | 内存 sqlite + httptest |
| EnrichAlbum：CAA 404 → 元数据入库、无 cover_path、仍 done | 内存 sqlite + httptest |
| EnrichAlbum：专辑不存在 → ErrAlbumNotFound | 内存 sqlite |
| 扫描器元数据阶段：遍历 pending、ctx 中断、计数 | 内存 sqlite + mock service |
| cover.go：cover_path 兜底命中 | httptest 路由 + 临时封面文件 |
| 迁移：albums 加列可执行 | go test ./internal/db/... |
| HTTP album_scrape：200/404/502 各分支 | httptest |

**所有测试用 httptest 桩，不打真实网络。**

---

## 不在本次范围内

- AcoustID 指纹识别（v0.3）
- 艺术家简介/头像
- Last.fm / Spotify 封面源
- 封面优先级可配置切换（默认内嵌优先，PRD 顺序）
- 前端专辑刮削按钮（后端接口先就位）
