package subsonic

import (
	"strings"
	"testing"
)

func seedPlaylist(t *testing.T, h *Handler) string {
	t.Helper()
	var adminID string
	h.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID)
	id, err := h.pl.Create(adminID, "测试单")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.pl.AddTracks(adminID, id, []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGetPlaylists(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	seedPlaylist(t, h)
	w := doReq(t, h, "/rest/getPlaylists?u=admin&p=secret&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"playlists"`) || !strings.Contains(b, "测试单") {
		t.Errorf("getPlaylists 应含歌单: %s", b)
	}
}

func TestGetPlaylist_WithEntries(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	id := seedPlaylist(t, h)
	w := doReq(t, h, "/rest/getPlaylist?u=admin&p=secret&id="+id+"&f=json")
	b := w.Body.String()
	if !strings.Contains(b, "以父之名") || !strings.Contains(b, `"songCount":1`) {
		t.Errorf("getPlaylist 应含曲目与计数: %s", b)
	}
}

func TestGetPlaylist_NotOwner(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	id := seedPlaylist(t, h)
	hash, _ := authHashForTest(t)
	bob, _ := h.users.Create("bob", hash, false)
	enc, _ := encForTest(h, "bobpw")
	h.users.UpdateSubsonicPW(bob.ID, enc)
	w := doReq(t, h, "/rest/getPlaylist?u=bob&p=bobpw&id="+id+"&f=json")
	if !strings.Contains(w.Body.String(), `"code":70`) {
		t.Errorf("非属主应 70: %s", w.Body.String())
	}
}

func TestCreateAndDeletePlaylist(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/createPlaylist?u=admin&p=secret&name=新单&songId=t1&songId=t2&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"playlist"`) || !strings.Contains(b, `"songCount":2`) {
		t.Fatalf("createPlaylist 应返回含 2 曲的歌单: %s", b)
	}
	var adminID, pid string
	h.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID)
	h.db.QueryRow(`SELECT id FROM playlists WHERE user_id=? LIMIT 1`, adminID).Scan(&pid)
	doReq(t, h, "/rest/deletePlaylist?u=admin&p=secret&id="+pid+"&f=json")
	var n int
	h.db.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id=?`, pid).Scan(&n)
	if n != 0 {
		t.Errorf("deletePlaylist 后应删除，剩 %d", n)
	}
}

func TestUpdatePlaylist_AddAndRemove(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	id := seedPlaylist(t, h) // 已含 t1
	doReq(t, h, "/rest/updatePlaylist?u=admin&p=secret&playlistId="+id+"&songIdToAdd=t2&songIdToAdd=t3&f=json")
	var adminID string
	h.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID)
	ids, _ := h.pl.TrackIDs(adminID, id)
	if len(ids) != 3 {
		t.Fatalf("加曲后应 3 首: %v", ids)
	}
	doReq(t, h, "/rest/updatePlaylist?u=admin&p=secret&playlistId="+id+"&songIndexToRemove=0&f=json")
	ids, _ = h.pl.TrackIDs(adminID, id)
	if len(ids) != 2 || ids[0] != "t2" {
		t.Errorf("删下标 0 后应剩 t2,t3: %v", ids)
	}
}
