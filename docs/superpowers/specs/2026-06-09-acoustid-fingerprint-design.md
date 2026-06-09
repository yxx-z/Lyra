# AcoustID 指纹识别设计文档

> 版本：1.0 · 日期：2026-06-09 · 状态：已批准

---

## 目标

为曲目计算音频指纹（chromaprint/fpcalc）并经 AcoustID 识别，存储权威标识 `tracks.acoustid` + `tracks.mbid`（recording MBID），用于精确识别（标签缺失/错误时的定位基础）。

对应 PRD：US-07（AcoustID 指纹识别），v0.3。

**可行性已验证**：用真实 AcoustID key + 容器内 fpcalc 1.5.1 对《晴天》识别成功（score 0.97，recording MBID + 正确「晴天 / 周杰倫」）。AcoustID 众包库覆盖了本库的华语曲目。

---

## 范围

**本次做：**
- 新包 `internal/acoustid`：fpcalc 运行器 + AcoustID 查询客户端 + FingerprintService 编排
- 配置 `acoustid.fpcalc_path`
- 扫描器曲目级指纹阶段（解耦、串行、节流、可中断、双门控）
- `NewScanner` 重构为接收 `ScrapeServices` 结构体（收纳 lyrics/metadata/fingerprint 三服务）

**本次不做（YAGNI / 后续独立 spec）：**
- 用 AcoustID 结果自动纠正标题/艺术家
- 联动专辑元数据（recording→release 精配）
- 前端按钮 / 按需接口
- 不改动现有 album/lyrics 刮削逻辑

---

## 关键前提（运行环境）

- **fpcalc 仅在 docker 镜像内可用**（`apk add chromaprint`，已确认 fpcalc 1.5.1）；开发机 `make dev-backend` 无 fpcalc。路径用 `acoustid.fpcalc_path`（默认 `fpcalc`）配置。
- **AcoustID 查询需 API key**（用户在 acoustid.org 注册）。未配 key → 指纹阶段整体跳过（零产出，不报错）。
- 因 fpcalc + key 是运行期依赖，**单元测试全部用 fake Fingerprinter + httptest 桩**，不依赖真二进制/真网络。真实端到端验证靠 docker + 用户 key（已先行验证可行）。

---

## 架构

### 新包 `internal/acoustid`

```
internal/acoustid/
├── fpcalc.go      新建：Fingerprinter 接口 + ExecFingerprinter（跑 fpcalc -json）
├── client.go      新建：AcoustIDClient.Lookup + pickResult（纯函数择优）+ 错误类型
└── service.go     新建：FingerprintService.IdentifyTrack + IdentifyOutcome + ErrTrackNotFound
```

### Fingerprinter 接口（fpcalc.go）

```go
// Fingerprinter 计算音频指纹。
type Fingerprinter interface {
    Calc(ctx context.Context, filePath string) (durationSec int, fingerprint string, err error)
}

// ExecFingerprinter 调用 fpcalc 二进制。
type ExecFingerprinter struct{ fpcalcPath string }
func NewExecFingerprinter(fpcalcPath string) *ExecFingerprinter
func (f *ExecFingerprinter) Calc(ctx, filePath) (int, string, error)
```

- `fpcalcPath` 为空时默认 `fpcalc`。
- 实现：`exec.CommandContext(ctx, fpcalcPath, "-json", filePath)`，设 `cmd.WaitDelay = 5*time.Second`（仿 `internal/scanner/probe.go`），`cmd.Output()` 取 stdout。
- 解析 JSON `{"duration": float, "fingerprint": string}`；duration 取整（int(d)）。
- 任一步失败 → 返回 error。

### AcoustIDClient（client.go）

```go
var ErrNoMatch = errors.New("指纹未匹配")

type acoustResult struct { /* score + recordings[].{id,title} 解析用 */ }
type IdentifyResult struct {
    AcoustID string  // results[0].id（AcoustID UUID）
    MBID     string  // recordings[0].id（recording MBID，可能空）
    Score    float64
}

type AcoustIDClient struct { baseURL, apiKey string; httpClient *http.Client }
func NewAcoustIDClient(baseURL, apiKey string, httpClient *http.Client) *AcoustIDClient
func (c *AcoustIDClient) Lookup(ctx context.Context, durationSec int, fingerprint string) (IdentifyResult, error)
```

- baseURL 默认 `https://api.acoustid.org`；endpoint `/v2/lookup`。
- **POST 表单**（指纹很长，避免 URL 限制）：`client=apiKey`、`duration`、`fingerprint`、`meta=recordings`。
- 解析响应 `{status, results:[{id, score, recordings:[{id, title}]}]}`。
- `status != "ok"` 或非 2xx → 普通 error。
- 调 `pickResult` 择优；无满足 → `ErrNoMatch`。

### pickResult（纯函数，可测）

```go
func pickResult(results []acoustResult) (IdentifyResult, bool)
```
- 取 `results[0]`（AcoustID 已按 score 降序）；`score < 0.9` → 不命中（false）。
- 命中：`AcoustID = results[0].id`，`MBID = results[0].recordings[0].id`（无 recordings 则空），`Score`。
- results 空 → false。

### FingerprintService（service.go）

```go
var ErrTrackNotFound = errors.New("曲目不存在")

type IdentifyOutcome struct {
    Status   string // "identified" | "nomatch"
    AcoustID string
    MBID     string
}

type FingerprintService struct {
    db *sql.DB
    fp Fingerprinter
    client *AcoustIDClient
}
func NewFingerprintService(db, fp Fingerprinter, client *AcoustIDClient) *FingerprintService
func (s *FingerprintService) IdentifyTrack(ctx context.Context, trackID string) (IdentifyOutcome, error)
```

**IdentifyTrack 流程：**
```
1. 载 track：file_path。不存在 → ErrTrackNotFound
2. fp.Calc(ctx, file_path) → (duration, fingerprint)。失败 → 返回 error（瞬时，acoustid 留 NULL）
3. client.Lookup(ctx, duration, fingerprint):
     ErrNoMatch → UPDATE tracks SET acoustid='' WHERE id=?（标记已尝试-无匹配）；返回 {Status:"nomatch"}
     其它 error → 透传（瞬时，留 NULL）
4. 命中：
     UPDATE tracks SET acoustid=? WHERE id=?（AcoustID UUID）
     若 MBID 非空：UPDATE tracks SET mbid=? WHERE id=?（best-effort，UNIQUE 冲突仅 warn 不致命）
     返回 {Status:"identified", AcoustID, MBID}
```

**状态语义：** `acoustid IS NULL` = 未尝试；`''` = 尝试过无匹配；非空 = AcoustID id。瞬时错误（fpcalc/网络）保持 NULL，下次扫描重试。

**错误映射：** `ErrTrackNotFound` 供调用方；其余 error 由扫描阶段计数、不中断。

---

## 配置

`internal/config/config.go` 的 `AcoustIDConfig` 加字段：
```go
type AcoustIDConfig struct {
    APIKey     string `yaml:"api_key"`
    FpcalcPath string `yaml:"fpcalc_path"`
}
```
`Default()` 中设 `FpcalcPath: "fpcalc"`（docker 镜像内 fpcalc 在 PATH，故默认名即可）。

---

## 扫描器集成

### NewScanner 重构为 ScrapeServices

把三个刮削 service 收进一个结构体，避免参数膨胀（上轮代码审查提过）：
```go
// ScrapeServices 聚合后台刮削/识别所需的服务，任一可为 nil。
type ScrapeServices struct {
    Lyrics      *lyrics.LyricsService
    Metadata    *metadata.MetadataService
    Fingerprint *acoustid.FingerprintService
}

func NewScanner(db *sql.DB, cfg config.LibraryConfig, ffprobePath string, services ScrapeServices, scrapeEnabled bool) *Scanner
```
- Scanner 内部存 `services ScrapeServices`（替换原 `lyricsService`/`metadataService` 字段）。各处 `s.lyricsService` → `s.services.Lyrics`，`s.metadataService` → `s.services.Metadata`。
- 6 个调用点更新：测试传 `scanner.ScrapeServices{}`（零值，全 nil）；`main.go`/`router.go` 传填充结构体。

### ScanStatus 新增

```go
Fingerprinted int64 `json:"fingerprinted"`
```
`Status()` 返回填充；`doScan` 重置区 `s.fingerprinted.Store(0)`；新增 `fingerprinted atomic.Int64`。

### doScan 追加指纹阶段

在元数据阶段之后、最终 `phase="idle"` 之前：
```go
if s.scrapeEnabled && s.services.Fingerprint != nil {
    s.phase.Store("fingerprint")
    s.fingerprintPending(ctx)
}
```

### fingerprintPending（仿 scrapeAlbumsPending）

```
查询 SELECT id FROM tracks WHERE acoustid IS NULL AND is_available=1
收集 ids，关闭 rows，检查 rows.Err()
空则返回；start/end slog
逐 id：
    select { <-ctx.Done(): return; default }
    outcome, err := services.Fingerprint.IdentifyTrack(ctx, id)
    err != nil → errors++（不中断，留 NULL 下次重试）
    outcome.Status=="identified" → fingerprinted++
    （nomatch 不计入 fingerprinted，也不计 errors）
    节流：select { <-time.After(350ms): case <-ctx.Done(): return }
```

### 门控

指纹阶段仅当 `scrapeEnabled && services.Fingerprint != nil`。`services.Fingerprint` 的构造由 main.go/router.go 决定——**仅当 `cfg.Scraper.AcoustID.APIKey != ""` 时才构造**，否则为 nil（阶段跳过）。

---

## 接线（router.go + main.go）

两处一致地构造（仅 key 非空时）：
```go
var fpSvc *acoustid.FingerprintService
if cfg.Scraper.AcoustID.APIKey != "" {
    fpSvc = acoustid.NewFingerprintService(
        db,
        acoustid.NewExecFingerprinter(cfg.Scraper.AcoustID.FpcalcPath),
        acoustid.NewAcoustIDClient("https://api.acoustid.org", cfg.Scraper.AcoustID.APIKey, nil),
    )
}
// NewScanner(..., scanner.ScrapeServices{Lyrics: lyricsService, Metadata: metadataService, Fingerprint: fpSvc}, cfg.Scraper.Enabled)
```
router.go 仅在 scanner 构造处需要（router 不直接用 fingerprint；scanner 持有）。注意 router.go 当前未持有 scanner 的构造——scanner 在 main.go 构造后注入 router。故 **fingerprintService 的构造只在 main.go**；router.go 不涉及（无按需接口）。

> 说明：与 lyrics/metadata 不同，本轮无 HTTP 按需接口，故 router.go 无需改动；只改 main.go 的 NewScanner 调用 + 各测试调用点签名。

---

## 测试策略

| 测试 | 方式 |
|------|------|
| pickResult：score 0.97 命中、取首 recording | 表驱动 |
| pickResult：score<0.9 → 不命中 | 表驱动 |
| pickResult：无 results → 不命中；命中但无 recordings → 只 AcoustID、MBID 空 | 表驱动 |
| AcoustIDClient.Lookup：httptest 灌伪造 JSON（命中/无匹配/status≠ok/500） | httptest |
| FingerprintService.IdentifyTrack：fake Fingerprinter + httptest + 内存 sqlite → 验 acoustid/mbid/状态 | fake + httptest + db.Open |
| IdentifyTrack：nomatch → acoustid='' | 同上 |
| IdentifyTrack：fpcalc 错误 → 返回 error、acoustid 仍 NULL | fake 返回 error |
| IdentifyTrack：曲目不存在 → ErrTrackNotFound | 内存 sqlite |
| 扫描器指纹阶段：fake service，验遍历 NULL 曲目、ctx 中断、fingerprinted 计数 | 内存 sqlite + fake |
| ExecFingerprinter.Calc：有 fpcalc 时跑真实文件，无则 `t.Skip` | 条件跳过 |
| ScrapeServices 重构：6 调用点编译通过、既有 scanner 测试全绿 | go build/test |

**所有核心测试不依赖真 fpcalc/真网络。** 真实端到端：合并后在 docker 用用户 key 验证（设计前已先验证《晴天》0.97 可行）。

---

## 不在本次范围内

- 自动改写本地标题/艺术家
- recording→release 联动专辑元数据精配
- 前端展示/按需识别按钮
- AcoustID 提交（contributing fingerprints）回 AcoustID 库
