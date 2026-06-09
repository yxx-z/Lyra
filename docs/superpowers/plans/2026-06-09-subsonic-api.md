# Subsonic API（第一期：浏览 + 播放）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Subsonic API 1.16.1 核心子集（`/rest`），让第三方客户端能连、浏览、播放。

**Architecture:** 新包 `internal/api/subsonic`：双格式（XML/JSON）响应封套、`p`/`t+s` 认证、12 个端点。stream/getCoverArt 复用 v1 的 StreamHandler/CoverHandler（导出按 id 的方法）。`/rest` 独立挂载、用 Subsonic 自身认证。

**Tech Stack:** Go 1.25（encoding/xml, encoding/json, crypto/md5, net/http, httptest, modernc.org/sqlite, chi）。

**Go 环境：** 含 `go` 命令的步骤前 `export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## 设计参考

实现前读 `docs/superpowers/specs/2026-06-09-subsonic-api-design.md`。

**关键既有代码：**
- `internal/config/config.go`：`cfg.Subsonic.{Enabled,Password}`、`cfg.Auth.Username`、`cfg.Transcode`、`cfg.Cache.TranscodeDir`。
- `internal/api/v1/stream.go`：`StreamHandler`，`NewStreamHandler(db, transcode, cacheDir)`，内部 `stream(w,r,trackID)` 做转码/直传。**本计划 Task 3 将其导出为 `StreamByID`。**
- `internal/api/v1/cover.go`：`CoverHandler`，`NewCoverHandler(db)`，内部 `getCover(w,r,albumID)`（内嵌→本地→cover_path）。**Task 3 导出为 `ServeCover`。**
- `internal/api/router.go`：`NewRouter(s,db,cfg)`，chi，v1 组在 `r.Route("/api/v1",...)`，末尾 `r.Handle("/*", fileServer)`。stream/cover handler 已在 v1 组构造。
- 表：`artists(id,name)`；`albums(id,title,artist_id,release_date,genre,cover_path,created_at)`；`tracks(id,title,artist_id,album_id,track_number,disc_number,duration,file_path,format,bitrate,play_count,last_played_at,is_available,created_at)`。
- `internal/db`：`db.Open(":memory:")` 跑迁移建全表。

---

## 文件结构

```
internal/api/subsonic/response.go        Response 封套 + DTO + writeResponse + writeError
internal/api/subsonic/response_test.go
internal/api/subsonic/auth.go            authenticate
internal/api/subsonic/auth_test.go
internal/api/subsonic/handler.go         Handler + NewHandler + RegisterRoutes + 认证中间件 + ping/getLicense/getMusicFolders
internal/api/subsonic/handler_test.go
internal/api/subsonic/browse.go          getArtists/getArtist/getAlbum/getSong/getAlbumList2 + childFromRow
internal/api/subsonic/browse_test.go
internal/api/subsonic/search.go          search3
internal/api/subsonic/search_test.go
internal/api/subsonic/media.go           getCoverArt/scrobble/stream
internal/api/subsonic/media_test.go
internal/api/v1/stream.go                改：导出 StreamByID
internal/api/v1/cover.go                 改：导出 ServeCover
internal/api/router.go                   改：挂 /rest
```

---

### Task 1: 响应封套 + DTO（response.go）

**Files:** Create `internal/api/subsonic/response.go`, `internal/api/subsonic/response_test.go`

- [ ] **Step 1: 写失败测试** — `internal/api/subsonic/response_test.go`:
```go
package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteResponse_XML(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/rest/ping", nil)
	writeResponse(w, r, &Response{License: &License{Valid: true}})

	body := w.Body.String()
	if !strings.Contains(body, `<subsonic-response`) || !strings.Contains(body, `status="ok"`) ||
		!strings.Contains(body, `version="1.16.1"`) || !strings.Contains(body, `<license valid="true"`) {
		t.Errorf("XML 输出不符: %s", body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestWriteResponse_JSON(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/rest/ping?f=json", nil)
	writeResponse(w, r, &Response{})

	var got map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("非合法 JSON: %v (%s)", err, w.Body.String())
	}
	sr := got["subsonic-response"]
	if sr["status"] != "ok" || sr["version"] != "1.16.1" {
		t.Errorf("JSON 封套不符: %v", sr)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/rest/ping?f=json", nil)
	writeError(w, r, 40, "用户名或密码错误")

	var got map[string]map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	sr := got["subsonic-response"]
	if sr["status"] != "failed" {
		t.Errorf("应 failed: %v", sr)
	}
	e, _ := sr["error"].(map[string]any)
	if e == nil || e["code"].(float64) != 40 {
		t.Errorf("error 字段不符: %v", sr)
	}
}

func TestResponse_AlbumListXMLArray(t *testing.T) {
	resp := &Response{AlbumList2: &AlbumList2{Album: []AlbumID3{{ID: "a1", Name: "X"}, {ID: "a2", Name: "Y"}}}}
	out, _ := xml.Marshal(resp)
	if strings.Count(string(out), "<album ") != 2 {
		t.Errorf("应有 2 个 <album> 元素: %s", out)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -run 'TestWriteResponse|TestWriteError|TestResponse' -v`
Expected: 编译失败（undefined）。

- [ ] **Step 3: 实现** — `internal/api/subsonic/response.go`:
```go
package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"log/slog"
	"net/http"
)

const subsonicAPIVersion = "1.16.1"
const subsonicXmlns = "http://subsonic.org/restapi"

type Response struct {
	XMLName       xml.Name       `xml:"subsonic-response" json:"-"`
	Xmlns         string         `xml:"xmlns,attr" json:"-"`
	Status        string         `xml:"status,attr" json:"status"`
	Version       string         `xml:"version,attr" json:"version"`
	Error         *Error         `xml:"error,omitempty" json:"error,omitempty"`
	License       *License       `xml:"license,omitempty" json:"license,omitempty"`
	MusicFolders  *MusicFolders  `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
	Artists       *ArtistsID3    `xml:"artists,omitempty" json:"artists,omitempty"`
	Artist        *ArtistID3     `xml:"artist,omitempty" json:"artist,omitempty"`
	Album         *AlbumID3      `xml:"album,omitempty" json:"album,omitempty"`
	AlbumList2    *AlbumList2    `xml:"albumList2,omitempty" json:"albumList2,omitempty"`
	Song          *Child         `xml:"song,omitempty" json:"song,omitempty"`
	SearchResult3 *SearchResult3 `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
}

type Error struct {
	Code    int    `xml:"code,attr" json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

type License struct {
	Valid bool `xml:"valid,attr" json:"valid"`
}

type MusicFolders struct {
	Folder []MusicFolder `xml:"musicFolder" json:"musicFolder"`
}
type MusicFolder struct {
	ID   int    `xml:"id,attr" json:"id"`
	Name string `xml:"name,attr" json:"name"`
}

type ArtistsID3 struct {
	IgnoredArticles string      `xml:"ignoredArticles,attr" json:"ignoredArticles"`
	Index           []IndexID3  `xml:"index" json:"index"`
}
type IndexID3 struct {
	Name   string      `xml:"name,attr" json:"name"`
	Artist []ArtistID3 `xml:"artist" json:"artist"`
}
type ArtistID3 struct {
	ID         string     `xml:"id,attr" json:"id"`
	Name       string     `xml:"name,attr" json:"name"`
	CoverArt   string     `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	AlbumCount int        `xml:"albumCount,attr" json:"albumCount"`
	Album      []AlbumID3 `xml:"album,omitempty" json:"album,omitempty"`
}
type AlbumID3 struct {
	ID        string  `xml:"id,attr" json:"id"`
	Name      string  `xml:"name,attr" json:"name"`
	Artist    string  `xml:"artist,attr" json:"artist"`
	ArtistID  string  `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	CoverArt  string  `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	SongCount int     `xml:"songCount,attr" json:"songCount"`
	Duration  int     `xml:"duration,attr" json:"duration"`
	Year      int     `xml:"year,attr,omitempty" json:"year,omitempty"`
	Genre     string  `xml:"genre,attr,omitempty" json:"genre,omitempty"`
	Created   string  `xml:"created,attr,omitempty" json:"created,omitempty"`
	Song      []Child `xml:"song,omitempty" json:"song,omitempty"`
}
type AlbumList2 struct {
	Album []AlbumID3 `xml:"album" json:"album"`
}
type Child struct {
	ID          string `xml:"id,attr" json:"id"`
	Parent      string `xml:"parent,attr,omitempty" json:"parent,omitempty"`
	IsDir       bool   `xml:"isDir,attr" json:"isDir"`
	Title       string `xml:"title,attr" json:"title"`
	Album       string `xml:"album,attr,omitempty" json:"album,omitempty"`
	Artist      string `xml:"artist,attr,omitempty" json:"artist,omitempty"`
	Track       int    `xml:"track,attr,omitempty" json:"track,omitempty"`
	Year        int    `xml:"year,attr,omitempty" json:"year,omitempty"`
	CoverArt    string `xml:"coverArt,attr,omitempty" json:"coverArt,omitempty"`
	Duration    int    `xml:"duration,attr,omitempty" json:"duration,omitempty"`
	BitRate     int    `xml:"bitRate,attr,omitempty" json:"bitRate,omitempty"`
	Suffix      string `xml:"suffix,attr,omitempty" json:"suffix,omitempty"`
	ContentType string `xml:"contentType,attr,omitempty" json:"contentType,omitempty"`
	AlbumID     string `xml:"albumId,attr,omitempty" json:"albumId,omitempty"`
	ArtistID    string `xml:"artistId,attr,omitempty" json:"artistId,omitempty"`
	Type        string `xml:"type,attr,omitempty" json:"type,omitempty"`
}
type SearchResult3 struct {
	Artist []ArtistID3 `xml:"artist,omitempty" json:"artist,omitempty"`
	Album  []AlbumID3  `xml:"album,omitempty" json:"album,omitempty"`
	Song   []Child     `xml:"song,omitempty" json:"song,omitempty"`
}

// writeResponse 按 f 参数输出 XML 或 JSON 的 subsonic-response 封套。
func writeResponse(w http.ResponseWriter, r *http.Request, resp *Response) {
	if resp.Status == "" {
		resp.Status = "ok"
	}
	resp.Version = subsonicAPIVersion
	resp.Xmlns = subsonicXmlns

	switch r.URL.Query().Get("f") {
	case "json", "jsonp":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		body, err := json.Marshal(map[string]*Response{"subsonic-response": resp})
		if err != nil {
			slog.Error("subsonic JSON 编码失败", "err", err)
			return
		}
		if cb := r.URL.Query().Get("callback"); r.URL.Query().Get("f") == "jsonp" && cb != "" {
			_, _ = w.Write([]byte(cb + "("))
			_, _ = w.Write(body)
			_, _ = w.Write([]byte(");"))
			return
		}
		_, _ = w.Write(body)
	default:
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(xml.Header))
		body, err := xml.Marshal(resp)
		if err != nil {
			slog.Error("subsonic XML 编码失败", "err", err)
			return
		}
		_, _ = w.Write(body)
	}
}

// writeError 输出 failed 状态的错误封套。
func writeError(w http.ResponseWriter, r *http.Request, code int, message string) {
	writeResponse(w, r, &Response{Status: "failed", Error: &Error{Code: code, Message: message}})
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -v`
Expected: PASS。

- [ ] **Step 5: 提交**
```bash
git add internal/api/subsonic/response.go internal/api/subsonic/response_test.go
git commit -m "feat(subsonic): 响应封套 + DTO（XML/JSON 双格式）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 2: 认证（auth.go）

**Files:** Create `internal/api/subsonic/auth.go`, `internal/api/subsonic/auth_test.go`

- [ ] **Step 1: 写失败测试** — `internal/api/subsonic/auth_test.go`:
```go
package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"testing"

	"github.com/yxx-z/lyra/internal/config"
)

func cfgWith(pw string, enabled bool) *config.Config {
	c := &config.Config{}
	c.Auth.Username = "admin"
	c.Subsonic.Password = pw
	c.Subsonic.Enabled = enabled
	return c
}

func TestAuth_PlainPassword(t *testing.T) {
	q := url.Values{"u": {"admin"}, "p": {"secret"}}
	if e := authenticate(q, cfgWith("secret", true)); e != nil {
		t.Errorf("正确明文密码应通过，得到 %+v", e)
	}
	q2 := url.Values{"u": {"admin"}, "p": {"wrong"}}
	if e := authenticate(q2, cfgWith("secret", true)); e == nil || e.Code != 40 {
		t.Errorf("错误密码应 40，得到 %+v", e)
	}
}

func TestAuth_EncPassword(t *testing.T) {
	enc := "enc:" + hex.EncodeToString([]byte("secret"))
	q := url.Values{"u": {"admin"}, "p": {enc}}
	if e := authenticate(q, cfgWith("secret", true)); e != nil {
		t.Errorf("enc: 密码应通过，得到 %+v", e)
	}
}

func TestAuth_TokenSalt(t *testing.T) {
	salt := "abc"
	sum := md5.Sum([]byte("secret" + salt))
	tok := hex.EncodeToString(sum[:])
	q := url.Values{"u": {"admin"}, "t": {tok}, "s": {salt}}
	if e := authenticate(q, cfgWith("secret", true)); e != nil {
		t.Errorf("正确 token 应通过，得到 %+v", e)
	}
	q2 := url.Values{"u": {"admin"}, "t": {"deadbeef"}, "s": {salt}}
	if e := authenticate(q2, cfgWith("secret", true)); e == nil || e.Code != 40 {
		t.Errorf("错误 token 应 40，得到 %+v", e)
	}
}

func TestAuth_WrongUser(t *testing.T) {
	q := url.Values{"u": {"bob"}, "p": {"secret"}}
	if e := authenticate(q, cfgWith("secret", true)); e == nil || e.Code != 40 {
		t.Errorf("错误用户名应 40，得到 %+v", e)
	}
}

func TestAuth_MissingParams(t *testing.T) {
	q := url.Values{"u": {"admin"}}
	if e := authenticate(q, cfgWith("secret", true)); e == nil || e.Code != 10 {
		t.Errorf("缺认证参数应 10，得到 %+v", e)
	}
}

func TestAuth_EmptyPasswordOrDisabled(t *testing.T) {
	q := url.Values{"u": {"admin"}, "p": {"secret"}}
	if e := authenticate(q, cfgWith("", true)); e == nil || e.Code != 40 {
		t.Errorf("空密码应 40，得到 %+v", e)
	}
	if e := authenticate(q, cfgWith("secret", false)); e == nil || e.Code != 40 {
		t.Errorf("禁用应 40，得到 %+v", e)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -run TestAuth -v`
Expected: 编译失败（undefined authenticate）。

- [ ] **Step 3: 实现** — `internal/api/subsonic/auth.go`:
```go
package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"strings"

	"net/url"

	"github.com/yxx-z/lyra/internal/config"
)

// authenticate 校验 Subsonic 请求参数；通过返回 nil，否则返回 *Error。
func authenticate(q url.Values, cfg *config.Config) *Error {
	if !cfg.Subsonic.Enabled {
		return &Error{Code: 40, Message: "Subsonic 未启用"}
	}
	pw := cfg.Subsonic.Password
	if pw == "" || q.Get("u") != cfg.Auth.Username {
		return &Error{Code: 40, Message: "用户名或密码错误"}
	}
	if p := q.Get("p"); p != "" {
		if strings.HasPrefix(p, "enc:") {
			if dec, err := hex.DecodeString(strings.TrimPrefix(p, "enc:")); err == nil {
				p = string(dec)
			}
		}
		if p == pw {
			return nil
		}
		return &Error{Code: 40, Message: "用户名或密码错误"}
	}
	if tok, salt := q.Get("t"), q.Get("s"); tok != "" && salt != "" {
		sum := md5.Sum([]byte(pw + salt))
		if hex.EncodeToString(sum[:]) == tok {
			return nil
		}
		return &Error{Code: 40, Message: "用户名或密码错误"}
	}
	return &Error{Code: 10, Message: "缺少认证参数"}
}
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -run TestAuth -v`
Expected: PASS。

- [ ] **Step 5: 提交**
```bash
git add internal/api/subsonic/auth.go internal/api/subsonic/auth_test.go
git commit -m "feat(subsonic): 认证（p / t+s / enc:）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 3: Handler + 路由 + ping/getLicense/getMusicFolders + 导出 v1 复用方法

**Files:** Create `internal/api/subsonic/handler.go`, `internal/api/subsonic/handler_test.go`; Modify `internal/api/v1/stream.go`, `internal/api/v1/cover.go`, `internal/api/router.go`

- [ ] **Step 1: 导出 v1 复用方法（先做，后续 Task 用）**

`internal/api/v1/stream.go`：把内部 `func (h *StreamHandler) stream(w, r, trackID string)` 重命名为导出的 `StreamByID`，并让 `Stream` 调用它：
```go
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	h.StreamByID(w, r, chi.URLParam(r, "id"))
}

// StreamByID 按 trackID 输出音频（直传或转码）。
func (h *StreamHandler) StreamByID(w http.ResponseWriter, r *http.Request, trackID string) {
```
（函数体不变，只改名 + 注释；内部 `h.serveTranscoded` 等不变。）

`internal/api/v1/cover.go`：把 `func (h *CoverHandler) getCover(w, r, albumID string)` 重命名为导出的 `ServeCover`，`GetCover` 调用它：
```go
func (h *CoverHandler) GetCover(w http.ResponseWriter, r *http.Request) {
	h.ServeCover(w, r, chi.URLParam(r, "id"))
}

// ServeCover 按 albumID 输出封面（内嵌→本地→cover_path）。
func (h *CoverHandler) ServeCover(w http.ResponseWriter, r *http.Request, albumID string) {
```
（函数体不变。若 cover.go 内有对 `getCover` 的其它调用一并改名。）

构建验证：`export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./internal/api/... && go test ./internal/api/v1/ 2>&1 | tail -5`（既有 v1 测试若调 `h.getCover`/`h.stream` 需同步改名为 `ServeCover`/`StreamByID`——grep `\.getCover(\|\.stream(` 改掉）。

- [ ] **Step 2: 写失败测试** — `internal/api/subsonic/handler_test.go`:
```go
package subsonic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
)

func testHandler(t *testing.T) (*Handler, *config.Config) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	cfg := &config.Config{}
	cfg.Auth.Username = "admin"
	cfg.Subsonic.Password = "secret"
	cfg.Subsonic.Enabled = true
	stream := v1.NewStreamHandler(d, cfg.Transcode, t.TempDir())
	cover := v1.NewCoverHandler(d)
	return NewHandler(d, cfg, stream, cover), cfg
}

// doReq 走完整 chi 路由（含认证中间件）。
func doReq(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/rest", h.RegisterRoutes)
	req := httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPing_OK(t *testing.T) {
	h, _ := testHandler(t)
	w := doReq(t, h, "/rest/ping.view?u=admin&p=secret&f=json")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("ping 失败: %d %s", w.Code, w.Body.String())
	}
}

func TestPing_AuthFail(t *testing.T) {
	h, _ := testHandler(t)
	w := doReq(t, h, "/rest/ping?u=admin&p=wrong&f=json")
	if !strings.Contains(w.Body.String(), `"status":"failed"`) || !strings.Contains(w.Body.String(), `"code":40`) {
		t.Errorf("认证失败应返回 failed/40: %s", w.Body.String())
	}
}

func TestGetLicense(t *testing.T) {
	h, _ := testHandler(t)
	w := doReq(t, h, "/rest/getLicense?u=admin&p=secret&f=json")
	if !strings.Contains(w.Body.String(), `"valid":true`) {
		t.Errorf("getLicense: %s", w.Body.String())
	}
}

func TestGetMusicFolders(t *testing.T) {
	h, _ := testHandler(t)
	w := doReq(t, h, "/rest/getMusicFolders?u=admin&p=secret&f=json")
	if !strings.Contains(w.Body.String(), `"musicFolder"`) {
		t.Errorf("getMusicFolders: %s", w.Body.String())
	}
}
```

- [ ] **Step 3: 实现** — `internal/api/subsonic/handler.go`:
```go
package subsonic

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/config"
)

// Handler 实现 Subsonic /rest 端点。
type Handler struct {
	db      *sql.DB
	cfg     *config.Config
	streamH *v1.StreamHandler // 字段名 streamH 以避开端点方法 stream 的命名冲突
	cover   *v1.CoverHandler
}

// NewHandler 创建 Subsonic handler，复用 v1 的 stream/cover。
func NewHandler(db *sql.DB, cfg *config.Config, stream *v1.StreamHandler, cover *v1.CoverHandler) *Handler {
	return &Handler{db: db, cfg: cfg, streamH: stream, cover: cover}
}

// reg 在 /rest 子路由上注册某端点的 /name 与 /name.view（GET+POST），套认证。
// 各 Task 增量调用 reg 注册自己实现的端点。
func (h *Handler) reg(r chi.Router, name string, fn http.HandlerFunc) {
	wrapped := h.withAuth(fn)
	r.Get("/"+name, wrapped)
	r.Get("/"+name+".view", wrapped)
	r.Post("/"+name, wrapped)
	r.Post("/"+name+".view", wrapped)
}

// RegisterRoutes 注册本期全部 Subsonic 端点。
// 注意：后续 Task（4/5/6）会往这里增量添加各自端点的 h.reg(...) 行。
func (h *Handler) RegisterRoutes(r chi.Router) {
	h.reg(r, "ping", h.ping)
	h.reg(r, "getLicense", h.getLicense)
	h.reg(r, "getMusicFolders", h.getMusicFolders)
	// Task 4: getArtists/getArtist/getAlbum/getAlbumList2/getSong
	// Task 5: search3
	// Task 6: getCoverArt/stream/scrobble
}

// withAuth 在调用真正 handler 前校验 Subsonic 认证。
func (h *Handler) withAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if e := authenticate(r.Form, h.cfg); e != nil {
			writeError(w, r, e.Code, e.Message)
			return
		}
		fn(w, r)
	}
}

func (h *Handler) ping(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, r, &Response{})
}

func (h *Handler) getLicense(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, r, &Response{License: &License{Valid: true}})
}

func (h *Handler) getMusicFolders(w http.ResponseWriter, r *http.Request) {
	writeResponse(w, r, &Response{MusicFolders: &MusicFolders{
		Folder: []MusicFolder{{ID: 0, Name: "Music"}},
	}})
}
```
注意：`withAuth` 用 `r.ParseForm()` + `r.Form`，这样 GET 查询参数和 POST 表单参数都能取到；`authenticate` 接收 `url.Values`，传 `r.Form`。

- [ ] **Step 4: 挂载路由** — `internal/api/router.go`：

import 加 `"github.com/yxx-z/lyra/internal/api/subsonic"`。在 `r.Route("/api/v1", ...)` 块**之后**、`r.Handle("/*", ...)` **之前**加：
```go
	subStream := v1.NewStreamHandler(db, cfg.Transcode, cfg.Cache.TranscodeDir)
	subCover := v1.NewCoverHandler(db)
	subHandler := subsonic.NewHandler(db, cfg, subStream, subCover)
	r.Route("/rest", subHandler.RegisterRoutes)
```

- [ ] **Step 5: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./internal/api/... -v 2>&1 | tail -20`
Expected: build 成功；ping/license/folders/auth 测试 + 既有 api 测试 PASS。

- [ ] **Step 6: 提交**
```bash
git add internal/api/subsonic/handler.go internal/api/subsonic/handler_test.go internal/api/v1/stream.go internal/api/v1/cover.go internal/api/router.go
git commit -m "feat(subsonic): Handler + /rest 路由 + 认证中间件 + ping/license/folders"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 4: 浏览端点（browse.go）

**Files:** Create `internal/api/subsonic/browse.go`, `internal/api/subsonic/browse_test.go`

- [ ] **Step 1: 写失败测试** — `internal/api/subsonic/browse_test.go`:
```go
package subsonic

import (
	"database/sql"
	"strings"
	"testing"
)

// seed 插入 1 艺术家 + 1 专辑 + 2 曲目。
func seed(t *testing.T, d *sql.DB) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('ar1','周杰伦')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id,release_date,genre) VALUES('al1','叶惠美','ar1','2003-07-31','Mandopop')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,track_number,disc_number,duration,file_path,format,bitrate,is_available) VALUES('t1','以父之名','ar1','al1',1,1,342,'/m/1.m4a','m4a',320,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,track_number,disc_number,duration,file_path,format,bitrate,is_available) VALUES('t2','晴天','ar1','al1',3,1,269,'/m/3.m4a','m4a',320,1)`); err != nil {
		t.Fatal(err)
	}
}

func TestGetArtists(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getArtists?u=admin&p=secret&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"id":"ar1"`) || !strings.Contains(b, `周杰伦`) || !strings.Contains(b, `"albumCount":1`) {
		t.Errorf("getArtists: %s", b)
	}
}

func TestGetArtist(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getArtist?u=admin&p=secret&id=ar1&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"id":"al1"`) || !strings.Contains(b, `叶惠美`) {
		t.Errorf("getArtist 应含其专辑: %s", b)
	}
}

func TestGetAlbum(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getAlbum?u=admin&p=secret&id=al1&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"songCount":2`) || !strings.Contains(b, `以父之名`) || !strings.Contains(b, `晴天`) {
		t.Errorf("getAlbum 应含 2 曲: %s", b)
	}
}

func TestGetSong(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getSong?u=admin&p=secret&id=t1&f=json")
	if !strings.Contains(w.Body.String(), `以父之名`) {
		t.Errorf("getSong: %s", w.Body.String())
	}
	w2 := doReq(t, h, "/rest/getSong?u=admin&p=secret&id=nope&f=json")
	if !strings.Contains(w2.Body.String(), `"code":70`) {
		t.Errorf("不存在曲目应 70: %s", w2.Body.String())
	}
}

func TestGetAlbumList2(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getAlbumList2?u=admin&p=secret&type=newest&f=json")
	if !strings.Contains(w.Body.String(), `"id":"al1"`) {
		t.Errorf("getAlbumList2: %s", w.Body.String())
	}
	w2 := doReq(t, h, "/rest/getAlbumList2?u=admin&p=secret&type=bogus&f=json")
	if !strings.Contains(w2.Body.String(), `"code":10`) {
		t.Errorf("未知 type 应 10: %s", w2.Body.String())
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -run 'TestGetArtist|TestGetAlbum|TestGetSong' -v`
Expected: 编译失败（端点未定义）。

- [ ] **Step 3: 实现** — `internal/api/subsonic/browse.go`:
```go
package subsonic

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

var suffixContentType = map[string]string{
	"mp3": "audio/mpeg", "flac": "audio/flac", "m4a": "audio/mp4", "aac": "audio/aac",
	"ogg": "audio/ogg", "opus": "audio/ogg", "wav": "audio/wav",
}

func contentTypeFor(format string) string {
	if ct, ok := suffixContentType[strings.ToLower(format)]; ok {
		return ct
	}
	return "audio/mpeg"
}

func yearFromDate(d string) int {
	if len(d) >= 4 {
		y, _ := strconv.Atoi(d[:4])
		return y
	}
	return 0
}

// childFromRow 从一行 tracks 扫描结果构造 Child。
func scanChild(rows *sql.Rows) (Child, error) {
	var c Child
	var albumTitle, artistName, format string
	var track, year, duration, bitrate int
	if err := rows.Scan(&c.ID, &c.Title, &c.AlbumID, &c.ArtistID, &albumTitle, &artistName,
		&track, &duration, &bitrate, &format, &year); err != nil {
		return Child{}, err
	}
	c.IsDir = false
	c.Parent = c.AlbumID
	c.Album = albumTitle
	c.Artist = artistName
	c.Track = track
	c.Year = year
	c.Duration = duration
	c.BitRate = bitrate
	c.Suffix = strings.ToLower(format)
	c.ContentType = contentTypeFor(format)
	c.CoverArt = c.AlbumID
	c.Type = "music"
	return c, nil
}

const trackSelect = `
	SELECT tr.id, tr.title, COALESCE(tr.album_id,''), COALESCE(tr.artist_id,''),
	       COALESCE(al.title,''), COALESCE(ar.name,''),
	       COALESCE(tr.track_number,0), COALESCE(tr.duration,0), COALESCE(tr.bitrate,0),
	       COALESCE(tr.format,''), CAST(substr(COALESCE(al.release_date,''),1,4) AS INTEGER)
	FROM tracks tr
	LEFT JOIN albums al ON al.id=tr.album_id
	LEFT JOIN artists ar ON ar.id=tr.artist_id`

func (h *Handler) getArtists(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`
		SELECT ar.id, ar.name, COUNT(al.id)
		FROM artists ar
		LEFT JOIN albums al ON al.artist_id=ar.id
		GROUP BY ar.id HAVING COUNT(al.id) > 0
		ORDER BY ar.name`)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	defer rows.Close()
	idx := map[string]*IndexID3{}
	var order []string
	for rows.Next() {
		var a ArtistID3
		if err := rows.Scan(&a.ID, &a.Name, &a.AlbumCount); err != nil {
			continue
		}
		key := indexKey(a.Name)
		if idx[key] == nil {
			idx[key] = &IndexID3{Name: key}
			order = append(order, key)
		}
		idx[key].Artist = append(idx[key].Artist, a)
	}
	artists := &ArtistsID3{IgnoredArticles: ""}
	for _, k := range order {
		artists.Index = append(artists.Index, *idx[k])
	}
	writeResponse(w, r, &Response{Artists: artists})
}

func indexKey(name string) string {
	for _, ru := range name {
		if unicode.IsLetter(ru) && ru < 128 {
			return strings.ToUpper(string(ru))
		}
		return "#"
	}
	return "#"
}

func (h *Handler) getArtist(w http.ResponseWriter, r *http.Request) {
	id := r.Form.Get("id")
	var a ArtistID3
	err := h.db.QueryRow(`SELECT id, name FROM artists WHERE id=?`, id).Scan(&a.ID, &a.Name)
	if err != nil {
		writeError(w, r, 70, "艺术家不存在")
		return
	}
	rows, err := h.db.Query(`
		SELECT al.id, al.title, COALESCE(al.release_date,''), COALESCE(al.genre,''),
		       (SELECT COUNT(*) FROM tracks WHERE album_id=al.id AND is_available=1),
		       (SELECT COALESCE(SUM(duration),0) FROM tracks WHERE album_id=al.id AND is_available=1)
		FROM albums al WHERE al.artist_id=? ORDER BY al.release_date`, id)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var al AlbumID3
		var date, genre string
		if err := rows.Scan(&al.ID, &al.Name, &date, &genre, &al.SongCount, &al.Duration); err != nil {
			continue
		}
		al.Artist = a.Name
		al.ArtistID = a.ID
		al.CoverArt = al.ID
		al.Year = yearFromDate(date)
		al.Genre = genre
		a.Album = append(a.Album, al)
	}
	a.AlbumCount = len(a.Album)
	writeResponse(w, r, &Response{Artist: &a})
}

func (h *Handler) getAlbum(w http.ResponseWriter, r *http.Request) {
	id := r.Form.Get("id")
	var al AlbumID3
	var date, genre, artistID string
	err := h.db.QueryRow(`
		SELECT al.id, al.title, COALESCE(al.artist_id,''), COALESCE(ar.name,''),
		       COALESCE(al.release_date,''), COALESCE(al.genre,'')
		FROM albums al LEFT JOIN artists ar ON ar.id=al.artist_id
		WHERE al.id=?`, id).Scan(&al.ID, &al.Name, &artistID, &al.Artist, &date, &genre)
	if err != nil {
		writeError(w, r, 70, "专辑不存在")
		return
	}
	al.ArtistID = artistID
	al.CoverArt = al.ID
	al.Year = yearFromDate(date)
	al.Genre = genre

	rows, err := h.db.Query(trackSelect+` WHERE tr.album_id=? AND tr.is_available=1 ORDER BY tr.disc_number, tr.track_number, tr.title`, id)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanChild(rows)
		if err != nil {
			continue
		}
		al.Song = append(al.Song, c)
	}
	al.SongCount = len(al.Song)
	for _, s := range al.Song {
		al.Duration += s.Duration
	}
	writeResponse(w, r, &Response{Album: &al})
}

func (h *Handler) getSong(w http.ResponseWriter, r *http.Request) {
	id := r.Form.Get("id")
	rows, err := h.db.Query(trackSelect+` WHERE tr.id=? AND tr.is_available=1`, id)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	defer rows.Close()
	if !rows.Next() {
		writeError(w, r, 70, "曲目不存在")
		return
	}
	c, err := scanChild(rows)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	writeResponse(w, r, &Response{Song: &c})
}

func (h *Handler) getAlbumList2(w http.ResponseWriter, r *http.Request) {
	typ := r.Form.Get("type")
	var orderBy string
	switch typ {
	case "newest", "recent":
		orderBy = "al.created_at DESC"
	case "alphabeticalByName", "":
		orderBy = "al.title"
	case "random":
		orderBy = "RANDOM()"
	default:
		writeError(w, r, 10, "不支持的 type")
		return
	}
	size := atoiDefault(r.Form.Get("size"), 10)
	if size > 500 {
		size = 500
	}
	offset := atoiDefault(r.Form.Get("offset"), 0)

	rows, err := h.db.Query(`
		SELECT al.id, al.title, COALESCE(al.artist_id,''), COALESCE(ar.name,''),
		       COALESCE(al.release_date,''), COALESCE(al.genre,''),
		       (SELECT COUNT(*) FROM tracks WHERE album_id=al.id AND is_available=1),
		       (SELECT COALESCE(SUM(duration),0) FROM tracks WHERE album_id=al.id AND is_available=1)
		FROM albums al LEFT JOIN artists ar ON ar.id=al.artist_id
		ORDER BY `+orderBy+` LIMIT ? OFFSET ?`, size, offset)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	defer rows.Close()
	list := &AlbumList2{}
	for rows.Next() {
		var al AlbumID3
		var date, genre string
		if err := rows.Scan(&al.ID, &al.Name, &al.ArtistID, &al.Artist, &date, &genre, &al.SongCount, &al.Duration); err != nil {
			continue
		}
		al.CoverArt = al.ID
		al.Year = yearFromDate(date)
		al.Genre = genre
		list.Album = append(list.Album, al)
	}
	writeResponse(w, r, &Response{AlbumList2: list})
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n
	}
	return def
}
```

- [ ] **Step 3b: 注册路由** — 在 `internal/api/subsonic/handler.go` 的 `RegisterRoutes` 里，`// Task 4:` 注释处加：
```go
	h.reg(r, "getArtists", h.getArtists)
	h.reg(r, "getArtist", h.getArtist)
	h.reg(r, "getAlbum", h.getAlbum)
	h.reg(r, "getAlbumList2", h.getAlbumList2)
	h.reg(r, "getSong", h.getSong)
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -v`
Expected: PASS（含本包全部）。

- [ ] **Step 5: 提交**
```bash
git add internal/api/subsonic/browse.go internal/api/subsonic/browse_test.go internal/api/subsonic/handler.go
git commit -m "feat(subsonic): getArtists/getArtist/getAlbum/getSong/getAlbumList2"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 5: search3（search.go）

**Files:** Create `internal/api/subsonic/search.go`, `internal/api/subsonic/search_test.go`

- [ ] **Step 1: 写失败测试** — `internal/api/subsonic/search_test.go`:
```go
package subsonic

import (
	"strings"
	"testing"
)

func TestSearch3(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/search3?u=admin&p=secret&query=晴天&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `晴天`) || !strings.Contains(b, `"searchResult3"`) {
		t.Errorf("search3 应命中曲目: %s", b)
	}
}

func TestSearch3_ArtistAlbum(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/search3?u=admin&p=secret&query=叶惠美&f=json")
	if !strings.Contains(w.Body.String(), `"id":"al1"`) {
		t.Errorf("search3 应命中专辑: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -run TestSearch3 -v`
Expected: 编译失败（search3 未定义）。

- [ ] **Step 3: 实现** — `internal/api/subsonic/search.go`:
```go
package subsonic

import (
	"net/http"
	"strings"
)

func (h *Handler) search3(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.Form.Get("query"))
	like := "%" + q + "%"
	if q == "" || q == "*" {
		like = "%"
	}
	res := &SearchResult3{}

	// 艺术家
	artistCount := atoiDefault(r.Form.Get("artistCount"), 20)
	if rows, err := h.db.Query(`SELECT id,name FROM artists WHERE name LIKE ? ORDER BY name LIMIT ?`, like, artistCount); err == nil {
		for rows.Next() {
			var a ArtistID3
			if err := rows.Scan(&a.ID, &a.Name); err == nil {
				res.Artist = append(res.Artist, a)
			}
		}
		rows.Close()
	}

	// 专辑
	albumCount := atoiDefault(r.Form.Get("albumCount"), 20)
	if rows, err := h.db.Query(`
		SELECT al.id, al.title, COALESCE(al.artist_id,''), COALESCE(ar.name,''),
		       COALESCE(al.release_date,''),
		       (SELECT COUNT(*) FROM tracks WHERE album_id=al.id AND is_available=1)
		FROM albums al LEFT JOIN artists ar ON ar.id=al.artist_id
		WHERE al.title LIKE ? ORDER BY al.title LIMIT ?`, like, albumCount); err == nil {
		for rows.Next() {
			var al AlbumID3
			var date string
			if err := rows.Scan(&al.ID, &al.Name, &al.ArtistID, &al.Artist, &date, &al.SongCount); err == nil {
				al.CoverArt = al.ID
				al.Year = yearFromDate(date)
				res.Album = append(res.Album, al)
			}
		}
		rows.Close()
	}

	// 曲目
	songCount := atoiDefault(r.Form.Get("songCount"), 20)
	if rows, err := h.db.Query(trackSelect+` WHERE tr.title LIKE ? AND tr.is_available=1 ORDER BY tr.title LIMIT ?`, like, songCount); err == nil {
		for rows.Next() {
			if c, err := scanChild(rows); err == nil {
				res.Song = append(res.Song, c)
			}
		}
		rows.Close()
	}

	writeResponse(w, r, &Response{SearchResult3: res})
}
```

- [ ] **Step 3b: 注册路由** — 在 `handler.go` 的 `RegisterRoutes` 里 `// Task 5:` 注释处加：
```go
	h.reg(r, "search3", h.search3)
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -run TestSearch3 -v`
Expected: PASS。

- [ ] **Step 5: 提交**
```bash
git add internal/api/subsonic/search.go internal/api/subsonic/search_test.go internal/api/subsonic/handler.go
git commit -m "feat(subsonic): search3"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

### Task 6: stream/getCoverArt/scrobble（media.go）

**Files:** Create `internal/api/subsonic/media.go`, `internal/api/subsonic/media_test.go`

- [ ] **Step 1: 写失败测试** — `internal/api/subsonic/media_test.go`:
```go
package subsonic

import (
	"strings"
	"testing"
)

func TestScrobble(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/scrobble?u=admin&p=secret&id=t1&f=json")
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("scrobble: %s", w.Body.String())
	}
	var pc int
	h.db.QueryRow(`SELECT play_count FROM tracks WHERE id='t1'`).Scan(&pc)
	if pc != 1 {
		t.Errorf("play_count 应为 1，得到 %d", pc)
	}
}

func TestStream_NotFound(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/stream?u=admin&p=secret&id=nope&f=json")
	// 不存在曲目 → v1 StreamByID 写 404（http.NotFound），主体非 subsonic 封套；只验状态码
	if w.Code != 404 {
		t.Errorf("不存在曲目 stream 应 404，得到 %d", w.Code)
	}
}

func TestGetCoverArt_NotFound(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// al1 无内嵌/本地/cover_path 封面 → 404
	w := doReq(t, h, "/rest/getCoverArt?u=admin&p=secret&id=al1&f=json")
	if w.Code != 404 {
		t.Errorf("无封面应 404，得到 %d", w.Code)
	}
}
```

- [ ] **Step 2: 运行确认失败** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/ -run 'TestScrobble|TestStream|TestGetCoverArt' -v`
Expected: 编译失败（端点未定义）。

- [ ] **Step 3: 实现** — `internal/api/subsonic/media.go`（`Handler` 字段已是 `streamH`，端点方法 `stream` 与之不冲突）:
```go
package subsonic

import "net/http"

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	// 复用 v1 转码/直传管线（按 trackID）；不存在曲目时 v1 写 404。
	h.streamH.StreamByID(w, r, r.Form.Get("id"))
}

func (h *Handler) getCoverArt(w http.ResponseWriter, r *http.Request) {
	// 复用 v1 封面优先级（内嵌→本地→cover_path）；找不到写 404。
	h.cover.ServeCover(w, r, r.Form.Get("id"))
}

func (h *Handler) scrobble(w http.ResponseWriter, r *http.Request) {
	if id := r.Form.Get("id"); id != "" {
		_, _ = h.db.Exec(`UPDATE tracks SET play_count=play_count+1, last_played_at=CURRENT_TIMESTAMP WHERE id=?`, id)
	}
	writeResponse(w, r, &Response{})
}
```

- [ ] **Step 3b: 注册路由** — 在 `handler.go` 的 `RegisterRoutes` 里 `// Task 6:` 注释处加：
```go
	h.reg(r, "getCoverArt", h.getCoverArt)
	h.reg(r, "stream", h.stream)
	h.reg(r, "scrobble", h.scrobble)
```

- [ ] **Step 4: 运行确认通过** — `export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./internal/api/subsonic/ -v 2>&1 | tail -20`
Expected: PASS（本包全部）。

- [ ] **Step 5: 提交**
```bash
git add internal/api/subsonic/media.go internal/api/subsonic/handler.go
git commit -m "feat(subsonic): stream/getCoverArt/scrobble（复用 v1）"
```
End commit body with: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## 完成标准

- `go test ./...` 全绿；`go build ./...` 成功
- `/rest` 12 端点可用,认证(p/t+s)生效,XML 与 JSON 两种格式都正确
- 第三方客户端可浏览(艺术家/专辑/曲目/搜索)并播放(stream)
- 全部测试 httptest + 内存 sqlite,不打真网络

## 验证（手动，docker）

1. 在 config.yaml 配 `subsonic.password: <密码>`；`make docker-build && docker compose up -d`
2. 手机装 Symfonium/DSub，服务器填 `http://<host>:4533`、用户名 admin、密码 = subsonic.password
3. 应能浏览艺术家/专辑、搜索、播放；`curl "http://127.0.0.1:4533/rest/ping.view?u=admin&p=<pw>&f=json"` 返回 status ok
