# 文件扫描器实现计划

> **给 AI 工作者：** 必须使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务执行本计划。步骤使用复选框（`- [ ]`）语法追踪进度。

**目标：** 实现 Lyra 音乐库文件扫描器，支持启动自动扫描、HTTP 手动触发、fsnotify 实时监听，处理混合目录结构（内嵌标签 → 目录推断 → 文件名兜底），写入 SQLite。

**架构：** `internal/scanner/` 包含 5 个职责单一的文件。Walker 遍历目录输出路径到 channel，4 个 worker goroutine 并发读取标签，单个 ingester goroutine 串行写 DB（避免 SQLite 写锁争用）。Scanner 主结构体协调所有组件，对外暴露 `Start/TriggerScan/Status/Stop` 四个方法。`NewRouter` 接受 `*scanner.Scanner` 参数，router 注册 `/api/v1/library/scan` 端点。

**技术栈：** Go 1.25+、`github.com/dhowden/tag`（标签读取）、`github.com/fsnotify/fsnotify`（文件监听）、`github.com/google/uuid`（已在 go.sum）、modernc SQLite（现有）

---

## 文件结构

```
internal/scanner/
├── scanner.go              Scanner 结构体，Start/TriggerScan/Status/Stop
├── scanner_test.go
├── walker.go               Walk(ctx, roots) → <-chan string
├── walker_test.go
├── tag_reader.go           IsAudioFile, TrackMeta, inferFromPath, Read
├── tag_reader_test.go
├── ingester.go             Ingester，findOrCreateArtist/Album，upsertTrack，MarkUnavailable
├── ingester_test.go
├── watcher.go              startWatcher，fsnotify 事件循环，去抖
├── watcher_test.go
└── testdata/
    └── .gitkeep

internal/api/
├── router.go               NewRouter(*scanner.Scanner)    ← 签名变更
├── router_test.go          ← 更新以适应新签名
└── v1/
    ├── library.go          LibraryHandler，TriggerScan，ScanStatus
    └── library_test.go

internal/db/
├── migrations/002_tracks_availability.up.sql   ← 新增
└── schema.sql                                  ← 更新

cmd/server/main.go          ← 注入 scanner
```

---

## 前置条件

```bash
export PATH=$PATH:/home/yxx/go-local/go/bin
cd /home/yxx/develop/Lyra
```

---

## 任务 1：DB 迁移 — is_available 字段

**文件：**
- 创建：`internal/db/migrations/002_tracks_availability.up.sql`
- 修改：`internal/db/schema.sql`
- 修改：`internal/db/db_test.go`

- [ ] **步骤 1：写失败测试**

在 `internal/db/db_test.go` 末尾追加：

```go
func TestOpen_TracksHasIsAvailableColumn(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// 若列不存在，INSERT 会失败
	_, err = db.Exec(
		`INSERT INTO tracks(id,title,file_path,is_available) VALUES('x','t','p',1)`,
	)
	if err != nil {
		t.Errorf("is_available 列不存在: %v", err)
	}
}
```

- [ ] **步骤 2：运行测试 —— 确认失败**

```bash
go test ./internal/db/... -run TestOpen_TracksHasIsAvailableColumn -v
```

预期：FAIL（`is_available` 列不存在）

- [ ] **步骤 3：创建迁移文件**

```sql
-- internal/db/migrations/002_tracks_availability.up.sql
ALTER TABLE tracks ADD COLUMN is_available INTEGER NOT NULL DEFAULT 1;
CREATE INDEX idx_tracks_available ON tracks(is_available);
```

- [ ] **步骤 4：更新 schema.sql**

在 `internal/db/schema.sql` 的 `tracks` 表中，找到 `last_played_at DATETIME,` 行，在其后（`created_at` 行之前）插入：

```sql
    is_available   INTEGER NOT NULL DEFAULT 1,
```

- [ ] **步骤 5：运行测试 —— 确认通过**

```bash
go test ./internal/db/... -v
```

预期：3 个测试全部 PASS

- [ ] **步骤 6：提交**

```bash
git add internal/db/
git commit -m "feat: 添加 tracks.is_available 字段迁移"
```

---

## 任务 2：安装新依赖

**文件：** `go.mod`、`go.sum`

- [ ] **步骤 1：安装依赖**

```bash
go get github.com/dhowden/tag@latest
go get github.com/fsnotify/fsnotify@latest
go get github.com/google/uuid@latest
go mod tidy
```

- [ ] **步骤 2：验证编译**

```bash
go build ./...
```

预期：编译成功，无错误

- [ ] **步骤 3：提交**

```bash
git add go.mod go.sum
git commit -m "chore: 添加 dhowden/tag、fsnotify、uuid 依赖"
```

---

## 任务 3：tag_reader — 元数据提取

**文件：**
- 创建：`internal/scanner/tag_reader.go`
- 创建：`internal/scanner/tag_reader_test.go`

- [ ] **步骤 1：写失败测试**

```go
// internal/scanner/tag_reader_test.go
package scanner

import (
	"path/filepath"
	"testing"
)

func TestIsAudioFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"song.mp3", true},
		{"song.FLAC", true},
		{"song.m4a", true},
		{"song.ogg", true},
		{"song.opus", true},
		{"song.wav", true},
		{"song.aiff", true},
		{"song.wma", true},
		{"song.txt", false},
		{"song.jpg", false},
		{"song.mp4", false},
	}
	for _, c := range cases {
		if got := IsAudioFile(c.path); got != c.want {
			t.Errorf("IsAudioFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestParseArtistAlbum(t *testing.T) {
	cases := []struct {
		input      string
		wantArtist string
		wantAlbum  string
	}{
		{
			"蔡琴 - 金片子 贰・魂萦旧梦 (2015) - WEB-DL - 16bit ALAC-HHWEB",
			"蔡琴",
			"金片子 贰・魂萦旧梦",
		},
		{
			"周杰伦 - 叶惠美 (2003)",
			"周杰伦",
			"叶惠美",
		},
		{
			"The Beatles - Abbey Road (1969) [FLAC]",
			"The Beatles",
			"Abbey Road",
		},
		{
			"NoSeparatorAlbum",
			"",
			"NoSeparatorAlbum",
		},
		{
			"周杰伦 - 七里香 (2004) - FLAC 24bit 96kHz",
			"周杰伦",
			"七里香",
		},
	}
	for _, c := range cases {
		gotArtist, gotAlbum := parseArtistAlbum(c.input)
		if gotArtist != c.wantArtist {
			t.Errorf("parseArtistAlbum(%q).artist = %q, want %q", c.input, gotArtist, c.wantArtist)
		}
		if gotAlbum != c.wantAlbum {
			t.Errorf("parseArtistAlbum(%q).album = %q, want %q", c.input, gotAlbum, c.wantAlbum)
		}
	}
}

func TestParseTrackNumber(t *testing.T) {
	cases := []struct {
		filename string
		want     int
	}{
		{"01. 渡口.flac", 1},
		{"02 - 被遗忘的时光.flac", 2},
		{"10_天涯.mp3", 10},
		{"无编号.flac", 0},
	}
	for _, c := range cases {
		if got := parseTrackNumber(c.filename); got != c.want {
			t.Errorf("parseTrackNumber(%q) = %d, want %d", c.filename, got, c.want)
		}
	}
}

func TestInferFromPath_DoubleLayer(t *testing.T) {
	// /music/蔡琴/金片子/01.flac → artist=蔡琴, album=金片子
	meta := TrackMeta{FilePath: filepath.Join("/music", "蔡琴", "金片子", "01.flac")}
	result := inferFromPath(meta, []string{"/music"})
	if result.Artist != "蔡琴" {
		t.Errorf("artist = %q, want 蔡琴", result.Artist)
	}
	if result.Album != "金片子" {
		t.Errorf("album = %q, want 金片子", result.Album)
	}
}

func TestInferFromPath_SingleLayerWithFormat(t *testing.T) {
	// /music/蔡琴 - 金片子 (2015) - WEB-DL/01.flac → artist=蔡琴, album=金片子
	meta := TrackMeta{FilePath: filepath.Join("/music", "蔡琴 - 金片子 (2015) - WEB-DL", "01.flac")}
	result := inferFromPath(meta, []string{"/music"})
	if result.Artist != "蔡琴" {
		t.Errorf("artist = %q, want 蔡琴", result.Artist)
	}
	if result.Album != "金片子" {
		t.Errorf("album = %q, want 金片子", result.Album)
	}
}

func TestInferFromPath_SingleLayerNoSeparator(t *testing.T) {
	// /music/杂项/song.flac → artist 空，album=杂项
	meta := TrackMeta{FilePath: filepath.Join("/music", "杂项", "song.flac")}
	result := inferFromPath(meta, []string{"/music"})
	if result.Artist != "" {
		t.Errorf("artist = %q, want empty", result.Artist)
	}
	if result.Album != "杂项" {
		t.Errorf("album = %q, want 杂项", result.Album)
	}
}

func TestInferFromPath_DirectlyInRoot(t *testing.T) {
	// /music/song.flac → 无目录推断
	meta := TrackMeta{FilePath: filepath.Join("/music", "song.flac")}
	result := inferFromPath(meta, []string{"/music"})
	if result.Artist != "" {
		t.Errorf("artist should be empty for root-level file, got %q", result.Artist)
	}
	if result.Album != "" {
		t.Errorf("album should be empty for root-level file, got %q", result.Album)
	}
}
```

- [ ] **步骤 2：运行测试 —— 确认失败**

```bash
go test ./internal/scanner/... -v
```

预期：FAIL（`IsAudioFile`、`parseArtistAlbum` 等未定义）

- [ ] **步骤 3：实现 tag_reader.go**

```go
// internal/scanner/tag_reader.go
package scanner

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true,
	".ogg": true, ".opus": true, ".wav": true,
	".aiff": true, ".aif": true, ".wma": true,
}

var (
	reYear    = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)
	reFormat  = regexp.MustCompile(`(?i)\s*-\s*(WEB|FLAC|MP3|AAC|ALAC|DL|CDRip|320k|16bit|24bit|SACD|HDCD).*$`)
	reBracket = regexp.MustCompile(`\s*\[.*?\]\s*$`)
	reTrackNo = regexp.MustCompile(`^(\d+)[\.\s\-_]+`)
)

// TrackMeta holds normalised metadata for a single audio file.
type TrackMeta struct {
	FilePath    string
	FileSize    int64
	Format      string
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	TrackNumber int
	DiscNumber  int
	Year        int
	Genre       string
	Duration    int // seconds
	Bitrate     int // kbps
	SampleRate  int // Hz
	Channels    int
}

// IsAudioFile reports whether path has a supported audio extension.
func IsAudioFile(path string) bool {
	return audioExts[strings.ToLower(filepath.Ext(path))]
}

// Read extracts metadata from path, falling back to path inference then defaults.
func Read(path string, libraryPaths []string) (TrackMeta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return TrackMeta{}, err
	}

	ext := strings.ToLower(filepath.Ext(path))
	meta := TrackMeta{
		FilePath: path,
		FileSize: info.Size(),
		Format:   strings.TrimPrefix(ext, "."),
	}

	// Priority 1: embedded tags
	if f, err := os.Open(path); err == nil {
		if t, err := tag.ReadFrom(f); err == nil {
			meta.Title = t.Title()
			meta.Artist = t.Artist()
			meta.AlbumArtist = t.AlbumArtist()
			meta.Album = t.Album()
			meta.Year = t.Year()
			meta.Genre = t.Genre()
			n, _ := t.Track()
			meta.TrackNumber = n
			d, _ := t.Disc()
			meta.DiscNumber = d
		}
		f.Close()
	}

	// Priority 2 & 3: path and filename inference for missing fields
	meta = inferFromPath(meta, libraryPaths)

	// Priority 4: final defaults
	base := filepath.Base(path)
	nameNoExt := strings.TrimSuffix(base, filepath.Ext(base))
	if meta.Title == "" {
		title := reTrackNo.ReplaceAllString(nameNoExt, "")
		meta.Title = strings.TrimSpace(title)
		if meta.Title == "" {
			meta.Title = nameNoExt
		}
	}
	if meta.Artist == "" {
		meta.Artist = "未知艺术家"
	}
	if meta.Album == "" {
		meta.Album = "未知专辑"
	}

	return meta, nil
}

// inferFromPath fills missing Artist/Album/TrackNumber using directory structure.
func inferFromPath(meta TrackMeta, libraryPaths []string) TrackMeta {
	dir := filepath.Dir(meta.FilePath)
	depth := dirDepth(dir, libraryPaths)

	switch {
	case depth >= 2:
		// /lib/Artist/Album/track → grandparent=artist, parent=album
		parent := filepath.Base(dir)
		grandparent := filepath.Base(filepath.Dir(dir))
		if meta.Artist == "" {
			meta.Artist = grandparent
		}
		if meta.Album == "" {
			meta.Album = parent
		}
	case depth == 1:
		// /lib/FolderName/track → try parse "Artist - Album" from folder
		parent := filepath.Base(dir)
		artist, album := parseArtistAlbum(parent)
		if meta.Artist == "" {
			meta.Artist = artist
		}
		if meta.Album == "" {
			meta.Album = album
		}
	}

	// Filename: extract track number if missing
	if meta.TrackNumber == 0 {
		meta.TrackNumber = parseTrackNumber(filepath.Base(meta.FilePath))
	}

	return meta
}

// dirDepth returns how many directory levels dir is below one of the library roots.
// Returns -1 if dir is not under any library root.
func dirDepth(dir string, roots []string) int {
	for _, root := range roots {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			continue
		}
		if rel == "." {
			return 0
		}
		if strings.HasPrefix(rel, "..") {
			continue
		}
		return len(strings.Split(rel, string(filepath.Separator)))
	}
	return -1
}

// parseArtistAlbum splits a folder name like "Artist - Album (Year) - Format"
// into artist and cleaned album. Returns ("", folderName) if no " - " found.
func parseArtistAlbum(name string) (artist, album string) {
	idx := strings.Index(name, " - ")
	if idx <= 0 {
		return "", cleanAlbumName(name)
	}
	artist = strings.TrimSpace(name[:idx])
	album = cleanAlbumName(strings.TrimSpace(name[idx+3:]))
	if album == "" {
		return "", cleanAlbumName(name)
	}
	return
}

func cleanAlbumName(s string) string {
	s = reFormat.ReplaceAllString(s, "")
	s = reYear.ReplaceAllString(s, "")
	s = reBracket.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func parseTrackNumber(filename string) int {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	m := reTrackNo.FindStringSubmatch(name)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
```

- [ ] **步骤 4：运行测试 —— 确认通过**

```bash
go test ./internal/scanner/... -v -run "TestIsAudio|TestParse|TestInfer"
```

预期：7 个测试全部 PASS

- [ ] **步骤 5：提交**

```bash
git add internal/scanner/tag_reader.go internal/scanner/tag_reader_test.go
git commit -m "feat: tag_reader — 标签读取、路径推断、文件名解析"
```

---

## 任务 4：walker — 目录遍历

**文件：**
- 创建：`internal/scanner/walker.go`
- 创建：`internal/scanner/walker_test.go`

- [ ] **步骤 1：写失败测试**

```go
// internal/scanner/walker_test.go
package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestWalk_FindsAudioFiles(t *testing.T) {
	dir := t.TempDir()
	// 创建混合文件
	files := map[string]bool{
		"song.mp3":         true,
		"album/track.flac": true,
		"image.jpg":        false,
		"readme.txt":       false,
		"sub/deep/a.ogg":   true,
	}
	for name := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte{}, 0644)
	}

	ctx := context.Background()
	ch := Walk(ctx, []string{dir})

	found := map[string]bool{}
	for p := range ch {
		rel, _ := filepath.Rel(dir, p)
		found[filepath.ToSlash(rel)] = true
	}

	for name, wantFound := range files {
		if found[name] != wantFound {
			t.Errorf("Walk(%q): found=%v, want=%v", name, found[name], wantFound)
		}
	}
}

func TestWalk_MultipleRoots(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir1, "a.mp3"), []byte{}, 0644)
	os.WriteFile(filepath.Join(dir2, "b.flac"), []byte{}, 0644)

	ctx := context.Background()
	var paths []string
	for p := range Walk(ctx, []string{dir1, dir2}) {
		paths = append(paths, filepath.Base(p))
	}
	sort.Strings(paths)
	if len(paths) != 2 || paths[0] != "a.mp3" || paths[1] != "b.flac" {
		t.Errorf("got %v", paths)
	}
}

func TestWalk_ContextCancel(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 100; i++ {
		os.WriteFile(filepath.Join(dir, filepath.FromSlash(
			filepath.Join("sub", "sub2", "sub3", fmt.Sprintf("%d.mp3", i)),
		)), []byte{}, 0644)
	}
	os.MkdirAll(filepath.Join(dir, "sub", "sub2", "sub3"), 0755)

	ctx, cancel := context.WithCancel(context.Background())
	ch := Walk(ctx, []string{dir})
	// 读取一个后取消
	<-ch
	cancel()
	// 排空 channel，确保不 hang
	for range ch {
	}
}
```

Note: add `"fmt"` to imports.

- [ ] **步骤 2：运行测试 —— 确认失败**

```bash
go test ./internal/scanner/... -run "TestWalk" -v
```

预期：FAIL（`Walk` 未定义）

- [ ] **步骤 3：实现 walker.go**

```go
// internal/scanner/walker.go
package scanner

import (
	"context"
	"io/fs"
	"path/filepath"
)

// Walk recursively finds audio files under each root.
// The returned channel is closed when all roots are exhausted or ctx is cancelled.
func Walk(ctx context.Context, roots []string) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		for _, root := range roots {
			filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if !IsAudioFile(path) {
					return nil
				}
				select {
				case ch <- path:
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			})
			if ctx.Err() != nil {
				return
			}
		}
	}()
	return ch
}
```

- [ ] **步骤 4：运行测试 —— 确认通过**

```bash
go test ./internal/scanner/... -run "TestWalk" -v
```

预期：3 个测试全部 PASS

- [ ] **步骤 6：提交**

```bash
git add internal/scanner/walker.go internal/scanner/walker_test.go
git commit -m "feat: walker — 递归音频文件遍历，支持 context 取消"
```

---

## 任务 5：ingester — DB 写入与去重

**文件：**
- 创建：`internal/scanner/ingester.go`
- 创建：`internal/scanner/ingester_test.go`

- [ ] **步骤 1：写失败测试**

```go
// internal/scanner/ingester_test.go
package scanner

import (
	"database/sql"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestIngest_CreatesArtistAlbumTrack(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)

	meta := TrackMeta{
		FilePath: "/music/蔡琴/金片子/01.flac",
		FileSize: 10000,
		Format:   "flac",
		Title:    "渡口",
		Artist:   "蔡琴",
		Album:    "金片子",
		Year:     1984,
	}
	if err := ing.Ingest(meta); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	var trackCount, artistCount, albumCount int
	d.QueryRow(`SELECT count(*) FROM tracks`).Scan(&trackCount)
	d.QueryRow(`SELECT count(*) FROM artists`).Scan(&artistCount)
	d.QueryRow(`SELECT count(*) FROM albums`).Scan(&albumCount)

	if trackCount != 1 {
		t.Errorf("tracks: want 1, got %d", trackCount)
	}
	if artistCount != 1 {
		t.Errorf("artists: want 1, got %d", artistCount)
	}
	if albumCount != 1 {
		t.Errorf("albums: want 1, got %d", albumCount)
	}
}

func TestIngest_Dedup_SameArtistCaseInsensitive(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)

	m1 := TrackMeta{FilePath: "/music/a.flac", Title: "A", Artist: "蔡琴", Album: "X"}
	m2 := TrackMeta{FilePath: "/music/b.flac", Title: "B", Artist: "蔡 琴 ", Album: "X"} // 空格变体
	ing.Ingest(m1)
	ing.Ingest(m2)

	var count int
	d.QueryRow(`SELECT count(*) FROM artists`).Scan(&count)
	if count != 1 {
		t.Errorf("artists: want 1 (deduped), got %d", count)
	}
}

func TestIngest_Upsert_SameFilePath(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)

	m := TrackMeta{FilePath: "/music/a.flac", Title: "旧标题", Artist: "A", Album: "B"}
	ing.Ingest(m)

	m.Title = "新标题"
	if err := ing.Ingest(m); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	var count int
	d.QueryRow(`SELECT count(*) FROM tracks`).Scan(&count)
	if count != 1 {
		t.Errorf("tracks: want 1 (upserted), got %d", count)
	}

	var title string
	d.QueryRow(`SELECT title FROM tracks WHERE file_path=?`, m.FilePath).Scan(&title)
	if title != "新标题" {
		t.Errorf("title: want 新标题, got %q", title)
	}
}

func TestMarkUnavailable(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)

	m := TrackMeta{FilePath: "/music/a.flac", Title: "T", Artist: "A", Album: "B"}
	ing.Ingest(m)

	if err := ing.MarkUnavailable("/music/a.flac"); err != nil {
		t.Fatalf("MarkUnavailable: %v", err)
	}

	var avail int
	d.QueryRow(`SELECT is_available FROM tracks WHERE file_path=?`, m.FilePath).Scan(&avail)
	if avail != 0 {
		t.Errorf("is_available: want 0, got %d", avail)
	}
}
```

在文件顶部 import 块加 `"database/sql"`。

- [ ] **步骤 2：运行测试 —— 确认失败**

```bash
go test ./internal/scanner/... -run "TestIngest|TestMark" -v
```

预期：FAIL（`NewIngester` 未定义）

- [ ] **步骤 3：实现 ingester.go**

```go
// internal/scanner/ingester.go
package scanner

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Ingester writes TrackMeta records to the database.
type Ingester struct {
	db *sql.DB
}

// NewIngester creates an Ingester backed by db.
func NewIngester(db *sql.DB) *Ingester {
	return &Ingester{db: db}
}

// Ingest upserts artist, album, and track for the given metadata.
func (ing *Ingester) Ingest(meta TrackMeta) error {
	trackArtistID, err := ing.findOrCreateArtist(meta.Artist)
	if err != nil {
		return fmt.Errorf("artist: %w", err)
	}

	albumArtistName := meta.AlbumArtist
	if albumArtistName == "" {
		albumArtistName = meta.Artist
	}
	albumArtistID, err := ing.findOrCreateArtist(albumArtistName)
	if err != nil {
		return fmt.Errorf("album artist: %w", err)
	}

	albumID, err := ing.findOrCreateAlbum(meta.Album, albumArtistID, meta.Year)
	if err != nil {
		return fmt.Errorf("album: %w", err)
	}

	return ing.upsertTrack(meta, trackArtistID, albumID)
}

// MarkUnavailable sets is_available=0 for the track at filePath.
func (ing *Ingester) MarkUnavailable(filePath string) error {
	_, err := ing.db.Exec(
		`UPDATE tracks SET is_available=0, updated_at=? WHERE file_path=?`,
		time.Now(), filePath,
	)
	return err
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (ing *Ingester) findOrCreateArtist(name string) (string, error) {
	var id string
	err := ing.db.QueryRow(
		`SELECT id FROM artists WHERE lower(trim(name))=?`, normalize(name),
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = uuid.New().String()
	now := time.Now()
	_, err = ing.db.Exec(
		`INSERT INTO artists(id,name,created_at,updated_at) VALUES(?,?,?,?)`,
		id, name, now, now,
	)
	return id, err
}

func (ing *Ingester) findOrCreateAlbum(title, artistID string, year int) (string, error) {
	var id string
	err := ing.db.QueryRow(
		`SELECT id FROM albums WHERE lower(trim(title))=? AND artist_id=?`,
		normalize(title), artistID,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = uuid.New().String()
	releaseDate := ""
	if year > 0 {
		releaseDate = fmt.Sprintf("%d", year)
	}
	now := time.Now()
	_, err = ing.db.Exec(
		`INSERT INTO albums(id,title,artist_id,release_date,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		id, title, artistID, releaseDate, now, now,
	)
	return id, err
}

func (ing *Ingester) upsertTrack(meta TrackMeta, artistID, albumID string) error {
	now := time.Now()
	_, err := ing.db.Exec(`
		INSERT INTO tracks(
			id,title,artist_id,album_id,track_number,disc_number,
			duration,file_path,file_size,format,bitrate,sample_rate,
			channels,scrape_status,is_available,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'pending',1,?,?)
		ON CONFLICT(file_path) DO UPDATE SET
			title=excluded.title,
			artist_id=excluded.artist_id,
			album_id=excluded.album_id,
			track_number=excluded.track_number,
			disc_number=excluded.disc_number,
			duration=excluded.duration,
			file_size=excluded.file_size,
			format=excluded.format,
			bitrate=excluded.bitrate,
			sample_rate=excluded.sample_rate,
			channels=excluded.channels,
			is_available=1,
			updated_at=excluded.updated_at`,
		uuid.New().String(),
		meta.Title, artistID, albumID,
		meta.TrackNumber, meta.DiscNumber,
		meta.Duration, meta.FilePath, meta.FileSize, meta.Format,
		meta.Bitrate, meta.SampleRate, meta.Channels,
		now, now,
	)
	return err
}
```

- [ ] **步骤 4：运行测试 —— 确认通过**

```bash
go test ./internal/scanner/... -run "TestIngest|TestMark" -v
```

预期：4 个测试全部 PASS

- [ ] **步骤 5：提交**

```bash
git add internal/scanner/ingester.go internal/scanner/ingester_test.go
git commit -m "feat: ingester — artist/album/track 去重写入，支持 upsert"
```

---

## 任务 6：scanner — 主协调器

**文件：**
- 创建：`internal/scanner/scanner.go`
- 创建：`internal/scanner/scanner_test.go`

- [ ] **步骤 1：写失败测试**

```go
// internal/scanner/scanner_test.go
package scanner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
)

func newTestScanner(t *testing.T, paths []string) *Scanner {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return NewScanner(d, config.LibraryConfig{Paths: paths})
}

func TestNewScanner_NotRunning(t *testing.T) {
	s := newTestScanner(t, nil)
	defer s.Stop()
	if s.Status().Running {
		t.Error("新建的 Scanner 不应处于运行状态")
	}
}

func TestTriggerScan_ReturnsBusyError(t *testing.T) {
	s := newTestScanner(t, nil)
	defer s.Stop()

	// 直接将 running 置为 true，避免依赖时序
	s.running.Store(true)
	defer s.running.Store(false)

	if err := s.TriggerScan(); !errors.Is(err, ErrScanInProgress) {
		t.Errorf("want ErrScanInProgress, got %v", err)
	}
}

func TestTriggerScan_SetsTotal(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.mp3", i)), []byte{}, 0644)
	}
	// 非音频文件不计入
	os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte{}, 0644)

	s := newTestScanner(t, []string{dir})
	defer s.Stop()
	s.TriggerScan()

	// 等待扫描完成（最多 3 秒）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !s.Status().Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := s.Status()
	if st.Total != 5 {
		t.Errorf("Total = %d, want 5", st.Total)
	}
	// 空文件读取标签会失败，计入 Errors；总数仍要正确
	if st.Total != st.Processed+st.Errors {
		t.Errorf("Total(%d) != Processed(%d)+Errors(%d)", st.Total, st.Processed, st.Errors)
	}
}

func TestStop_DoesNotHang(t *testing.T) {
	s := newTestScanner(t, nil)
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Stop() 超时")
	}
}
```

在文件顶部加 `"fmt"` 到 import。

- [ ] **步骤 2：运行测试 —— 确认失败**

```bash
go test ./internal/scanner/... -run "TestNewScanner|TestTriggerScan|TestStop" -v
```

预期：FAIL（`NewScanner` 未定义）

- [ ] **步骤 3：实现 scanner.go**

```go
// internal/scanner/scanner.go
package scanner

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yxx-z/lyra/internal/config"
)

// ErrScanInProgress is returned by TriggerScan when a scan is already running.
var ErrScanInProgress = errors.New("扫描正在进行中")

// ScanStatus is a point-in-time snapshot of scanner progress.
type ScanStatus struct {
	Running   bool      `json:"running"`
	Total     int64     `json:"total"`
	Processed int64     `json:"processed"`
	Errors    int64     `json:"errors"`
	StartedAt time.Time `json:"started_at"`
}

// Scanner orchestrates directory walking, tag reading, and DB ingestion.
type Scanner struct {
	db  *sql.DB
	cfg config.LibraryConfig
	ing *Ingester

	running   atomic.Bool
	total     atomic.Int64
	processed atomic.Int64
	errors    atomic.Int64

	mu        sync.RWMutex
	startedAt time.Time

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewScanner creates a Scanner. Call Start to begin scanning.
func NewScanner(db *sql.DB, cfg config.LibraryConfig) *Scanner {
	return &Scanner{
		db:     db,
		cfg:    cfg,
		ing:    NewIngester(db),
		stopCh: make(chan struct{}),
	}
}

// Start begins an initial full scan (if paths configured) and starts fsnotify watcher (if cfg.Watch).
func (s *Scanner) Start() error {
	if len(s.cfg.Paths) > 0 {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runScan()
		}()
	}
	if s.cfg.Watch && len(s.cfg.Paths) > 0 {
		if err := startWatcher(s); err != nil {
			// watcher failure is non-fatal — log and continue
			_ = err
		}
	}
	return nil
}

// TriggerScan starts a full scan in the background. Returns ErrScanInProgress if already running.
func (s *Scanner) TriggerScan() error {
	if s.running.Load() {
		return ErrScanInProgress
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runScan()
	}()
	return nil
}

// Status returns a snapshot of current scan progress.
func (s *Scanner) Status() ScanStatus {
	s.mu.RLock()
	startedAt := s.startedAt
	s.mu.RUnlock()
	return ScanStatus{
		Running:   s.running.Load(),
		Total:     s.total.Load(),
		Processed: s.processed.Load(),
		Errors:    s.errors.Load(),
		StartedAt: startedAt,
	}
}

// Stop signals the scanner to halt and waits for all goroutines to exit.
func (s *Scanner) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *Scanner) runScan() {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)

	s.total.Store(0)
	s.processed.Store(0)
	s.errors.Store(0)
	s.mu.Lock()
	s.startedAt = time.Now()
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel context when stop is requested
	go func() {
		select {
		case <-s.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	paths := Walk(ctx, s.cfg.Paths)

	type result struct {
		meta TrackMeta
		err  error
	}
	results := make(chan result, 8)

	const numWorkers = 4
	var workerWg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for path := range paths {
				s.total.Add(1)
				meta, err := Read(path, s.cfg.Paths)
				results <- result{meta, err}
			}
		}()
	}

	go func() {
		workerWg.Wait()
		close(results)
	}()

	for r := range results {
		if r.err != nil {
			s.errors.Add(1)
			continue
		}
		if err := s.ing.Ingest(r.meta); err != nil {
			s.errors.Add(1)
		} else {
			s.processed.Add(1)
		}
	}
}
```

- [ ] **步骤 4：运行测试（含 race detector）**

```bash
go test ./internal/scanner/... -race -run "TestNewScanner|TestTriggerScan|TestStop" -v
```

预期：4 个测试全部 PASS，无 race 报告

- [ ] **步骤 5：提交**

```bash
git add internal/scanner/scanner.go internal/scanner/scanner_test.go
git commit -m "feat: scanner — 协调器，Start/TriggerScan/Status/Stop"
```

---

## 任务 7：watcher — fsnotify 实时监听

**文件：**
- 创建：`internal/scanner/watcher.go`
- 创建：`internal/scanner/watcher_test.go`

- [ ] **步骤 1：写失败测试**

```go
// internal/scanner/watcher_test.go
package scanner

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDebounce_MultipleEventsCollapse(t *testing.T) {
	var callCount atomic.Int32
	fn := func() { callCount.Add(1) }

	d := newDebouncer(100 * time.Millisecond)
	// 触发 5 次，应该只执行一次
	for i := 0; i < 5; i++ {
		d.trigger("key", fn)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)

	if callCount.Load() != 1 {
		t.Errorf("callCount = %d, want 1", callCount.Load())
	}
}

func TestDebounce_DifferentKeysIndependent(t *testing.T) {
	var countA, countB atomic.Int32
	d := newDebouncer(50 * time.Millisecond)
	d.trigger("a", func() { countA.Add(1) })
	d.trigger("b", func() { countB.Add(1) })
	time.Sleep(150 * time.Millisecond)

	if countA.Load() != 1 || countB.Load() != 1 {
		t.Errorf("a=%d b=%d, want 1 1", countA.Load(), countB.Load())
	}
}
```

- [ ] **步骤 2：运行测试 —— 确认失败**

```bash
go test ./internal/scanner/... -run "TestDebounce" -v
```

预期：FAIL（`newDebouncer` 未定义）

- [ ] **步骤 3：实现 watcher.go**

```go
// internal/scanner/watcher.go
package scanner

import (
	"io/fs"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debouncer coalesces rapid-fire events for the same key into a single call.
type debouncer struct {
	delay  time.Duration
	timers map[string]*time.Timer
	mu     sync.Mutex
}

func newDebouncer(delay time.Duration) *debouncer {
	return &debouncer{delay: delay, timers: make(map[string]*time.Timer)}
}

func (d *debouncer) trigger(key string, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[key]; ok {
		t.Reset(d.delay)
		return
	}
	d.timers[key] = time.AfterFunc(d.delay, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()
		fn()
	})
}

func startWatcher(s *Scanner) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, p := range s.cfg.Paths {
		if err := addDirsRecursive(w, p); err != nil {
			w.Close()
			return err
		}
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer w.Close()
		runWatchLoop(s, w)
	}()
	return nil
}

func addDirsRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		return w.Add(path)
	})
}

func runWatchLoop(s *Scanner, w *fsnotify.Watcher) {
	db := newDebouncer(500 * time.Millisecond)
	for {
		select {
		case <-s.stopCh:
			return
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			handleFSEvent(s, db, event)
		case <-w.Errors:
			// non-fatal; continue
		}
	}
}

func handleFSEvent(s *Scanner, db *debouncer, event fsnotify.Event) {
	path := event.Name
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		if IsAudioFile(path) {
			s.ing.MarkUnavailable(path)
		}
		return
	}
	if !IsAudioFile(path) {
		return
	}
	db.trigger(path, func() {
		meta, err := Read(path, s.cfg.Paths)
		if err != nil {
			return
		}
		s.ing.Ingest(meta)
	})
}
```

- [ ] **步骤 4：运行测试 —— 确认通过**

```bash
go test ./internal/scanner/... -run "TestDebounce" -v
```

预期：2 个测试全部 PASS

- [ ] **步骤 5：运行全部 scanner 测试（含 race）**

```bash
go test ./internal/scanner/... -race -v
```

预期：所有测试 PASS，无 race

- [ ] **步骤 6：提交**

```bash
git add internal/scanner/watcher.go internal/scanner/watcher_test.go
git commit -m "feat: watcher — fsnotify 实时监听，500ms 去抖"
```

---

## 任务 8：HTTP API — 扫描端点

**文件：**
- 创建：`internal/api/v1/library.go`
- 创建：`internal/api/v1/library_test.go`
- 修改：`internal/api/router.go`（签名改为接受 `*scanner.Scanner`）
- 修改：`internal/api/router_test.go`（更新以适应新签名）

- [ ] **步骤 1：写失败测试**

```go
// internal/api/v1/library_test.go
package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/scanner"
)

func newTestHandler(t *testing.T) *LibraryHandler {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := scanner.NewScanner(d, config.LibraryConfig{})
	return NewLibraryHandler(s)
}

func TestTriggerScan_Returns200(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/library/scan", nil)
	w := httptest.NewRecorder()
	h.TriggerScan(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body map[string]bool
	json.NewDecoder(w.Body).Decode(&body)
	if !body["ok"] {
		t.Errorf("want ok=true, got %v", body)
	}
}

func TestScanStatus_Returns200WithFields(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/scan/status", nil)
	w := httptest.NewRecorder()
	h.ScanStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if _, ok := body["running"]; !ok {
		t.Error("response missing 'running' field")
	}
}
```

- [ ] **步骤 2：运行测试 —— 确认失败**

```bash
go test ./internal/api/... -v
```

预期：FAIL（`v1` 包不存在）

- [ ] **步骤 3：实现 internal/api/v1/library.go**

```go
// internal/api/v1/library.go
package v1

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/yxx-z/lyra/internal/scanner"
)

// LibraryHandler handles /api/v1/library/* endpoints.
type LibraryHandler struct {
	scanner *scanner.Scanner
}

// NewLibraryHandler creates a handler backed by s.
func NewLibraryHandler(s *scanner.Scanner) *LibraryHandler {
	return &LibraryHandler{scanner: s}
}

// TriggerScan handles POST /api/v1/library/scan.
func (h *LibraryHandler) TriggerScan(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := h.scanner.TriggerScan(); err != nil {
		if errors.Is(err, scanner.ErrScanInProgress) {
			w.WriteHeader(http.StatusConflict)
			if err2 := json.NewEncoder(w).Encode(map[string]string{"error": "扫描正在进行中"}); err2 != nil {
				slog.Error("写响应失败", "err", err2)
			}
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}

// ScanStatus handles GET /api/v1/library/scan/status.
func (h *LibraryHandler) ScanStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(h.scanner.Status()); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
```

- [ ] **步骤 4：更新 internal/api/router.go**

完整替换 `internal/api/router.go`：

```go
// internal/api/router.go
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/scanner"
	"github.com/yxx-z/lyra/ui"
)

const version = "0.1.0"

// NewRouter builds the application router. s must not be nil.
func NewRouter(s *scanner.Scanner) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", handleHealth)

	lib := v1.NewLibraryHandler(s)
	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/library/scan", lib.TriggerScan)
		r.Get("/library/scan/status", lib.ScanStatus)
	})

	sub, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		panic("embed ui/dist 失败: " + err.Error())
	}
	r.Handle("/*", http.FileServer(http.FS(sub)))

	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": version,
	}); err != nil {
		slog.Error("写 health 响应失败", "err", err)
	}
}
```

- [ ] **步骤 5：更新 internal/api/router_test.go**

完整替换 `internal/api/router_test.go`：

```go
// internal/api/router_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/scanner"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := scanner.NewScanner(d, config.LibraryConfig{})
	return NewRouter(s)
}

func TestHealth_Returns200WithStatusOK(t *testing.T) {
	r := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("解析响应体失败: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("期望 status=ok，实际 %q", body["status"])
	}
	if body["version"] == "" {
		t.Errorf("version 字段不应为空")
	}
}
```

- [ ] **步骤 6：运行所有测试**

```bash
go test ./... -v
```

预期：所有包测试全部 PASS

- [ ] **步骤 7：提交**

```bash
git add internal/api/ 
git commit -m "feat: 扫描器 HTTP API — POST /api/v1/library/scan，GET /api/v1/library/scan/status"
```

---

## 任务 9：main.go — 组装 scanner

**文件：**
- 修改：`cmd/server/main.go`

- [ ] **步骤 1：更新 main.go**

完整替换 `cmd/server/main.go`：

```go
// cmd/server/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yxx-z/lyra/internal/api"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/scanner"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("加载配置失败", "err", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.Database.Path)
	if err != nil {
		slog.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	sc := scanner.NewScanner(database, cfg.Library)

	router := api.NewRouter(sc)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("Lyra 启动", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	if err := sc.Start(); err != nil {
		slog.Error("启动扫描器失败", "err", err)
	}

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		slog.Error("服务器启动失败", "err", err)
		os.Exit(1)
	}

	slog.Info("正在关闭服务器")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("优雅关闭失败", "err", err)
	}
	sc.Stop()
}
```

- [ ] **步骤 2：编译验证**

```bash
go build ./cmd/server
rm -f server
```

预期：编译成功，无错误

- [ ] **步骤 3：运行全部测试**

```bash
go test ./... -race
```

预期：所有包测试全部 PASS，无 race

- [ ] **步骤 4：冒烟测试**

```bash
go build -o lyra ./cmd/server
./lyra &
sleep 2
# 健康检查
curl -s http://localhost:4533/health
# 触发扫描（无音乐库路径，返回 ok）
curl -s -X POST http://localhost:4533/api/v1/library/scan
# 查询状态
curl -s http://localhost:4533/api/v1/library/scan/status
kill %1
rm -f lyra
```

预期：
- `/health` → `{"status":"ok","version":"0.1.0"}`
- `POST /scan` → `{"ok":true}`
- `GET /scan/status` → `{"running":false,...}`

- [ ] **步骤 5：提交并推送**

```bash
git add cmd/server/main.go
git commit -m "feat: main.go 注入 scanner，启动时自动扫描"
git push origin master
```

---

## 自检清单

**规格覆盖：**
- [x] US-01（自动扫描）→ 任务 9，`sc.Start()` 在 main.go 启动时调用
- [x] US-02（文件系统事件）→ 任务 7，fsnotify + 去抖
- [x] US-03（手动触发）→ 任务 8，`POST /api/v1/library/scan`
- [x] US-04（扫描进度）→ 任务 8，`GET /api/v1/library/scan/status`
- [x] 混合目录结构 → 任务 3，优先级 2a/2b 目录推断
- [x] 单层 `艺术家 - 专辑 (年份) - 格式` → 任务 3，`parseArtistAlbum` + `cleanAlbumName`
- [x] 标签缺失兜底 → 任务 3，默认值填充
- [x] artist/album 大小写去重 → 任务 5，`normalize()` 函数
- [x] is_available 软删除 → 任务 1（迁移）+ 任务 5（ingester.MarkUnavailable）
- [x] ffprobe 音频属性（可选）→ 任务 3，`Read()` 中标注为 TODO（v0.1 填 0）
- [x] DB 写入串行（单 ingester goroutine）→ 任务 6，scanner.runScan 架构
- [x] router.go 更新签名 → 任务 8
- [x] router_test.go 更新 → 任务 8
