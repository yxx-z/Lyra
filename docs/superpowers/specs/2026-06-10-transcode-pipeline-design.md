# 转码管线重构设计文档

> 版本：1.0 · 日期：2026-06-10 · 状态：已批准

---

## 目标

重构当前转码实现，解决四类痛点：

1. **尊重客户端请求**：实现 Subsonic 的 `maxBitRate` / `format`（含 `raw`）参数；智能直传，FLAC/AAC 等不再被无谓转成 mp3。
2. **边转边播（低延迟）**：ffmpeg 输出直接管道给响应，首字节立即到达，不再等整首转完；客户端断开即终止 ffmpeg。
3. **缓存治理**：转码缓存加容量上限 + LRU 回收，避免无限膨胀。
4. **多输出编码**：支持 mp3 / opus / aac 三种输出，不止 mp3。

对应 PRD：流媒体播放体验、Subsonic 兼容（stream 端点）。

---

## 现状与不足

入口 `internal/api/v1/stream.go` 的 `StreamHandler.StreamByID`：

- 直传白名单仅 `{mp3, ogg, opus, wav}`；FLAC/m4a/aac 一律转 mp3（音质损失 + 无谓 CPU）。
- 转码固定输出 mp3、固定 `default_bitrate`，**完全忽略** Subsonic 的 `maxBitRate`/`format`。
- 先把整首转成磁盘文件（`cmd.Run()` 跑完）再 `ServeFile` → 首播 TTFB 高。
- 转码用 `context.Background()`，客户端断开后 ffmpeg 仍跑完。
- 转码缓存只增不删，无容量上限。
- `config.transcode.ffprobe_path` 定义了但未使用。
- router 为 v1 与 subsonic **各构造一个 `StreamHandler`** → 两套独立缓存/锁，同曲并发会各转一次、回收互相干扰。

---

## 架构

新建独立包 `internal/transcode/`（仓库已预留空目录），把转码逻辑从 `v1/stream.go` 抽出：

```
internal/transcode/
├── decide.go     纯函数 Plan(source, params) → Decision；无副作用、可单测
├── codec.go      编码注册表：mp3/opus/aac → {ffmpeg 编码参数, 容器, Content-Type, 扩展名}
├── cache.go      磁盘缓存：key/path + per-key 锁 + LRU 容量回收
├── service.go    Service.Serve(w, r, source)：编排 直传 / 命中缓存 / 管道转码+写缓存 / seek 回退
└── *_test.go
internal/api/v1/stream.go   瘦身：查库拿 {path, format, bitrate} → 解析请求参数 → 调 transcode.Service
internal/api/router.go      构造单个 transcode.Service，注入 v1 与 subsonic（共享缓存/锁）
```

**StreamHandler（v1）保留为薄封装**：负责按 trackID 查库、解析 HTTP 参数、把 `Source` 与 `Params` 交给 `transcode.Service`。Subsonic 的 `stream` 端点同样调用它（`StreamByID` 从 `r` 读 `format`/`maxBitRate`）。

---

## 组件设计

### Source 与 Params

```go
// Source 来自 tracks 表的一行
type Source struct {
    Path    string // file_path
    Format  string // 小写，如 flac / mp3；为空时由文件扩展名兜底
    Bitrate int    // kbps，可能为 0（未知）
}

// Params 来自客户端请求
type Params struct {
    Format     string // raw|mp3|opus|aac|""（未指定）
    MaxBitRate int    // kbps，0 = 未指定
}
```

### 决策逻辑（decide.go，纯函数）

无损源格式集合：`flac, wav, alac, ape`（其 bitrate 视为无上限，不参与"已在预算内"判断、不参与封顶）。

```
Plan(src, p):
1. p.Format == "raw"                         → 直传
2. p.Format 为空:
     p.MaxBitRate == 0                        → 直传
     src 有损 且 src.Bitrate>0 且 src.Bitrate ≤ p.MaxBitRate → 直传
     否则                                      → 转 mp3，码率 = chooseBitrate
3. p.Format ∈ {mp3,opus,aac}:
     p.Format == src.Format 且 (p.MaxBitRate==0 或 (src 有损 且 src.Bitrate>0 且 src.Bitrate ≤ p.MaxBitRate)) → 直传
     否则                                      → 转 p.Format，码率 = chooseBitrate
4. p.Format 为其它未知值                       → 视同 "mp3" 走第 3 条

chooseBitrate(p, src, defaultBitrate):
    base = p.MaxBitRate>0 ? p.MaxBitRate : defaultBitrate
    若 src 有损 且 src.Bitrate>0: base = min(base, src.Bitrate)   // 绝不升码率
    return base
```

`Decision` 结构：

```go
type Decision struct {
    Passthrough bool   // true → 直传原文件
    Codec       string // 转码时：mp3|opus|aac
    Bitrate     int    // 转码时：目标 kbps
    ContentType string // 直传时按 src.Format 推；转码时按 Codec 推
    Ext         string // 转码缓存文件扩展名
}
```

**要点**：默认直传；只在客户端明确要求（指定编码，或 maxBitRate 低于源码率）时转码；绝不升码率；判断只用 DB 的 format/bitrate，**不跑 ffprobe**。

### 编码注册表（codec.go）

| format | ffmpeg 编码器 | `-f` 容器 | Content-Type | 扩展名 |
|--------|--------------|-----------|--------------|--------|
| mp3（默认/回退） | libmp3lame | mp3 | audio/mpeg | .mp3 |
| opus | libopus | ogg | audio/ogg | .opus |
| aac | aac（原生） | adts | audio/aac | .aac |

直传时 Content-Type 由 `src.Format` 推（沿用现有 `audioContentTypes` 思路并补 flac→audio/flac、m4a/aac→audio/mp4 等）；找不到映射时回退 `application/octet-stream`，仍可播放/下载。

### 缓存（cache.go）

- key = `(trackID, codec, bitrate)`；路径 `{dir}/{safeID}_{codec}_{bitrate}k.{ext}`（`safeID = filepath.Base(trackID)` 防越界，沿用现有做法）。
- per-key `sync.Mutex`（沿用现有 `inflight` map）：保证同 key 只有一个转码写缓存。
- **容量回收**：新增配置 `cache.transcode_max_size_mb`（默认 2048，0 = 不限）。每次成功提升一个新缓存文件后做一次清扫：统计目录总大小，若超上限则按文件 mtime 升序（最旧优先）删除，删到 ≤ 上限；跳过刚写入的文件。
- **LRU 近似**：命中缓存时 `os.Chtimes` 把文件 mtime 刷成当前时间，使 mtime ≈ 最近访问时间，回收时据此淘汰。

### 编排（service.go）

`Service.Serve(w, r, src)`：

1. 解析 `Params`（见下"参数解析"）。
2. `dec := Plan(src, params)`。
3. `dec.Passthrough` → 设 Content-Type、`http.ServeFile(w, r, src.Path)`（自带 Range/拖动），返回。
4. 否则计算 cachePath / key：
   - **命中缓存**（`os.Stat` 成功）→ 刷 mtime、`http.ServeFile`，返回。
   - **未命中且为带非零偏移的 Range 请求**（缓存生成前拖动）→ 回退：阻塞式转成完整缓存文件（持 key 锁，转完 rename），再 `ServeFile`。保证可 seek。
   - **未命中且从头播**：
     - 抢 key 锁成功 → 起 ffmpeg，stdout 经 `io.MultiWriter` 同时写「HTTP 响应」与「临时缓存文件 `dst.tmp`」；正常结束 → `rename` 提升为正式缓存 → 触发回收；失败/取消 → 删 `dst.tmp`。
     - 抢锁失败（同 key 正在转）→ **纯管道转码**（ffmpeg → 响应，不写缓存），避免第二个客户端等整首。
5. ffmpeg 一律 `exec.CommandContext(r.Context(), …)`：客户端断开 → 进程被杀。
6. 管道转码不预知长度 → 不设 Content-Length（chunked）；首播期间不可 seek，转完入缓存后可。

### 参数解析

- Subsonic `stream`：`maxBitRate`（kbps 整数）、`format`（含 `raw`）。中间件已 `ParseForm`，从 `r.Form` 读。
- v1 web 端点 `/api/v1/tracks/:id/stream`：当前不带参数 → `Params{}` → 走默认（直传）。**行为变化**：FLAC 等将直传原文件给浏览器（现代浏览器可播 FLAC/AAC），不再转 mp3。后续如需可加 `?format=`/`?maxBitRate=`，本期不做。

---

## 错误处理

- 转码在**首字节前**失败 → 返回 HTTP 500（v1 用现有 `writeJSONError`；Subsonic stream 为二进制端点，HTTP 错误码即可，客户端可容忍）。
- 已发出字节后 ffmpeg 失败 → 无法改状态码，停止并 `slog` 记录。
- 客户端断开（`r.Context()` 取消）→ ffmpeg 被杀、删临时文件，不视为错误。
- `src.Format` 为空 → 用文件扩展名兜底推断；仍为空时直传走 `application/octet-stream`。

---

## 测试策略

| 测试 | 方式 |
|------|------|
| 决策逻辑 Plan：直传/转码/封顶/raw/各格式组合/无损源/未知 format 回退 | 表驱动纯单测，不碰 ffmpeg（正确性主战场） |
| 编码注册表：format → 参数/Content-Type/扩展名；未知回退 mp3 | 表驱动 |
| 缓存回收：造假文件，验 LRU 删除顺序、容量上限、跳过新文件；命中刷 mtime | 临时目录 |
| 管道转码：边出字节边写临时文件 → 成功 rename 提升缓存 | 假 ffmpeg 脚本（testdata，按参数吐确定字节、可 sleep）注入 FFmpegPath |
| 客户端断开 → 进程被杀、删临时文件 | 假 ffmpeg + 取消 context |
| 命中缓存 → ServeFile（带 Range） | httptest |
| 直传路径：format=raw / 默认 | httptest，无需 ffmpeg |
| 并发同 key：第二请求纯管道不写缓存 | 假 ffmpeg + 并发 |

**全部不依赖真 ffmpeg、不打网络**（管道相关用 testdata 假 ffmpeg 脚本；本项目面向 Linux/Docker，脚本可用）。

---

## 不在本次范围内

- 自适应码率 / HLS 分片
- 每客户端的转码偏好持久化（Subsonic 多用户的 per-player 设置）
- v1 web 端点的 `format`/`maxBitRate` 查询参数（结构已留好，按需再加）
- 转码缓存预热
- ffprobe 探测（本期用 DB 的 format/bitrate；如未来发现 DB 元数据不可靠再引入）
