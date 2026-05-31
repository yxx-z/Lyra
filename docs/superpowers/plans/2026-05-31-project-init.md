# Lyra — 项目初始化实现计划

> **给 AI 工作者：** 必须使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务执行本计划。步骤使用复选框（`- [ ]`）语法追踪进度。

**目标：** 搭建 Lyra 音乐服务器骨架 —— Go 模块、配置系统、SQLite 数据库层、带 `/health` 端点的 Chi HTTP 服务器、嵌入式 Vue 3 前端、Dockerfile 和 Makefile —— 最终产出一个能启动并提供服务的单一二进制文件。

**架构：** 单个 Go 二进制文件内嵌 Vue 3 前端（由 Vite 构建，输出到 `ui/dist/`，通过 `//go:embed` 嵌入）。后端使用 Chi 路由，`modernc.org/sqlite` 作为纯 Go SQLite 驱动（无 CGo），通过手写迁移器从嵌入式 FS 读取 SQL 文件执行迁移。前端源码放在 `web/`，构建输出落到 `ui/dist/`，该目录是独立的 Go 包（`package ui`），仅负责 embed。

**技术栈：** Go 1.22+、Chi v5、modernc.org/sqlite、gopkg.in/yaml.v3、Vue 3 + TypeScript + Vite + Naive UI + Pinia、Docker

---

## 前置条件

开始任何任务前，确保以下工具已安装：

```bash
# 安装 Go 1.22+（WSL2/Ubuntu）
wget https://go.dev/dl/go1.22.4.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.22.4.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version  # 应输出 go1.22.x

# Node 已安装（v24）
node --version
npm --version

# Docker（任务 7 需要）
docker --version
```

---

## 文件结构

```
lyra/
├── cmd/server/
│   └── main.go                     # 入口，组装 config + db + router
├── internal/
│   ├── api/
│   │   ├── router.go               # Chi 路由，/health 端点
│   │   └── router_test.go
│   ├── config/
│   │   ├── config.go               # Config 结构体 + Load() + Default()
│   │   └── config_test.go
│   └── db/
│       ├── db.go                   # Open()、WAL 模式、迁移运行器
│       ├── db_test.go
│       ├── schema.sql              # 参考 schema（不编译进二进制）
│       └── migrations/
│           └── 001_init.up.sql     # 第一个迁移文件
├── ui/
│   ├── ui.go                       # package ui；//go:embed all:dist
│   └── dist/                       # Vite 构建输出（已 gitignore，保留 .gitkeep）
│       └── .gitkeep
├── web/                            # Vue 3 源码
│   ├── src/
│   │   ├── App.vue
│   │   └── main.ts
│   ├── vite.config.ts              # outDir: ../ui/dist
│   └── package.json
├── config.example.yaml
├── .gitignore
├── Makefile
├── Dockerfile
├── docker-compose.yml
└── CLAUDE.md
```

---

## 任务 1：环境 + Go 模块 + 目录骨架

**涉及文件：**
- 创建：`go.mod`
- 创建：所有 `internal/`、`ui/`、`web/src/` 目录

- [ ] **步骤 1：验证 Go 已安装**

```bash
go version
```

预期输出：`go version go1.22.x linux/amd64`

- [ ] **步骤 2：初始化 Go 模块**

```bash
cd /home/yxx/develop/Lyra
go mod init github.com/yxx-z/lyra
```

预期：在当前目录创建 `go.mod`，内容含 `module github.com/yxx-z/lyra` 和 `go 1.22`

- [ ] **步骤 3：创建所有目录**

```bash
mkdir -p \
  cmd/server \
  internal/api \
  internal/config \
  internal/db/migrations \
  internal/db/query \
  internal/scanner \
  internal/metadata \
  internal/lyrics \
  internal/transcode \
  internal/cache \
  ui/dist \
  web/src/components \
  web/src/stores \
  web/src/api \
  data
```

- [ ] **步骤 4：创建占位 main.go**

```go
// cmd/server/main.go
package main

func main() {}
```

- [ ] **步骤 5：为 ui/dist 添加 .gitkeep（确保目录在构建前就被 git 追踪）**

```bash
touch ui/dist/.gitkeep
```

- [ ] **步骤 6：提交**

```bash
git add go.mod cmd/ internal/ ui/ web/ data/
git commit -m "chore: 初始化 Go 模块和目录骨架"
```

---

## 任务 2：配置系统

**涉及文件：**
- 创建：`internal/config/config.go`
- 创建：`internal/config/config_test.go`
- 创建：`config.example.yaml`

- [ ] **步骤 1：写失败测试**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"testing"
)

func TestDefault_Defaults(t *testing.T) {
	cfg := Default()
	if cfg.Server.Port != 4533 {
		t.Errorf("期望端口 4533，实际 %d", cfg.Server.Port)
	}
	if cfg.Transcode.DefaultFormat != "mp3" {
		t.Errorf("期望 mp3，实际 %s", cfg.Transcode.DefaultFormat)
	}
	if cfg.Transcode.DefaultBitrate != 192 {
		t.Errorf("期望码率 192，实际 %d", cfg.Transcode.DefaultBitrate)
	}
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	cfg, err := Load("does-not-exist.yaml")
	if err != nil {
		t.Fatalf("不应报错，实际: %v", err)
	}
	if cfg.Server.Port != 4533 {
		t.Errorf("期望默认端口 4533，实际 %d", cfg.Server.Port)
	}
}

func TestLoad_OverridesPort(t *testing.T) {
	f, err := os.CreateTemp("", "lyra-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("server:\n  port: 9090\n")
	f.Close()

	cfg, err := Load(f.Name())
	if err != nil {
		t.Fatalf("不应报错，实际: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("期望端口 9090，实际 %d", cfg.Server.Port)
	}
}
```

- [ ] **步骤 2：运行测试 —— 确认失败**

```bash
go test ./internal/config/...
```

预期：FAIL —— `Default` 和 `Load` 未定义

- [ ] **步骤 3：安装 yaml 依赖**

```bash
go get gopkg.in/yaml.v3
```

- [ ] **步骤 4：实现 config.go**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Auth      AuthConfig      `yaml:"auth"`
	Library   LibraryConfig   `yaml:"library"`
	Database  DatabaseConfig  `yaml:"database"`
	Cache     CacheConfig     `yaml:"cache"`
	Scraper   ScraperConfig   `yaml:"scraper"`
	Transcode TranscodeConfig `yaml:"transcode"`
	Subsonic  SubsonicConfig  `yaml:"subsonic"`
}

type ServerConfig struct {
	Host    string `yaml:"host"`
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"base_url"`
}

type AuthConfig struct {
	Disable bool   `yaml:"disable"`
	Token   string `yaml:"token"`
}

type LibraryConfig struct {
	Paths        []string `yaml:"paths"`
	ScanInterval int      `yaml:"scan_interval"`
	Watch        bool     `yaml:"watch"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type CacheConfig struct {
	ArtworkDir       string `yaml:"artwork_dir"`
	ArtworkMaxSizeMB int    `yaml:"artwork_max_size_mb"`
}

type ScraperConfig struct {
	Enabled     bool              `yaml:"enabled"`
	MusicBrainz MusicBrainzConfig `yaml:"musicbrainz"`
	LastFM      LastFMConfig      `yaml:"lastfm"`
	AcoustID    AcoustIDConfig    `yaml:"acoustid"`
	Netease     NeteaseConfig     `yaml:"netease"`
	Spotify     SpotifyConfig     `yaml:"spotify"`
}

type MusicBrainzConfig struct {
	UserAgent string `yaml:"user_agent"`
}

type LastFMConfig struct {
	APIKey string `yaml:"api_key"`
}

type AcoustIDConfig struct {
	APIKey string `yaml:"api_key"`
}

type NeteaseConfig struct {
	Enabled bool `yaml:"enabled"`
}

type SpotifyConfig struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

type TranscodeConfig struct {
	FFmpegPath     string `yaml:"ffmpeg_path"`
	DefaultFormat  string `yaml:"default_format"`
	DefaultBitrate int    `yaml:"default_bitrate"`
}

type SubsonicConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Password string `yaml:"password"`
}

func Default() *Config {
	return &Config{
		Server:   ServerConfig{Host: "0.0.0.0", Port: 4533},
		Auth:     AuthConfig{Disable: false},
		Library:  LibraryConfig{ScanInterval: 3600, Watch: true},
		Database: DatabaseConfig{Path: "./data/music.db"},
		Cache:    CacheConfig{ArtworkDir: "./data/artwork", ArtworkMaxSizeMB: 2048},
		Scraper:  ScraperConfig{Enabled: true, Netease: NeteaseConfig{Enabled: true}},
		Transcode: TranscodeConfig{
			FFmpegPath:     "ffmpeg",
			DefaultFormat:  "mp3",
			DefaultBitrate: 192,
		},
		Subsonic: SubsonicConfig{Enabled: true},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("打开配置文件 %q: %w", path, err)
	}
	defer f.Close()
	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件: %w", err)
	}
	return cfg, nil
}
```

- [ ] **步骤 5：运行测试 —— 确认通过**

```bash
go test ./internal/config/... -v
```

预期：3 个测试全部 PASS

- [ ] **步骤 6：创建 config.example.yaml**

```yaml
# config.example.yaml
server:
  host: 0.0.0.0
  port: 4533
  base_url: ""           # 反向代理时设置，如 /music

auth:
  disable: false         # 内网纯信任环境可设为 true 关闭认证
  token: ""              # 留空时启动自动生成并写回配置文件

library:
  paths:
    - /music
  scan_interval: 3600    # 秒，0 = 仅手动扫描
  watch: true

database:
  path: ./data/music.db

cache:
  artwork_dir: ./data/artwork
  artwork_max_size_mb: 2048

scraper:
  enabled: true
  musicbrainz:
    user_agent: "Lyra/0.1 (your@email.com)"
  lastfm:
    api_key: ""
  acoustid:
    api_key: ""
  netease:
    enabled: true
  spotify:
    client_id: ""
    client_secret: ""

transcode:
  ffmpeg_path: ffmpeg
  default_format: mp3
  default_bitrate: 192

subsonic:
  enabled: true
  password: ""
```

- [ ] **步骤 7：提交**

```bash
git add internal/config/ config.example.yaml go.mod go.sum
git commit -m "feat: 添加配置系统"
```

---

## 任务 3：数据库层

**涉及文件：**
- 创建：`internal/db/schema.sql`
- 创建：`internal/db/migrations/001_init.up.sql`
- 创建：`internal/db/db.go`
- 创建：`internal/db/db_test.go`

- [ ] **步骤 1：写失败测试**

```go
// internal/db/db_test.go
package db

import (
	"testing"
)

func TestOpen_CreatesTablesOnFirstRun(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer db.Close()

	var count int
	row := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('artists','albums','tracks','lyrics')`,
	)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if count != 4 {
		t.Errorf("期望 4 张核心表，实际 %d", count)
	}
}

func TestOpen_IdempotentMigrations(t *testing.T) {
	tmp := t.TempDir() + "/test.db"
	db1, err := Open(tmp)
	if err != nil {
		t.Fatalf("第一次 Open 失败: %v", err)
	}
	db1.Close()

	// 第二次打开同一文件，迁移应幂等，不报错
	db2, err := Open(tmp)
	if err != nil {
		t.Fatalf("第二次 Open（幂等性测试）失败: %v", err)
	}
	db2.Close()
}
```

- [ ] **步骤 2：运行测试 —— 确认失败**

```bash
go test ./internal/db/...
```

预期：FAIL —— `Open` 未定义

- [ ] **步骤 3：安装 modernc sqlite**

```bash
go get modernc.org/sqlite
```

- [ ] **步骤 4：创建 schema.sql（参考文档，不编译进二进制）**

```sql
-- internal/db/schema.sql
-- Schema 参考文件，变更时同步写一个新的 migrations/*.up.sql

CREATE TABLE artists (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    sort_name   TEXT,
    biography   TEXT,
    image_url   TEXT,
    mbid        TEXT UNIQUE,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE albums (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    artist_id    TEXT REFERENCES artists(id),
    release_date TEXT,
    genre        TEXT,
    cover_path   TEXT,
    mbid         TEXT UNIQUE,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tracks (
    id             TEXT PRIMARY KEY,
    title          TEXT NOT NULL,
    artist_id      TEXT REFERENCES artists(id),
    album_id       TEXT REFERENCES albums(id),
    track_number   INTEGER,
    disc_number    INTEGER DEFAULT 1,
    duration       INTEGER,
    file_path      TEXT NOT NULL UNIQUE,
    file_size      INTEGER,
    format         TEXT,
    bitrate        INTEGER,
    sample_rate    INTEGER,
    channels       INTEGER,
    mbid           TEXT,
    acoustid       TEXT,
    scrape_status  TEXT DEFAULT 'pending',
    play_count     INTEGER DEFAULT 0,
    last_played_at DATETIME,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE lyrics (
    track_id    TEXT PRIMARY KEY REFERENCES tracks(id),
    lrc_content TEXT,
    yrc_content TEXT,
    source      TEXT,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE playlists (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE playlist_tracks (
    id          INTEGER PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES playlists(id),
    track_id    TEXT NOT NULL REFERENCES tracks(id),
    position    INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_playlist_tracks_pos ON playlist_tracks(playlist_id, position);

CREATE INDEX idx_tracks_album         ON tracks(album_id);
CREATE INDEX idx_tracks_artist        ON tracks(artist_id);
CREATE INDEX idx_tracks_scrape_status ON tracks(scrape_status);
CREATE INDEX idx_albums_artist        ON albums(artist_id);
```

- [ ] **步骤 5：创建第一个迁移文件（内容与 schema.sql 一致）**

```sql
-- internal/db/migrations/001_init.up.sql
CREATE TABLE artists (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    sort_name   TEXT,
    biography   TEXT,
    image_url   TEXT,
    mbid        TEXT UNIQUE,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE albums (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    artist_id    TEXT REFERENCES artists(id),
    release_date TEXT,
    genre        TEXT,
    cover_path   TEXT,
    mbid         TEXT UNIQUE,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tracks (
    id             TEXT PRIMARY KEY,
    title          TEXT NOT NULL,
    artist_id      TEXT REFERENCES artists(id),
    album_id       TEXT REFERENCES albums(id),
    track_number   INTEGER,
    disc_number    INTEGER DEFAULT 1,
    duration       INTEGER,
    file_path      TEXT NOT NULL UNIQUE,
    file_size      INTEGER,
    format         TEXT,
    bitrate        INTEGER,
    sample_rate    INTEGER,
    channels       INTEGER,
    mbid           TEXT,
    acoustid       TEXT,
    scrape_status  TEXT DEFAULT 'pending',
    play_count     INTEGER DEFAULT 0,
    last_played_at DATETIME,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE lyrics (
    track_id    TEXT PRIMARY KEY REFERENCES tracks(id),
    lrc_content TEXT,
    yrc_content TEXT,
    source      TEXT,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE playlists (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE playlist_tracks (
    id          INTEGER PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES playlists(id),
    track_id    TEXT NOT NULL REFERENCES tracks(id),
    position    INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_playlist_tracks_pos ON playlist_tracks(playlist_id, position);

CREATE INDEX idx_tracks_album         ON tracks(album_id);
CREATE INDEX idx_tracks_artist        ON tracks(artist_id);
CREATE INDEX idx_tracks_scrape_status ON tracks(scrape_status);
CREATE INDEX idx_albums_artist        ON albums(artist_id);
```

- [ ] **步骤 6：实现 db.go**

```go
// internal/db/db.go
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations
var migrationsFS embed.FS

func Open(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("创建数据库目录: %w", err)
			}
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 PRAGMA: %w", err)
	}
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("执行迁移: %w", err)
	}
	return db, nil
}

func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		var applied int
		db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=?`, e.Name()).Scan(&applied)
		if applied > 0 {
			continue
		}
		content, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("应用迁移 %s: %w", e.Name(), err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, e.Name()); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **步骤 7：运行测试 —— 确认通过**

```bash
go test ./internal/db/... -v
```

预期：2 个测试全部 PASS

- [ ] **步骤 8：提交**

```bash
git add internal/db/ go.mod go.sum
git commit -m "feat: 添加 SQLite 数据库层及嵌入式迁移运行器"
```

---

## 任务 4：HTTP 服务器 + /health 端点

**涉及文件：**
- 创建：`internal/api/router.go`
- 创建：`internal/api/router_test.go`
- 修改：`cmd/server/main.go`

- [ ] **步骤 1：写失败测试**

```go
// internal/api/router_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth_Returns200WithStatusOK(t *testing.T) {
	r := NewRouter()
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

- [ ] **步骤 2：运行测试 —— 确认失败**

```bash
go test ./internal/api/...
```

预期：FAIL —— `NewRouter` 未定义

- [ ] **步骤 3：安装 Chi**

```bash
go get github.com/go-chi/chi/v5
```

- [ ] **步骤 4：实现 router.go**

```go
// internal/api/router.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const version = "0.1.0"

func NewRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", handleHealth)

	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": version,
	})
}
```

- [ ] **步骤 5：运行测试 —— 确认通过**

```bash
go test ./internal/api/... -v
```

预期：`TestHealth_Returns200WithStatusOK` PASS

- [ ] **步骤 6：实现 main.go**

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

	router := api.NewRouter()
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("Lyra 启动", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("服务器错误", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("正在关闭服务器")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}
```

- [ ] **步骤 7：验证编译通过**

```bash
go build ./cmd/server
```

预期：在当前目录生成二进制文件，无错误

- [ ] **步骤 8：冒烟测试运行中的服务器**

```bash
./server &
sleep 1
curl -s http://localhost:4533/health
kill %1
rm server
```

预期输出：`{"status":"ok","version":"0.1.0"}`

- [ ] **步骤 9：提交**

```bash
git add internal/api/ cmd/server/ go.mod go.sum
git commit -m "feat: HTTP 服务器与 /health 端点"
```

---

## 任务 5：前端脚手架

**涉及文件：**
- 创建：`web/`（通过 npm create 生成 Vite 项目）
- 修改：`web/vite.config.ts`（设置 outDir 为 `../ui/dist`）
- 修改：`web/src/App.vue`
- 修改：`web/src/main.ts`

**关于 ui/dist 路径的说明：** Go 的 `//go:embed` 不允许 `..` 路径穿越，embed 文件必须是被嵌入路径的祖先目录。因此，前端构建输出放到 `ui/dist/`（`web/` 的兄弟目录），由 `ui/ui.go` 负责 embed。这与技术文档中 `web/dist` 的描述不同，是 Go embed 机制的约束。

- [ ] **步骤 1：生成 Vite 项目**

从仓库根目录执行：

```bash
npm create vite@latest web -- --template vue-ts
```

如有交互提示：选择 framework=Vue，variant=TypeScript。若 `web/` 目录已有内容，确认覆盖。

- [ ] **步骤 2：安装前端依赖**

```bash
cd web
npm install
npm install naive-ui pinia howler @types/howler
cd ..
```

- [ ] **步骤 3：更新 vite.config.ts，将输出目录改为 ui/dist**

完整替换 `web/vite.config.ts`：

```typescript
// web/vite.config.ts
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: '../ui/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:4533',
      '/health': 'http://localhost:4533',
      '/rest': 'http://localhost:4533',
    },
  },
})
```

- [ ] **步骤 4：用 Lyra 占位页替换默认 App.vue**

```vue
<!-- web/src/App.vue -->
<template>
  <n-config-provider :theme="darkTheme">
    <n-layout style="height: 100vh">
      <n-layout-content>
        <div style="display:flex;align-items:center;justify-content:center;height:100vh;flex-direction:column;gap:1rem">
          <h1 style="font-size:3rem;margin:0">Lyra</h1>
          <p style="color:#888;margin:0">自托管音乐服务器 —— 即将上线</p>
        </div>
      </n-layout-content>
    </n-layout>
  </n-config-provider>
</template>

<script setup lang="ts">
import { NConfigProvider, NLayout, NLayoutContent, darkTheme } from 'naive-ui'
</script>
```

- [ ] **步骤 5：更新 main.ts，引入 Pinia**

```typescript
// web/src/main.ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'

const app = createApp(App)
app.use(createPinia())
app.mount('#app')
```

- [ ] **步骤 6：构建前端，确认输出落在 ui/dist**

```bash
cd web && npm run build && cd ..
ls ui/dist/
```

预期：`ui/dist/` 中存在 `index.html` 和 `assets/` 目录

- [ ] **步骤 7：提交**

```bash
git add web/
git commit -m "feat: 初始化 Vue 3 + TypeScript 前端脚手架"
```

---

## 任务 6：Go embed —— 将前端打入二进制

**涉及文件：**
- 创建：`ui/ui.go`
- 修改：`internal/api/router.go`

- [ ] **步骤 1：创建 ui/ui.go**

```go
// ui/ui.go
package ui

import "embed"

//go:embed all:dist
var Dist embed.FS
```

- [ ] **步骤 2：在 router.go 中添加静态文件服务**

完整替换 `internal/api/router.go`：

```go
// internal/api/router.go
package api

import (
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yxx-z/lyra/ui"
)

const version = "0.1.0"

func NewRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", handleHealth)

	// 所有非 API 路由返回嵌入的前端文件
	sub, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		panic("embed ui/dist 失败: " + err.Error())
	}
	r.Handle("/*", http.FileServer(http.FS(sub)))

	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": version,
	})
}
```

- [ ] **步骤 3：运行测试 —— 确认 health 测试仍然通过**

```bash
go test ./internal/api/... -v
```

预期：`TestHealth_Returns200WithStatusOK` PASS

- [ ] **步骤 4：构建完整二进制并冒烟测试静态文件服务**

```bash
go build -o lyra ./cmd/server
./lyra &
sleep 1
curl -s http://localhost:4533/health
curl -s http://localhost:4533/ | head -5
kill %1
rm lyra
```

预期：
- `/health` → `{"status":"ok","version":"0.1.0"}`
- `/` → 以 `<!DOCTYPE html>` 开头

- [ ] **步骤 5：提交**

```bash
git add ui/ internal/api/ go.mod go.sum
git commit -m "feat: 通过 ui 包将 Vue 前端嵌入 Go 二进制"
```

---

## 任务 7：构建工具 + Docker + CLAUDE.md

**涉及文件：**
- 创建：`Makefile`
- 创建：`Dockerfile`
- 创建：`docker-compose.yml`
- 修改：`.gitignore`
- 创建：`CLAUDE.md`

- [ ] **步骤 1：创建 .gitignore**

```gitignore
# Go 构建产物
lyra
server

# 前端构建产物（由 make build-frontend 重新生成）
ui/dist/
!ui/dist/.gitkeep
web/node_modules/

# 运行时数据
data/

# 系统 / 编辑器
.DS_Store
*.swp
```

- [ ] **步骤 2：创建 Makefile**

```makefile
.PHONY: build build-frontend test dev-backend dev-frontend docker-build clean

build: build-frontend
	go build -o lyra ./cmd/server

build-frontend:
	cd web && npm run build

test:
	go test ./...

dev-backend:
	go run ./cmd/server --config config.yaml

dev-frontend:
	cd web && npm run dev

docker-build:
	docker build -t lyra:latest .

clean:
	rm -f lyra
	rm -rf ui/dist/*
	touch ui/dist/.gitkeep
```

- [ ] **步骤 3：创建 Dockerfile**

```dockerfile
# 阶段 1：构建前端
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
# vite.config.ts 设置 outDir: ../ui/dist，输出到 /app/ui/dist
RUN npm run build

# 阶段 2：构建 Go 二进制
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 用真实构建产物覆盖 .gitkeep 占位文件
COPY --from=frontend /app/ui/dist ./ui/dist
RUN go build -o lyra ./cmd/server

# 阶段 3：最小运行时镜像
FROM alpine:3.19
RUN apk add --no-cache ffmpeg chromaprint
COPY --from=builder /app/lyra /usr/local/bin/lyra
EXPOSE 4533
CMD ["lyra"]
```

- [ ] **步骤 4：创建 docker-compose.yml**

```yaml
services:
  lyra:
    image: lyra:latest
    build: .
    ports:
      - "4533:4533"
    volumes:
      - /your/music:/music:ro
      - ./data:/app/data
    environment:
      - LIBRARY_PATHS=/music
    restart: unless-stopped
```

- [ ] **步骤 5：创建 CLAUDE.md**

```markdown
# Lyra — Claude Code 参考文档

自托管音乐服务器，Go 后端 + Vue 3 前端打包为单一二进制文件。

## 常用命令

| 任务 | 命令 |
|------|------|
| 完整构建 | `make build` |
| 仅构建前端 | `make build-frontend` |
| 运行后端（开发） | `make dev-backend` |
| 运行前端（开发） | `make dev-frontend` |
| 运行全部测试 | `make test` |
| 构建 Docker 镜像 | `make docker-build` |

## 开发工作流

开两个终端：
1. `make dev-frontend` —— Vite 开发服务器监听 :5173，将 /api、/rest 代理到 :4533
2. `make dev-backend` —— Go 服务器监听 :4533

前端访问 http://localhost:5173，API 访问 http://localhost:4533。

## 架构

```
cmd/server/main.go      入口，组装 config + db + router
internal/config/        YAML 配置加载器
internal/db/            SQLite（modernc 纯 Go，无 CGo）
  migrations/           *.up.sql 按字母顺序依次执行
internal/api/           Chi 路由 —— /health、静态前端、后续 API 路由
ui/ui.go                //go:embed all:dist —— 编译时嵌入前端
web/                    Vue 3 源码（npm 项目）
  vite.config.ts        outDir: ../ui/dist（而非 web/dist，受 Go embed 路径约束）
```

## 关键技术决策

- **modernc.org/sqlite**（非 mattn/go-sqlite3）：纯 Go，无 CGo，可轻松交叉编译到 arm64
- **手写迁移运行器**：按字母顺序读取 `internal/db/migrations/*.up.sql`，在 `schema_migrations` 表中追踪已执行版本；避免 golang-migrate 的 CGo sqlite 依赖
- **ui/ 包负责 embed**：Go `//go:embed` 不能使用 `..` 路径，前端输出放到 `ui/dist/`（`web/` 的兄弟目录），由 `ui/ui.go` 嵌入
- **Subsonic 密码独立**：`subsonic.password` 配置项与管理员 Token 隔离

## 添加数据库迁移

1. 创建 `internal/db/migrations/NNN_描述.up.sql`（NNN 为下一个序号，补零对齐）
2. 同步更新 `internal/db/schema.sql` 至最新状态
3. 运行 `go test ./internal/db/...` 验证迁移可以正常执行
```

- [ ] **步骤 6：验证 Docker 构建成功**

```bash
make docker-build
```

预期：镜像 `lyra:latest` 创建成功，无构建错误

- [ ] **步骤 7：冒烟测试 Docker 容器**

```bash
docker run --rm -p 4533:4533 lyra:latest &
sleep 3
curl -s http://localhost:4533/health
docker stop $(docker ps -q --filter ancestor=lyra:latest)
```

预期：`{"status":"ok","version":"0.1.0"}`

- [ ] **步骤 8：最终运行完整测试套件**

```bash
make test
```

预期：所有测试 PASS，无失败

- [ ] **步骤 9：最终提交并推送**

```bash
git add .gitignore Makefile Dockerfile docker-compose.yml CLAUDE.md docs/
git commit -m "chore: 添加 Makefile、Dockerfile、docker-compose、CLAUDE.md"
git push origin master
```

---

## 自检清单

**需求覆盖：**
- [x] US-30（YAML 配置）→ 任务 2
- [x] US-31（重启后数据持久化）→ 任务 3（SQLite WAL，db.Open 自动创建数据目录）
- [x] Health 端点 + Docker（v0.1 验收标准）→ 任务 4 + 7
- [x] 二进制命名 `lyra` → 全文统一
- [x] 单二进制部署 → 任务 6 embed

**本计划范围之外（后续独立计划）：**
- 文件扫描器（US-01、US-03）
- Subsonic API 端点（US-28、US-29）
- 元数据刮削
- 歌词
- 完整 Web UI（播放器、专辑墙、搜索）
