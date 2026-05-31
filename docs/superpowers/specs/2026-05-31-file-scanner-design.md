# 文件扫描器设计文档

> 版本：1.0 · 日期：2026-05-31 · 状态：已批准

---

## 目标

实现 Lyra 音乐库文件扫描器，支持：
- 启动时自动全量扫描
- HTTP API 手动触发
- fsnotify 实时文件系统监听
- 混合目录结构（有规范目录 + 单文件混合）
- 内嵌标签为主，目录结构辅助，文件名兜底

对应 PRD：US-01（自动扫描入库）、US-02（文件系统事件触发）、US-03（手动触发）、US-04（扫描进度）

---

## 架构

### 文件结构

```
internal/scanner/
├── scanner.go      Scanner 结构体，对外公共接口，协调各组件
├── walker.go       递归遍历目录，过滤音频文件，输出路径到 channel
├── tag_reader.go   读取单个文件的内嵌标签，返回规范化 TrackMeta 结构体
├── ingester.go     写入 DB（artist/album/track 去重 + upsert）
└── watcher.go      fsnotify 监听，文件变化时触发增量处理
```

### 公共接口（scanner.go）

```go
type Scanner struct {
    db     *sql.DB
    cfg    config.LibraryConfig
    status ScanStatus       // 原子读写
    mu     sync.RWMutex
    stopCh chan struct{}
    wg     sync.WaitGroup
}

type ScanStatus struct {
    Running   bool      `json:"running"`
    Total     int64     `json:"total"`
    Processed int64     `json:"processed"`
    Errors    int64     `json:"errors"`
    StartedAt time.Time `json:"started_at"`
}

func NewScanner(db *sql.DB, cfg config.LibraryConfig) *Scanner
func (s *Scanner) Start() error        // 后台扫描 + 启动 fsnotify（若 cfg.Watch=true）
func (s *Scanner) TriggerScan() error  // 手动触发全量扫描，已在运行返回 ErrScanInProgress
func (s *Scanner) Status() ScanStatus  // 返回当前进度快照（值拷贝）
func (s *Scanner) Stop()               // 优雅关闭，等待 wg.Wait()
```

---

## 扫描流水线

```
Start() / TriggerScan()
    │
    ▼
walker.Walk(paths)               递归遍历，过滤非音频文件扩展名
    │  chan string（文件路径）
    ▼
4 个 worker goroutine            并发读取内嵌标签
    │  tag_reader.Read(path) → TrackMeta
    ▼
ingester goroutine（单个）        串行写 DB，避免 SQLite 写锁争用
    ├─ 查找或创建 artist
    ├─ 查找或创建 album
    └─ upsert track（file_path UNIQUE）
         scrape_status = 'pending'
```

**worker 数量固定为 4**，磁盘 IO 瓶颈下更多 worker 收益递减。

---

## 元数据提取优先级

对于每个字段，按以下顺序尝试，取第一个非空值：

| 优先级 | 来源 | 条件 |
|--------|------|------|
| 1 | 内嵌标签（ID3v2 / Vorbis Comment / iTunes Tag） | 始终尝试 |
| 2a | 目录推断（双层）：祖父目录名 → artist，父目录名 → album | 文件距库根 ≥ 2 层 |
| 2b | 目录推断（单层解析）：解析父目录名中的 `艺术家 - 专辑` 模式 | 文件距库根 = 1 层 |
| 3 | 文件名推断：去扩展名 → title，解析 `NN - 曲名` 格式 → track_number | 始终尝试 |
| 4 | 默认值 | title = 文件名，artist = "未知艺术家"，album = "未知专辑" |

### 单层目录解析规则（优先级 2b）

处理如下命名约定：

```
蔡琴 - 金片子 贰・魂萦旧梦 (2015) - WEB-DL - 16bit ALAC-HHWEB/
周杰伦 - 叶惠美 (2003)/
The Beatles - Abbey Road (1969) [FLAC]/
```

解析算法：
1. 以第一个 ` - `（空格-连字符-空格）为分隔符切割
2. 左侧部分 trim → artist
3. 右侧部分继续清洗 → album：
   - 去掉尾部年份：`\s*\(\d{4}\)` 
   - 去掉尾部格式信息：`\s*-\s*(WEB|FLAC|MP3|AAC|ALAC|DL|CDRip|320k).*`（大小写不敏感）
   - 去掉尾部方括号块：`\s*\[.*\]`
   - trim 剩余空白
4. 若切割后左侧为空或右侧为空，则整个目录名作为 album，artist 留空

**注意**：WEB-DL / ALAC 等来源的文件内嵌标签通常完整，2b 主要用于标签被抹掉的情况。

**去重规则**：查找 artist/album 时用 `strings.ToLower(strings.TrimSpace(name))` 规范化，存储保留原始大小写。

---

## TrackMeta 结构体

```go
// tag_reader.go
type TrackMeta struct {
    FilePath   string
    FileSize   int64
    Format     string    // "mp3" / "flac" / "m4a" 等
    Title      string
    Artist     string
    Album      string
    AlbumArtist string
    TrackNumber int
    DiscNumber  int
    Year        int
    Genre       string
    Duration    int       // 秒
    Bitrate     int       // kbps
    SampleRate  int       // Hz
    Channels    int
}
```

**支持的格式**：`.mp3` `.flac` `.m4a` `.ogg` `.opus` `.wav` `.aiff` `.wma`

**音频属性说明**：`dhowden/tag` 不提供 duration/bitrate/sample_rate/channels。v0.1 策略：
- 若系统有 `ffprobe`（已是可选依赖），用 `ffprobe -v error -show_entries format=duration,bit_rate -show_entries stream=sample_rate,channels` 获取
- `ffprobe` 不可用时这四个字段填 0，不影响播放，后续可补全
- `Duration` 字段单位为秒（整数截断）

---

## fsnotify 增量处理

```
文件系统事件
    ├─ Create / Write  → tag_reader.Read() → ingester.Ingest()
    ├─ Remove          → UPDATE tracks SET is_available=0 WHERE file_path=?
    └─ Rename          → 旧路径 is_available=0；新路径 upsert
```

事件去抖：同一路径 500ms 内的多次事件合并为一次处理（防止编辑器保存时的重复触发）。

---

## DB 变更

新增迁移 `002_tracks_availability.up.sql`：

```sql
ALTER TABLE tracks ADD COLUMN is_available INTEGER NOT NULL DEFAULT 1;
CREATE INDEX idx_tracks_available ON tracks(is_available);
```

`schema.sql` 同步更新。

---

## HTTP API

挂载到现有 Chi router 的 `/api/v1` 前缀下：

```
POST /api/v1/library/scan
    成功：200 {"ok": true}
    已在扫描：409 {"error": "扫描正在进行中"}

GET  /api/v1/library/scan/status
    200 {
        "running": true,
        "total": 12000,
        "processed": 3400,
        "errors": 2,
        "started_at": "2026-05-31T10:00:00Z"
    }
```

---

## main.go 变更

```
db.Open()
    ↓
scanner.NewScanner(db, cfg.Library)
    ↓
api.NewRouter(scanner)    // scanner 注入 router，router 注册 /api/v1 端点
    ↓
scanner.Start()           // 启动后台扫描 + fsnotify
    ↓
http.ListenAndServe()

// 优雅关闭时：
srv.Shutdown(ctx)
scanner.Stop()
```

---

## 测试策略

| 测试 | 方式 |
|------|------|
| tag_reader 读取各格式标签 | 用 `internal/scanner/testdata/` 下的小样本文件（≤ 10KB，提交到 git）|
| 目录推断逻辑（双层 + 单层解析） | 单元测试，覆盖：双层路径、`艺术家 - 专辑 (年份) - 格式`、无 ` - ` 的单层路径 |
| ingester 去重 | 内存 SQLite，重复 Ingest 同一文件验证不重复 |
| walker 过滤 | 单元测试，临时目录 + 混合文件 |
| 扫描进度原子性 | 并发调用 Status() 验证无 race |
| fsnotify 事件去抖 | 单元测试，模拟快速连续事件 |
| HTTP API | httptest，验证 409 和状态字段 |

---

## 不在本次范围内

- AcoustID 音频指纹（v0.3）
- MusicBrainz 刮削（v0.2）
- 封面提取（v0.2）
- 扫描进度 WebSocket 实时推送（v0.4）
- 用户手动修正元数据（v0.3）
