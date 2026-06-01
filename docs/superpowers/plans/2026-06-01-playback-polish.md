# 播放体验打磨实现计划

> **给 AI 工作者：** 必须使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务执行本计划。步骤使用复选框（`- [ ]`）语法追踪进度。

**目标：** 通过 ffprobe 提取曲目时长、转码结果磁盘缓存（自带 Range/seek 支持）、前端加载与错误提示，打磨 v0.1 播放体验。

**架构：** 新增 `scanner.Probe`（ffprobe 封装）在扫描时填充 duration/bitrate/sample_rate/channels；新增 `TranscodeCache` 把 ffmpeg 转码结果缓存到磁盘并用 `http.ServeFile` 提供（获得 Range 支持，避免重复转码）；前端 player store 用 HTML5 Audio 的 playing/waiting/error 事件驱动加载与错误状态。

**技术栈：** Go 1.25+、ffprobe/ffmpeg（外部进程）、modernc SQLite、Vue 3 + Pinia

---

## 前置条件

```bash
export PATH=$PATH:/home/yxx/go-local/go/bin
cd /home/yxx/develop/Lyra
which ffprobe ffmpeg   # 本机应已安装（转码功能依赖）
```

---

## 文件结构

```
internal/config/config.go              ← TranscodeConfig 加 FfprobePath，CacheConfig 加 TranscodeDir
config.example.yaml                    ← 同步

internal/scanner/probe.go              ← 新建：Probe + parseProbeOutput
internal/scanner/probe_test.go         ← 新建
internal/scanner/tag_reader.go         ← Read 增加 ffprobePath 参数
internal/scanner/tag_reader_test.go    ← 更新调用签名
internal/scanner/scanner.go            ← Scanner 持有 ffprobePath；NewScanner 签名变更
internal/scanner/scanner_test.go       ← 更新 NewScanner 调用
internal/scanner/watcher.go            ← Read 调用传 ffprobePath

internal/api/v1/transcode_cache.go     ← 新建：TranscodeCache
internal/api/v1/transcode_cache_test.go ← 新建
internal/api/v1/stream.go              ← 转码分支改为缓存 + ServeFile
internal/api/v1/stream_test.go         ← 更新 NewStreamHandler 调用

internal/api/router.go                 ← NewStreamHandler 传 cache 目录
cmd/server/main.go                     ← NewScanner 传 ffprobePath

web/src/stores/player.ts               ← 加 isLoading / playbackError
web/src/components/PlayerBar.vue       ← 加载图标 + 错误 toast
```

---

## 任务 1：Config 新增 FfprobePath 和 TranscodeDir

**文件：**
- 修改：`internal/config/config.go`
- 修改：`config.example.yaml`

- [ ] **步骤 1：更新 config_test.go，先确认失败**

在 `internal/config/config_test.go` 末尾追加：

```go
func TestDefault_FfprobeAndTranscodeDir(t *testing.T) {
	cfg := Default()
	if cfg.Transcode.FfprobePath != "ffprobe" {
		t.Errorf("want ffprobe, got %q", cfg.Transcode.FfprobePath)
	}
	if cfg.Cache.TranscodeDir != "./data/transcode" {
		t.Errorf("want ./data/transcode, got %q", cfg.Cache.TranscodeDir)
	}
}
```

运行确认失败：
```bash
go test ./internal/config/... -run TestDefault_FfprobeAndTranscodeDir -v
```

预期：FAIL（字段不存在，编译错误）

- [ ] **步骤 2：修改 CacheConfig 和 TranscodeConfig**

`internal/config/config.go` 中找到 `CacheConfig`：

```go
type CacheConfig struct {
	ArtworkDir       string `yaml:"artwork_dir"`
	ArtworkMaxSizeMB int    `yaml:"artwork_max_size_mb"`
}
```

替换为：

```go
type CacheConfig struct {
	ArtworkDir       string `yaml:"artwork_dir"`
	ArtworkMaxSizeMB int    `yaml:"artwork_max_size_mb"`
	TranscodeDir     string `yaml:"transcode_dir"`
}
```

找到 `TranscodeConfig`：

```go
type TranscodeConfig struct {
	FFmpegPath     string `yaml:"ffmpeg_path"`
	DefaultFormat  string `yaml:"default_format"`
	DefaultBitrate int    `yaml:"default_bitrate"`
}
```

替换为：

```go
type TranscodeConfig struct {
	FFmpegPath     string `yaml:"ffmpeg_path"`
	FfprobePath    string `yaml:"ffprobe_path"`
	DefaultFormat  string `yaml:"default_format"`
	DefaultBitrate int    `yaml:"default_bitrate"`
}
```

- [ ] **步骤 3：更新 Default()**

找到 `Cache: CacheConfig{ArtworkDir: "./data/artwork", ArtworkMaxSizeMB: 2048},`，替换为：

```go
		Cache:    CacheConfig{ArtworkDir: "./data/artwork", ArtworkMaxSizeMB: 2048, TranscodeDir: "./data/transcode"},
```

找到 `Transcode` 块：

```go
		Transcode: TranscodeConfig{
			FFmpegPath:     "ffmpeg",
			DefaultFormat:  "mp3",
			DefaultBitrate: 192,
```

在 `FFmpegPath` 行后插入 `FfprobePath` 行：

```go
		Transcode: TranscodeConfig{
			FFmpegPath:     "ffmpeg",
			FfprobePath:    "ffprobe",
			DefaultFormat:  "mp3",
			DefaultBitrate: 192,
```

- [ ] **步骤 4：运行配置测试，确认通过**

```bash
go test ./internal/config/... -v
```

预期：所有测试 PASS

- [ ] **步骤 5：更新 config.example.yaml**

在 `transcode:` 段的 `ffmpeg_path: ffmpeg` 行后加 `ffprobe_path: ffprobe`；在 `cache:` 段的 `artwork_max_size_mb` 行后加 `transcode_dir: ./data/transcode`。最终两段形如：

```yaml
cache:
  artwork_dir: ./data/artwork
  artwork_max_size_mb: 2048
  transcode_dir: ./data/transcode

transcode:
  ffmpeg_path: ffmpeg
  ffprobe_path: ffprobe
  default_format: mp3
  default_bitrate: 192
```

- [ ] **步骤 6：提交**

```bash
git add internal/config/config.go internal/config/config_test.go config.example.yaml
git commit -m "feat: config 新增 ffprobe_path 和 transcode_dir"
```

---

## 任务 2：ffprobe 探测器

**文件：**
- 创建：`internal/scanner/probe.go`
- 创建：`internal/scanner/probe_test.go`

- [ ] **步骤 1：写失败测试**

创建 `internal/scanner/probe_test.go`：

```go
// internal/scanner/probe_test.go
package scanner

import "testing"

func TestParseProbeOutput_FullData(t *testing.T) {
	jsonOut := `{
		"streams": [{"sample_rate": "44100", "channels": 2}],
		"format": {"duration": "245.760000", "bit_rate": "320000"}
	}`
	props, err := parseProbeOutput([]byte(jsonOut))
	if err != nil {
		t.Fatalf("parseProbeOutput: %v", err)
	}
	if props.Duration != 245 {
		t.Errorf("Duration = %d, want 245", props.Duration)
	}
	if props.Bitrate != 320 {
		t.Errorf("Bitrate = %d, want 320", props.Bitrate)
	}
	if props.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100", props.SampleRate)
	}
	if props.Channels != 2 {
		t.Errorf("Channels = %d, want 2", props.Channels)
	}
}

func TestParseProbeOutput_MissingFields(t *testing.T) {
	jsonOut := `{"streams": [], "format": {}}`
	props, err := parseProbeOutput([]byte(jsonOut))
	if err != nil {
		t.Fatalf("parseProbeOutput: %v", err)
	}
	if props.Duration != 0 || props.Bitrate != 0 || props.SampleRate != 0 || props.Channels != 0 {
		t.Errorf("want all zero, got %+v", props)
	}
}

func TestParseProbeOutput_BadJSON(t *testing.T) {
	_, err := parseProbeOutput([]byte("not json"))
	if err == nil {
		t.Error("want error for bad JSON")
	}
}

func TestProbe_FfprobeNotFound(t *testing.T) {
	_, err := Probe("/nonexistent/ffprobe", "/tmp/whatever.mp3")
	if err == nil {
		t.Error("want error when ffprobe binary missing")
	}
}
```

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/scanner/... -run "TestParseProbe|TestProbe" -v
```

预期：FAIL（`parseProbeOutput`、`Probe` 未定义）

- [ ] **步骤 3：实现 probe.go**

创建 `internal/scanner/probe.go`：

```go
// internal/scanner/probe.go
package scanner

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// AudioProps holds audio properties extracted via ffprobe.
type AudioProps struct {
	Duration   int // seconds
	Bitrate    int // kbps
	SampleRate int // Hz
	Channels   int
}

// Probe runs ffprobe on filePath and returns its audio properties.
// Returns an error if ffprobe is unavailable or fails; callers should
// degrade to zero values without aborting the scan.
func Probe(ffprobePath, filePath string) (AudioProps, error) {
	cmd := exec.Command(
		ffprobePath,
		"-v", "error",
		"-print_format", "json",
		"-show_entries", "format=duration,bit_rate:stream=sample_rate,channels",
		filePath,
	)
	out, err := cmd.Output()
	if err != nil {
		return AudioProps{}, err
	}
	return parseProbeOutput(out)
}

// parseProbeOutput parses ffprobe JSON into AudioProps. Missing or malformed
// numeric fields degrade to 0 individually (only a JSON syntax error fails).
func parseProbeOutput(data []byte) (AudioProps, error) {
	var raw struct {
		Streams []struct {
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
		} `json:"format"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return AudioProps{}, err
	}

	var props AudioProps
	if d, err := strconv.ParseFloat(strings.TrimSpace(raw.Format.Duration), 64); err == nil {
		props.Duration = int(d)
	}
	if br, err := strconv.Atoi(strings.TrimSpace(raw.Format.BitRate)); err == nil {
		props.Bitrate = br / 1000
	}
	if len(raw.Streams) > 0 {
		if sr, err := strconv.Atoi(strings.TrimSpace(raw.Streams[0].SampleRate)); err == nil {
			props.SampleRate = sr
		}
		props.Channels = raw.Streams[0].Channels
	}
	return props, nil
}
```

- [ ] **步骤 4：运行测试，确认通过**

```bash
go test ./internal/scanner/... -run "TestParseProbe|TestProbe" -v
```

预期：4 个测试全部 PASS

- [ ] **步骤 5：提交**

```bash
git add internal/scanner/probe.go internal/scanner/probe_test.go
git commit -m "feat: scanner — ffprobe 音频属性探测器"
```

---

## 任务 3：Read 集成 ffprobe

**文件：**
- 修改：`internal/scanner/tag_reader.go`
- 修改：`internal/scanner/tag_reader_test.go`

- [ ] **步骤 1：更新现有测试的调用签名（先让它反映新签名）**

`internal/scanner/tag_reader_test.go` 中没有直接调用 `Read` 的测试（都是测 `inferFromPath`、`parseArtistAlbum` 等）。确认这点：

```bash
grep -n "Read(" internal/scanner/tag_reader_test.go
```

预期：无输出（无需改测试）。若有输出，把调用改成 `Read(path, paths, "")` 形式。

新增一个针对降级行为的测试，追加到 `tag_reader_test.go` 末尾：

```go
func TestRead_FfprobeUnavailable_DurationZero(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(f, []byte("notreal"), 0644); err != nil {
		t.Fatal(err)
	}
	// ffprobe 路径无效 → duration 应保持 0，且不报错
	meta, err := Read(f, []string{dir}, "/nonexistent/ffprobe")
	if err != nil {
		t.Fatalf("Read should not fail when ffprobe missing: %v", err)
	}
	if meta.Duration != 0 {
		t.Errorf("Duration = %d, want 0 (ffprobe unavailable)", meta.Duration)
	}
}
```

确认测试文件已 import `os` 和 `path/filepath`（已有，因为其他测试用到）。

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/scanner/... -run "TestRead_FfprobeUnavailable" -v
```

预期：FAIL（`Read` 现在只接受 2 个参数，编译错误）

- [ ] **步骤 3：修改 Read 函数签名和实现**

`internal/scanner/tag_reader.go` 中找到：

```go
func Read(path string, libraryPaths []string) (TrackMeta, error) {
```

替换为：

```go
func Read(path string, libraryPaths []string, ffprobePath string) (TrackMeta, error) {
```

在函数体 `return meta, nil` 之前（即默认值填充之后），插入 ffprobe 提取：

```go
	// ffprobe 提取音频属性（失败保持 0，不阻断扫描）
	if ffprobePath != "" {
		if props, err := Probe(ffprobePath, path); err == nil {
			meta.Duration = props.Duration
			meta.Bitrate = props.Bitrate
			meta.SampleRate = props.SampleRate
			meta.Channels = props.Channels
		}
	}

	return meta, nil
```

- [ ] **步骤 4：运行测试，确认通过**

```bash
go test ./internal/scanner/... -run "TestRead_FfprobeUnavailable" -v
```

预期：PASS

注意：此时 `scanner.go` 和 `watcher.go` 调用 `Read` 仍是旧签名，**整个包会编译失败**。这是预期的——任务 4 修复。可以先继续。

- [ ] **步骤 5：提交**

```bash
git add internal/scanner/tag_reader.go internal/scanner/tag_reader_test.go
git commit -m "feat: Read 集成 ffprobe 提取 duration"
```

---

## 任务 4：Scanner 传递 ffprobePath

**文件：**
- 修改：`internal/scanner/scanner.go`
- 修改：`internal/scanner/watcher.go`
- 修改：`internal/scanner/scanner_test.go`
- 修改：`cmd/server/main.go`

- [ ] **步骤 1：修改 Scanner 结构体和 NewScanner**

`internal/scanner/scanner.go` 中找到结构体字段区（`ing *Ingester` 那行下方），在 `cfg config.LibraryConfig` 后加字段。找到：

```go
type Scanner struct {
	db  *sql.DB
	cfg config.LibraryConfig
	ing *Ingester
```

替换为：

```go
type Scanner struct {
	db          *sql.DB
	cfg         config.LibraryConfig
	ing         *Ingester
	ffprobePath string
```

找到 `NewScanner`：

```go
func NewScanner(db *sql.DB, cfg config.LibraryConfig) *Scanner {
	return &Scanner{
		db:     db,
		cfg:    cfg,
		ing:    NewIngester(db),
		stopCh: make(chan struct{}),
	}
}
```

替换为：

```go
func NewScanner(db *sql.DB, cfg config.LibraryConfig, ffprobePath string) *Scanner {
	return &Scanner{
		db:          db,
		cfg:         cfg,
		ing:         NewIngester(db),
		ffprobePath: ffprobePath,
		stopCh:      make(chan struct{}),
	}
}
```

- [ ] **步骤 2：修改 scanner.go 的 Read 调用**

找到 `meta, err := Read(path, s.cfg.Paths)`（约 161 行），替换为：

```go
				meta, err := Read(path, s.cfg.Paths, s.ffprobePath)
```

- [ ] **步骤 3：修改 watcher.go 的 Read 调用**

`internal/scanner/watcher.go` 中找到 `meta, err := Read(path, s.cfg.Paths)`（约 97 行），替换为：

```go
		meta, err := Read(path, s.cfg.Paths, s.ffprobePath)
```

- [ ] **步骤 4：修改 scanner_test.go 的 NewScanner 调用**

`internal/scanner/scanner_test.go` 中找到所有 `NewScanner(...)` 调用。先查看：

```bash
grep -n "NewScanner(" internal/scanner/scanner_test.go
```

把每处 `NewScanner(d, config.LibraryConfig{...})` 改为 `NewScanner(d, config.LibraryConfig{...}, "")`（空字符串表示测试中不调用 ffprobe）。

- [ ] **步骤 5：修改 main.go**

`cmd/server/main.go` 中找到：

```go
	sc := scanner.NewScanner(database, cfg.Library)
```

替换为：

```go
	sc := scanner.NewScanner(database, cfg.Library, cfg.Transcode.FfprobePath)
```

- [ ] **步骤 6：运行全部 scanner 测试 + 编译**

```bash
go test ./internal/scanner/... -v 2>&1 | tail -15
go build ./...
```

预期：所有 scanner 测试 PASS，全项目编译成功

- [ ] **步骤 7：提交**

```bash
git add internal/scanner/scanner.go internal/scanner/watcher.go internal/scanner/scanner_test.go cmd/server/main.go
git commit -m "feat: Scanner 传递 ffprobePath 到 Read"
```

---

## 任务 5：转码磁盘缓存

**文件：**
- 创建：`internal/api/v1/transcode_cache.go`
- 创建：`internal/api/v1/transcode_cache_test.go`

- [ ] **步骤 1：写失败测试**

创建 `internal/api/v1/transcode_cache_test.go`：

```go
// internal/api/v1/transcode_cache_test.go
package v1

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscodeCache_Path(t *testing.T) {
	c := NewTranscodeCache("/cache")
	p := c.Path("abc123", "mp3", 192)
	want := filepath.Join("/cache", "abc123_mp3_192k.mp3")
	if p != want {
		t.Errorf("Path = %q, want %q", p, want)
	}
}

func TestTranscodeCache_PathSanitizesID(t *testing.T) {
	c := NewTranscodeCache("/cache")
	// trackID 理论上是 UUID，不含路径分隔符；但防御性测试确保不会逃逸目录
	p := c.Path("../etc/passwd", "mp3", 192)
	if strings.Contains(p, "..") {
		t.Errorf("Path must not contain '..': %q", p)
	}
}

func TestTranscodeCache_LockForSameKeySameMutex(t *testing.T) {
	c := NewTranscodeCache("/cache")
	l1 := c.lockFor("k1")
	l2 := c.lockFor("k1")
	if l1 != l2 {
		t.Error("lockFor same key should return same mutex")
	}
	l3 := c.lockFor("k2")
	if l1 == l3 {
		t.Error("lockFor different keys should return different mutexes")
	}
}
```

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/api/v1/... -run "TestTranscodeCache" -v
```

预期：FAIL（`NewTranscodeCache` 未定义）

- [ ] **步骤 3：实现 transcode_cache.go**

创建 `internal/api/v1/transcode_cache.go`：

```go
// internal/api/v1/transcode_cache.go
package v1

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// TranscodeCache manages on-disk cached transcode outputs and per-key locks
// that prevent two requests from transcoding the same track concurrently.
type TranscodeCache struct {
	dir      string
	mu       sync.Mutex
	inflight map[string]*sync.Mutex
}

// NewTranscodeCache creates a cache rooted at dir.
func NewTranscodeCache(dir string) *TranscodeCache {
	return &TranscodeCache{
		dir:      dir,
		inflight: make(map[string]*sync.Mutex),
	}
}

// Path returns the cache file path for a track in the given format and bitrate.
// The trackID is sanitised to its base name so it cannot escape the cache dir.
func (c *TranscodeCache) Path(trackID, format string, bitrate int) string {
	safeID := filepath.Base(trackID)
	name := fmt.Sprintf("%s_%s_%dk.%s", safeID, format, bitrate, format)
	return filepath.Join(c.dir, name)
}

// key derives the lock key from the cache file name.
func (c *TranscodeCache) key(trackID, format string, bitrate int) string {
	return fmt.Sprintf("%s_%s_%dk", filepath.Base(trackID), format, bitrate)
}

// lockFor returns the mutex guarding a cache key, creating it lazily.
func (c *TranscodeCache) lockFor(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.inflight[key]; ok {
		return m
	}
	m := &sync.Mutex{}
	c.inflight[key] = m
	return m
}

// keyContainsTraversal is a defensive guard documented via test; Path already
// sanitises via filepath.Base, so this helper is intentionally simple.
var _ = strings.Contains
```

注意：`var _ = strings.Contains` 是为了让 import 不报未使用（测试文件用了 strings，但实现文件没直接用）。**更干净的做法**：实现文件不 import strings，删掉这行，让测试文件自己 import strings（测试文件已 import）。所以实现文件应删除 `"strings"` import 和 `var _ = strings.Contains` 行。最终实现文件 import 块为：

```go
import (
	"fmt"
	"path/filepath"
	"sync"
)
```

- [ ] **步骤 4：运行测试，确认通过**

```bash
go test ./internal/api/v1/... -run "TestTranscodeCache" -v
```

预期：3 个测试全部 PASS

- [ ] **步骤 5：提交**

```bash
git add internal/api/v1/transcode_cache.go internal/api/v1/transcode_cache_test.go
git commit -m "feat: 转码磁盘缓存（缓存键 + 并发锁）"
```

---

## 任务 6：stream.go 接入缓存

**文件：**
- 修改：`internal/api/v1/stream.go`
- 修改：`internal/api/v1/stream_test.go`
- 修改：`internal/api/router.go`

- [ ] **步骤 1：更新 NewStreamHandler 签名（先让现有测试反映）**

`internal/api/v1/stream_test.go` 中找到所有 `NewStreamHandler(d)` 调用：

```bash
grep -n "NewStreamHandler(" internal/api/v1/stream_test.go
```

把每处 `NewStreamHandler(d)` 改为 `NewStreamHandler(d, config.TranscodeConfig{}, t.TempDir())`。`stream_test.go` 已 import `config`，若 `t.TempDir()` 处缺 import 不影响（testing 已 import）。

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/api/v1/... -run "TestStream" -v
```

预期：FAIL（NewStreamHandler 签名不匹配，编译错误）

- [ ] **步骤 3：重写 stream.go**

完整替换 `internal/api/v1/stream.go`：

```go
// internal/api/v1/stream.go
package v1

import (
	"database/sql"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/config"
)

var audioContentTypes = map[string]string{
	"mp3":  "audio/mpeg",
	"ogg":  "audio/ogg",
	"opus": "audio/ogg",
	"wav":  "audio/wav",
}

// StreamHandler handles GET /api/v1/tracks/:id/stream.
type StreamHandler struct {
	db        *sql.DB
	transcode config.TranscodeConfig
	cache     *TranscodeCache
}

// NewStreamHandler creates a StreamHandler backed by db, using transcode config
// and a disk cache rooted at cacheDir.
func NewStreamHandler(db *sql.DB, transcode config.TranscodeConfig, cacheDir string) *StreamHandler {
	if transcode.FFmpegPath == "" {
		transcode.FFmpegPath = "ffmpeg"
	}
	if transcode.DefaultFormat == "" {
		transcode.DefaultFormat = "mp3"
	}
	if transcode.DefaultBitrate == 0 {
		transcode.DefaultBitrate = 192
	}
	return &StreamHandler{
		db:        db,
		transcode: transcode,
		cache:     NewTranscodeCache(cacheDir),
	}
}

// Stream handles GET /api/v1/tracks/:id/stream.
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	h.stream(w, r, chi.URLParam(r, "id"))
}

func (h *StreamHandler) stream(w http.ResponseWriter, r *http.Request, trackID string) {
	var filePath, format string
	err := h.db.QueryRow(
		`SELECT file_path, format FROM tracks WHERE id=? AND is_available=1`,
		trackID,
	).Scan(&filePath, &format)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	format = strings.ToLower(format)
	if ct, ok := audioContentTypes[format]; ok {
		w.Header().Set("Content-Type", ct)
		http.ServeFile(w, r, filePath)
		return
	}

	h.serveTranscoded(w, r, trackID, filePath)
}

// serveTranscoded serves an mp3 transcode of filePath, caching the result to
// disk so subsequent requests (and Range/seek) are served via http.ServeFile.
func (h *StreamHandler) serveTranscoded(w http.ResponseWriter, r *http.Request, trackID, filePath string) {
	bitrate := h.transcode.DefaultBitrate
	cachePath := h.cache.Path(trackID, "mp3", bitrate)
	key := h.cache.key(trackID, "mp3", bitrate)

	// 命中缓存：直接 ServeFile（自带 Range）
	if _, err := os.Stat(cachePath); err == nil {
		w.Header().Set("Content-Type", "audio/mpeg")
		http.ServeFile(w, r, cachePath)
		return
	}

	// 未命中：加锁转码（同一曲目并发请求只转一次）
	lock := h.cache.lockFor(key)
	lock.Lock()
	// double-check：可能其他请求已转好
	if _, err := os.Stat(cachePath); err != nil {
		if terr := h.transcodeToFile(r, filePath, cachePath, bitrate); terr != nil {
			lock.Unlock()
			if r.Context().Err() != nil {
				return // 客户端断开
			}
			writeJSONError(w, http.StatusInternalServerError, "转码失败")
			return
		}
	}
	lock.Unlock()

	w.Header().Set("Content-Type", "audio/mpeg")
	http.ServeFile(w, r, cachePath)
}

// transcodeToFile transcodes filePath to mp3 at the given bitrate, writing to a
// temp file then atomically renaming to dst. A failed run cleans up the temp file.
func (h *StreamHandler) transcodeToFile(r *http.Request, filePath, dst string, bitrate int) error {
	if err := os.MkdirAll(h.cache.dir, 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"

	cmd := exec.CommandContext(
		r.Context(),
		h.transcode.FFmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-i", filePath,
		"-vn",
		"-codec:a", "libmp3lame",
		"-b:a", strconv.Itoa(bitrate)+"k",
		"-f", "mp3",
		"-y",
		tmp,
	)
	if err := cmd.Run(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
```

- [ ] **步骤 4：更新 router.go 传缓存目录**

`internal/api/router.go` 中找到：

```go
		stream := v1.NewStreamHandler(db, cfg.Transcode)
```

替换为：

```go
		stream := v1.NewStreamHandler(db, cfg.Transcode, cfg.Cache.TranscodeDir)
```

- [ ] **步骤 5：更新 stream_test.go 的转码测试**

现有 `TestStream_TranscodesM4AToMP3` 用了一个 mock ffmpeg 脚本，但它把 `MP3DATA` 打到 **stdout**（`printf MP3DATA`）。新实现改为**写输出文件**（ffmpeg 最后一个参数是文件路径，不是 `pipe:1`），所以 mock 脚本必须改成写到它的最后一个参数。同时 `NewStreamHandler` 多了 cacheDir 参数。

完整替换 `TestStream_TranscodesM4AToMP3` 为：

```go
func TestStream_TranscodesM4AToMP3(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.m4a")
	if err := os.WriteFile(audioFile, []byte("FAKEM4ADATA"), 0644); err != nil {
		t.Fatal(err)
	}
	// mock ffmpeg：把 MP3DATA 写到最后一个参数（输出文件路径），而非 stdout
	ffmpeg := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\neval \"out=\\${$#}\"\nprintf MP3DATA > \"$out\"\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','B','a1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t1','T','a1','al1',?,'m4a',1,'pending')`, audioFile); err != nil {
		t.Fatal(err)
	}

	h := NewStreamHandler(d, config.TranscodeConfig{
		FFmpegPath:     ffmpeg,
		DefaultFormat:  "mp3",
		DefaultBitrate: 192,
	}, t.TempDir())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream", nil)
	h.stream(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("want Content-Type audio/mpeg, got %q", ct)
	}
	if body := w.Body.String(); body != "MP3DATA" {
		t.Errorf("want transcoded body MP3DATA, got %q", body)
	}
}

func TestStream_TranscodeCacheHit(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.m4a")
	if err := os.WriteFile(audioFile, []byte("FAKEM4ADATA"), 0644); err != nil {
		t.Fatal(err)
	}
	// mock ffmpeg：调用计数器 —— 第二次请求应命中缓存，不再调用
	ffmpeg := filepath.Join(dir, "ffmpeg")
	marker := filepath.Join(dir, "called")
	script := "#!/bin/sh\neval \"out=\\${$#}\"\nprintf MP3DATA > \"$out\"\necho x >> " + marker + "\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','B','a1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t1','T','a1','al1',?,'m4a',1,'pending')`, audioFile); err != nil {
		t.Fatal(err)
	}

	cacheDir := t.TempDir()
	h := NewStreamHandler(d, config.TranscodeConfig{FFmpegPath: ffmpeg, DefaultBitrate: 192}, cacheDir)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream", nil)
		h.stream(w, req, "t1")
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: want 200, got %d", i, w.Code)
		}
	}

	// ffmpeg 只应被调用一次（第二次命中缓存）
	data, _ := os.ReadFile(marker)
	if got := strings.Count(string(data), "x"); got != 1 {
		t.Errorf("ffmpeg called %d times, want 1 (second request should hit cache)", got)
	}
}
```

测试文件需 import `strings`（`TestStream_TranscodeCacheHit` 用到）。在 import 块加 `"strings"`。

`TestStream_ServesFile`（mp3 直传）和 `TestStream_NotFound` 的 `NewStreamHandler(d)` 调用也要改为 `NewStreamHandler(d, config.TranscodeConfig{}, t.TempDir())`（已在步骤 1 提及，此处确认）。`TestStream_ServesFile` 走 ServeFile 不调 ffmpeg，逻辑不变。

- [ ] **步骤 6：运行全部 v1 测试 + 编译**

```bash
go test ./internal/api/... -v 2>&1 | tail -20
go build ./...
```

预期：所有测试 PASS，编译成功

- [ ] **步骤 7：提交**

```bash
git add internal/api/v1/stream.go internal/api/v1/stream_test.go internal/api/router.go
git commit -m "feat: 转码结果磁盘缓存，ServeFile 提供（支持 Range/seek）"
```

---

## 任务 7：前端加载与错误状态

**文件：**
- 修改：`web/src/stores/player.ts`
- 修改：`web/src/components/PlayerBar.vue`

- [ ] **步骤 1：player store 新增状态**

`web/src/stores/player.ts` 中，在 `const repeatMode = ...` 那一组 ref 声明后追加：

```ts
  const isLoading = ref(false)
  const playbackError = ref<string | null>(null)
```

- [ ] **步骤 2：绑定 audio 事件**

在现有 `audio.addEventListener('error', ...)` 块**替换**为下面三段（playing/waiting/error）。找到：

```ts
  audio.addEventListener('error', (e) => {
    console.error('Audio playback error: ', e)
    isPlaying.value = false
  })
```

替换为：

```ts
  audio.addEventListener('playing', () => {
    isLoading.value = false
  })

  audio.addEventListener('waiting', () => {
    isLoading.value = true
  })

  audio.addEventListener('error', () => {
    isLoading.value = false
    isPlaying.value = false
    playbackError.value = currentTrack.value
      ? `无法播放《${currentTrack.value.title}》`
      : '播放失败'
  })
```

- [ ] **步骤 3：playTrack / playAtIndex 开头重置状态**

在 `playTrack` 函数体开头（`if (newQueue ...` 之前）插入：

```ts
    isLoading.value = true
    playbackError.value = null
```

在 `playAtIndex` 函数体内、`if (index >= 0 && index < queue.value.length) {` 之后插入：

```ts
      isLoading.value = true
      playbackError.value = null
```

- [ ] **步骤 4：新增 clearError 并导出新状态**

在 `function toggleMute()` 之后新增：

```ts
  function clearError() {
    playbackError.value = null
  }
```

在 `return { ... }` 块中，加入 `isLoading`、`playbackError`、`clearError`：

找到 return 块结尾的 `toggleMute` 行，改为：

```ts
    toggleMute,
    isLoading,
    playbackError,
    clearError
```

- [ ] **步骤 5：构建前端验证 store 无类型错误**

```bash
cd web && npm run build 2>&1 | tail -20 && cd ..
```

预期：构建成功（此时 PlayerBar 还没用新状态，但 store 应通过类型检查）

- [ ] **步骤 6：PlayerBar 显示加载图标和错误 toast**

`web/src/components/PlayerBar.vue` —— 先查看现有结构，确认从 store 解构的位置和播放按钮模板：

```bash
grep -n "usePlayerStore\|isPlaying\|togglePlay\|storeToRefs\|<template>" web/src/components/PlayerBar.vue | head
```

在组件从 store 取值处加入 `isLoading`、`playbackError`、`clearError`（沿用该文件现有的解构方式，storeToRefs 用于 ref 状态，方法直接取）。

在播放/暂停按钮的模板处，根据 `isLoading` 切换显示：加载时显示一个旋转图标（CSS class `loading-spinner`），否则显示原有的播放/暂停图标。例如把原按钮内容包一层条件：

```vue
        <button class="control-btn play-btn" @click="togglePlay" :disabled="isLoading">
          <span v-if="isLoading" class="loading-spinner" aria-label="加载中"></span>
          <span v-else>{{ isPlaying ? '⏸' : '▶' }}</span>
        </button>
```

（图标符号按文件现有写法调整；关键是 `isLoading` 时显示 spinner 且禁用按钮。）

在模板根部（播放条最外层元素内）加入错误 toast：

```vue
    <transition name="fade">
      <div v-if="playbackError" class="playback-error-toast" @click="clearError">
        {{ playbackError }}
      </div>
    </transition>
```

在该组件的 `<style scoped>` 末尾加入样式（复用玻璃拟态变量，若无则用具体值）：

```css
.loading-spinner {
  display: inline-block;
  width: 18px;
  height: 18px;
  border: 2px solid rgba(255, 255, 255, 0.3);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.playback-error-toast {
  position: absolute;
  bottom: 100%;
  left: 50%;
  transform: translateX(-50%);
  margin-bottom: 12px;
  padding: 10px 18px;
  background: rgba(220, 38, 38, 0.85);
  backdrop-filter: blur(12px);
  color: #fff;
  border-radius: 12px;
  font-size: 14px;
  white-space: nowrap;
  cursor: pointer;
  z-index: 50;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
```

- [ ] **步骤 7：错误 toast 自动消失**

在 PlayerBar 的 `<script setup>` 中，对 `playbackError` 加 watch，4 秒后自动清除。先确认已 import `watch`（若没有则从 vue 加 import）。加入：

```ts
watch(playbackError, (val) => {
  if (val) {
    setTimeout(() => clearError(), 4000)
  }
})
```

- [ ] **步骤 8：构建前端，确认通过**

```bash
cd web && npm run build 2>&1 | tail -20 && cd ..
```

预期：构建成功，无类型错误

- [ ] **步骤 9：提交**

```bash
git add web/src/stores/player.ts web/src/components/PlayerBar.vue
git commit -m "feat(web): 播放加载提示 + 失败 toast"
```

---

## 任务 8：完整验证 + 推送

**文件：** 无新增，端到端验证

- [ ] **步骤 1：全量测试 + 构建**

```bash
go test ./... 2>&1 | grep -E "^(ok|FAIL)"
go build ./...
cd web && npm run build 2>&1 | tail -5 && cd ..
```

预期：所有 Go 测试 PASS，Go 编译成功，前端构建成功

- [ ] **步骤 2：完整构建二进制并冒烟测试时长提取**

需要本机有 ffprobe 和真实音频文件。若无真实文件，跳过此步的时长断言，仅验证服务启动。

```bash
go build -o lyra ./cmd/server
./lyra &
sleep 2
curl -s http://localhost:4533/health
# 若配置了音乐库路径并触发扫描，可查扫描状态
curl -s -X POST http://localhost:4533/api/v1/library/scan 2>/dev/null || true
kill %1
rm -f lyra
```

预期：`/health` 返回 `{"status":"ok","version":"0.1.0"}`

- [ ] **步骤 3：推送**

```bash
git push origin master
```

---

## 自检清单

**规格覆盖：**
- [x] ffprobe 探测器（probe.go，降级返回 error）→ 任务 2
- [x] 配置 FfprobePath / TranscodeDir → 任务 1
- [x] Read 集成 ffprobe → 任务 3
- [x] Scanner 传递 ffprobePath（scanner + watcher 两个调用点）→ 任务 4
- [x] 旧数据补齐（无需改 ingester，upsert 已更新 duration）→ 任务 4 验证（重扫即覆盖）
- [x] 转码缓存（缓存键 + 并发锁 + 原子写入）→ 任务 5、6
- [x] 转码缓存命中走 ServeFile（Range/seek）→ 任务 6
- [x] 前端 isLoading（playing/waiting 事件）→ 任务 7
- [x] 前端 playbackError（error 事件 + toast）→ 任务 7
- [x] main.go / router.go 接线 → 任务 4、6

**已知限制（spec 已列，计划不实现）：**
- 转码缓存无自动清理（YAGNI）
- 转码码率固定 192k（用户确认后续优化）
