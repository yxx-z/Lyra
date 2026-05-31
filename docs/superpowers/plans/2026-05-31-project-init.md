# Lyra — Project Initialization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Scaffold the Lyra music server — Go module, config system, SQLite DB layer, Chi HTTP server with `/health`, embedded Vue 3 frontend, Dockerfile, and Makefile — producing a single binary that starts and serves.

**Architecture:** Single Go binary serves an embedded Vue 3 frontend (built by Vite, output to `ui/dist/`, embedded via `//go:embed`). Backend uses Chi for routing, `modernc.org/sqlite` for a pure-Go SQLite driver (no CGo), and a hand-rolled migration runner reading SQL files from an embedded FS. Frontend source lives in `web/`, its build output lands in `ui/dist/` which is a separate Go package (`package ui`) responsible only for the embed.

**Tech Stack:** Go 1.22+, Chi v5, modernc.org/sqlite, gopkg.in/yaml.v3, Vue 3 + TypeScript + Vite + Naive UI + Pinia, Docker

---

## Prerequisites

Before starting any task, ensure the following are installed:

```bash
# Install Go 1.22+ (WSL2/Ubuntu)
wget https://go.dev/dl/go1.22.4.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.22.4.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version  # should print go1.22.x

# Node is already installed (v24)
node --version
npm --version

# Docker (for Task 7)
docker --version
```

---

## File Map

```
lyra/
├── cmd/server/
│   └── main.go                     # entry point, wires config + db + router
├── internal/
│   ├── api/
│   │   ├── router.go               # Chi router, /health endpoint
│   │   └── router_test.go
│   ├── config/
│   │   ├── config.go               # Config struct + Load() + Default()
│   │   └── config_test.go
│   └── db/
│       ├── db.go                   # Open(), WAL mode, migration runner
│       ├── db_test.go
│       ├── schema.sql              # reference schema (not embedded)
│       └── migrations/
│           └── 001_init.up.sql     # first migration
├── ui/
│   ├── ui.go                       # package ui; //go:embed all:dist
│   └── dist/                       # Vite output (gitignored, .gitkeep present)
│       └── .gitkeep
├── web/                            # Vue 3 source
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

## Task 1: Environment + Go module + directory skeleton

**Files:**
- Create: `go.mod`
- Create: all `internal/`, `ui/`, `web/src/` directories

- [ ] **Step 1: Verify Go is installed**

```bash
go version
```

Expected: `go version go1.22.x linux/amd64`

- [ ] **Step 2: Initialize Go module**

```bash
cd /home/yxx/develop/Lyra
go mod init github.com/yxx-z/lyra
```

Expected: `go.mod` created containing `module github.com/yxx-z/lyra` and `go 1.22`

- [ ] **Step 3: Create all directories**

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

- [ ] **Step 4: Create stub main.go**

```go
// cmd/server/main.go
package main

func main() {}
```

- [ ] **Step 5: Add .gitkeep for ui/dist so the directory is tracked before the build**

```bash
touch ui/dist/.gitkeep
```

- [ ] **Step 6: Commit**

```bash
git add go.mod cmd/ internal/ ui/ web/ data/
git commit -m "chore: init Go module and directory skeleton"
```

---

## Task 2: Configuration system

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `config.example.yaml`

- [ ] **Step 1: Write the failing tests**

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
		t.Errorf("want port 4533, got %d", cfg.Server.Port)
	}
	if cfg.Transcode.DefaultFormat != "mp3" {
		t.Errorf("want mp3, got %s", cfg.Transcode.DefaultFormat)
	}
	if cfg.Transcode.DefaultBitrate != 192 {
		t.Errorf("want 192, got %d", cfg.Transcode.DefaultBitrate)
	}
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	cfg, err := Load("does-not-exist.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 4533 {
		t.Errorf("want 4533, got %d", cfg.Server.Port)
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
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("want 9090, got %d", cfg.Server.Port)
	}
}
```

- [ ] **Step 2: Run tests — confirm FAIL**

```bash
go test ./internal/config/...
```

Expected: FAIL — `Default` and `Load` undefined

- [ ] **Step 3: Install yaml dependency**

```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 4: Implement config.go**

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
		return nil, fmt.Errorf("open config %q: %w", path, err)
	}
	defer f.Close()
	if err := yaml.NewDecoder(f).Decode(cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}
```

- [ ] **Step 5: Run tests — confirm PASS**

```bash
go test ./internal/config/... -v
```

Expected: 3 tests PASS

- [ ] **Step 6: Create config.example.yaml**

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

- [ ] **Step 7: Commit**

```bash
git add internal/config/ config.example.yaml go.mod go.sum
git commit -m "feat: add configuration system"
```

---

## Task 3: Database layer

**Files:**
- Create: `internal/db/schema.sql`
- Create: `internal/db/migrations/001_init.up.sql`
- Create: `internal/db/db.go`
- Create: `internal/db/db_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/db/db_test.go
package db

import (
	"testing"
)

func TestOpen_CreatesTablesOnFirstRun(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var count int
	row := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('artists','albums','tracks','lyrics')`,
	)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 4 {
		t.Errorf("want 4 core tables, got %d", count)
	}
}

func TestOpen_IdempotentMigrations(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	db.Close()

	// Opening the same in-memory DB won't persist, but we can verify Open()
	// itself doesn't error when called twice on the same path.
	// Use a temp file to test idempotency.
	tmp := t.TempDir() + "/test.db"
	db1, err := Open(tmp)
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}
	db1.Close()

	db2, err := Open(tmp)
	if err != nil {
		t.Fatalf("Open 2 (idempotency): %v", err)
	}
	db2.Close()
}
```

- [ ] **Step 2: Run tests — confirm FAIL**

```bash
go test ./internal/db/...
```

Expected: FAIL — `Open` undefined

- [ ] **Step 3: Install modernc sqlite**

```bash
go get modernc.org/sqlite
```

- [ ] **Step 4: Create schema.sql (reference document, not compiled into binary)**

```sql
-- internal/db/schema.sql
-- Source of truth for the schema. Changes go into a new migrations/*.up.sql file.

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

- [ ] **Step 5: Create first migration (content mirrors schema.sql)**

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

- [ ] **Step 6: Implement db.go**

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
				return nil, fmt.Errorf("create db dir: %w", err)
			}
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("pragmas: %w", err)
	}
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
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
			return fmt.Errorf("apply %s: %w", e.Name(), err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, e.Name()); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 7: Run tests — confirm PASS**

```bash
go test ./internal/db/... -v
```

Expected: 2 tests PASS

- [ ] **Step 8: Commit**

```bash
git add internal/db/ go.mod go.sum
git commit -m "feat: add SQLite database layer with embedded migration runner"
```

---

## Task 4: HTTP server + health endpoint

**Files:**
- Create: `internal/api/router.go`
- Create: `internal/api/router_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write the failing test**

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
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("want status=ok, got %q", body["status"])
	}
	if body["version"] == "" {
		t.Errorf("want non-empty version")
	}
}
```

- [ ] **Step 2: Run test — confirm FAIL**

```bash
go test ./internal/api/...
```

Expected: FAIL — `NewRouter` undefined

- [ ] **Step 3: Install Chi**

```bash
go get github.com/go-chi/chi/v5
```

- [ ] **Step 4: Implement router.go**

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

- [ ] **Step 5: Run test — confirm PASS**

```bash
go test ./internal/api/... -v
```

Expected: `TestHealth_Returns200WithStatusOK` PASS

- [ ] **Step 6: Implement main.go**

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
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	database, err := db.Open(cfg.Database.Path)
	if err != nil {
		slog.Error("open database", "err", err)
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
		slog.Info("lyra starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}
```

- [ ] **Step 7: Verify it compiles**

```bash
go build ./cmd/server
```

Expected: binary created in current directory, no errors

- [ ] **Step 8: Smoke test the running server**

```bash
./server &
sleep 1
curl -s http://localhost:4533/health
kill %1
rm server
```

Expected output: `{"status":"ok","version":"0.1.0"}`

- [ ] **Step 9: Commit**

```bash
git add internal/api/ cmd/server/ go.mod go.sum
git commit -m "feat: HTTP server with /health endpoint"
```

---

## Task 5: Frontend scaffold

**Files:**
- Create: `web/` (Vite project via npm create)
- Modify: `web/vite.config.ts` (set outDir to `../ui/dist`)
- Modify: `web/src/App.vue`
- Modify: `web/src/main.ts`

**Note on ui/dist path:** Go's `//go:embed` cannot traverse `..` — the embed file must live in a directory that is an ancestor of the embedded path. Therefore, frontend build output goes to `ui/dist/` (a sibling of `web/`), and `ui/ui.go` embeds it. This differs from the `web/dist` path mentioned in the tech spec.

- [ ] **Step 1: Scaffold Vite project**

Run from repo root (`/home/yxx/develop/Lyra`):

```bash
npm create vite@latest web -- --template vue-ts
```

If prompted interactively: select framework=Vue, variant=TypeScript. If `web/` already has content, Vite may ask to overwrite — confirm yes.

- [ ] **Step 2: Install frontend dependencies**

```bash
cd web
npm install
npm install naive-ui pinia howler @types/howler
cd ..
```

- [ ] **Step 3: Update vite.config.ts to output to ui/dist**

Replace the entire contents of `web/vite.config.ts`:

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

- [ ] **Step 4: Replace App.vue with Lyra placeholder**

```vue
<!-- web/src/App.vue -->
<template>
  <n-config-provider :theme="darkTheme">
    <n-layout style="height: 100vh">
      <n-layout-content>
        <div style="display:flex;align-items:center;justify-content:center;height:100vh;flex-direction:column;gap:1rem">
          <h1 style="font-size:3rem;margin:0">Lyra</h1>
          <p style="color:#888;margin:0">Self-hosted music server — coming soon</p>
        </div>
      </n-layout-content>
    </n-layout>
  </n-config-provider>
</template>

<script setup lang="ts">
import { NConfigProvider, NLayout, NLayoutContent, darkTheme } from 'naive-ui'
</script>
```

- [ ] **Step 5: Update main.ts to use Pinia**

```typescript
// web/src/main.ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'

const app = createApp(App)
app.use(createPinia())
app.mount('#app')
```

- [ ] **Step 6: Build frontend and verify output lands in ui/dist**

```bash
cd web && npm run build && cd ..
ls ui/dist/
```

Expected: `index.html` and an `assets/` directory inside `ui/dist/`

- [ ] **Step 7: Commit**

```bash
git add web/
git commit -m "feat: scaffold Vue 3 + TypeScript frontend"
```

---

## Task 6: Go embed — serve frontend from binary

**Files:**
- Create: `ui/ui.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/router_test.go`

- [ ] **Step 1: Create ui/ui.go**

```go
// ui/ui.go
package ui

import "embed"

//go:embed all:dist
var Dist embed.FS
```

- [ ] **Step 2: Add static file handler to router.go**

Replace `internal/api/router.go` in full:

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

	// Serve embedded frontend for all non-API routes
	sub, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		panic("embed ui/dist: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	r.Handle("/*", fileServer)

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

- [ ] **Step 3: Verify tests still pass (health test is unaffected)**

```bash
go test ./internal/api/... -v
```

Expected: `TestHealth_Returns200WithStatusOK` PASS

- [ ] **Step 4: Build full binary and smoke test static serving**

```bash
go build -o lyra ./cmd/server
./lyra &
sleep 1
# Health endpoint
curl -s http://localhost:4533/health
# Static file — should return HTML
curl -s http://localhost:4533/ | head -5
kill %1
rm lyra
```

Expected:
- `/health` → `{"status":"ok","version":"0.1.0"}`
- `/` → starts with `<!DOCTYPE html>`

- [ ] **Step 5: Commit**

```bash
git add ui/ internal/api/ go.mod go.sum
git commit -m "feat: embed Vue frontend into Go binary via ui package"
```

---

## Task 7: Build tooling + Docker + CLAUDE.md

**Files:**
- Create: `Makefile`
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Modify: `.gitignore`
- Create: `CLAUDE.md`

- [ ] **Step 1: Create .gitignore**

```gitignore
# Go build output
lyra
server

# Frontend build output (regenerated by make build-frontend)
ui/dist/
!ui/dist/.gitkeep
web/node_modules/

# Runtime data
data/

# OS / editor
.DS_Store
*.swp
```

- [ ] **Step 2: Create Makefile**

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

- [ ] **Step 3: Create Dockerfile**

```dockerfile
# Stage 1: build frontend
FROM node:20-alpine AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
# vite.config.ts sets outDir: ../ui/dist → outputs to /app/ui/dist
RUN npm run build

# Stage 2: build Go binary
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Overwrite the .gitkeep placeholder with the real build
COPY --from=frontend /app/ui/dist ./ui/dist
RUN go build -o lyra ./cmd/server

# Stage 3: minimal runtime image
FROM alpine:3.19
RUN apk add --no-cache ffmpeg chromaprint
COPY --from=builder /app/lyra /usr/local/bin/lyra
EXPOSE 4533
CMD ["lyra"]
```

- [ ] **Step 4: Create docker-compose.yml**

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

- [ ] **Step 5: Create CLAUDE.md**

```markdown
# Lyra — Claude Code Reference

Self-hosted music server. Go backend + Vue 3 frontend in a single binary.

## Quick Commands

| Task | Command |
|------|---------|
| Build everything | `make build` |
| Build frontend only | `make build-frontend` |
| Run backend (dev) | `make dev-backend` |
| Run frontend (dev) | `make dev-frontend` |
| Run all tests | `make test` |
| Build Docker image | `make docker-build` |

## Dev Workflow

Two terminals:
1. `make dev-frontend` — Vite dev server on :5173, proxies /api and /rest to :4533
2. `make dev-backend` — Go server on :4533

Frontend at http://localhost:5173, API at http://localhost:4533.

## Architecture

```
cmd/server/main.go      entry point
internal/config/        YAML config loader
internal/db/            SQLite via modernc (pure Go, no CGo)
  migrations/           *.up.sql files applied in alphabetical order
internal/api/           Chi router — /health, static frontend, future API routes
ui/ui.go                //go:embed all:dist — embeds web/dist at compile time
web/                    Vue 3 source (npm project)
  vite.config.ts        outDir: ../ui/dist (not web/dist — Go embed path constraint)
```

## Key Technical Decisions

- **modernc.org/sqlite** (not mattn/go-sqlite3): pure Go, no CGo, cross-compiles to arm64 easily
- **Hand-rolled migration runner**: reads `internal/db/migrations/*.up.sql` in alphabetical order, tracks applied versions in `schema_migrations` table; avoids golang-migrate's CGo sqlite dependency
- **ui/ package for embed**: Go's `//go:embed` cannot traverse `..`, so frontend output goes to `ui/dist/` (sibling of `web/`), embedded by `ui/ui.go`
- **Subsonic API password is separate** from the admin token (`subsonic.password` in config)

## Adding a Database Migration

1. Create `internal/db/migrations/NNN_description.up.sql` (NNN = next number, zero-padded)
2. Update `internal/db/schema.sql` to reflect the new state
3. Run `go test ./internal/db/...` to verify migration applies cleanly
```

- [ ] **Step 6: Verify Docker build succeeds**

```bash
make docker-build
```

Expected: image `lyra:latest` created, no build errors

- [ ] **Step 7: Smoke test Docker container**

```bash
docker run --rm -p 4533:4533 lyra:latest &
sleep 3
curl -s http://localhost:4533/health
docker stop $(docker ps -q --filter ancestor=lyra:latest)
```

Expected: `{"status":"ok","version":"0.1.0"}`

- [ ] **Step 8: Run full test suite one final time**

```bash
make test
```

Expected: all tests PASS, no failures

- [ ] **Step 9: Final commit and push**

```bash
git add .gitignore Makefile Dockerfile docker-compose.yml CLAUDE.md docs/
git commit -m "chore: add Makefile, Dockerfile, docker-compose, CLAUDE.md"
git push origin master
```

---

## Self-Review Checklist

**Spec coverage:**
- [x] US-30 (YAML config) → Task 2
- [x] US-31 (data persistence on restart) → Task 3 (SQLite WAL, data dir created by db.Open)
- [x] US-18 web playback / US-28 Subsonic client compatibility → router scaffold in Task 4 (stubs for v0.1 feature tasks)
- [x] Health endpoint + Docker (v0.1 acceptance criteria) → Tasks 4 + 7
- [x] Binary naming `lyra` → consistent throughout
- [x] Single binary deployment → embed in Task 6

**Out of scope for this plan (separate feature plans):**
- File scanner (US-01, US-03)
- Subsonic API endpoints (US-28, US-29)
- Metadata scraping
- Lyrics
- Full Web UI (player, album grid, search)
