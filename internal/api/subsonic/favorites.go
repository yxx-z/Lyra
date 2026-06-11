package subsonic

import (
	"net/http"

	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/userdata"
)

func (h *Handler) star(w http.ResponseWriter, r *http.Request)   { h.setStar(w, r, true) }
func (h *Handler) unstar(w http.ResponseWriter, r *http.Request) { h.setStar(w, r, false) }

// getStarred2 返回当前用户收藏的歌曲/专辑/歌手。
func (h *Handler) getStarred2(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	res := &Starred2{}

	songIDs, _ := h.store.StarredIDs(u.ID, userdata.TypeSong)
	for _, id := range songIDs {
		if c, ok := h.childByID(id); ok {
			c.Starred = "starred"
			res.Song = append(res.Song, c)
		}
	}
	albumIDs, _ := h.store.StarredIDs(u.ID, userdata.TypeAlbum)
	for _, id := range albumIDs {
		if al, ok := h.albumSummaryByID(id); ok {
			al.Starred = "starred"
			res.Album = append(res.Album, al)
		}
	}
	artistIDs, _ := h.store.StarredIDs(u.ID, userdata.TypeArtist)
	for _, id := range artistIDs {
		if ar, ok := h.artistSummaryByID(id); ok {
			ar.Starred = "starred"
			res.Artist = append(res.Artist, ar)
		}
	}
	writeResponse(w, r, &Response{Starred2: res})
}

// albumSummaryByID 构造一个不含曲目的 AlbumID3（用于 starred 列表）。
func (h *Handler) albumSummaryByID(id string) (AlbumID3, bool) {
	var al AlbumID3
	var date, genre, artistID string
	err := h.db.QueryRow(`
		SELECT al.id, al.title, COALESCE(al.artist_id,''), COALESCE(ar.name,''),
		       COALESCE(al.release_date,''), COALESCE(al.genre,''),
		       (SELECT COUNT(*) FROM tracks WHERE album_id=al.id AND is_available=1),
		       (SELECT COALESCE(SUM(duration),0) FROM tracks WHERE album_id=al.id AND is_available=1)
		FROM albums al LEFT JOIN artists ar ON ar.id=al.artist_id WHERE al.id=?`, id).
		Scan(&al.ID, &al.Name, &artistID, &al.Artist, &date, &genre, &al.SongCount, &al.Duration)
	if err != nil {
		return AlbumID3{}, false
	}
	al.ArtistID = artistID
	al.CoverArt = al.ID
	al.Year = yearFromDate(date)
	al.Genre = genre
	return al, true
}

// artistSummaryByID 构造一个不含专辑的 ArtistID3。
func (h *Handler) artistSummaryByID(id string) (ArtistID3, bool) {
	var ar ArtistID3
	err := h.db.QueryRow(`
		SELECT ar.id, ar.name, (SELECT COUNT(*) FROM albums WHERE artist_id=ar.id)
		FROM artists ar WHERE ar.id=?`, id).Scan(&ar.ID, &ar.Name, &ar.AlbumCount)
	if err != nil {
		return ArtistID3{}, false
	}
	return ar, true
}

// annotateSongs 用当前用户的歌曲收藏批量标注 Child.Starred。
func (h *Handler) annotateSongs(u *auth.User, songs []Child) {
	if u == nil || len(songs) == 0 {
		return
	}
	m, err := h.store.StarredMap(u.ID, userdata.TypeSong)
	if err != nil || len(m) == 0 {
		return
	}
	for i := range songs {
		if ts, ok := m[songs[i].ID]; ok {
			songs[i].Starred = ts
		}
	}
}

// setStar 按 id（歌曲）、albumId、artistId 三类多值参数加/取消收藏。
func (h *Handler) setStar(w http.ResponseWriter, r *http.Request, on bool) {
	u := userFromCtx(r.Context())
	apply := func(itemType string, ids []string) {
		for _, id := range ids {
			if id == "" {
				continue
			}
			if on {
				_ = h.store.Star(u.ID, itemType, id)
			} else {
				_ = h.store.Unstar(u.ID, itemType, id)
			}
		}
	}
	apply(userdata.TypeSong, r.Form["id"])
	apply(userdata.TypeAlbum, r.Form["albumId"])
	apply(userdata.TypeArtist, r.Form["artistId"])
	writeResponse(w, r, &Response{})
}
