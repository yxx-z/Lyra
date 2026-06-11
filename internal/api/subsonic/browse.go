package subsonic

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/userdata"
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

// scanChild 从一行 tracks 扫描结果构造 Child。
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
	u := userFromCtx(r.Context())
	typ := r.Form.Get("type")
	size := atoiDefault(r.Form.Get("size"), 10)
	if size > 500 {
		size = 500
	}
	offset := atoiDefault(r.Form.Get("offset"), 0)

	const base = `
		SELECT al.id, al.title, COALESCE(al.artist_id,''), COALESCE(ar.name,''),
		       COALESCE(al.release_date,''), COALESCE(al.genre,''),
		       (SELECT COUNT(*) FROM tracks WHERE album_id=al.id AND is_available=1),
		       (SELECT COALESCE(SUM(duration),0) FROM tracks WHERE album_id=al.id AND is_available=1)
		FROM albums al LEFT JOIN artists ar ON ar.id=al.artist_id `

	var query string
	var args []any
	switch typ {
	case "newest":
		query = base + `ORDER BY al.created_at DESC LIMIT ? OFFSET ?`
		args = []any{size, offset}
	case "alphabeticalByName", "":
		query = base + `ORDER BY al.title LIMIT ? OFFSET ?`
		args = []any{size, offset}
	case "random":
		query = base + `ORDER BY RANDOM() LIMIT ? OFFSET ?`
		args = []any{size, offset}
	case "recent":
		query = base + `JOIN (
			SELECT t.album_id AS aid, MAX(ps.last_played_at) AS lp
			FROM play_stats ps JOIN tracks t ON t.id=ps.track_id
			WHERE ps.user_id=? AND ps.last_played_at IS NOT NULL AND t.album_id IS NOT NULL
			GROUP BY t.album_id
		) r ON r.aid=al.id ORDER BY r.lp DESC LIMIT ? OFFSET ?`
		args = []any{userID(u), size, offset}
	case "frequent":
		query = base + `JOIN (
			SELECT t.album_id AS aid, SUM(ps.play_count) AS pc
			FROM play_stats ps JOIN tracks t ON t.id=ps.track_id
			WHERE ps.user_id=? AND t.album_id IS NOT NULL
			GROUP BY t.album_id
		) f ON f.aid=al.id ORDER BY f.pc DESC LIMIT ? OFFSET ?`
		args = []any{userID(u), size, offset}
	case "starred":
		query = base + `JOIN starred s ON s.item_id=al.id AND s.item_type='album' AND s.user_id=?
			ORDER BY s.created_at DESC LIMIT ? OFFSET ?`
		args = []any{userID(u), size, offset}
	default:
		writeError(w, r, 10, "不支持的 type")
		return
	}

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	defer rows.Close()

	var starredMap map[string]string
	if u != nil {
		starredMap, _ = h.store.StarredMap(u.ID, userdata.TypeAlbum)
	}
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
		if ts, ok := starredMap[al.ID]; ok {
			al.Starred = ts
		}
		list.Album = append(list.Album, al)
	}
	writeResponse(w, r, &Response{AlbumList2: list})
}

// userID 安全取用户 id（u 可能为 nil）。
func userID(u *auth.User) string {
	if u == nil {
		return ""
	}
	return u.ID
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
