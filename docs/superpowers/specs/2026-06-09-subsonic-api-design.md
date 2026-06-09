# Subsonic API（第一期：浏览 + 播放）设计文档

> 版本：1.0 · 日期：2026-06-09 · 状态：已批准

---

## 目标

实现 Subsonic API 1.16.1 的核心子集，让第三方客户端（DSub / Symfonium / play:Sub / Substreamer 等）能连接、浏览音乐库并播放，满足 PRD v0.1 验收项「用 DSub 连接能浏览并播放」。

对应 PRD：US-12/14（Subsonic 兼容），v0.1。

---

## 范围

**第一期（本设计）：**
- 基础设施：`/rest` 路由组、Subsonic 认证（`p` 与 `t+s` 两种）、响应封套（XML + JSON 双格式）
- 端点：`ping`、`getLicense`、`getMusicFolders`、`getArtists`、`getArtist`、`getAlbum`、`getAlbumList2`、`getSong`、`getCoverArt`、`stream`、`search3`、`scrobble`

**第二期（后续独立 spec）：** 播放列表、star/rating、`getLyrics`、getGenres、getRandomSongs、旧版 getIndexes/getMusicDirectory、getArtistInfo2 等。

---

## 架构

新包 `internal/api/subsonic`（与 v1 隔离，Subsonic 认证/格式自成一体）：

```
internal/api/subsonic/
├── response.go    Response 封套（双 XML/JSON 标签）+ writeResponse(w,r,resp) + writeError + DTO 类型
├── auth.go        authenticate(query, *config.Config) *Error
├── handler.go     Handler{db,cfg} + NewHandler + RegisterRoutes + ping/getLicense/getMusicFolders + 认证中间件
├── browse.go      getArtists/getArtist/getAlbum/getAlbumList2/getSong
├── media.go       stream / getCoverArt / scrobble
└── search.go      search3
（各文件配 _test.go）
```

`internal/api/router.go`：在 v1 组之外挂 `/rest`（用 Subsonic 自身认证，不走 BearerAuth）。

---

## 响应封套（response.go）

```go
const subsonicAPIVersion = "1.16.1"

type Response struct {
    XMLName xml.Name `xml:"subsonic-response" json:"-"`
    Xmlns   string   `xml:"xmlns,attr" json:"-"`
    Status  string   `xml:"status,attr" json:"status"`           // "ok" | "failed"
    Version string   `xml:"version,attr" json:"version"`
    Error        *Error        `xml:"error,omitempty" json:"error,omitempty"`
    License      *License      `xml:"license,omitempty" json:"license,omitempty"`
    MusicFolders *MusicFolders `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
    Artists      *ArtistsID3   `xml:"artists,omitempty" json:"artists,omitempty"`
    Artist       *ArtistID3    `xml:"artist,omitempty" json:"artist,omitempty"`
    Album        *AlbumID3     `xml:"album,omitempty" json:"album,omitempty"`
    AlbumList2   *AlbumList2   `xml:"albumList2,omitempty" json:"albumList2,omitempty"`
    Song         *Child        `xml:"song,omitempty" json:"song,omitempty"`
    SearchResult3 *SearchResult3 `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
}

type Error struct {
    Code    int    `xml:"code,attr" json:"code"`
    Message string `xml:"message,attr" json:"message"`
}
```

DTO（双标签，XML 用属性，JSON 用字段）：
- `License{Valid bool xml:"valid,attr"}`（恒 true）
- `MusicFolders{Folder []MusicFolder}`；`MusicFolder{ID int "id,attr"; Name "name,attr"}`（单个 id=0 name="Music"）
- `ArtistsID3{IgnoredArticles "ignoredArticles,attr"; Index []IndexID3}`；`IndexID3{Name "name,attr"; Artist []ArtistID3}`
- `ArtistID3{ID,Name,CoverArt,AlbumCount; Album []AlbumID3}`（getArtist 时填 Album）
- `AlbumID3{ID,Name,Artist,ArtistID,CoverArt,SongCount,Duration,Year(omitempty),Genre(omitempty),Created; Song []Child}`（getAlbum 时填 Song）
- `AlbumList2{Album []AlbumID3}`
- `Child{ID,Parent,Title,Album,Artist,AlbumID,ArtistID,IsDir bool,Track(omitempty),Year(omitempty),CoverArt,Duration,BitRate(omitempty),Suffix,ContentType,Path,Type="music"}`（曲目/歌曲通用）
- `SearchResult3{Artist []ArtistID3; Album []AlbumID3; Song []Child}`

**writeResponse(w, r, resp)：**
```
resp.Status 默认 "ok"；resp.Version=subsonicAPIVersion；resp.Xmlns="http://subsonic.org/restapi"
f := r.URL.Query().Get("f")
if f=="json" || f=="jsonp":
    w.Header Content-Type application/json
    body := json.Marshal(map[string]any{"subsonic-response": resp})
    f=="jsonp" 且有 callback → 包成 callback(body);（jsonp 可选，先按 json 处理 + callback 包裹）
else:
    Content-Type application/xml；写 xml.Header + xml.Marshal(resp)
```
**writeError(w, r, code, message)：** 构造 `Response{Status:"failed", Error:&Error{code,message}}` 走 writeResponse。错误码：10 缺参、40 用户名/密码错、70 数据未找到、0/通用。

---

## 认证（auth.go）

```go
func authenticate(q url.Values, cfg *config.Config) *Error
```
- `cfg.Subsonic.Enabled == false` → Error{Code:40, "Subsonic 未启用"}（明确错误，不挂死）
- `u := q.Get("u")`；`pw := cfg.Subsonic.Password`
- `pw == "" || u != cfg.Auth.Username` → Error{40, "用户名或密码错误"}
- `p := q.Get("p")` 非空：去 `enc:` 前缀则 hex 解码，比对 == pw → 通过 / 否则 40
- 否则 `t := q.Get("t"), s := q.Get("s")` 都非空：`md5hex(pw + s) == t` → 通过 / 否则 40
- 都没有 → Error{10, "缺少认证参数"}
- 通过返回 nil

认证中间件：`RegisterRoutes` 给每个端点包一层，先 `authenticate`，失败 `writeError` 返回，成功调真正 handler。

---

## 端点行为（browse.go / media.go / search.go）

所有 id 用现有 UUID。coverArt 字段统一填 **album id**。Child 的 duration/track/year 从 tracks 表，suffix/contentType 由 format 推（如 flac→audio/flac）。

- **ping**：空 ok。
- **getLicense**：`License{Valid:true}`。
- **getMusicFolders**：单个 `{id:0, name:"Music"}`。
- **getArtists**：`SELECT ar.id, ar.name, COUNT(album)` 按艺术家；按 `name` 首字母大写分组成 IndexID3（非字母归 "#"）；`ignoredArticles=""`。
- **getArtist(id)**：艺术家 + 其专辑（`SELECT albums WHERE artist_id=? ORDER BY release_date`），每专辑带 songCount/duration。
- **getAlbum(id)**：专辑 + 其曲目（`SELECT tracks WHERE album_id=? AND is_available=1 ORDER BY disc_number,track_number`）→ Song []Child。
- **getSong(id)**：单曲 → Child；不存在 → error 70。
- **getAlbumList2**：`type`（newest=created_at desc / alphabeticalByName=title / random）+ `size`(默认10,上限500) + `offset`(默认0) → AlbumList2。未知 type → error 10。
- **getCoverArt(id)**：id 当 album id，复用现有封面优先级（内嵌 → 同目录 → cover_path）；找不到 → 404 或 error 70。
- **stream(id)**：复用现有转码管线按 trackID 输出音频（支持 `maxBitRate`/`format` 可选，缺省走默认转码）；不存在 → error 70。
- **scrobble(id)**：`UPDATE tracks SET play_count=play_count+1, last_played_at=CURRENT_TIMESTAMP WHERE id=?`；返回空 ok（`submission` 参数忽略）。
- **search3**：`query`（`*`/空 → 全部）+ artistCount/albumCount/songCount(默认20) + 各 offset；对 artists.name / albums.title / tracks.title 做 LIKE 匹配 → SearchResult3。

---

## 路由（router.go）

```go
sub := subsonic.NewHandler(db, cfg)
r.Route("/rest", sub.RegisterRoutes)   // 内部为每端点注册 "/{name}" 与 "/{name}.view"，套认证中间件
```
`RegisterRoutes(r chi.Router)`：对每个端点 `reg(r, "ping", h.ping)` → `r.Get("/ping", wrap)` + `r.Get("/ping.view", wrap)`（GET + 也接受 POST 表单参数？Subsonic 多用 GET，先 GET；stream 也 GET）。`wrap` 先认证。

---

## 测试策略

| 测试 | 方式 |
|------|------|
| writeResponse：XML 与 JSON（f=json）两种输出正确（封套、属性/字段、数组） | 单测对比关键片段 |
| authenticate：p 正确/错误、enc: 解码、t+s 正确/错误、缺参 10、空密码/禁用 40 | 表驱动 |
| ping/getLicense/getMusicFolders | httptest + 认证参数，验 status=ok + 字段 |
| getArtists/getArtist/getAlbum/getSong/getAlbumList2 | 内存 sqlite 预置 artist/album/track，httptest 验关键字段（id/name/songCount 等），XML 与 JSON 各验一次 |
| search3：命中 artist/album/song | 内存 sqlite + httptest |
| scrobble：play_count+1 | 内存 sqlite 验 DB |
| getCoverArt / stream：复用底层逻辑（cover 临时文件 / 转码可用时输出，stream 不存在→error） | httptest |
| 认证失败 → failed 封套（XML+JSON） | httptest |

**全部 httptest + 内存 sqlite，不打真网络。** 真实客户端联调（DSub 等）合并后在 docker 手动验证。

---

## 不在本次范围内（第二期）

- 播放列表（getPlaylists/createPlaylist/updatePlaylist/deletePlaylist）
- star/unstar/setRating/getStarred2
- getLyrics（把现有歌词暴露给 Subsonic 客户端）
- getGenres/getRandomSongs/getArtistInfo2
- 旧版文件浏览 getIndexes/getMusicDirectory
- 多用户（v1.0）
