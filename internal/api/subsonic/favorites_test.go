package subsonic

import (
	"testing"
)

func TestStarUnstar_Song(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	doReq(t, h, "/rest/star?u=admin&p=secret&id=t1&f=json")
	var n int
	h.db.QueryRow(`SELECT COUNT(*) FROM starred WHERE item_type='song' AND item_id='t1'`).Scan(&n)
	if n != 1 {
		t.Fatalf("star 后应有 1 行，实际 %d", n)
	}
	doReq(t, h, "/rest/unstar?u=admin&p=secret&id=t1&f=json")
	h.db.QueryRow(`SELECT COUNT(*) FROM starred WHERE item_type='song' AND item_id='t1'`).Scan(&n)
	if n != 0 {
		t.Errorf("unstar 后应为 0，实际 %d", n)
	}
}

func TestStar_AlbumAndArtist(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// 取 seed 出的真实专辑/歌手 id（避免对不存在 id 加星——本实现不校验存在，但用真实 id 更贴近）
	var albumID, artistID string
	h.db.QueryRow(`SELECT COALESCE(album_id,''), COALESCE(artist_id,'') FROM tracks WHERE id='t1'`).Scan(&albumID, &artistID)
	doReq(t, h, "/rest/star?u=admin&p=secret&albumId="+albumID+"&artistId="+artistID+"&f=json")
	var albums, artists int
	h.db.QueryRow(`SELECT COUNT(*) FROM starred WHERE item_type='album' AND item_id=?`, albumID).Scan(&albums)
	h.db.QueryRow(`SELECT COUNT(*) FROM starred WHERE item_type='artist' AND item_id=?`, artistID).Scan(&artists)
	if albums != 1 || artists != 1 {
		t.Errorf("专辑/歌手收藏应各 1：albums=%d artists=%d", albums, artists)
	}
}

