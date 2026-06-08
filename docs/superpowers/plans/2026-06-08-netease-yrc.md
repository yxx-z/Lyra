# 网易云歌词 + YRC 逐字歌词 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增网易云 Provider（Go 内 eapi 加密直连），拉取并解析 YRC 逐字歌词，接入既有 Provider 链，前端实现 Apple Music 式逐字扫光渲染。

**Architecture:** `internal/lyrics` 新增三个文件：eapi 加密层（纯函数）、YRC 解析器（纯函数）、NeteaseProvider（组合前两者 + HTTP）。链顺序调整为 `embedded → netease → lrclib`，按 `scraper.netease.enabled` 条件接入。前端按 `has_yrc` 切换到逐字扫光渲染。

**Tech Stack:** Go 1.25（crypto/aes、crypto/md5、net/http、httptest）、Vue 3 + TypeScript。

**Go 环境：** 每个含 `go` 命令的步骤前先 `export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## 设计参考

实现前阅读 `docs/superpowers/specs/2026-06-08-netease-yrc-design.md`。关键既有接口（勿改）：

```go
// internal/lyrics/provider.go
var ErrInvalidQuery = errors.New("歌词查询参数不足")
var ErrNotFound     = errors.New("歌词不存在")
type Query struct { TrackName, ArtistName, AlbumName string; Duration int; FilePath string } // Duration 单位=秒
type Result struct { LRCContent, PlainContent, YRCContent, Source string }
type Provider interface { Name() string; Fetch(ctx context.Context, q Query) (Result, error) }
```

配置字段：`cfg.Scraper.Netease.Enabled bool`（默认 true）。

---

## 文件结构

```
internal/lyrics/
├── netease_crypto.go        新增：eapi 加密（eapiEncryptParams / aesECBEncrypt）
├── netease_crypto_test.go   新增：加密往返自洽测试
├── netease_yrc.go           新增：parseYRC（原始 YRC → 归一化 JSON）+ yrcDoc/yrcLine/yrcWord 类型
├── netease_yrc_test.go      新增：YRC 解析单测
├── netease.go               新增：NeteaseProvider + 匹配 + HTTP 请求层
└── netease_test.go          新增：匹配单测 + Fetch httptest 集成测试

internal/api/router.go       改：第 63-67 行，按 netease.enabled 条件构造链
cmd/server/main.go           改：第 68-72 行，同上

web/src/components/LyricsPanel.vue  改：YRC 逐字扫光渲染
```

---

### Task 1: eapi 加密层

**Files:**
- Create: `internal/lyrics/netease_crypto.go`
- Test: `internal/lyrics/netease_crypto_test.go`

- [ ] **Step 1: 写失败测试**

`internal/lyrics/netease_crypto_test.go`：
```go
package lyrics

import (
	"bytes"
	"crypto/aes"
	"encoding/hex"
	"strings"
	"testing"
)

// 测试内解密辅助：AES-128-ECB + PKCS7 去填充
func aesECBDecryptForTest(t *testing.T, src, key []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	bs := block.BlockSize()
	if len(src) == 0 || len(src)%bs != 0 {
		t.Fatalf("bad ciphertext length %d", len(src))
	}
	out := make([]byte, len(src))
	for i := 0; i < len(src); i += bs {
		block.Decrypt(out[i:i+bs], src[i:i+bs])
	}
	pad := int(out[len(out)-1])
	if pad < 1 || pad > bs {
		t.Fatalf("bad padding %d", pad)
	}
	return out[:len(out)-pad]
}

func TestEapiEncryptParamsRoundTrip(t *testing.T) {
	path := "/api/song/lyric/v1"
	text := `{"id":"123","yrc":"0"}`

	params := eapiEncryptParams(path, text)

	// 1. 输出必须是大写 hex
	if params != strings.ToUpper(params) {
		t.Errorf("params 应为大写 hex，得到 %q", params)
	}
	raw, err := hex.DecodeString(params)
	if err != nil {
		t.Fatalf("params 非合法 hex: %v", err)
	}

	// 2. 用 eapi key 解密应还原出 data 结构：path-36cd479b6b5-text-36cd479b6b5-digest
	plain := aesECBDecryptForTest(t, raw, []byte(eapiKey))
	parts := bytes.Split(plain, []byte("-36cd479b6b5-"))
	if len(parts) != 3 {
		t.Fatalf("解密后应为 3 段，得到 %d 段: %q", len(parts), plain)
	}
	if string(parts[0]) != path {
		t.Errorf("path 段 = %q, want %q", parts[0], path)
	}
	if string(parts[1]) != text {
		t.Errorf("text 段 = %q, want %q", parts[1], text)
	}
	if len(parts[2]) != 32 {
		t.Errorf("digest 段应为 32 位 md5 hex，得到 %q", parts[2])
	}

	// 3. 确定性：同输入同输出
	if eapiEncryptParams(path, text) != params {
		t.Error("eapiEncryptParams 应是确定性的")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -run TestEapiEncryptParams -v`
Expected: 编译失败（`undefined: eapiEncryptParams` / `eapiKey`）

- [ ] **Step 3: 写最小实现**

`internal/lyrics/netease_crypto.go`：
```go
package lyrics

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"encoding/hex"
	"strings"
)

// eapiKey 是网易云 eapi 接口的 AES-128-ECB 密钥（公开常量）。
const eapiKey = "e82ckenh8dichen8"

// eapiEncryptParams 按网易云 eapi 协议加密请求体，返回大写 hex 字符串。
// path 为 API 路径（如 "/api/song/lyric/v1"），text 为 JSON 参数体。
func eapiEncryptParams(path, text string) string {
	digestInput := "nobody" + path + "use" + text + "md5forencrypt"
	sum := md5.Sum([]byte(digestInput))
	digest := hex.EncodeToString(sum[:])
	data := path + "-36cd479b6b5-" + text + "-36cd479b6b5-" + digest
	enc := aesECBEncrypt([]byte(data), []byte(eapiKey))
	return strings.ToUpper(hex.EncodeToString(enc))
}

// aesECBEncrypt 对 src 做 AES-ECB + PKCS7 填充加密。
func aesECBEncrypt(src, key []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		// key 为编译期常量，长度固定 16，不会出错
		panic(err)
	}
	bs := block.BlockSize()
	pad := bs - len(src)%bs
	src = append(src, bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(src))
	for i := 0; i < len(src); i += bs {
		block.Encrypt(out[i:i+bs], src[i:i+bs])
	}
	return out
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -run TestEapiEncryptParams -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/lyrics/netease_crypto.go internal/lyrics/netease_crypto_test.go
git commit -m "feat(lyrics): 网易云 eapi 加密层"
```

---

### Task 2: YRC 解析器

**Files:**
- Create: `internal/lyrics/netease_yrc.go`
- Test: `internal/lyrics/netease_yrc_test.go`

- [ ] **Step 1: 写失败测试**

`internal/lyrics/netease_yrc_test.go`：
```go
package lyrics

import (
	"encoding/json"
	"testing"
)

func TestParseYRC_Basic(t *testing.T) {
	raw := "[12100,3000](12100,300,0)作(12400,300,0)词(12700,400,0)人\n[15100,2000](15100,500,0)歌(15600,500,0)手"

	out, err := parseYRC(raw)
	if err != nil {
		t.Fatalf("parseYRC err: %v", err)
	}

	var doc yrcDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("输出非合法 JSON: %v (%s)", err, out)
	}
	if len(doc.Lines) != 2 {
		t.Fatalf("应解析出 2 行，得到 %d", len(doc.Lines))
	}
	l0 := doc.Lines[0]
	if l0.Start != 12.1 || l0.End != 15.1 {
		t.Errorf("行0 时间 = %v~%v, want 12.1~15.1", l0.Start, l0.End)
	}
	if len(l0.Words) != 3 {
		t.Fatalf("行0 应 3 字，得到 %d", len(l0.Words))
	}
	if l0.Words[0].Text != "作" || l0.Words[0].Start != 12.1 || l0.Words[0].End != 12.4 {
		t.Errorf("行0字0 = %+v, want {作 12.1 12.4}", l0.Words[0])
	}
}

func TestParseYRC_SkipsMetadataLines(t *testing.T) {
	raw := "{\"t\":0,\"c\":[{\"tx\":\"作词: 某人\"}]}\n[1000,1000](1000,1000,0)字"

	out, err := parseYRC(raw)
	if err != nil {
		t.Fatalf("parseYRC err: %v", err)
	}
	var doc yrcDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("输出非合法 JSON: %v", err)
	}
	if len(doc.Lines) != 1 {
		t.Errorf("元信息行应被丢弃，仅留 1 行，得到 %d", len(doc.Lines))
	}
}

func TestParseYRC_EmptyReturnsEmptyString(t *testing.T) {
	out, err := parseYRC("{\"t\":0,\"c\":[]}\n\n")
	if err != nil {
		t.Fatalf("parseYRC err: %v", err)
	}
	if out != "" {
		t.Errorf("无有效歌词行应返回空串，得到 %q", out)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -run TestParseYRC -v`
Expected: 编译失败（`undefined: parseYRC` / `yrcDoc`）

- [ ] **Step 3: 写最小实现**

`internal/lyrics/netease_yrc.go`：
```go
package lyrics

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// yrcWord 是一个逐字单元（时间单位=秒）。
type yrcWord struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// yrcLine 是一行逐字歌词。
type yrcLine struct {
	Start float64   `json:"start"`
	End   float64   `json:"end"`
	Words []yrcWord `json:"words"`
}

// yrcDoc 是归一化后的 YRC 文档，序列化后存入 lyrics.yrc_content。
type yrcDoc struct {
	Lines []yrcLine `json:"lines"`
}

var (
	yrcLineHead = regexp.MustCompile(`^\[(\d+),(\d+)\]`)
	yrcWordRe   = regexp.MustCompile(`\((\d+),(\d+),\d+\)([^(]*)`)
)

// parseYRC 将网易云原始 YRC 解析为归一化 JSON 字符串；无任何有效歌词行时返回空串。
// 无 [起,长] 行头的行（如 {"t":..} 元信息）一律跳过；单行解析失败不影响其它行。
func parseYRC(raw string) (string, error) {
	doc := yrcDoc{Lines: []yrcLine{}}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		head := yrcLineHead.FindStringSubmatch(line)
		if head == nil {
			continue
		}
		lineStart, _ := strconv.Atoi(head[1])
		lineDur, _ := strconv.Atoi(head[2])

		words := make([]yrcWord, 0, 8)
		for _, m := range yrcWordRe.FindAllStringSubmatch(line, -1) {
			ws, _ := strconv.Atoi(m[1])
			wd, _ := strconv.Atoi(m[2])
			text := m[3]
			if strings.TrimSpace(text) == "" {
				continue
			}
			words = append(words, yrcWord{
				Start: float64(ws) / 1000,
				End:   float64(ws+wd) / 1000,
				Text:  text,
			})
		}
		if len(words) == 0 {
			continue
		}
		doc.Lines = append(doc.Lines, yrcLine{
			Start: float64(lineStart) / 1000,
			End:   float64(lineStart+lineDur) / 1000,
			Words: words,
		})
	}
	if len(doc.Lines) == 0 {
		return "", nil
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -run TestParseYRC -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/lyrics/netease_yrc.go internal/lyrics/netease_yrc_test.go
git commit -m "feat(lyrics): YRC 逐字歌词解析器（归一化 JSON）"
```

---

### Task 3: 匹配逻辑（normalize / titleMatches / pickMatch）

**Files:**
- Create: `internal/lyrics/netease.go`（本任务先只放匹配相关类型与函数）
- Test: `internal/lyrics/netease_test.go`

- [ ] **Step 1: 写失败测试**

`internal/lyrics/netease_test.go`：
```go
package lyrics

import "testing"

func TestPickMatch(t *testing.T) {
	songs := []neteaseSong{
		{ID: 1, Name: "晴天 (Live)", DurationMS: 200000}, // 时长差大
		{ID: 2, Name: "晴天", DurationMS: 269000},        // 命中：标题含 + 时长差 ~0
		{ID: 3, Name: "其它歌", DurationMS: 269000},      // 标题不含
	}

	got, ok := pickMatch(songs, "晴天", 269)
	if !ok {
		t.Fatal("应匹配到 id=2")
	}
	if got.ID != 2 {
		t.Errorf("匹配 id = %d, want 2", got.ID)
	}
}

func TestPickMatch_DurationTooFar(t *testing.T) {
	songs := []neteaseSong{{ID: 1, Name: "晴天", DurationMS: 200000}}
	if _, ok := pickMatch(songs, "晴天", 269); ok {
		t.Error("时长差 >3s 不应匹配")
	}
}

func TestPickMatch_TitleNotContained(t *testing.T) {
	songs := []neteaseSong{{ID: 1, Name: "完全不同", DurationMS: 269000}}
	if _, ok := pickMatch(songs, "晴天", 269); ok {
		t.Error("标题不互相包含不应匹配")
	}
}

func TestNormalizeText(t *testing.T) {
	// 全角空格/字母转半角、小写、折叠空白
	if got := normalizeText("　Ｈｅｌｌｏ   World "); got != "hello world" {
		t.Errorf("normalizeText = %q, want %q", got, "hello world")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -run 'TestPickMatch|TestNormalizeText' -v`
Expected: 编译失败（`undefined: neteaseSong` / `pickMatch` / `normalizeText`）

- [ ] **Step 3: 写最小实现**

`internal/lyrics/netease.go`（先建文件，仅含匹配部分；Task 4 再追加 Provider）：
```go
package lyrics

import "strings"

// neteaseSong 是搜索候选中我们关心的字段。
type neteaseSong struct {
	ID         int64
	Name       string
	DurationMS int
}

// normalizeText 归一化文本：去首尾空格、转小写、全角转半角、折叠空白。
func normalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '　': // 全角空格
			b.WriteRune(' ')
		case r >= '！' && r <= '～': // 全角 ASCII 区
			b.WriteRune(r - 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// titleMatches 判断两个标题归一化后是否互相包含。
func titleMatches(a, b string) bool {
	na, nb := normalizeText(a), normalizeText(b)
	if na == "" || nb == "" {
		return false
	}
	return strings.Contains(na, nb) || strings.Contains(nb, na)
}

// pickMatch 从候选中选时长差 ≤3 秒且标题互含的第一首。
func pickMatch(songs []neteaseSong, wantTitle string, wantDurationSec int) (neteaseSong, bool) {
	for _, s := range songs {
		diff := wantDurationSec - s.DurationMS/1000
		if diff < 0 {
			diff = -diff
		}
		if diff <= 3 && titleMatches(s.Name, wantTitle) {
			return s, true
		}
	}
	return neteaseSong{}, false
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -run 'TestPickMatch|TestNormalizeText' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/lyrics/netease.go internal/lyrics/netease_test.go
git commit -m "feat(lyrics): 网易云候选匹配（时长+标题归一化）"
```

---

### Task 4: NeteaseProvider（HTTP 请求层 + Fetch）

**Files:**
- Modify: `internal/lyrics/netease.go`（追加 Provider、请求层）
- Test: `internal/lyrics/netease_test.go`（追加 httptest 集成测试）

- [ ] **Step 1: 写失败测试**

在 `internal/lyrics/netease_test.go` 中，先把 Task 3 留下的 `import "testing"` 这一行**替换**为下面的 import 块（避免重复导入 testing），再在文件末尾追加后续测试函数：
```go
import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)
```
```go
// newTestProvider 返回指向 httptest server 的 NeteaseProvider。
func newTestProvider(srv *httptest.Server) *NeteaseProvider {
	p := NewNeteaseProvider(srv.Client())
	p.baseURL = srv.URL
	return p
}

func TestNeteaseFetch_Hit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/eapi/cloudsearch/pc"):
			w.Write([]byte(`{"result":{"songs":[{"id":2,"name":"晴天","dt":269000,"ar":[{"name":"周杰伦"}]}]}}`))
		case strings.Contains(r.URL.Path, "/eapi/song/lyric/v1"):
			w.Write([]byte(`{"lrc":{"lyric":"[00:01.00]普通歌词"},"yrc":{"lyric":"[1000,1000](1000,1000,0)字"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestProvider(srv)
	res, err := p.Fetch(context.Background(), Query{TrackName: "晴天", ArtistName: "周杰伦", Duration: 269})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Source != "netease" {
		t.Errorf("Source = %q, want netease", res.Source)
	}
	if !strings.Contains(res.LRCContent, "普通歌词") {
		t.Errorf("LRCContent 缺失: %q", res.LRCContent)
	}
	if !strings.Contains(res.YRCContent, `"words"`) {
		t.Errorf("YRCContent 应为归一化 JSON: %q", res.YRCContent)
	}
}

func TestNeteaseFetch_NoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":{"songs":[{"id":9,"name":"别的歌","dt":100000,"ar":[{"name":"X"}]}]}}`))
	}))
	defer srv.Close()
	p := newTestProvider(srv)
	_, err := p.Fetch(context.Background(), Query{TrackName: "晴天", ArtistName: "周杰伦", Duration: 269})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("无匹配应返回 ErrNotFound，得到 %v", err)
	}
}

func TestNeteaseFetch_EmptyLyric(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/eapi/cloudsearch/pc"):
			w.Write([]byte(`{"result":{"songs":[{"id":2,"name":"晴天","dt":269000,"ar":[{"name":"周杰伦"}]}]}}`))
		default:
			w.Write([]byte(`{"lrc":{"lyric":""},"yrc":{"lyric":""}}`))
		}
	}))
	defer srv.Close()
	p := newTestProvider(srv)
	_, err := p.Fetch(context.Background(), Query{TrackName: "晴天", ArtistName: "周杰伦", Duration: 269})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("空歌词应返回 ErrNotFound，得到 %v", err)
	}
}

func TestNeteaseFetch_InvalidQuery(t *testing.T) {
	p := NewNeteaseProvider(nil)
	_, err := p.Fetch(context.Background(), Query{TrackName: "", ArtistName: "x", Duration: 100})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("缺曲名应返回 ErrInvalidQuery，得到 %v", err)
	}
}

func TestNeteaseFetch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := newTestProvider(srv)
	_, err := p.Fetch(context.Background(), Query{TrackName: "晴天", ArtistName: "周杰伦", Duration: 269})
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("服务端 500 应返回普通 error（非 ErrNotFound），得到 %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -run TestNeteaseFetch -v`
Expected: 编译失败（`undefined: NewNeteaseProvider` / `NeteaseProvider`）

- [ ] **Step 3: 写最小实现**

在 `internal/lyrics/netease.go` 追加（同时把文件顶部 import 补齐）：
```go
// 文件顶部 import 改为：
// import (
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"net/http"
// 	"net/url"
// 	"strconv"
// 	"strings"
// 	"time"
// )

const neteaseDefaultBaseURL = "https://interface.music.163.com"

// NeteaseProvider 通过网易云 eapi 接口获取歌词（含 YRC 逐字）。
type NeteaseProvider struct {
	httpClient *http.Client
	baseURL    string
}

// NewNeteaseProvider 创建 provider；httpClient 为 nil 时用 10s 超时默认客户端。
func NewNeteaseProvider(httpClient *http.Client) *NeteaseProvider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &NeteaseProvider{httpClient: httpClient, baseURL: neteaseDefaultBaseURL}
}

// Name 实现 Provider。
func (p *NeteaseProvider) Name() string { return "netease" }

// Fetch 实现 Provider：搜索 → 匹配 → 取词 → 解析 YRC。
func (p *NeteaseProvider) Fetch(ctx context.Context, q Query) (Result, error) {
	if strings.TrimSpace(q.TrackName) == "" || strings.TrimSpace(q.ArtistName) == "" {
		return Result{}, ErrInvalidQuery
	}

	songs, err := p.search(ctx, q.TrackName+" "+q.ArtistName)
	if err != nil {
		return Result{}, err
	}
	song, ok := pickMatch(songs, q.TrackName, q.Duration)
	if !ok {
		return Result{}, ErrNotFound
	}

	lrc, yrcRaw, err := p.lyric(ctx, song.ID)
	if err != nil {
		return Result{}, err
	}

	yrcJSON := ""
	if strings.TrimSpace(yrcRaw) != "" {
		yrcJSON, err = parseYRC(yrcRaw)
		if err != nil {
			return Result{}, err
		}
	}
	if strings.TrimSpace(lrc) == "" && strings.TrimSpace(yrcJSON) == "" {
		return Result{}, ErrNotFound
	}

	return Result{
		LRCContent: strings.TrimSpace(lrc),
		YRCContent: yrcJSON,
		Source:     "netease",
	}, nil
}

// search 调 eapi cloudsearch，返回候选歌曲。
func (p *NeteaseProvider) search(ctx context.Context, keyword string) ([]neteaseSong, error) {
	payload := map[string]string{
		"s": keyword, "type": "1", "limit": "10", "offset": "0", "total": "true",
	}
	body, err := p.eapiPost(ctx, "/api/cloudsearch/pc", payload)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Result struct {
			Songs []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
				Dt   int    `json:"dt"`
			} `json:"songs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("netease search 解码失败: %w", err)
	}
	songs := make([]neteaseSong, 0, len(parsed.Result.Songs))
	for _, s := range parsed.Result.Songs {
		songs = append(songs, neteaseSong{ID: s.ID, Name: s.Name, DurationMS: s.Dt})
	}
	return songs, nil
}

// lyric 调 eapi song/lyric/v1，返回 (普通lrc, 原始yrc)。
func (p *NeteaseProvider) lyric(ctx context.Context, songID int64) (string, string, error) {
	payload := map[string]string{
		"id": strconv.FormatInt(songID, 10),
		"cp": "false", "lv": "0", "kv": "0", "tv": "0", "rv": "0", "yv": "0", "ytv": "0", "yrc": "0",
	}
	body, err := p.eapiPost(ctx, "/api/song/lyric/v1", payload)
	if err != nil {
		return "", "", err
	}
	var parsed struct {
		Lrc struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
		Yrc struct {
			Lyric string `json:"lyric"`
		} `json:"yrc"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("netease lyric 解码失败: %w", err)
	}
	return parsed.Lrc.Lyric, parsed.Yrc.Lyric, nil
}

// eapiPost 对 apiPath 做 eapi 加密并 POST，返回响应体（明文 JSON）。
// 请求 URL 由 baseURL + (apiPath 中 /api 换成 /eapi) 构成。
func (p *NeteaseProvider) eapiPost(ctx context.Context, apiPath string, payload map[string]string) ([]byte, error) {
	text, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	params := eapiEncryptParams(apiPath, string(text))
	form := url.Values{}
	form.Set("params", params)

	reqURL := p.baseURL + strings.Replace(apiPath, "/api", "/eapi", 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) NeteaseMusicDesktop/2.10.2")
	req.Header.Set("Referer", "https://music.163.com")
	req.Header.Set("Cookie", "os=pc; appver=8.9.70")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("netease 接口状态 %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
```

注意：上面用到 `io.ReadAll`，需在 import 加 `"io"`。完整 import 块：
```go
import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)
```
（`strings` 已被 Task 3 的匹配函数使用，合并到同一 import 块即可。）

- [ ] **Step 4: 运行测试确认通过**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/lyrics/ -v`
Expected: PASS（本包所有测试，含 Task 1-4）

- [ ] **Step 5: 提交**

```bash
git add internal/lyrics/netease.go internal/lyrics/netease_test.go
git commit -m "feat(lyrics): NeteaseProvider（eapi 搜索+取词+YRC）"
```

---

### Task 5: 接入 Provider 链（按 netease.enabled 条件）

**Files:**
- Modify: `internal/api/router.go:63-67`
- Modify: `cmd/server/main.go:68-72`

- [ ] **Step 1: 改 router.go**

把 `internal/api/router.go` 第 63-67 行：
```go
		lyricsService := lyricspkg.NewLyricsService(
			db,
			lyricspkg.NewEmbeddedProvider(),
			lyricspkg.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil),
		)
```
替换为：
```go
		providers := []lyricspkg.Provider{lyricspkg.NewEmbeddedProvider()}
		if cfg.Scraper.Netease.Enabled {
			providers = append(providers, lyricspkg.NewNeteaseProvider(nil))
		}
		providers = append(providers, lyricspkg.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil))
		lyricsService := lyricspkg.NewLyricsService(db, providers...)
```

- [ ] **Step 2: 改 main.go**

把 `cmd/server/main.go` 第 68-72 行：
```go
	lyricsService := lyrics.NewLyricsService(
		database,
		lyrics.NewEmbeddedProvider(),
		lyrics.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil),
	)
```
替换为：
```go
	lyricsProviders := []lyrics.Provider{lyrics.NewEmbeddedProvider()}
	if cfg.Scraper.Netease.Enabled {
		lyricsProviders = append(lyricsProviders, lyrics.NewNeteaseProvider(nil))
	}
	lyricsProviders = append(lyricsProviders, lyrics.NewLRCLIBClient("https://lrclib.net", cfg.Scraper.MusicBrainz.UserAgent, nil))
	lyricsService := lyrics.NewLyricsService(database, lyricsProviders...)
```

- [ ] **Step 3: 编译 + 全量测试**

Run: `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./...`
Expected: build 成功；所有测试 PASS（既有 router/main 测试不受影响）

- [ ] **Step 4: 提交**

```bash
git add internal/api/router.go cmd/server/main.go
git commit -m "feat(lyrics): 链顺序 embedded→netease→lrclib（按 netease.enabled）"
```

---

### Task 6: 前端 YRC 逐字扫光渲染

**Files:**
- Modify: `web/src/components/LyricsPanel.vue`

说明：现有组件已有 `lrcLines`/`parseLrc`/`synced`/`syncLyricsIndex`/`currentLineIndex`，纯文本与 LRC 路径保留不变。新增 YRC 模式：`yrcLines` 数据 + 逐字扫光渲染，优先级 yrc > lrc > plain > no_lyrics。

- [ ] **Step 1: 新增 YRC 数据类型与状态**

在 `<script setup>` 顶部 `interface LyricLine {...}` 之后追加类型，并在状态区（`const synced = ref(false)` 附近）追加 ref：
```ts
interface YrcWord { start: number; end: number; text: string }
interface YrcLineData { start: number; end: number; words: YrcWord[] }
```
```ts
// YRC 逐字歌词（优先于 LRC）；非空即进入逐字扫光模式
const yrcLines = ref<YrcLineData[]>([])
```

- [ ] **Step 2: loadLyrics 增加 YRC 分支**

把 `loadLyrics` 中的 try 块：
```ts
    const res = await props.api.getLyrics(track.trackId)
    if (res.has_lrc && res.lrc_content) {
      lrcLines.value = parseLrc(res.lrc_content)
    } else {
      error.value = 'no_lyrics'
    }
```
替换为：
```ts
    const res = await props.api.getLyrics(track.trackId)
    if (res.has_yrc && res.yrc_content) {
      try {
        const doc = JSON.parse(res.yrc_content) as { lines: YrcLineData[] }
        yrcLines.value = Array.isArray(doc.lines) ? doc.lines : []
      } catch {
        yrcLines.value = []
      }
    }
    if (yrcLines.value.length === 0 && res.has_lrc && res.lrc_content) {
      lrcLines.value = parseLrc(res.lrc_content)
    }
    if (yrcLines.value.length === 0 && lrcLines.value.length === 0) {
      error.value = 'no_lyrics'
    }
```
并在 `loadLyrics` 顶部重置区（`lrcLines.value = []` 旁）追加：
```ts
  yrcLines.value = []
```

- [ ] **Step 3: 新增逐字填充纯函数与当前行计算**

在 `<script setup>` 内新增（放在 `syncLyricsIndex` 附近）：
```ts
// 单字填充百分比（0~100），用于扫光渐变
function wordFillPercent(word: YrcWord, time: number): number {
  if (time <= word.start) return 0
  if (time >= word.end || word.end <= word.start) return 100
  return ((time - word.start) / (word.end - word.start)) * 100
}

// 当前 YRC 行索引（currentTime 落在哪行）
const yrcCurrentLine = computed(() => {
  const time = playerStore.currentTime
  let idx = 0
  for (let i = 0; i < yrcLines.value.length; i++) {
    if (time >= yrcLines.value[i].start) idx = i
    else break
  }
  return idx
})
```

- [ ] **Step 4: 模板增加 YRC 渲染分支**

把模板中「无歌词」分支的 `v-else-if` 条件由：
```html
        <div v-else-if="error === 'no_lyrics' || lrcLines.length === 0" class="empty-state" ...>
```
改为：
```html
        <div v-else-if="error === 'no_lyrics' || (lrcLines.length === 0 && yrcLines.length === 0)" class="empty-state" ...>
```
并在「标准滚动歌词面板」分支（`<div v-else ref="scrollerRef" ...>`）**之前**插入 YRC 分支：
```html
        <!-- C0. YRC 逐字扫光面板 -->
        <div v-else-if="yrcLines.length > 0" ref="scrollerRef" class="lyrics-scroller">
          <button
            v-for="(line, idx) in yrcLines"
            :key="idx"
            :class="{ active: idx === yrcCurrentLine }"
            class="lyric-line"
            type="button"
            @click="seekToLine(line.start)"
          >
            <span
              v-for="(word, widx) in line.words"
              :key="widx"
              class="yrc-word"
              :style="idx === yrcCurrentLine
                ? { backgroundSize: wordFillPercent(word, playerStore.currentTime) + '% 100%' }
                : { backgroundSize: '0% 100%' }"
            >{{ word.text }}</span>
          </button>
        </div>
```
（`seekToLine` 现有签名 `(time:number)`；YRC 行点击传 `line.start`。注意 `seekToLine` 当前有 `if (!synced.value ...) return` 守卫——见 Step 5 调整。）

- [ ] **Step 5: 让 seek 在 YRC 模式可用**

把 `seekToLine` 的守卫：
```ts
function seekToLine(time: number) {
  if (!synced.value || time < 0) return
  playerStore.seek(time)
  syncLyricsIndex()
}
```
改为（YRC 模式也允许 seek）：
```ts
function seekToLine(time: number) {
  if (time < 0) return
  if (!synced.value && yrcLines.value.length === 0) return
  playerStore.seek(time)
  syncLyricsIndex()
}
```

- [ ] **Step 6: YRC 模式下滚动当前行**

把 `watch(() => playerStore.currentTime, ...)` 块：
```ts
watch(() => playerStore.currentTime, () => {
  if (props.isOpen) {
    syncLyricsIndex()
  }
})
```
改为（YRC 模式滚动到当前行）：
```ts
watch(() => playerStore.currentTime, () => {
  if (!props.isOpen) return
  if (yrcLines.value.length > 0) {
    scrollToActiveLine()
  } else {
    syncLyricsIndex()
  }
})
```

- [ ] **Step 7: 增加扫光 CSS**

在 `<style scoped>` 末尾追加：
```css
/* YRC 逐字扫光：渐变从左到右填充，未填充部分为暗淡色 */
.yrc-word {
  background-image: linear-gradient(to right, var(--text, #fff) 50%, var(--text-dim, rgba(255,255,255,0.35)) 50%);
  background-size: 0% 100%;
  background-repeat: no-repeat;
  background-position: left center;
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  color: var(--text-dim, rgba(255,255,255,0.35));
  transition: background-size 0.1s linear;
  white-space: pre;
}
```

- [ ] **Step 8: 构建验证**

Run: `make build-frontend`
Expected: vue-tsc 类型检查通过；vite 构建成功，无报错

- [ ] **Step 9: 提交**

```bash
git add web/src/components/LyricsPanel.vue
git commit -m "feat(web): 歌词面板 YRC 逐字扫光渲染（Apple Music 式）"
```

---

## 完成标准

- `go test ./...` 全绿；`go build ./...` 成功
- `make build-frontend` 成功
- 链顺序 `embedded → netease → lrclib`，`netease.enabled=false` 时不接入网易云
- 有 YRC 的曲目逐字扫光，无 YRC 退回 LRC 逐行，再退纯文本

## 验证（手动，需真实网络与样本曲目）

1. `make build` 后启动，删一首中文曲目歌词、`scrape_status` 置 pending
2. 点歌词面板「获取歌词」→ 期望 `source:netease`、面板逐字扫光
3. `netease.enabled=false` 重启 → 同曲目刮削应走 lrclib（无 YRC，逐行高亮）
