# 播放体验打磨设计文档

> 版本：1.0 · 日期：2026-06-01 · 状态：已批准

---

## 目标

打磨 v0.1 已上线的播放功能，解决四个问题，统一围绕 ffmpeg/ffprobe 工具链：

1. **曲目时长不显示** —— 库中所有 `duration` 都是 0，因为扫描从未提取时长
2. **转码流无法 seek** —— 转码分支用管道输出，不支持 HTTP Range
3. **转码重复执行** —— 每次请求都重新跑 ffmpeg，无缓存
4. **播放失败/转码中无提示** —— 前端只 console.log，UI 无反馈

关键设计洞察：**转码磁盘缓存同时解决问题 2 和 3**（缓存文件用 `http.ServeFile` 提供，自带 Range 支持）。

---

## 改动总览

```
1. ffprobe 探测器（新文件）    → internal/scanner/probe.go
2. 扫描集成 + 旧数据补齐（改） → tag_reader.go、scanner.go、ingester.go
3. 转码磁盘缓存（新文件+改）   → internal/api/v1/transcode_cache.go、stream.go
4. 前端状态提示（改）         → stores/player.ts、PlayerBar.vue
配置变更                      → config.go（ffprobe_path、transcode_dir）
```

---

## 第一部分：ffprobe 探测器

### 新文件 `internal/scanner/probe.go`

```go
type AudioProps struct {
    Duration   int // 秒
    Bitrate    int // kbps
    SampleRate int // Hz
    Channels   int
}

// Probe 调用 ffprobe 提取音频属性。
// ffprobe 不可用或失败时返回零值 AudioProps + error（调用方降级为 0，不阻断扫描）。
func Probe(ffprobePath, filePath string) (AudioProps, error)
```

实现：
```
ffprobe -v error -print_format json \
  -show_entries format=duration,bit_rate:stream=sample_rate,channels \
  <file>
```
解析返回的 JSON：
- `format.duration` 是浮点秒字符串 → 转 int（截断）
- `format.bit_rate` 是 bps 字符串 → /1000 得 kbps
- `streams[0].sample_rate`、`streams[0].channels` → 取第一个音频流

### 配置变更

`TranscodeConfig` 新增 `FfprobePath`：

```go
type TranscodeConfig struct {
    FFmpegPath     string `yaml:"ffmpeg_path"`
    FfprobePath    string `yaml:"ffprobe_path"`    // 新增
    DefaultFormat  string `yaml:"default_format"`
    DefaultBitrate int    `yaml:"default_bitrate"`
}
```

`Default()` 中设 `FfprobePath: "ffprobe"`，`config.example.yaml` 同步加 `ffprobe_path: ffprobe`。

### 关键设计点

- ffprobe 不可用时降级填 0，**不阻断扫描**，与当前行为一致
- `Probe` 是独立纯函数，可单测（mock ffprobe 或用真实 testdata）

---

## 第二部分：扫描集成 + 旧数据补齐

### tag_reader.go

`Read` 函数签名增加 `ffprobePath` 参数：

```go
func Read(path string, libraryPaths []string, ffprobePath string) (TrackMeta, error) {
    // ... 现有标签读取 + 路径推断 + 默认值 ...

    // 新增：ffprobe 提取音频属性（失败保持 0，不报错）
    if props, err := Probe(ffprobePath, path); err == nil {
        meta.Duration = props.Duration
        meta.Bitrate = props.Bitrate
        meta.SampleRate = props.SampleRate
        meta.Channels = props.Channels
    }
    return meta, nil
}
```

### scanner.go

调用 `Read` 处把 `ffprobePath` 从 config 传入。Scanner 需要持有 ffprobe 路径——扩展 `NewScanner` 或让 Scanner 持有 `TranscodeConfig`。

实现选择：`Scanner` 新增字段 `ffprobePath string`，由 `NewScanner` 的调用方（main.go）从 `cfg.Transcode.FfprobePath` 传入。这样 scanner 不直接依赖整个 config。

### ingester.go —— 旧数据补齐（无需改动）

已确认 `upsertTrack` 的 `ON CONFLICT(file_path) DO UPDATE` 子句**已经**包含 `duration=excluded.duration`、`bitrate`、`sample_rate`、`channels` 的更新（ingester.go:121-126）。

因此全量扫描重新遍历所有文件、重新 `Read`（这次带 ffprobe）后，存量 duration=0 的记录会被新值覆盖。**ingester 无需任何改动**，存量数据在下次扫描（启动自动扫描或手动触发）时自动补齐。

---

## 第三部分：转码磁盘缓存

### 配置变更

`CacheConfig` 新增 `TranscodeDir`：

```go
type CacheConfig struct {
    ArtworkDir       string `yaml:"artwork_dir"`
    ArtworkMaxSizeMB int    `yaml:"artwork_max_size_mb"`
    TranscodeDir     string `yaml:"transcode_dir"`   // 新增
}
```

`Default()` 设 `TranscodeDir: "./data/transcode"`，config.example.yaml 同步。

### 新文件 `internal/api/v1/transcode_cache.go`

```go
type TranscodeCache struct {
    dir      string
    inflight sync.Map // 缓存键 → *sync.Mutex，防止并发转码同一曲目
}

func NewTranscodeCache(dir string) *TranscodeCache

// Path 返回给定 trackID + 格式 + 码率的缓存文件路径。
func (c *TranscodeCache) Path(trackID, format string, bitrate int) string

// lockFor 返回该缓存键的互斥锁（懒创建）。
func (c *TranscodeCache) lockFor(key string) *sync.Mutex
```

缓存键格式：`<trackID>_<format>_<bitrate>k.<format>`，例如 `abc123_mp3_192k.mp3`。

### stream.go 转码分支改造

```
请求不兼容格式
    │
    ▼
计算缓存路径 cachePath
    │
    ├─ os.Stat(cachePath) 成功 → http.ServeFile(cachePath)   ← Range 自动支持
    │
    └─ 不存在：
         lock := cache.lockFor(key); lock.Lock()
         double-check：再次 Stat（可能其他请求已转好）
         若仍不存在：
             ffmpeg 转码 → 写 cachePath + ".tmp"
             成功 → os.Rename(.tmp, cachePath)   ← 原子写入
             失败 → 删除 .tmp，返回 500
         lock.Unlock()
         http.ServeFile(cachePath)
```

### 关键设计点

1. **原子写入**：先写 `.tmp` 再 `os.Rename`，进程被杀不留半文件
2. **并发保护**：`inflight sync.Map` 按缓存键加锁，避免两个 ffmpeg 同时写同一文件；第二个请求等锁释放后直接命中缓存
3. **首次延迟**：方案 A 的代价，首次播放等完整转码（几秒）。前端用 loading 提示掩盖（见第四部分）
4. **目标格式固定 mp3 / 192k**（沿用现有 `transcode.default_*`），v0.1 不支持客户端指定码率
5. **不做缓存清理**（YAGNI）：`transcode_dir` 会持续增长，LRU/容量限制留待以后。本设计文档"已知限制"中标注

### ffmpeg 命令

沿用现有实现，但输出到文件而非 pipe：
```
ffmpeg -hide_banner -loglevel error -i <input> -vn \
  -codec:a libmp3lame -b:a 192k -f mp3 <cachePath>.tmp
```
用 `exec.CommandContext(r.Context(), ...)`，客户端断开则取消转码（清理 .tmp）。

---

## 第四部分：前端播放状态提示

### player store (`stores/player.ts`)

新增两个状态：

```ts
const playbackError = ref<string | null>(null)
const isLoading = ref(false)
```

绑定 HTML5 Audio 原生事件：

```ts
audio.addEventListener('playing', () => { isLoading.value = false })
audio.addEventListener('waiting', () => { isLoading.value = true })
audio.addEventListener('error', () => {
  isLoading.value = false
  isPlaying.value = false
  playbackError.value = currentTrack.value
    ? `无法播放《${currentTrack.value.title}》`
    : '播放失败'
})
```

`playTrack` / `playAtIndex` 开头设 `isLoading.value = true; playbackError.value = null`。

导出 `playbackError`、`isLoading`、`clearError()`。

### UI 展示 (`PlayerBar.vue`)

- `isLoading=true` → 播放按钮位置显示旋转图标 + "加载中…"（首次转码时即"转码中"体感）
- `playbackError` → toast 横幅，带歌名，约 4 秒自动消失，复用现有玻璃拟态样式

### 关键设计点

- store 只暴露状态，UI 决定展示，关注点分离
- `waiting` 事件天然覆盖"等 ffmpeg 转码"，无需后端配合
- loading 与 error 互斥，error 出现时清 loading

---

## 测试策略

| 测试 | 方式 |
|------|------|
| Probe 解析 ffprobe JSON | 单测，喂固定 JSON 字符串给解析函数（解析逻辑与 exec 分离）|
| Probe ffprobe 不可用 | 单测，传不存在的 ffprobe 路径，断言返回 error + 零值 |
| Read 填充 duration | 集成测试（需 ffprobe 环境）或 mock |
| upsert 更新 duration | 内存 SQLite，两次 Ingest 同文件不同 duration，断言取新值 |
| 转码缓存命中 | 临时目录，预放缓存文件，断言走 ServeFile 不调 ffmpeg |
| 转码缓存原子写入 | 单测，断言 .tmp → rename 逻辑 |
| 缓存键格式 | 单测 `Path()` 输出 |

---

## 已知限制（v0.1 范围外）

- **转码缓存无自动清理**：`transcode_dir` 持续增长，LRU/容量上限留待后续
- **转码码率固定**：不支持客户端指定（`?bitrate=` 参数留待 v0.4）
- **首次播放延迟**：不兼容格式首次播放需等完整转码，前端 loading 提示缓解
- **ffprobe 必需才能显示时长**：未安装则 duration 仍为 0（降级，不报错）
