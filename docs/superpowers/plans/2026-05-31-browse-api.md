# 浏览 REST API 实现计划

> **给 AI 工作者：** 必须使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务执行本计划。步骤使用复选框（`- [ ]`）语法追踪进度。

**目标：** 实现 Lyra Web UI 所需的后端 REST API：浏览专辑/艺术家、音频流、封面图、搜索，以及用户名+密码认证。

**架构：** 薄 handler 层直接查 SQLite，无 service 中间层。`NewRouter` 签名扩展为接收 `*sql.DB` 和 `*config.Config`。认证中间件保护所有 `/api/v1/*` 路由（`/auth/login` 除外）。

**技术栈：** Go 1.25+、Chi v5、modernc SQLite、dhowden/tag（封面提取）、`crypto/rand`（token 生成）

---

## 前置条件

```bash
export PATH=$PATH:/home/yxx/go-local/go/bin
cd /home/yxx/develop/Lyra
```

---

## 文件结构

```
internal/config/config.go          ← 新增 Username、Password 字段
config.example.yaml                ← 同步更新
cmd/server/main.go                 ← token 生成逻辑

internal/api/middleware/
└── auth.go                        ← BearerAuth 中间件（新建）
└── auth_test.go

internal/api/v1/
├── auth.go                        ← POST /auth/login（新建）
├── auth_test.go
├── testhelpers_test.go            ← 共用测试辅助函数（新建）
├── albums.go                      ← GET /albums, GET /albums/:id（新建）
├── albums_test.go
├── artists.go                     ← GET /artists, GET /artists/:id（新建）
├── artists_test.go
├── cover.go                       ← GET /cover/:id（新建）
├── cover_test.go
├── stream.go                      ← GET /tracks/:id/stream（新建）
├── stream_test.go
├── search.go                      ← GET /search?q=（新建）
├── search_test.go
├── library.go                     ← 已有，保持不动
└── library_test.go                ← 已有，保持不动

internal/api/router.go             ← 签名扩展，注册所有新路由
internal/api/router_test.go        ← 更新以适配新签名
```

---

## 任务 1：Config 新增认证字段 + Token 生成

**文件：**
- 修改：`internal/config/config.go`
- 修改：`config.example.yaml`
- 修改：`cmd/server/main.go`

- [ ] **步骤 1：更新 config_test.go，先确认失败**

在 `internal/config/config_test.go` 末尾追加：

```go
func TestDefault_AuthUsernameIsAdmin(t *testing.T) {
	cfg := Default()
	if cfg.Auth.Username != "admin" {
		t.Errorf("want username=admin, got %q", cfg.Auth.Username)
	}
}
```

运行确认失败：
```bash
go test ./internal/config/... -run TestDefault_AuthUsernameIsAdmin -v
```

预期：FAIL（`Username` 字段不存在）

- [ ] **步骤 2：修改 AuthConfig 结构体**

找到 `internal/config/config.go` 中的 `AuthConfig`：

```go
type AuthConfig struct {
	Disable bool   `yaml:"disable"`
	Token   string `yaml:"token"`
}
```

替换为：

```go
type AuthConfig struct {
	Disable  bool   `yaml:"disable"`
	Token    string `yaml:"token"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}
```

同时在 `Default()` 函数中，将 `Auth: AuthConfig{Disable: false},` 改为：

```go
Auth: AuthConfig{Disable: false, Username: "admin"},
```

- [ ] **步骤 3：运行配置测试，确认通过**

```bash
go test ./internal/config/... -v
```

预期：所有测试 PASS

- [ ] **步骤 4：更新 config.example.yaml**

在 `auth:` 段落中，在 `token: ""` 行之后追加：

```yaml
  username: admin        # Web UI 登录用户名
  password: ""           # Web UI 登录密码，留空时启动打印警告
```

- [ ] **步骤 5：更新 main.go，添加 Token 生成和密码警告**

在 `cmd/server/main.go` 的 import 块中加入 `"crypto/rand"` 和 `"encoding/hex"`。

找到 `sc := scanner.NewScanner(database, cfg.Library)` 这一行，在它**之前**插入：

```go
// 若 token 为空且认证未禁用，自动生成 token 并打印
if cfg.Auth.Token == "" && !cfg.Auth.Disable {
    b := make([]byte, 16)
    if _, err := rand.Read(b); err != nil {
        slog.Error("生成 token 失败", "err", err)
        os.Exit(1)
    }
    cfg.Auth.Token = hex.EncodeToString(b)
    slog.Info("已生成认证 Token（请在 config.yaml 中固化）", "token", cfg.Auth.Token)
}
if cfg.Auth.Password == "" && !cfg.Auth.Disable {
    slog.Warn("auth.password 未设置，请在 config.yaml 中配置登录密码")
}
```

- [ ] **步骤 6：验证编译**

```bash
go build ./cmd/server
rm -f server
```

预期：编译成功

- [ ] **步骤 7：提交**

```bash
git add internal/config/ config.example.yaml cmd/server/main.go go.mod go.sum
git commit -m "feat: config 新增 Username/Password，main.go 自动生成 Token"
```

---

## 任务 2：Auth 中间件 + 登录端点

**文件：**
- 创建：`internal/api/middleware/auth.go`
- 创建：`internal/api/middleware/auth_test.go`
- 创建：`internal/api/v1/auth.go`
- 创建：`internal/api/v1/auth_test.go`

- [ ] **步骤 1：写中间件失败测试**

```go
// internal/api/middleware/auth_test.go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

func TestBearerAuth_ValidToken(t *testing.T) {
	h := BearerAuth("secret", false)(http.HandlerFunc(okHandler))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

func TestBearerAuth_InvalidToken(t *testing.T) {
	h := BearerAuth("secret", false)(http.HandlerFunc(okHandler))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestBearerAuth_NoToken(t *testing.T) {
	h := BearerAuth("secret", false)(http.HandlerFunc(okHandler))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestBearerAuth_Disabled(t *testing.T) {
	h := BearerAuth("", true)(http.HandlerFunc(okHandler))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}
```

运行确认失败：
```bash
go test ./internal/api/middleware/... -v
```

预期：FAIL（`BearerAuth` 未定义）

- [ ] **步骤 2：实现 auth.go 中间件**

创建 `internal/api/middleware/auth.go`：

```go
// internal/api/middleware/auth.go
package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// BearerAuth returns a middleware that validates the Authorization: Bearer header.
// If disabled is true, all requests are passed through without validation.
func BearerAuth(token string, disabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if disabled {
				next.ServeHTTP(w, r)
				return
			}
			parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" || parts[1] != token {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				if err := json.NewEncoder(w).Encode(map[string]string{"error": "未授权"}); err != nil {
					slog.Error("写响应失败", "err", err)
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **步骤 3：运行中间件测试，确认通过**

```bash
go test ./internal/api/middleware/... -v
```

预期：4 个测试全部 PASS

- [ ] **步骤 4：写登录端点失败测试**

创建 `internal/api/v1/auth_test.go`：

```go
// internal/api/v1/auth_test.go
package v1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/config"
)

func newTestAuthHandler() *AuthHandler {
	return NewAuthHandler(&config.Config{
		Auth: config.AuthConfig{
			Username: "admin",
			Password: "pass123",
			Token:    "test-token",
		},
	})
}

func TestLogin_Success(t *testing.T) {
	h := newTestAuthHandler()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "pass123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["token"] != "test-token" {
		t.Errorf("want token=test-token, got %q", resp["token"])
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h := newTestAuthHandler()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestLogin_BadJSON(t *testing.T) {
	h := newTestAuthHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}
```

- [ ] **步骤 5：运行确认失败**

```bash
go test ./internal/api/v1/... -run "TestLogin" -v
```

预期：FAIL（`AuthHandler`、`NewAuthHandler` 未定义）

- [ ] **步骤 6：实现登录端点**

创建 `internal/api/v1/auth.go`：

```go
// internal/api/v1/auth.go
package v1

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/yxx-z/lyra/internal/config"
)

// AuthHandler handles /api/v1/auth/* endpoints.
type AuthHandler struct {
	cfg *config.Config
}

// NewAuthHandler creates an AuthHandler backed by cfg.
func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{cfg: cfg}
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		if err2 := json.NewEncoder(w).Encode(map[string]string{"error": "请求格式错误"}); err2 != nil {
			slog.Error("写响应失败", "err", err2)
		}
		return
	}
	if req.Username != h.cfg.Auth.Username || req.Password != h.cfg.Auth.Password {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "用户名或密码错误"}); err != nil {
			slog.Error("写响应失败", "err", err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"token": h.cfg.Auth.Token}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
```

- [ ] **步骤 7：运行所有新测试，确认通过**

```bash
go test ./internal/api/middleware/... ./internal/api/v1/... -run "TestBearerAuth|TestLogin" -v
```

预期：7 个测试全部 PASS

- [ ] **步骤 8：提交**

```bash
git add internal/api/middleware/ internal/api/v1/auth.go internal/api/v1/auth_test.go
git commit -m "feat: auth 中间件 + 登录端点"
```

---

## 任务 3：Albums 端点

**文件：**
- 创建：`internal/api/v1/testhelpers_test.go`
- 创建：`internal/api/v1/albums.go`
- 创建：`internal/api/v1/albums_test.go`

- [ ] **步骤 1：创建共用测试辅助**

创建 `internal/api/v1/testhelpers_test.go`：

```go
// internal/api/v1/testhelpers_test.go
package v1

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

// insertTestData 插入一组标准测试数据：1 个艺术家，1 个专辑，2 首曲目。
func insertTestData(t *testing.T, d *sql.DB) {
	t.Helper()
	stmts := []string{
		`INSERT INTO artists(id,name,created_at,updated_at) VALUES('a1','蔡琴',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO albums(id,title,artist_id,release_date,created_at,updated_at) VALUES('al1','金片子','a1','1984',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		`INSERT INTO tracks(id,title,artist_id,album_id,track_number,disc_number,duration,file_path,format,is_available,created_at,updated_at,scrape_status) VALUES('t1','渡口','a1','al1',1,1,245,'/tmp/t1.flac','flac',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'pending')`,
		`INSERT INTO tracks(id,title,artist_id,album_id,track_number,disc_number,duration,file_path,format,is_available,created_at,updated_at,scrape_status) VALUES('t2','被遗忘的时光','a1','al1',2,1,210,'/tmp/t2.flac','flac',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'pending')`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("insertTestData: %v", err)
		}
	}
}
```

- [ ] **步骤 2：写失败测试**

创建 `internal/api/v1/albums_test.go`：

```go
// internal/api/v1/albums_test.go
package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAlbums_ReturnsAlbums(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewAlbumsHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums", nil)
	w := httptest.NewRecorder()
	h.ListAlbums(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Albums []map[string]interface{} `json:"albums"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Albums) != 1 {
		t.Fatalf("want 1 album, got %d", len(resp.Albums))
	}
	if resp.Albums[0]["title"] != "金片子" {
		t.Errorf("want title=金片子, got %v", resp.Albums[0]["title"])
	}
	if resp.Albums[0]["artist"] != "蔡琴" {
		t.Errorf("want artist=蔡琴, got %v", resp.Albums[0]["artist"])
	}
	if resp.Albums[0]["track_count"].(float64) != 2 {
		t.Errorf("want track_count=2, got %v", resp.Albums[0]["track_count"])
	}
}

func TestGetAlbum_ReturnsTracks(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewAlbumsHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums/al1", nil)
	// chi URLParam は router でセットされるため、ここでは直接呼ぶ
	w := httptest.NewRecorder()
	h.getAlbum(w, req, "al1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["title"] != "金片子" {
		t.Errorf("want title=金片子, got %v", resp["title"])
	}
	tracks := resp["tracks"].([]interface{})
	if len(tracks) != 2 {
		t.Fatalf("want 2 tracks, got %d", len(tracks))
	}
}

func TestGetAlbum_NotFound(t *testing.T) {
	d := newTestDB(t)
	h := NewAlbumsHandler(d)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums/nonexistent", nil)
	h.getAlbum(w, req, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
```

- [ ] **步骤 3：运行确认失败**

```bash
go test ./internal/api/v1/... -run "TestListAlbums|TestGetAlbum" -v
```

预期：FAIL（`NewAlbumsHandler` 未定义）

- [ ] **步骤 4：实现 albums.go**

创建 `internal/api/v1/albums.go`：

```go
// internal/api/v1/albums.go
package v1

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// AlbumSummary is returned in the list endpoint.
type AlbumSummary struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	ArtistID   string `json:"artist_id"`
	Year       int    `json:"year"`
	TrackCount int    `json:"track_count"`
	CoverURL   string `json:"cover_url"`
}

// TrackSummary is returned inside an album detail.
type TrackSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	TrackNumber int    `json:"track_number"`
	DiscNumber  int    `json:"disc_number"`
	Duration    int    `json:"duration"`
	Format      string `json:"format"`
	Bitrate     int    `json:"bitrate"`
	StreamURL   string `json:"stream_url"`
}

// AlbumDetail is returned by the single album endpoint.
type AlbumDetail struct {
	AlbumSummary
	Tracks []TrackSummary `json:"tracks"`
}

// AlbumsHandler handles /api/v1/albums/* endpoints.
type AlbumsHandler struct {
	db *sql.DB
}

// NewAlbumsHandler creates an AlbumsHandler backed by db.
func NewAlbumsHandler(db *sql.DB) *AlbumsHandler {
	return &AlbumsHandler{db: db}
}

// ListAlbums handles GET /api/v1/albums.
func (h *AlbumsHandler) ListAlbums(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`
		SELECT a.id, a.title, COALESCE(ar.name,''), COALESCE(a.artist_id,''),
		       COALESCE(a.release_date,''), COUNT(t.id)
		FROM albums a
		LEFT JOIN artists ar ON a.artist_id = ar.id
		LEFT JOIN tracks t ON t.album_id = a.id AND t.is_available = 1
		GROUP BY a.id
		ORDER BY ar.name, a.title`)
	if err != nil {
		http.Error(w, `{"error":"查询失败"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	albums := make([]AlbumSummary, 0)
	for rows.Next() {
		var al AlbumSummary
		var releaseDate string
		if err := rows.Scan(&al.ID, &al.Title, &al.Artist, &al.ArtistID, &releaseDate, &al.TrackCount); err != nil {
			continue
		}
		al.Year, _ = strconv.Atoi(releaseDate)
		al.CoverURL = "/api/v1/cover/" + al.ID
		albums = append(albums, al)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"albums": albums}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}

// GetAlbum handles GET /api/v1/albums/:id.
func (h *AlbumsHandler) GetAlbum(w http.ResponseWriter, r *http.Request) {
	h.getAlbum(w, r, chi.URLParam(r, "id"))
}

func (h *AlbumsHandler) getAlbum(w http.ResponseWriter, r *http.Request, id string) {
	var al AlbumDetail
	var releaseDate string
	err := h.db.QueryRow(`
		SELECT a.id, a.title, COALESCE(ar.name,''), COALESCE(a.artist_id,''), COALESCE(a.release_date,'')
		FROM albums a
		LEFT JOIN artists ar ON a.artist_id = ar.id
		WHERE a.id = ?`, id).
		Scan(&al.ID, &al.Title, &al.Artist, &al.ArtistID, &releaseDate)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, `{"error":"查询失败"}`, http.StatusInternalServerError)
		return
	}
	al.Year, _ = strconv.Atoi(releaseDate)
	al.CoverURL = "/api/v1/cover/" + al.ID

	rows, err := h.db.Query(`
		SELECT id, title, track_number, disc_number, duration, format, bitrate
		FROM tracks
		WHERE album_id = ? AND is_available = 1
		ORDER BY disc_number, track_number, title`, id)
	if err != nil {
		http.Error(w, `{"error":"查询曲目失败"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	al.Tracks = make([]TrackSummary, 0)
	for rows.Next() {
		var t TrackSummary
		if err := rows.Scan(&t.ID, &t.Title, &t.TrackNumber, &t.DiscNumber, &t.Duration, &t.Format, &t.Bitrate); err != nil {
			continue
		}
		t.StreamURL = "/api/v1/tracks/" + t.ID + "/stream"
		al.Tracks = append(al.Tracks, t)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(al); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
```

- [ ] **步骤 5：运行测试，确认通过**

```bash
go test ./internal/api/v1/... -run "TestListAlbums|TestGetAlbum" -v
```

预期：3 个测试全部 PASS

- [ ] **步骤 6：提交**

```bash
git add internal/api/v1/testhelpers_test.go internal/api/v1/albums.go internal/api/v1/albums_test.go
git commit -m "feat: albums 浏览端点"
```

---

## 任务 4：Artists 端点

**文件：**
- 创建：`internal/api/v1/artists.go`
- 创建：`internal/api/v1/artists_test.go`

- [ ] **步骤 1：写失败测试**

创建 `internal/api/v1/artists_test.go`：

```go
// internal/api/v1/artists_test.go
package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListArtists_ReturnsArtists(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewArtistsHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artists", nil)
	w := httptest.NewRecorder()
	h.ListArtists(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Artists []map[string]interface{} `json:"artists"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Artists) != 1 {
		t.Fatalf("want 1 artist, got %d", len(resp.Artists))
	}
	if resp.Artists[0]["name"] != "蔡琴" {
		t.Errorf("want name=蔡琴, got %v", resp.Artists[0]["name"])
	}
	if resp.Artists[0]["album_count"].(float64) != 1 {
		t.Errorf("want album_count=1, got %v", resp.Artists[0]["album_count"])
	}
}

func TestGetArtist_ReturnsAlbums(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewArtistsHandler(d)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artists/a1", nil)
	h.getArtist(w, req, "a1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["name"] != "蔡琴" {
		t.Errorf("want name=蔡琴, got %v", resp["name"])
	}
	albums := resp["albums"].([]interface{})
	if len(albums) != 1 {
		t.Fatalf("want 1 album, got %d", len(albums))
	}
}

func TestGetArtist_NotFound(t *testing.T) {
	d := newTestDB(t)
	h := NewArtistsHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artists/nonexistent", nil)
	h.getArtist(w, req, "nonexistent")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
```

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/api/v1/... -run "TestListArtists|TestGetArtist" -v
```

- [ ] **步骤 3：实现 artists.go**

创建 `internal/api/v1/artists.go`：

```go
// internal/api/v1/artists.go
package v1

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// ArtistSummary is returned in the list endpoint.
type ArtistSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AlbumCount int    `json:"album_count"`
}

// ArtistDetail is returned by the single artist endpoint.
type ArtistDetail struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Albums []AlbumSummary `json:"albums"`
}

// ArtistsHandler handles /api/v1/artists/* endpoints.
type ArtistsHandler struct {
	db *sql.DB
}

// NewArtistsHandler creates an ArtistsHandler backed by db.
func NewArtistsHandler(db *sql.DB) *ArtistsHandler {
	return &ArtistsHandler{db: db}
}

// ListArtists handles GET /api/v1/artists.
func (h *ArtistsHandler) ListArtists(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`
		SELECT a.id, a.name, COUNT(al.id)
		FROM artists a
		LEFT JOIN albums al ON al.artist_id = a.id
		GROUP BY a.id
		ORDER BY a.name`)
	if err != nil {
		http.Error(w, `{"error":"查询失败"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	artists := make([]ArtistSummary, 0)
	for rows.Next() {
		var a ArtistSummary
		if err := rows.Scan(&a.ID, &a.Name, &a.AlbumCount); err != nil {
			continue
		}
		artists = append(artists, a)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"artists": artists}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}

// GetArtist handles GET /api/v1/artists/:id.
func (h *ArtistsHandler) GetArtist(w http.ResponseWriter, r *http.Request) {
	h.getArtist(w, r, chi.URLParam(r, "id"))
}

func (h *ArtistsHandler) getArtist(w http.ResponseWriter, r *http.Request, id string) {
	var ar ArtistDetail
	err := h.db.QueryRow(`SELECT id, name FROM artists WHERE id = ?`, id).
		Scan(&ar.ID, &ar.Name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, `{"error":"查询失败"}`, http.StatusInternalServerError)
		return
	}

	rows, err := h.db.Query(`
		SELECT al.id, al.title, COALESCE(ar2.name,''), COALESCE(al.artist_id,''),
		       COALESCE(al.release_date,''), COUNT(t.id)
		FROM albums al
		LEFT JOIN artists ar2 ON al.artist_id = ar2.id
		LEFT JOIN tracks t ON t.album_id = al.id AND t.is_available = 1
		WHERE al.artist_id = ?
		GROUP BY al.id
		ORDER BY al.release_date DESC, al.title`, id)
	if err != nil {
		http.Error(w, `{"error":"查询专辑失败"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	ar.Albums = make([]AlbumSummary, 0)
	for rows.Next() {
		var al AlbumSummary
		var releaseDate string
		if err := rows.Scan(&al.ID, &al.Title, &al.Artist, &al.ArtistID, &releaseDate, &al.TrackCount); err != nil {
			continue
		}
		al.Year, _ = strconv.Atoi(releaseDate)
		al.CoverURL = "/api/v1/cover/" + al.ID
		ar.Albums = append(ar.Albums, al)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(ar); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
```

- [ ] **步骤 4：运行测试，确认通过**

```bash
go test ./internal/api/v1/... -run "TestListArtists|TestGetArtist" -v
```

预期：3 个测试全部 PASS

- [ ] **步骤 5：提交**

```bash
git add internal/api/v1/artists.go internal/api/v1/artists_test.go
git commit -m "feat: artists 浏览端点"
```

---

## 任务 5：Cover 封面端点

**文件：**
- 创建：`internal/api/v1/cover.go`
- 创建：`internal/api/v1/cover_test.go`

- [ ] **步骤 1：写失败测试**

创建 `internal/api/v1/cover_test.go`：

```go
// internal/api/v1/cover_test.go
package v1

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGetCover_CoverJpg(t *testing.T) {
	d := newTestDB(t)
	// 创建临时目录和 cover.jpg
	dir := t.TempDir()
	coverPath := filepath.Join(dir, "cover.jpg")
	os.WriteFile(coverPath, []byte("FAKEJPEG"), 0644)

	// 插入指向该目录的曲目
	d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`)
	d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','Album','a1')`)
	d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t1','T','a1','al1',?,'' ,1,'pending')`,
		filepath.Join(dir, "song.flac"))

	h := NewCoverHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cover/al1", nil)
	h.getCover(w, req, "al1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("want Content-Type image/jpeg, got %q", ct)
	}
}

func TestGetCover_NotFound(t *testing.T) {
	d := newTestDB(t)
	h := NewCoverHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cover/noalbum", nil)
	h.getCover(w, req, "noalbum")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
```

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/api/v1/... -run "TestGetCover" -v
```

- [ ] **步骤 3：实现 cover.go**

创建 `internal/api/v1/cover.go`：

```go
// internal/api/v1/cover.go
package v1

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
	"github.com/go-chi/chi/v5"
)

// CoverHandler handles GET /api/v1/cover/:id.
type CoverHandler struct {
	db *sql.DB
}

// NewCoverHandler creates a CoverHandler backed by db.
func NewCoverHandler(db *sql.DB) *CoverHandler {
	return &CoverHandler{db: db}
}

// GetCover handles GET /api/v1/cover/:id (album ID).
func (h *CoverHandler) GetCover(w http.ResponseWriter, r *http.Request) {
	h.getCover(w, r, chi.URLParam(r, "id"))
}

func (h *CoverHandler) getCover(w http.ResponseWriter, r *http.Request, albumID string) {
	// 取该专辑的第一个可用曲目路径
	var filePath string
	err := h.db.QueryRow(
		`SELECT file_path FROM tracks WHERE album_id=? AND is_available=1 LIMIT 1`,
		albumID,
	).Scan(&filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 优先级 1：文件内嵌封面
	if data, mimeType := extractEmbeddedCover(filePath); len(data) > 0 {
		w.Header().Set("Content-Type", mimeType)
		w.Write(data)
		return
	}

	// 优先级 2：同目录 cover.jpg / folder.jpg
	dir := filepath.Dir(filePath)
	for _, name := range []string{
		"cover.jpg", "Cover.jpg", "cover.jpeg", "Cover.jpeg",
		"folder.jpg", "Folder.jpg",
		"cover.png", "Cover.png",
	} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		mimeType := "image/jpeg"
		if strings.HasSuffix(strings.ToLower(name), ".png") {
			mimeType = "image/png"
		}
		w.Header().Set("Content-Type", mimeType)
		w.Write(data)
		return
	}

	http.NotFound(w, r)
}

func extractEmbeddedCover(filePath string) ([]byte, string) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, ""
	}
	defer f.Close()

	tags, err := tag.ReadFrom(f)
	if err != nil {
		return nil, ""
	}
	pic := tags.Picture()
	if pic == nil || len(pic.Data) == 0 {
		return nil, ""
	}
	mimeType := pic.MIMEType
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	return pic.Data, mimeType
}
```

- [ ] **步骤 4：运行测试，确认通过**

```bash
go test ./internal/api/v1/... -run "TestGetCover" -v
```

预期：2 个测试全部 PASS

- [ ] **步骤 5：提交**

```bash
git add internal/api/v1/cover.go internal/api/v1/cover_test.go
git commit -m "feat: cover 封面端点（内嵌 + cover.jpg）"
```

---

## 任务 6：Stream 音频流端点

**文件：**
- 创建：`internal/api/v1/stream.go`
- 创建：`internal/api/v1/stream_test.go`

- [ ] **步骤 1：写失败测试**

创建 `internal/api/v1/stream_test.go`：

```go
// internal/api/v1/stream_test.go
package v1

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestStream_ServesFile(t *testing.T) {
	d := newTestDB(t)
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "test.mp3")
	// 写入假 MP3 数据（足够让 http.ServeFile 正常工作）
	os.WriteFile(audioFile, []byte("FAKEMP3DATA"), 0644)

	d.Exec(`INSERT INTO artists(id,name) VALUES('a1','A')`)
	d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','B','a1')`)
	d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t1','T','a1','al1',?,'mp3',1,'pending')`, audioFile)

	h := NewStreamHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/t1/stream", nil)
	h.stream(w, req, "t1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("want Content-Type audio/mpeg, got %q", ct)
	}
}

func TestStream_NotFound(t *testing.T) {
	d := newTestDB(t)
	h := NewStreamHandler(d)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tracks/nope/stream", nil)
	h.stream(w, req, "nope")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}
```

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/api/v1/... -run "TestStream" -v
```

- [ ] **步骤 3：实现 stream.go**

创建 `internal/api/v1/stream.go`：

```go
// internal/api/v1/stream.go
package v1

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
)

var audioContentTypes = map[string]string{
	"mp3":  "audio/mpeg",
	"flac": "audio/flac",
	"m4a":  "audio/mp4",
	"ogg":  "audio/ogg",
	"opus": "audio/ogg",
	"wav":  "audio/wav",
	"aiff": "audio/aiff",
	"aif":  "audio/aiff",
	"wma":  "audio/x-ms-wma",
}

// StreamHandler handles GET /api/v1/tracks/:id/stream.
type StreamHandler struct {
	db *sql.DB
}

// NewStreamHandler creates a StreamHandler backed by db.
func NewStreamHandler(db *sql.DB) *StreamHandler {
	return &StreamHandler{db: db}
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

	ct, ok := audioContentTypes[format]
	if !ok {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	// http.ServeFile handles Range requests, Content-Length, ETag automatically.
	http.ServeFile(w, r, filePath)
}
```

- [ ] **步骤 4：运行测试，确认通过**

```bash
go test ./internal/api/v1/... -run "TestStream" -v
```

预期：2 个测试全部 PASS

- [ ] **步骤 5：提交**

```bash
git add internal/api/v1/stream.go internal/api/v1/stream_test.go
git commit -m "feat: stream 音频流端点，支持 HTTP Range"
```

---

## 任务 7：Search 搜索端点

**文件：**
- 创建：`internal/api/v1/search.go`
- 创建：`internal/api/v1/search_test.go`

- [ ] **步骤 1：写失败测试**

创建 `internal/api/v1/search_test.go`：

```go
// internal/api/v1/search_test.go
package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearch_FindsTrack(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewSearchHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=渡口", nil)
	w := httptest.NewRecorder()
	h.Search(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	tracks := resp["tracks"].([]interface{})
	if len(tracks) != 1 {
		t.Fatalf("want 1 track, got %d", len(tracks))
	}
}

func TestSearch_EmptyQuery_Returns400(t *testing.T) {
	d := newTestDB(t)
	h := NewSearchHandler(d)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	w := httptest.NewRecorder()
	h.Search(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestSearch_FindsArtist(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewSearchHandler(d)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=蔡琴", nil)
	w := httptest.NewRecorder()
	h.Search(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	artists := resp["artists"].([]interface{})
	if len(artists) != 1 {
		t.Fatalf("want 1 artist, got %d", len(artists))
	}
}
```

- [ ] **步骤 2：运行确认失败**

```bash
go test ./internal/api/v1/... -run "TestSearch" -v
```

- [ ] **步骤 3：实现 search.go**

创建 `internal/api/v1/search.go`：

```go
// internal/api/v1/search.go
package v1

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
)

// TrackResult is a search result for a track.
type TrackResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	Album     string `json:"album"`
	AlbumID   string `json:"album_id"`
	Duration  int    `json:"duration"`
	StreamURL string `json:"stream_url"`
}

// AlbumResult is a search result for an album.
type AlbumResult struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	CoverURL string `json:"cover_url"`
}

// ArtistResult is a search result for an artist.
type ArtistResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SearchResponse is the full search response.
type SearchResponse struct {
	Tracks  []TrackResult  `json:"tracks"`
	Albums  []AlbumResult  `json:"albums"`
	Artists []ArtistResult `json:"artists"`
}

// SearchHandler handles GET /api/v1/search.
type SearchHandler struct {
	db *sql.DB
}

// NewSearchHandler creates a SearchHandler backed by db.
func NewSearchHandler(db *sql.DB) *SearchHandler {
	return &SearchHandler{db: db}
}

// Search handles GET /api/v1/search?q=keyword.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "参数 q 不能为空"})
		return
	}

	like := "%" + q + "%"
	resp := SearchResponse{
		Tracks:  make([]TrackResult, 0),
		Albums:  make([]AlbumResult, 0),
		Artists: make([]ArtistResult, 0),
	}

	// 搜索曲目
	rows, err := h.db.Query(`
		SELECT t.id, t.title, COALESCE(ar.name,''), COALESCE(al.title,''),
		       COALESCE(t.album_id,''), t.duration
		FROM tracks t
		LEFT JOIN artists ar ON t.artist_id = ar.id
		LEFT JOIN albums al ON t.album_id = al.id
		WHERE t.is_available = 1
		  AND (t.title LIKE ? OR ar.name LIKE ?)
		LIMIT 20`, like, like)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var tr TrackResult
			if err := rows.Scan(&tr.ID, &tr.Title, &tr.Artist, &tr.Album, &tr.AlbumID, &tr.Duration); err != nil {
				continue
			}
			tr.StreamURL = "/api/v1/tracks/" + tr.ID + "/stream"
			resp.Tracks = append(resp.Tracks, tr)
		}
	}

	// 搜索专辑
	albumRows, err := h.db.Query(`
		SELECT a.id, a.title, COALESCE(ar.name,'')
		FROM albums a
		LEFT JOIN artists ar ON a.artist_id = ar.id
		WHERE a.title LIKE ? OR ar.name LIKE ?
		LIMIT 20`, like, like)
	if err == nil {
		defer albumRows.Close()
		for albumRows.Next() {
			var al AlbumResult
			if err := albumRows.Scan(&al.ID, &al.Title, &al.Artist); err != nil {
				continue
			}
			al.CoverURL = "/api/v1/cover/" + al.ID
			resp.Albums = append(resp.Albums, al)
		}
	}

	// 搜索艺术家
	artistRows, err := h.db.Query(`
		SELECT id, name FROM artists WHERE name LIKE ? LIMIT 20`, like)
	if err == nil {
		defer artistRows.Close()
		for artistRows.Next() {
			var ar ArtistResult
			if err := artistRows.Scan(&ar.ID, &ar.Name); err != nil {
				continue
			}
			resp.Artists = append(resp.Artists, ar)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
```

- [ ] **步骤 4：运行测试，确认通过**

```bash
go test ./internal/api/v1/... -run "TestSearch" -v
```

预期：3 个测试全部 PASS

- [ ] **步骤 5：提交**

```bash
git add internal/api/v1/search.go internal/api/v1/search_test.go
git commit -m "feat: search 搜索端点（LIKE 全文匹配）"
```

---

## 任务 8：Router 连接所有端点

**文件：**
- 修改：`internal/api/router.go`
- 修改：`internal/api/router_test.go`
- 修改：`cmd/server/main.go`

- [ ] **步骤 1：运行现有测试，确认当前通过**

```bash
go test ./internal/api/... -v
```

预期：现有测试全部 PASS（稍后会短暂失败）

- [ ] **步骤 2：完整替换 internal/api/router.go**

```go
// internal/api/router.go
package api

import (
	"database/sql"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/yxx-z/lyra/internal/api/middleware"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/scanner"
	"github.com/yxx-z/lyra/ui"
)

const version = "0.1.0"

// NewRouter builds the application router.
func NewRouter(s *scanner.Scanner, db *sql.DB, cfg *config.Config) http.Handler {
	r := chi.NewRouter()
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)

	r.Get("/health", handleHealth)

	// 认证端点（不需要 token）
	authH := v1.NewAuthHandler(cfg)
	r.Post("/api/v1/auth/login", authH.Login)

	// 受保护的 /api/v1/* 路由
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.BearerAuth(cfg.Auth.Token, cfg.Auth.Disable))

		// 扫描（已有）
		lib := v1.NewLibraryHandler(s)
		r.Post("/library/scan", lib.TriggerScan)
		r.Get("/library/scan/status", lib.ScanStatus)

		// 浏览
		albums := v1.NewAlbumsHandler(db)
		r.Get("/albums", albums.ListAlbums)
		r.Get("/albums/{id}", albums.GetAlbum)

		artists := v1.NewArtistsHandler(db)
		r.Get("/artists", artists.ListArtists)
		r.Get("/artists/{id}", artists.GetArtist)

		// 媒体
		cover := v1.NewCoverHandler(db)
		r.Get("/cover/{id}", cover.GetCover)

		stream := v1.NewStreamHandler(db)
		r.Get("/tracks/{id}/stream", stream.Stream)

		// 搜索
		search := v1.NewSearchHandler(db)
		r.Get("/search", search.Search)
	})

	// 嵌入前端（兜底路由）
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

- [ ] **步骤 3：完整替换 internal/api/router_test.go**

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
	cfg := &config.Config{
		Auth: config.AuthConfig{Disable: true},
	}
	return NewRouter(s, d, cfg)
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

- [ ] **步骤 4：更新 cmd/server/main.go 传入新参数**

找到 `router := api.NewRouter(sc)` 这一行，替换为：

```go
router := api.NewRouter(sc, database, cfg)
```

- [ ] **步骤 5：运行全部测试**

```bash
go test ./... -v 2>&1 | grep -E "^(ok|FAIL|---)"
```

预期：所有包全部 PASS

- [ ] **步骤 6：冒烟测试**

```bash
go build -o lyra ./cmd/server
./lyra &
sleep 2
curl -s http://localhost:4533/health
curl -s -X POST http://localhost:4533/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":""}'
kill %1
rm -f lyra
```

预期：
- `/health` → `{"status":"ok","version":"0.1.0"}`
- `/auth/login` → `{"error":"用户名或密码错误"}`（因为 password 为空）

- [ ] **步骤 7：提交并推送**

```bash
git add internal/api/ cmd/server/main.go
git commit -m "feat: 连接浏览 REST API 到 router，更新签名"
git push origin master
```

---

## 自检清单

**规格覆盖：**
- [x] POST /auth/login（200/401/400）→ 任务 2
- [x] BearerAuth 中间件（有效/无效/禁用）→ 任务 2
- [x] GET /albums，GET /albums/:id → 任务 3
- [x] GET /artists，GET /artists/:id → 任务 4
- [x] GET /cover/:id（cover.jpg + 内嵌）→ 任务 5
- [x] GET /tracks/:id/stream（HTTP Range，Content-Type 映射）→ 任务 6
- [x] GET /search?q=（曲目/专辑/艺术家，空 q 返回 400）→ 任务 7
- [x] Router 注册所有端点，auth 中间件保护 → 任务 8
- [x] config.yaml Username/Password 默认值 → 任务 1
- [x] Token 自动生成 → 任务 1
