package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"log/slog"
	"net/http"
)

const subsonicAPIVersion = "1.16.1"
const subsonicXmlns = "http://subsonic.org/restapi"

// OpenSubsonic 服务器标识：客户端（如 Symfonium）据此识别服务器类型并启用扩展能力。
const (
	openSubsonicType          = "lyra"
	openSubsonicServerVersion = "0.1.0"
)

type Response struct {
	XMLName       xml.Name       `xml:"subsonic-response" json:"-"`
	Xmlns         string         `xml:"xmlns,attr" json:"-"`
	Status        string         `xml:"status,attr" json:"status"`
	Version       string         `xml:"version,attr" json:"version"`
	Type          string         `xml:"type,attr,omitempty" json:"type,omitempty"`
	ServerVersion string         `xml:"serverVersion,attr,omitempty" json:"serverVersion,omitempty"`
	OpenSubsonic  bool           `xml:"openSubsonic,attr,omitempty" json:"openSubsonic,omitempty"`
	Error         *Error         `xml:"error,omitempty" json:"error,omitempty"`
	License       *License       `xml:"license,omitempty" json:"license,omitempty"`
	MusicFolders  *MusicFolders  `xml:"musicFolders,omitempty" json:"musicFolders,omitempty"`
	Artists       *ArtistsID3    `xml:"artists,omitempty" json:"artists,omitempty"`
	Artist        *ArtistID3     `xml:"artist,omitempty" json:"artist,omitempty"`
	Album         *AlbumID3      `xml:"album,omitempty" json:"album,omitempty"`
	AlbumList2    *AlbumList2    `xml:"albumList2,omitempty" json:"albumList2,omitempty"`
	Song          *Child         `xml:"song,omitempty" json:"song,omitempty"`
	SearchResult3 *SearchResult3 `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
	Genres        *Genres        `xml:"genres,omitempty" json:"genres,omitempty"`
	Starred2      *Starred2      `xml:"starred2,omitempty" json:"starred2,omitempty"`
	Bookmarks     *Bookmarks     `xml:"bookmarks,omitempty" json:"bookmarks,omitempty"`
	PlayQueue     *PlayQueue     `xml:"playQueue,omitempty" json:"playQueue,omitempty"`
	Playlists     *Playlists     `xml:"playlists,omitempty" json:"playlists,omitempty"`
	Playlist      *Playlist      `xml:"playlist,omitempty" json:"playlist,omitempty"`
}

// 以下为第一期未实现、但客户端（Symfonium 等）启动时会探测的端点的空容器，
// 返回合法的空 Subsonic 响应以免客户端因纯文本 404 解析失败而中断同步。
type Genres struct {
	Genre []Genre `xml:"genre" json:"genre"`
}
type Genre struct {
	SongCount  int    `xml:"songCount,attr" json:"songCount"`
	AlbumCount int    `xml:"albumCount,attr" json:"albumCount"`
	Value      string `xml:",chardata" json:"value"`
}
type Starred2 struct {
	Artist []ArtistID3 `xml:"artist,omitempty" json:"artist,omitempty"`
	Album  []AlbumID3  `xml:"album,omitempty" json:"album,omitempty"`
	Song   []Child     `xml:"song,omitempty" json:"song,omitempty"`
}
type Bookmarks struct {
	Bookmark []Bookmark `xml:"bookmark" json:"bookmark"`
}
type Bookmark struct {
	Position int64  `xml:"position,attr" json:"position"`
	Username string `xml:"username,attr" json:"username"`
	Comment  string `xml:"comment,attr,omitempty" json:"comment,omitempty"`
	Created  string `xml:"created,attr" json:"created"`
	Changed  string `xml:"changed,attr" json:"changed"`
	Entry    Child  `xml:"entry" json:"entry"`
}
type PlayQueue struct {
	Current   string  `xml:"current,attr,omitempty" json:"current,omitempty"`
	Position  int64   `xml:"position,attr,omitempty" json:"position,omitempty"`
	Username  string  `xml:"username,attr" json:"username"`
	Changed   string  `xml:"changed,attr" json:"changed"`
	ChangedBy string  `xml:"changedBy,attr,omitempty" json:"changedBy,omitempty"`
	Entry     []Child `xml:"entry,omitempty" json:"entry,omitempty"`
}
type Playlists struct {
	Playlist []Playlist `xml:"playlist" json:"playlist"`
}
type Playlist struct {
	ID        string  `xml:"id,attr" json:"id"`
	Name      string  `xml:"name,attr" json:"name"`
	Comment   string  `xml:"comment,attr,omitempty" json:"comment,omitempty"`
	Owner     string  `xml:"owner,attr" json:"owner"`
	Public    bool    `xml:"public,attr" json:"public"`
	SongCount int     `xml:"songCount,attr" json:"songCount"`
	Duration  int     `xml:"duration,attr" json:"duration"`
	Created   string  `xml:"created,attr" json:"created"`
	Changed   string  `xml:"changed,attr" json:"changed"`
	Entry     []Child `xml:"entry,omitempty" json:"entry,omitempty"`
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
	IgnoredArticles string     `xml:"ignoredArticles,attr" json:"ignoredArticles"`
	Index           []IndexID3 `xml:"index" json:"index"`
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
	Starred    string     `xml:"starred,attr,omitempty" json:"starred,omitempty"`
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
	Starred   string  `xml:"starred,attr,omitempty" json:"starred,omitempty"`
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
	Starred     string `xml:"starred,attr,omitempty" json:"starred,omitempty"`
}
type SearchResult3 struct {
	Artist []ArtistID3 `xml:"artist,omitempty" json:"artist,omitempty"`
	Album  []AlbumID3  `xml:"album,omitempty" json:"album,omitempty"`
	Song   []Child     `xml:"song,omitempty" json:"song,omitempty"`
}

// writeResponse 按 f 参数输出 XML 或 JSON 的 subsonic-response 封套。
func writeResponse(w http.ResponseWriter, r *http.Request, resp *Response) {
	_ = r.ParseForm()
	if resp.Status == "" {
		resp.Status = "ok"
	}
	resp.Version = subsonicAPIVersion
	resp.Xmlns = subsonicXmlns
	resp.Type = openSubsonicType
	resp.ServerVersion = openSubsonicServerVersion
	resp.OpenSubsonic = true

	switch r.Form.Get("f") {
	case "json", "jsonp":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		body, err := json.Marshal(map[string]*Response{"subsonic-response": resp})
		if err != nil {
			slog.Error("subsonic JSON 编码失败", "err", err)
			return
		}
		if cb := r.Form.Get("callback"); r.Form.Get("f") == "jsonp" && cb != "" {
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
