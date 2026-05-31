# Lyra 技术设计文档

> 版本：0.1 · 日期：2026-05-31 · 语言：Go

---

## 1. 项目定位

### 1.1 目标

构建一个自托管的开源音乐服务器，核心差异化点：

- **刮削质量高**：支持音频指纹识别，对中文音乐库友好
- **部署极简**：单二进制，无外部依赖（除 ffmpeg/fpcalc）
- **协议兼容**：实现 Subsonic API，复用成熟的第三方客户端生态
- **现代 Web UI**：内置播放器，逐行/逐字歌词同步

### 1.2 对标项目与差异

| 项目 | 差距 |
|------|------|
| Navidrome | 刮削弱（只读标签），无指纹识别，无中文适配 |
| Jellyfin | 音乐是附属功能，体验差 |
| Beets | 纯 CLI，无播放器，普通用户不友好 |
| Plex | 商业化，重度 |

### 1.3 暂不支持的功能（避免范围蔓延）

- 播客 / 有声书
- 多用户权限管理（v1 阶段）
- 移动端原生 App（依赖 Subsonic 生态解决）

---

## 2. 技术栈

### 2.1 后端

| 组件 | 选型 | 说明 |
|------|------|------|
| 语言 | Go 1.22+ | 单二进制，交叉编译，goroutine 并发 |
| HTTP 框架 | [Chi](https://github.com/go-chi/chi) | 轻量，兼容标准库 |
| 数据库 | SQLite（[modernc/sqlite](https://gitlab.com/cznic/sqlite)） | 纯 Go，无 CGo 依赖 |
| ORM / QueryBuilder | [sqlc](https://sqlc.dev/) | 从 SQL 生成类型安全代码 |
| 迁移 | [golang-migrate](https://github.com/golang-migrate/migrate) | |
| 配置 | YAML + 环境变量覆盖 | |
| 文件监听 | [fsnotify](https://github.com/fsnotify/fsnotify) | 实时感知音乐库变动 |
| 标签读取 | [dhowden/tag](https://github.com/dhowden/tag) | 纯 Go，支持 MP3/FLAC/M4A/OGG，无 CGo |
| 音频转码 | ffmpeg（外部进程） | |
| 指纹分析 | fpcalc（Chromaprint，外部进程） | |
| 日志 | [slog](https://pkg.go.dev/log/slog)（标准库） | |

### 2.2 前端

| 组件 | 选型 |
|------|------|
| 框架 | Vue 3 + TypeScript |
| 构建 | Vite |
| UI 组件 | Naive UI |
| 状态管理 | Pinia |
| 音频播放 | Web Audio API + Howler.js |
| 打包方式 | 编译进 Go 二进制（embed.FS） |

### 2.3 外部依赖（运行时）

```
ffmpeg    # 转码、封面提取
fpcalc    # 音频指纹（Chromaprint）
```

两者均为可选：未安装时禁用对应功能，不影响基础播放。

---

## 3. 系统架构

```
┌─────────────────────────────────────────────────┐
│                   Client Layer                   │
│  Web UI (Vue3)  │  Subsonic Apps  │  REST API   │
└────────┬────────┴────────┬────────┴──────┬──────┘
         │                 │               │
┌────────▼─────────────────▼───────────────▼──────┐
│                    API Layer                      │
│    /api/v1 (自有)    │    /rest (Subsonic)        │
└────────────────────────┬────────────────────────┘
                         │
┌────────────────────────▼────────────────────────┐
│                  Service Layer                    │
│  LibraryService │ MetadataService │ LyricsService│
│  StreamService  │ SearchService   │ CacheService │
└──────┬──────────┬────────────────┬──────────────┘
       │          │                │
┌──────▼──┐  ┌───▼────────┐  ┌────▼──────────────┐
│ Scanner │  │  Scraper   │  │    Transcoder      │
│(fsnotify│  │MusicBrainz │  │  (ffmpeg wrapper)  │
│ + walk) │  │Last.fm     │  └───────────────────┘
└──────┬──┘  │AcoustID    │
       │     │NetEase     │
┌──────▼─────▼────────────┐
│       Data Layer         │
│  SQLite  │  File Cache   │
│          │  (封面/歌词)   │
└──────────────────────────┘
```

---

## 4. 目录结构

```
lyra/
├── cmd/
│   └── server/
│       └── main.go               # 入口
├── internal/
│   ├── api/
│   │   ├── subsonic/             # Subsonic API 实现
│   │   │   ├── browsing.go
│   │   │   ├── media.go
│   │   │   ├── search.go
│   │   │   └── stream.go
│   │   └── v1/                   # 自有 REST API
│   │       ├── library.go
│   │       ├── lyrics.go
│   │       └── metadata.go
│   ├── scanner/
│   │   ├── scanner.go            # 库扫描主逻辑
│   │   ├── watcher.go            # fsnotify 实时监听
│   │   └── tag_reader.go         # 读取内嵌标签
│   ├── metadata/
│   │   ├── scraper.go            # 刮削协调器
│   │   ├── musicbrainz.go        # MusicBrainz API
│   │   ├── lastfm.go             # Last.fm API
│   │   ├── acoustid.go           # AcoustID 指纹匹配
│   │   ├── netease.go            # 网易云音乐 API
│   │   └── fingerprint.go        # fpcalc 调用封装
│   ├── lyrics/
│   │   ├── lrclib.go             # LRCLIB API
│   │   ├── netease_lyrics.go     # 网易云歌词
│   │   └── lrc_parser.go         # LRC / YRC 解析
│   ├── transcode/
│   │   └── ffmpeg.go             # ffmpeg 封装
│   ├── db/
│   │   ├── migrations/           # SQL 迁移文件
│   │   ├── query/                # sqlc 生成的代码
│   │   └── schema.sql
│   ├── cache/
│   │   └── artwork.go            # 封面图片缓存
│   └── config/
│       └── config.go
├── web/                          # Vue3 前端
│   ├── src/
│   │   ├── components/
│   │   │   ├── Player.vue        # 播放器
│   │   │   ├── LyricsView.vue    # 歌词显示
│   │   │   └── AlbumGrid.vue
│   │   ├── stores/
│   │   │   ├── player.ts
│   │   │   └── library.ts
│   │   └── api/
│   │       └── client.ts
│   └── dist/                     # 构建输出（embed 进二进制）
├── config.example.yaml
├── Dockerfile
├── Makefile
└── README.md
```

---

## 5. 数据库 Schema

```sql
-- 艺术家
CREATE TABLE artists (
    id          TEXT PRIMARY KEY,  -- UUID（内部生成，与刮削状态无关）
    name        TEXT NOT NULL,
    sort_name   TEXT,
    biography   TEXT,
    image_url   TEXT,
    mbid        TEXT UNIQUE,       -- MusicBrainz Artist ID，刮削后填入，可为空
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 专辑
CREATE TABLE albums (
    id              TEXT PRIMARY KEY,  -- UUID（内部生成）
    title           TEXT NOT NULL,
    artist_id       TEXT REFERENCES artists(id),
    release_date    TEXT,
    genre           TEXT,
    cover_path      TEXT,              -- 本地缓存路径
    mbid            TEXT UNIQUE,       -- MusicBrainz Release ID，刮削后填入，可为空
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 曲目
CREATE TABLE tracks (
    id              TEXT PRIMARY KEY,  -- UUID（内部生成）
    title           TEXT NOT NULL,
    artist_id       TEXT REFERENCES artists(id),
    album_id        TEXT REFERENCES albums(id),
    track_number    INTEGER,
    disc_number     INTEGER DEFAULT 1,
    duration        INTEGER,           -- 秒
    file_path       TEXT NOT NULL UNIQUE,
    file_size       INTEGER,
    format          TEXT,              -- mp3/flac/aac/...
    bitrate         INTEGER,
    sample_rate     INTEGER,
    channels        INTEGER,
    mbid            TEXT,              -- MusicBrainz Recording ID
    acoustid        TEXT,              -- AcoustID
    scrape_status   TEXT DEFAULT 'pending',  -- pending/done/failed/manual
    play_count      INTEGER DEFAULT 0,
    last_played_at  DATETIME,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 歌词
CREATE TABLE lyrics (
    track_id    TEXT PRIMARY KEY REFERENCES tracks(id),
    lrc_content TEXT,        -- 标准 LRC 格式
    yrc_content TEXT,        -- 逐字歌词（网易云 YRC）
    source      TEXT,        -- lrclib/netease/embedded
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 播放队列 / 收藏（v0.4）
CREATE TABLE playlists (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE playlist_tracks (
    id          INTEGER PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES playlists(id),
    track_id    TEXT NOT NULL REFERENCES tracks(id),
    position    INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_playlist_tracks_pos ON playlist_tracks(playlist_id, position);

-- 索引
CREATE INDEX idx_tracks_album ON tracks(album_id);
CREATE INDEX idx_tracks_artist ON tracks(artist_id);
CREATE INDEX idx_tracks_scrape_status ON tracks(scrape_status);
CREATE INDEX idx_albums_artist ON albums(artist_id);
```

---

## 6. 刮削策略

### 6.1 流程图

```
文件入库
    │
    ▼
① 读内嵌标签（ID3 / Vorbis Comment / M4A）
    │
    ├─ 有 MusicBrainz Recording ID ──────────────────────┐
    │                                                     │
    ├─ 有 Title + Artist，置信度足够 → MusicBrainz 模糊搜索 │
    │                                                     │
    └─ 标签缺失或模糊度高 → ② Chromaprint 指纹           │
              │                                           │
              └→ AcoustID API → 获得 MBID ───────────────┘
                                                         │
                                              ③ MusicBrainz API
                                           精确查询完整元数据
                                                         │
                                    ┌────────────────────┤
                                    │                    │
                              ④ Last.fm              ⑤ 中文歌曲？
                           艺术家简介/图片             → 网易云 API
                           专辑补充封面               补充元数据/封面
                                    │
                              ⑥ Spotify API
                           高清封面（可选）
                                    │
                              ⑦ 查询歌词
                           LRCLIB → 网易云
                           （优先 LRC 格式）
                                    │
                              写入数据库
                         scrape_status = done
```

### 6.2 置信度规则

| 条件 | 行为 |
|------|------|
| 有 MBID | 直接精确查询，跳过模糊匹配 |
| Title + Artist 匹配度 > 90% | 自动接受 |
| 匹配度 60%~90% | 记录候选，标记为 `needs_review`，用户在 UI 中确认 |
| 匹配度 < 60% | 触发 AcoustID 指纹识别 |
| 指纹识别失败 | 标记为 `failed`，保留内嵌标签原始数据 |

### 6.3 中文歌曲识别增强

- 优先使用网易云 API 搜索（中文元数据质量更高）
- 搜索时结合语言标记（`lang: zh`）过滤 MusicBrainz 结果
- 封面优先级：内嵌 > 本地 `cover.jpg` > 网易云 > Last.fm > Spotify

---

## 7. API 设计

### 7.1 Subsonic API（兼容层）

实现核心子集，协议版本 **1.16.1**，覆盖主流客户端（DSub、Symfonium、Ultrasonic、Feishin）：

```
GET /rest/ping
GET /rest/getMusicFolders
GET /rest/getArtists
GET /rest/getArtist
GET /rest/getAlbum
GET /rest/getSong
GET /rest/search3
GET /rest/stream
GET /rest/getCoverArt
GET /rest/getLyrics          # 返回 LRC 内容
GET /rest/getRandomSongs
GET /rest/getSongsByGenre
GET /rest/scrobble           # 记录播放次数
```

### 7.2 自有 REST API

```
# 库管理
POST   /api/v1/library/scan          # 触发手动扫描
GET    /api/v1/library/scan/status   # 扫描进度

# 元数据
GET    /api/v1/tracks/:id
PATCH  /api/v1/tracks/:id/metadata   # 手动修正元数据
POST   /api/v1/tracks/:id/scrape     # 重新刮削

# 歌词
GET    /api/v1/tracks/:id/lyrics     # 返回 LRC / YRC

# 封面
GET    /api/v1/albums/:id/cover
GET    /api/v1/artists/:id/image

# 播放
GET    /api/v1/stream/:id            # 音频流
GET    /api/v1/stream/:id?format=mp3&bitrate=320  # 转码流
```

### 7.3 认证

- **Web UI / 自有 API**：Bearer Token 认证，Token 启动时生成写入配置文件；内网可通过 `auth.disable: true` 关闭
- **Subsonic API**：独立密码字段（`subsonic.password`），与管理 Token 隔离，符合 Subsonic 协议的 `u/p/t/s` 参数鉴权
- 认证中间件统一在 Chi router 层注入，Subsonic 和自有路由各自挂不同的 middleware

---

## 8. 歌词模块

### 8.1 LRC 格式解析

```go
type LrcLine struct {
    TimeMs  int64   // 毫秒时间戳
    Text    string
}

// 标准 LRC：[mm:ss.xx]歌词
// 扩展 LRC：[mm:ss.xx]<mm:ss.xx>字<mm:ss.xx>字  （逐字）
```

### 8.2 YRC 格式（网易云逐字歌词）

网易云的 YRC 格式支持逐字高亮，解析后转换为内部统一格式存储，前端按需渲染。

### 8.3 查询优先级

```
1. 数据库缓存（已刮削）
2. 内嵌 LYRICS 标签
3. LRCLIB API（有 LRC 时间轴）
4. 网易云 API（中文歌曲覆盖率高，有 YRC）
5. 纯文本歌词回退（无时间轴）
```

---

## 9. 转码模块

```
请求参数：format=[mp3|aac|opus]  bitrate=[128|192|320]

策略：
  - 源文件格式 = 请求格式 且 码率符合 → 直接流式传输，零转码
  - 否则 → ffmpeg 实时管道转码

ffmpeg 调用示例：
  ffmpeg -i <input> -vn -acodec libmp3lame -ab 192k -f mp3 pipe:1
```

---

## 10. 配置文件

```yaml
# config.yaml
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
  watch: true            # 实时监听文件变动

database:
  path: ./data/music.db

cache:
  artwork_dir: ./data/artwork
  artwork_max_size_mb: 2048

scraper:
  enabled: true
  musicbrainz:
    user_agent: "MyMusicServer/0.1 (your@email.com)"
  lastfm:
    api_key: ""
  acoustid:
    api_key: ""
  netease:
    enabled: true        # 中文歌曲增强
  spotify:
    client_id: ""
    client_secret: ""

transcode:
  ffmpeg_path: ffmpeg    # PATH 中找不到时指定绝对路径
  default_format: mp3
  default_bitrate: 192

subsonic:
  enabled: true
  password: ""           # Subsonic 客户端使用的独立密码（与管理 Token 隔离）
```

---

## 11. 开发路线图

### v0.1 — 最小可用（MVP）

- [ ] 文件扫描 + 内嵌标签读取
- [ ] SQLite 存储
- [ ] Subsonic API 核心端点（浏览 + 流媒体）
- [ ] 基础 Web UI（列表 + 播放器）
- [ ] Dockerfile + docker-compose（单架构 amd64）

### v0.2 — 刮削

- [ ] MusicBrainz API 集成
- [ ] Last.fm 艺术家信息 + 封面
- [ ] LRCLIB 歌词 + LRC 显示

### v0.3 — 精准识别

- [ ] AcoustID 指纹识别
- [ ] 网易云 API 适配（元数据 + 封面 + 歌词）
- [ ] YRC 逐字歌词渲染（随网易云 API 同期）
- [ ] 用户修正元数据 UI（管理页候选项确认）
- [ ] fsnotify 实时文件监听 + 扫描进度 API

### v0.4 — 体验完善

- [ ] 播放历史 + 统计
- [ ] 播放列表管理
- [ ] 移动端 Web UI 优化
- [ ] 转码码率选择 UI
- [ ] 库状态管理页（格式占比、刮削状态）
- [ ] ARM64 多架构镜像发布（amd64 + arm64）

### v1.0 — 稳定发布

- [ ] Spotify API 高清封面（可选，用户提供 Client ID/Secret）
- [ ] 手动上传 / 粘贴 LRC 歌词
- [ ] 多用户支持
- [ ] 完整文档
- [ ] CI/CD + 自动发布

---

## 12. 部署

### Docker（推荐）

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o lyra ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ffmpeg chromaprint
COPY --from=builder /app/lyra /usr/local/bin/
EXPOSE 4533
CMD ["lyra"]
```

```yaml
# docker-compose.yml
services:
  music-server:
    image: lyra:latest
    ports:
      - "4533:4533"
    volumes:
      - /your/music:/music:ro
      - ./data:/app/data
    environment:
      - LIBRARY_PATHS=/music
```

### 二进制直接运行

```bash
./lyra --config config.yaml
```

---

## 13. 参考资源

| 资源 | 说明 |
|------|------|
| [MusicBrainz API Docs](https://musicbrainz.org/doc/MusicBrainz_API) | 主要元数据源 |
| [AcoustID API](https://acoustid.org/webservice) | 指纹匹配 |
| [Last.fm API](https://www.last.fm/api) | 艺术家信息 |
| [LRCLIB](https://lrclib.net/docs) | 开源歌词库 |
| [Subsonic API](http://www.subsonic.org/pages/api.jsp) | 协议文档 |
| [Navidrome 源码](https://github.com/navidrome/navidrome) | Go 实现参考 |
| [Beets 源码](https://github.com/beetbox/beets) | 刮削逻辑参考 |
| [NeteaseCloudMusicApi](https://github.com/Binaryify/NeteaseCloudMusicApi) | 网易云非官方 API |
