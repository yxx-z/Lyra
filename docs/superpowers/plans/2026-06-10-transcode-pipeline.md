# 转码管线重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把转码逻辑抽到独立包 `internal/transcode`，支持 Subsonic `maxBitRate`/`format`(含 raw)、智能直传、边转边播 + 后台写缓存、LRU 缓存回收、mp3/opus/aac 多编码。

**Architecture:** 新包 `internal/transcode`：`decide.go`(纯决策)、`codec.go`(编码注册表)、`cache.go`(磁盘缓存 + LRU 回收)、`service.go`(编排直传/命中/管道+缓存/seek 回退)。`v1.StreamHandler` 瘦身为薄封装(查库 → 调 Service)，v1 与 subsonic 共享同一个 Service 实例。

**Tech Stack:** Go 1.25（os/exec、io.MultiWriter、context、httptest），ffmpeg(libmp3lame/libopus/aac)。

**Go 环境：** 含 `go` 命令的步骤前 `export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## 设计参考

实现前读 `docs/superpowers/specs/2026-06-10-transcode-pipeline-design.md`。

**关键既有代码：**
- `internal/api/v1/stream.go`：现有 `StreamHandler`、`NewStreamHandler(db, transcode, cacheDir)`、`StreamByID(w,r,trackID)`、`serveTranscoded`、`transcodeToFile`、`prepareAudioResponse`、`audioContentTypes`。**Task 5 用新包替换这些。**
- `internal/api/v1/transcode_cache.go` + `_test.go`：现有 `TranscodeCache`。**Task 5 删除（逻辑迁到 `internal/transcode/cache.go`）。**
- `internal/api/v1/auth.go:86`：`writeJSONError(w, status, message)`（v1 内部用）。
- `internal/api/router.go:57,87-90`：v1 与 subsonic 各构造一个 `StreamHandler`。**Task 5 改为共享一个。**
- `internal/config/config.go`：`TranscodeConfig{FFmpegPath,FfprobePath,DefaultFormat,DefaultBitrate}`、`CacheConfig{ArtworkDir,ArtworkMaxSizeMB,TranscodeDir}`。**Task 5 给 CacheConfig 加 `TranscodeMaxSizeMB`。**
- 表 `tracks`：`file_path`、`format`、`bitrate`、`is_available`。
- 假 ffmpeg 测试法（见现有 `stream_test.go`）：把 shell 脚本写进临时文件、chmod +x、当作 ffmpeg 路径。

**文件结构：**
```
internal/transcode/codec.go          编码注册表 + 源 Content-Type
internal/transcode/codec_test.go
internal/transcode/decide.go         Source/Params/Decision + Plan（纯函数）
internal/transcode/decide_test.go
internal/transcode/cache.go          Cache：path/key/锁 + LRU 回收 + touch
internal/transcode/cache_test.go
internal/transcode/service.go        Service.Serve 编排
internal/transcode/service_test.go
internal/api/v1/stream.go            改：薄封装调 Service
internal/api/v1/stream_test.go       改：适配新签名与默认直传行为
internal/api/v1/transcode_cache.go   删
internal/api/v1/transcode_cache_test.go 删
internal/api/router.go               改：共享一个 Service
internal/config/config.go            改：CacheConfig 加 TranscodeMaxSizeMB
config.example.yaml                  改：cache 下加 transcode_max_size_mb
```

---

### Task 1: 编码注册表（codec.go）

**Files:** Create `internal/transcode/codec.go`, `internal/transcode/codec_test.go`

- [ ] **Step 1: 写失败测试** — `internal/transcode/codec_test.go`:
```go
package transcode

import "testing"

func TestCodecFor(t *testing.T) {
	if c := codecFor("opus"); c.Name != "opus" || c.ContentType != "audio/ogg" || c.Ext != "opus" {
		t.Errorf("opus 映射不符: %+v", c)
	}
	if c := codecFor("aac"); c.ContentType != "audio/aac" || c.Ext != "aac" {
		t.Errorf("aac 映射不符: %+v", c)
	}
	// 未知名回退 mp3
	if c := codecFor("flac"); c.Name != "mp3" {
		t.Errorf("未知编码应回退 mp3，得到 %s", c.Name)
	}
}

func TestContentTypeForSource(t *testing.T) {
	cases := map[string]string{"mp3": "audio/mpeg", "flac": "audio/flac", "m4a": "audio/mp4", "FLAC": "audio/flac"}
	for in, want := range cases {
		if got := contentTypeForSource(in); got != want {
			t.Errorf("contentTypeForSource(%q)=%q want %q", in, got, want)
		}
	}
	if got := contentTypeForSource("xyz"); got != "application/octet-stream" {
		t.Errorf("未知格式应回退 octet-stream，得到 %q", got)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/transcode/ -run 'TestCodec|TestContentType' -v`
Expected: 编译失败（undefined）。

- [ ] **Step 3: 实现** — `internal/transcode/codec.go`:
```go
package transcode

import "strings"

// Codec 描述一种输出编码的 ffmpeg 参数与 HTTP 元数据。
type Codec struct {
	Name        string   // mp3|opus|aac
	Args        []string // ffmpeg 编码/容器参数（置于 -i 之后、码率/输出之前）
	ContentType string
	Ext         string
}

// 输出编码注册表。
var codecs = map[string]Codec{
	"mp3":  {Name: "mp3", Args: []string{"-c:a", "libmp3lame", "-f", "mp3"}, ContentType: "audio/mpeg", Ext: "mp3"},
	"opus": {Name: "opus", Args: []string{"-c:a", "libopus", "-f", "ogg"}, ContentType: "audio/ogg", Ext: "opus"},
	"aac":  {Name: "aac", Args: []string{"-c:a", "aac", "-f", "adts"}, ContentType: "audio/aac", Ext: "aac"},
}

// codecFor 返回目标编码；未知名回退 mp3。
func codecFor(name string) Codec {
	if c, ok := codecs[strings.ToLower(name)]; ok {
		return c
	}
	return codecs["mp3"]
}

// 直传时按源格式推 Content-Type。
var sourceContentTypes = map[string]string{
	"mp3": "audio/mpeg", "flac": "audio/flac", "m4a": "audio/mp4", "aac": "audio/aac",
	"ogg": "audio/ogg", "opus": "audio/ogg", "wav": "audio/wav", "alac": "audio/mp4", "ape": "audio/x-ape",
}

// contentTypeForSource 直传时按源格式推 Content-Type；未知回退 application/octet-stream。
func contentTypeForSource(format string) string {
	if ct, ok := sourceContentTypes[strings.ToLower(format)]; ok {
		return ct
	}
	return "application/octet-stream"
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/transcode/ -v`
Expected: PASS。

- [ ] **Step 5: 提交**
```bash
git add internal/transcode/codec.go internal/transcode/codec_test.go
git commit -m "feat(transcode): 编码注册表（mp3/opus/aac）+ 源 Content-Type"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 2: 转码决策（decide.go）

**Files:** Create `internal/transcode/decide.go`, `internal/transcode/decide_test.go`

- [ ] **Step 1: 写失败测试** — `internal/transcode/decide_test.go`:
```go
package transcode

import "testing"

func TestPlan(t *testing.T) {
	const def = 192
	tests := []struct {
		name      string
		src       Source
		p         Params
		wantPass  bool
		wantCodec string
		wantBr    int
	}{
		{"raw 强制直传", Source{Format: "flac", Bitrate: 1000}, Params{Format: "raw"}, true, "", 0},
		{"无参数直传", Source{Format: "flac"}, Params{}, true, "", 0},
		{"有损源在预算内直传", Source{Format: "mp3", Bitrate: 128}, Params{MaxBitRate: 192}, true, "", 0},
		{"有损源超预算转mp3并封顶", Source{Format: "mp3", Bitrate: 320}, Params{MaxBitRate: 128}, false, "mp3", 128},
		{"无损源限码率转mp3", Source{Format: "flac"}, Params{MaxBitRate: 256}, false, "mp3", 256},
		{"指定opus转码", Source{Format: "flac"}, Params{Format: "opus"}, false, "opus", def},
		{"指定opus带码率", Source{Format: "mp3", Bitrate: 320}, Params{Format: "opus", MaxBitRate: 96}, false, "opus", 96},
		{"同编码同预算直传", Source{Format: "mp3", Bitrate: 128}, Params{Format: "mp3", MaxBitRate: 192}, true, "", 0},
		{"绝不升码率", Source{Format: "mp3", Bitrate: 128}, Params{Format: "mp3", MaxBitRate: 320}, true, "", 0},
		{"未知format回退mp3转码", Source{Format: "flac"}, Params{Format: "weird"}, false, "mp3", def},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Plan(tc.src, tc.p, def)
			if d.Passthrough != tc.wantPass {
				t.Fatalf("Passthrough=%v want %v (%+v)", d.Passthrough, tc.wantPass, d)
			}
			if !tc.wantPass && (d.Codec != tc.wantCodec || d.Bitrate != tc.wantBr) {
				t.Errorf("Codec/Bitrate=%s/%d want %s/%d", d.Codec, d.Bitrate, tc.wantCodec, tc.wantBr)
			}
		})
	}
}

func TestPlan_ContentType(t *testing.T) {
	if d := Plan(Source{Format: "flac"}, Params{}, 192); d.ContentType != "audio/flac" {
		t.Errorf("直传 flac 应 audio/flac，得到 %q", d.ContentType)
	}
	if d := Plan(Source{Format: "flac"}, Params{Format: "opus"}, 192); d.ContentType != "audio/ogg" || d.Ext != "opus" {
		t.Errorf("转 opus 应 audio/ogg/.opus，得到 %q/%q", d.ContentType, d.Ext)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/transcode/ -run TestPlan -v`
Expected: 编译失败（undefined Plan/Source/Params/Decision）。

- [ ] **Step 3: 实现** — `internal/transcode/decide.go`:
```go
package transcode

import "strings"

// Source 来自 tracks 表的一行。
type Source struct {
	ID      string
	Path    string
	Format  string // 小写最佳；为空时调用方应已用扩展名兜底
	Bitrate int    // kbps，0 = 未知
}

// Params 来自客户端请求。
type Params struct {
	Format     string // raw|mp3|opus|aac|""（未指定）
	MaxBitRate int    // kbps，0 = 未指定
}

// Decision 决定直传或转码及其参数。
type Decision struct {
	Passthrough bool
	Codec       string // 转码时 mp3|opus|aac
	Bitrate     int    // 转码时 kbps
	ContentType string
	Ext         string // 转码时缓存扩展名
}

var losslessFormats = map[string]bool{"flac": true, "wav": true, "alac": true, "ape": true}

func isLossless(format string) bool { return losslessFormats[strings.ToLower(format)] }

// Plan 按源与客户端参数决定直传还是转码。defaultBitrate 为配置默认码率(kbps)。
func Plan(src Source, p Params, defaultBitrate int) Decision {
	sf := strings.ToLower(src.Format)
	pf := strings.ToLower(p.Format)

	pass := Decision{Passthrough: true, ContentType: contentTypeForSource(sf)}

	// 1. raw → 直传
	if pf == "raw" {
		return pass
	}
	// 2. 未指定 format
	if pf == "" {
		if p.MaxBitRate == 0 {
			return pass
		}
		if !isLossless(sf) && src.Bitrate > 0 && src.Bitrate <= p.MaxBitRate {
			return pass
		}
		return transcodeDecision("mp3", p, src, defaultBitrate)
	}
	// 3/4. 指定 format（未知值回退 mp3）
	target := pf
	if _, ok := codecs[target]; !ok {
		target = "mp3"
	}
	if target == sf {
		if p.MaxBitRate == 0 || (!isLossless(sf) && src.Bitrate > 0 && src.Bitrate <= p.MaxBitRate) {
			return pass
		}
	}
	return transcodeDecision(target, p, src, defaultBitrate)
}

func transcodeDecision(codecName string, p Params, src Source, defaultBitrate int) Decision {
	c := codecFor(codecName)
	br := defaultBitrate
	if p.MaxBitRate > 0 {
		br = p.MaxBitRate
	}
	if !isLossless(src.Format) && src.Bitrate > 0 && src.Bitrate < br {
		br = src.Bitrate // 绝不升码率
	}
	return Decision{Codec: c.Name, Bitrate: br, ContentType: c.ContentType, Ext: c.Ext}
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/transcode/ -v`
Expected: PASS。

- [ ] **Step 5: 提交**
```bash
git add internal/transcode/decide.go internal/transcode/decide_test.go
git commit -m "feat(transcode): 转码决策 Plan（直传/按需转码/绝不升码率）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 3: 磁盘缓存 + LRU 回收（cache.go）

**Files:** Create `internal/transcode/cache.go`, `internal/transcode/cache_test.go`

- [ ] **Step 1: 写失败测试** — `internal/transcode/cache_test.go`:
```go
package transcode

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachePathAndKey(t *testing.T) {
	c := NewCache("/tmp/x", 0)
	if p := c.Path("t1", "opus", 128); p != filepath.Join("/tmp/x", "t1_opus_128k.opus") {
		t.Errorf("Path=%q", p)
	}
	if k := c.key("t1", "opus", 128); k != "t1_opus_128k" {
		t.Errorf("key=%q", k)
	}
	// trackID 不能越出缓存目录
	if p := c.Path("../../etc/passwd", "mp3", 192); filepath.Dir(p) != "/tmp/x" {
		t.Errorf("越界路径未净化: %q", p)
	}
}

func TestCacheEvict_LRU(t *testing.T) {
	dir := t.TempDir()
	// 三个 1MB 文件，mtime 由旧到新：old < mid < new
	write := func(name string, ageMin int) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, 1024*1024), 0o644); err != nil {
			t.Fatal(err)
		}
		tm := time.Now().Add(time.Duration(-ageMin) * time.Minute)
		if err := os.Chtimes(p, tm, tm); err != nil {
			t.Fatal(err)
		}
		return p
	}
	oldF := write("old.mp3", 30)
	midF := write("mid.mp3", 20)
	newF := write("new.mp3", 10)

	// 上限 2MB → 需删到 ≤2MB，最旧的 old 先删；keep=newF 永不删
	c := NewCache(dir, 2)
	c.evict(newF)

	if _, err := os.Stat(oldF); !os.IsNotExist(err) {
		t.Errorf("最旧文件应被删除")
	}
	if _, err := os.Stat(midF); err != nil {
		t.Errorf("mid 不应被删（删 old 后已达标）")
	}
	if _, err := os.Stat(newF); err != nil {
		t.Errorf("keep 文件不应被删")
	}
}

func TestCacheEvict_Unlimited(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.mp3")
	os.WriteFile(p, make([]byte, 1024), 0o644)
	NewCache(dir, 0).evict("") // 0 = 不限，不删任何东西
	if _, err := os.Stat(p); err != nil {
		t.Errorf("不限容量时不应删除")
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/transcode/ -run TestCache -v`
Expected: 编译失败（undefined NewCache/Path/key/evict）。

- [ ] **Step 3: 实现** — `internal/transcode/cache.go`:
```go
package transcode

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Cache 管理磁盘转码缓存：路径、per-key 锁、LRU 容量回收。
type Cache struct {
	dir      string
	maxBytes int64 // 0 = 不限
	mu       sync.Mutex
	// inflight：cache key → 锁。规模受 (trackID,codec,bitrate) 组合数约束（≈ 库规模），
	// 不随请求数增长，条目不回收，在此规模可接受。
	inflight map[string]*sync.Mutex
}

// NewCache 创建根于 dir 的缓存；maxSizeMB ≤0 表示不限容量。
func NewCache(dir string, maxSizeMB int) *Cache {
	return &Cache{
		dir:      dir,
		maxBytes: int64(maxSizeMB) * 1024 * 1024,
		inflight: make(map[string]*sync.Mutex),
	}
}

// Path 返回缓存文件路径；trackID 取 base 以防越出缓存目录。
func (c *Cache) Path(trackID, codec string, bitrate int) string {
	name := fmt.Sprintf("%s_%s_%dk.%s", filepath.Base(trackID), codec, bitrate, codecFor(codec).Ext)
	return filepath.Join(c.dir, name)
}

func (c *Cache) key(trackID, codec string, bitrate int) string {
	return fmt.Sprintf("%s_%s_%dk", filepath.Base(trackID), codec, bitrate)
}

// lockFor 返回某 key 的锁，惰性创建。
func (c *Cache) lockFor(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.inflight[key]; ok {
		return m
	}
	m := &sync.Mutex{}
	c.inflight[key] = m
	return m
}

// touch 把文件 mtime 刷成当前，作为 LRU 的"最近访问"近似。
func (c *Cache) touch(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

// evict 若缓存目录总大小超上限，按 mtime 最旧优先删除，删到 ≤ 上限。
// keep 为本次刚写入、需跳过的文件路径（传 "" 表示无）。
func (c *Cache) evict(keep string) {
	if c.maxBytes <= 0 {
		return
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	type fileInfo struct {
		path string
		size int64
		mod  time.Time
	}
	var files []fileInfo
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{filepath.Join(c.dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total <= c.maxBytes {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if total <= c.maxBytes {
			break
		}
		if f.path == keep {
			continue
		}
		if err := os.Remove(f.path); err == nil {
			total -= f.size
		}
	}
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/transcode/ -v`
Expected: PASS。

- [ ] **Step 5: 提交**
```bash
git add internal/transcode/cache.go internal/transcode/cache_test.go
git commit -m "feat(transcode): 磁盘缓存 + LRU 容量回收 + per-key 锁"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 4: 编排 Service（service.go）

**Files:** Create `internal/transcode/service.go`, `internal/transcode/service_test.go`

- [ ] **Step 1: 写失败测试** — `internal/transcode/service_test.go`:
```go
package transcode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// fakeFFmpeg 写一个假 ffmpeg 脚本：最后一个参数若为 pipe:1 则输出到 stdout，否则写到该文件。
// body 为输出内容；sleep 秒数用于测试取消。
func fakeFFmpeg(t *testing.T, body string, sleepSec int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\n"
	if sleepSec > 0 {
		script += "sleep " + strconv.Itoa(sleepSec) + "\n"
	}
	script += `out=""
for a in "$@"; do out="$a"; done
if [ "$out" = "pipe:1" ]; then printf '` + body + `'; else printf '` + body + `' > "$out"; fi
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func newSource(t *testing.T, format, data string) Source {
	t.Helper()
	f := filepath.Join(t.TempDir(), "in."+format)
	if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return Source{ID: "t1", Path: f, Format: format}
}

func TestServe_Passthrough(t *testing.T) {
	svc := NewService(fakeFFmpeg(t, "X", 0), 192, NewCache(t.TempDir(), 0))
	src := newSource(t, "mp3", "ORIGINALMP3")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream", nil) // 无参数 → 直传
	svc.Serve(w, r, src)
	if w.Code != 200 || w.Body.String() != "ORIGINALMP3" {
		t.Errorf("直传应原样返回文件: %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type=%q", ct)
	}
}

func TestServe_PipeAndCache(t *testing.T) {
	cache := NewCache(t.TempDir(), 0)
	svc := NewService(fakeFFmpeg(t, "TRANSCODED", 0), 192, cache)
	src := newSource(t, "flac", "FLACDATA")
	// 指定 format=opus 触发转码
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream?format=opus", nil)
	svc.Serve(w, r, src)
	if w.Body.String() != "TRANSCODED" {
		t.Errorf("管道应输出转码字节: %q", w.Body.String())
	}
	// 缓存文件应被提升
	cp := cache.Path("t1", "opus", 192)
	if _, err := os.Stat(cp); err != nil {
		t.Errorf("缓存文件应存在: %v", err)
	}
	// 再次请求命中缓存
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/stream?format=opus", nil)
	svc.Serve(w2, r2, src)
	if w2.Body.String() != "TRANSCODED" {
		t.Errorf("命中缓存应返回缓存内容: %q", w2.Body.String())
	}
}

func TestServe_ClientCancel(t *testing.T) {
	cache := NewCache(t.TempDir(), 0)
	svc := NewService(fakeFFmpeg(t, "LATE", 3), 192, cache) // ffmpeg 睡 3s
	src := newSource(t, "flac", "FLACDATA")
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/stream?format=opus", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	svc.Serve(w, r, src) // 应在 ffmpeg 被取消后返回
	// 取消后不应留下被提升的缓存文件
	if _, err := os.Stat(cache.Path("t1", "opus", 192)); err == nil {
		t.Errorf("取消后不应提升缓存")
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/transcode/ -run TestServe -v`
Expected: 编译失败（undefined NewService/Serve）。

- [ ] **Step 3: 实现** — `internal/transcode/service.go`:
```go
package transcode

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Service 按客户端参数对音频源直传或转码输出，转码结果落盘缓存。
type Service struct {
	ffmpegPath     string
	defaultBitrate int
	cache          *Cache
}

// NewService 创建转码服务。
func NewService(ffmpegPath string, defaultBitrate int, cache *Cache) *Service {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if defaultBitrate == 0 {
		defaultBitrate = 192
	}
	return &Service{ffmpegPath: ffmpegPath, defaultBitrate: defaultBitrate, cache: cache}
}

// Serve 处理一个流请求：直传 / 命中缓存 / 管道转码+写缓存 / seek 回退。
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, src Source) {
	dec := Plan(src, parseParams(r), s.defaultBitrate)

	if dec.Passthrough {
		w.Header().Set("Content-Type", dec.ContentType)
		http.ServeFile(w, r, src.Path)
		return
	}

	cachePath := s.cache.Path(src.ID, dec.Codec, dec.Bitrate)
	key := s.cache.key(src.ID, dec.Codec, dec.Bitrate)

	// 命中缓存
	if _, err := os.Stat(cachePath); err == nil {
		s.cache.touch(cachePath)
		prepareAudio(w, r, dec.ContentType)
		http.ServeFile(w, r, cachePath)
		return
	}

	// 缓存生成前带偏移拖动 → 阻塞式转成完整文件再服务（保证可 seek）
	if hasRangeOffset(r) {
		lock := s.cache.lockFor(key)
		lock.Lock()
		if _, err := os.Stat(cachePath); err != nil {
			if terr := s.transcodeToFile(r.Context(), src.Path, cachePath, dec); terr != nil {
				lock.Unlock()
				if r.Context().Err() != nil {
					return
				}
				http.Error(w, "转码失败", http.StatusInternalServerError)
				return
			}
			s.cache.evict(cachePath)
		}
		lock.Unlock()
		prepareAudio(w, r, dec.ContentType)
		http.ServeFile(w, r, cachePath)
		return
	}

	// 正常从头播：抢到锁 → 管道+写缓存；抢不到（同 key 正在转）→ 纯管道不写缓存
	lock := s.cache.lockFor(key)
	if lock.TryLock() {
		defer lock.Unlock()
		s.pipeAndCache(w, r, src.Path, cachePath, dec)
	} else {
		s.runPipe(w, r, src.Path, dec, nil)
	}
}

// parseParams 从请求读取 format / maxBitRate（GET query 与 POST 表单皆可）。
func parseParams(r *http.Request) Params {
	_ = r.ParseForm()
	br, _ := strconv.Atoi(r.Form.Get("maxBitRate"))
	return Params{Format: r.Form.Get("format"), MaxBitRate: br}
}

// hasRangeOffset 判断是否为带非零起点的 Range 请求（"bytes=0-" 视为从头，不算偏移）。
func hasRangeOffset(r *http.Request) bool {
	rng := r.Header.Get("Range")
	return rng != "" && !strings.HasPrefix(rng, "bytes=0-")
}

func prepareAudio(w http.ResponseWriter, r *http.Request, contentType string) {
	r.Header.Del("If-Modified-Since")
	r.Header.Del("If-None-Match")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
}

// command 构造 ffmpeg 命令；output 为 "pipe:1"（stdout）或目标文件路径。
func (s *Service) command(ctx context.Context, srcPath, output string, dec Decision) *exec.Cmd {
	args := []string{"-hide_banner", "-loglevel", "error", "-i", srcPath, "-vn"}
	args = append(args, codecFor(dec.Codec).Args...)
	args = append(args, "-b:a", strconv.Itoa(dec.Bitrate)+"k", "-y", output)
	return exec.CommandContext(ctx, s.ffmpegPath, args...)
}

// pipeAndCache 管道转码：边写响应边写临时缓存，成功后原子提升并回收。
func (s *Service) pipeAndCache(w http.ResponseWriter, r *http.Request, srcPath, cachePath string, dec Decision) {
	if err := os.MkdirAll(s.cache.dir, 0o755); err != nil {
		http.Error(w, "转码失败", http.StatusInternalServerError)
		return
	}
	tmp := cachePath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		http.Error(w, "转码失败", http.StatusInternalServerError)
		return
	}
	ok := s.runPipe(w, r, srcPath, dec, f)
	f.Close()
	if !ok {
		_ = os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		_ = os.Remove(tmp)
		slog.Warn("提升转码缓存失败", "err", err)
		return
	}
	s.cache.evict(cachePath)
}

// runPipe 起 ffmpeg 把输出写到 w（以及可选 extra，如缓存文件）。
// 首字节前失败 → 写 500 返回 false；已发出字节后失败 → 记录并返回 false（无法改状态码）。
func (s *Service) runPipe(w http.ResponseWriter, r *http.Request, srcPath string, dec Decision, extra io.Writer) bool {
	cmd := s.command(r.Context(), srcPath, "pipe:1", dec)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "转码失败", http.StatusInternalServerError)
		return false
	}
	if err := cmd.Start(); err != nil {
		http.Error(w, "转码失败", http.StatusInternalServerError)
		return false
	}

	// 先读首块，确认有输出再写响应头
	buf := make([]byte, 32*1024)
	n, rerr := stdout.Read(buf)
	if n == 0 {
		_ = cmd.Wait()
		if r.Context().Err() != nil {
			return false
		}
		http.Error(w, "转码失败", http.StatusInternalServerError)
		return false
	}

	prepareAudio(w, r, dec.ContentType)
	dst := io.Writer(w)
	if extra != nil {
		dst = io.MultiWriter(w, extra)
	}
	if _, werr := dst.Write(buf[:n]); werr != nil {
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
		return false
	}
	var copyErr error
	if rerr == nil {
		_, copyErr = io.Copy(dst, stdout)
	}
	waitErr := cmd.Wait()
	if copyErr != nil || waitErr != nil {
		if r.Context().Err() != nil {
			return false // 客户端断开/取消
		}
		slog.Warn("转码管道失败", "err", errors.Join(copyErr, waitErr))
		return false
	}
	return true
}

// transcodeToFile 阻塞式转成完整文件（seek 回退用）：写临时文件再原子改名。
func (s *Service) transcodeToFile(ctx context.Context, srcPath, dst string, dec Decision) error {
	if err := os.MkdirAll(s.cache.dir, 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := s.command(ctx, srcPath, tmp, dec).Run(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/transcode/ -v`
Expected: PASS（含 TestServe_Passthrough/PipeAndCache/ClientCancel）。

- [ ] **Step 5: 提交**
```bash
git add internal/transcode/service.go internal/transcode/service_test.go
git commit -m "feat(transcode): Service 编排（直传/命中/管道+缓存/seek 回退/取消）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 5: 接入 v1/subsonic/router + 配置 + 清理旧代码

**Files:** Modify `internal/config/config.go`, `config.example.yaml`, `internal/api/v1/stream.go`, `internal/api/v1/stream_test.go`, `internal/api/router.go`; Delete `internal/api/v1/transcode_cache.go`, `internal/api/v1/transcode_cache_test.go`

- [ ] **Step 1: 加配置字段** — `internal/config/config.go` 的 `CacheConfig` 加 `TranscodeMaxSizeMB`：
```go
type CacheConfig struct {
	ArtworkDir         string `yaml:"artwork_dir"`
	ArtworkMaxSizeMB   int    `yaml:"artwork_max_size_mb"`
	TranscodeDir       string `yaml:"transcode_dir"`
	TranscodeMaxSizeMB int    `yaml:"transcode_max_size_mb"`
}
```
`config.example.yaml` 的 `cache:` 块加一行（在 `artwork_max_size_mb` 下方）：
```yaml
  transcode_dir: ./data/transcode
  transcode_max_size_mb: 2048   # 转码缓存上限，0=不限
```
（若 `cache:` 块已无 `transcode_dir`，按上面补全两行；保持缩进与现有一致。）

- [ ] **Step 2: 重写 stream.go** — `internal/api/v1/stream.go` 全文替换为：
```go
// internal/api/v1/stream.go
package v1

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/transcode"
)

// StreamHandler 按 trackID 查库并委托 transcode.Service 输出音频。
type StreamHandler struct {
	db  *sql.DB
	svc *transcode.Service
}

// NewStreamHandler 创建 StreamHandler，复用传入的转码 Service。
func NewStreamHandler(db *sql.DB, svc *transcode.Service) *StreamHandler {
	return &StreamHandler{db: db, svc: svc}
}

// Stream handles GET /api/v1/tracks/:id/stream.
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	h.StreamByID(w, r, chi.URLParam(r, "id"))
}

// StreamByID 按 trackID 查库后委托 Service 直传或转码。
func (h *StreamHandler) StreamByID(w http.ResponseWriter, r *http.Request, trackID string) {
	var filePath, format string
	var bitrate int
	err := h.db.QueryRow(
		`SELECT file_path, COALESCE(format,''), COALESCE(bitrate,0) FROM tracks WHERE id=? AND is_available=1`,
		trackID,
	).Scan(&filePath, &format, &bitrate)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	format = strings.ToLower(format)
	if format == "" {
		// 用扩展名兜底
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
	}
	h.svc.Serve(w, r, transcode.Source{ID: trackID, Path: filePath, Format: format, Bitrate: bitrate})
}
```

- [ ] **Step 3: 删除旧缓存文件**
```bash
git rm internal/api/v1/transcode_cache.go internal/api/v1/transcode_cache_test.go
```

- [ ] **Step 4: 改 router 共享 Service** — `internal/api/router.go`：
import 加 `"github.com/yxx-z/lyra/internal/transcode"`。把第 57 行的 `stream := v1.NewStreamHandler(...)` 与第 87-89 行的 `subStream := ...` 改为共享一个 Service 和一个 StreamHandler。

将 `r.Route("/api/v1", ...)` 内第 57 行：
```go
		stream := v1.NewStreamHandler(db, cfg.Transcode, cfg.Cache.TranscodeDir)
		r.Get("/tracks/{id}/stream", stream.Stream)
```
改为：
```go
		r.Get("/tracks/{id}/stream", streamH.Stream)
```
并在 `NewRouter` 函数体顶部（`r := chi.NewRouter()` 之后、`r.Route("/api/v1", ...)` 之前）构造共享实例：
```go
	tcache := transcode.NewCache(cfg.Cache.TranscodeDir, cfg.Cache.TranscodeMaxSizeMB)
	tsvc := transcode.NewService(cfg.Transcode.FFmpegPath, cfg.Transcode.DefaultBitrate, tcache)
	streamH := v1.NewStreamHandler(db, tsvc)
```
再把第 87-90 行的 subsonic 装配改为复用 `streamH`：
```go
	subCover := v1.NewCoverHandler(db)
	subHandler := subsonic.NewHandler(db, cfg, streamH, subCover)
	r.Route("/rest", subHandler.RegisterRoutes)
```
（删除原 `subStream := v1.NewStreamHandler(...)` 行。）

- [ ] **Step 5: 改 stream_test.go** — `internal/api/v1/stream_test.go` 全文替换为（适配新签名、默认直传行为，转码用假 ffmpeg + `?format=mp3`）：
```go
// internal/api/v1/stream_test.go
package v1

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yxx-z/lyra/internal/transcode"
)

func newStreamHandler(t *testing.T, ffmpegPath string) (*StreamHandler, *sql.DB) {
	t.Helper()
	d := newTestDB(t)
	cache := transcode.NewCache(t.TempDir(), 0)
	svc := transcode.NewService(ffmpegPath, 192, cache)
	return NewStreamHandler(d, svc), d
}

func TestStream_PassthroughMP3(t *testing.T) {
	h, d := newStreamHandler(t, "ffmpeg")
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.mp3")
	if err := os.WriteFile(audioFile, []byte("ORIGINALMP3"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','B','a1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t1','T','a1','al1',?,'mp3',1,'pending')`, audioFile); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream", nil)
	h.StreamByID(w, req, "t1")
	if w.Code != http.StatusOK || w.Body.String() != "ORIGINALMP3" {
		t.Fatalf("直传应原样返回: %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type=%q", ct)
	}
}

func TestStream_TranscodeOnFormatParam(t *testing.T) {
	// 假 ffmpeg：最后一参为 pipe:1 写 stdout，否则写文件
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\nout=\"\"\nfor a in \"$@\"; do out=\"$a\"; done\nif [ \"$out\" = \"pipe:1\" ]; then printf MP3DATA; else printf MP3DATA > \"$out\"; fi\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	h, d := newStreamHandler(t, ffmpeg)
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.flac")
	if err := os.WriteFile(audioFile, []byte("FLACDATA"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','B','a1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t1','T','a1','al1',?,'flac',1,'pending')`, audioFile); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream?format=mp3", nil)
	h.StreamByID(w, req, "t1")
	if w.Code != http.StatusOK || w.Body.String() != "MP3DATA" {
		t.Fatalf("转码应返回 ffmpeg 输出: %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type=%q", ct)
	}
}

func TestStream_NotFound(t *testing.T) {
	h, _ := newStreamHandler(t, "ffmpeg")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/nope/stream", nil)
	h.StreamByID(w, req, "nope")
	if w.Code != http.StatusNotFound {
		t.Errorf("不存在曲目应 404，得到 %d", w.Code)
	}
}
```
注意：`newTestDB` 来自现有 `testhelpers_test.go`（同包），无需重新定义。

- [ ] **Step 6: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./... 2>&1 | tail -20`
Expected: build 成功；`internal/transcode`、`internal/api/v1`、`internal/api`、`internal/api/subsonic` 及全仓库测试 PASS。

- [ ] **Step 7: 提交**
```bash
git add internal/config/config.go config.example.yaml internal/api/v1/stream.go internal/api/v1/stream_test.go internal/api/router.go
git commit -m "feat(transcode): 接入 v1/subsonic/router（共享 Service）+ 缓存上限配置，移除旧 v1 转码缓存"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## 完成标准

- `go build ./...` 成功；`go test ./...` 全绿
- Subsonic `stream` 支持 `maxBitRate`/`format`/`raw`；默认直传，FLAC 不再无谓转 mp3；绝不升码率
- 转码边转边播（首字节即出），客户端断开杀 ffmpeg
- 转码缓存超 `transcode_max_size_mb` 时 LRU 回收
- 支持 mp3/opus/aac 输出
- v1 与 subsonic 共享一个 transcode.Service（单一缓存/锁）
- 全部测试不依赖真 ffmpeg、不打网络

## 验证（手动，docker）

1. `make docker-build && docker compose up -d`
2. `curl -s "http://127.0.0.1:4533/rest/stream.view?u=admin&p=admin&id=<flac曲目id>&format=raw" -o /tmp/raw.flac` → 得原始 FLAC
3. `curl -s "http://127.0.0.1:4533/rest/stream.view?u=admin&p=admin&id=<id>&format=opus&maxBitRate=96" -o /tmp/o.opus` → 得 opus
4. Symfonium 设置里把"流式格式/码率"调成 opus 96k，验证播放正常、首播无明显等待
