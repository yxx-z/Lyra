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
