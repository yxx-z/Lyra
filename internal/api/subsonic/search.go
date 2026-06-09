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
