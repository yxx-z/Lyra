package subsonic

import (
	"strings"
	"testing"

	"github.com/yxx-z/lyra/internal/auth"
)

func TestBookmarks_CreateGetDelete(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)

	w := doReq(t, h, "/rest/createBookmark?u=admin&p=secret&id=t1&position=42000&comment=hi&f=json")
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("createBookmark: %s", w.Body.String())
	}
	w = doReq(t, h, "/rest/getBookmarks?u=admin&p=secret&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"position":42000`) || !strings.Contains(b, `"comment":"hi"`) ||
		!strings.Contains(b, `"username":"admin"`) || !strings.Contains(b, `以父之名`) {
		t.Errorf("getBookmarks 应含书签与 Entry: %s", b)
	}
	doReq(t, h, "/rest/deleteBookmark?u=admin&p=secret&id=t1&f=json")
	w = doReq(t, h, "/rest/getBookmarks?u=admin&p=secret&f=json")
	if strings.Contains(w.Body.String(), `以父之名`) {
		t.Errorf("删除后不应再含该书签: %s", w.Body.String())
	}
}

func TestBookmarks_Upsert(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	doReq(t, h, "/rest/createBookmark?u=admin&p=secret&id=t1&position=1000&f=json")
	doReq(t, h, "/rest/createBookmark?u=admin&p=secret&id=t1&position=2000&f=json")
	var count int
	var pos int64
	h.db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(position),0) FROM bookmarks WHERE track_id='t1'`).Scan(&count, &pos)
	if count != 1 || pos != 2000 {
		t.Errorf("同曲应 upsert 覆盖：count=%d position=%d（期望 1 / 2000）", count, pos)
	}
}

func TestBookmark_TrackNotFound(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/createBookmark?u=admin&p=secret&id=nope&position=1&f=json")
	if !strings.Contains(w.Body.String(), `"code":70`) {
		t.Errorf("不存在曲目应 70: %s", w.Body.String())
	}
}

func TestPlayQueue_SaveGet(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	doReq(t, h, "/rest/savePlayQueue?u=admin&p=secret&id=t1&id=t2&current=t2&position=5000&c=Symfonium&f=json")
	w := doReq(t, h, "/rest/getPlayQueue?u=admin&p=secret&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"current":"t2"`) || !strings.Contains(b, `"position":5000`) ||
		!strings.Contains(b, `以父之名`) || !strings.Contains(b, `晴天`) {
		t.Errorf("getPlayQueue 应含队列与 current/position: %s", b)
	}
	if strings.Index(b, `以父之名`) > strings.Index(b, `晴天`) {
		t.Errorf("队列顺序应为 t1 在前: %s", b)
	}
}

func TestPlayQueue_Empty(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getPlayQueue?u=admin&p=secret&f=json")
	if !strings.Contains(w.Body.String(), `"status":"ok"`) || strings.Contains(w.Body.String(), `"playQueue"`) {
		t.Errorf("未保存队列应 ok 且无 playQueue: %s", w.Body.String())
	}
	doReq(t, h, "/rest/savePlayQueue?u=admin&p=secret&id=t1&f=json")
	doReq(t, h, "/rest/savePlayQueue?u=admin&p=secret&f=json") // 无 id → 清空
	w = doReq(t, h, "/rest/getPlayQueue?u=admin&p=secret&f=json")
	if strings.Contains(w.Body.String(), `以父之名`) {
		t.Errorf("清空后不应再含曲目: %s", w.Body.String())
	}
}

func TestGetBookmarks_EmptyIsArray(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getBookmarks?u=admin&p=secret&f=json")
	if strings.Contains(w.Body.String(), `"bookmark":null`) {
		t.Errorf("空书签应为 []，不应为 null: %s", w.Body.String())
	}
}

// TestBookmarks_PerUserIsolation 确认用户 A 的书签不出现在用户 B 的 getBookmarks（per-user 隔离）。
func TestBookmarks_PerUserIsolation(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// 造第二个用户 bob，独立的 Subsonic 密码 bobpw
	hash, _ := auth.HashPassword("loginpw")
	bob, err := h.users.Create("bob", hash, false)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := auth.Encrypt(h.key, "bobpw")
	if err := h.users.UpdateSubsonicPW(bob.ID, enc); err != nil {
		t.Fatal(err)
	}

	// admin 建一个书签
	doReq(t, h, "/rest/createBookmark?u=admin&p=secret&id=t1&position=1000&f=json")
	// bob 不应看到 admin 的书签
	w := doReq(t, h, "/rest/getBookmarks?u=bob&p=bobpw&f=json")
	if strings.Contains(w.Body.String(), "以父之名") {
		t.Errorf("bob 不应看到 admin 的书签: %s", w.Body.String())
	}
	// admin 仍应看到自己的书签
	w = doReq(t, h, "/rest/getBookmarks?u=admin&p=secret&f=json")
	if !strings.Contains(w.Body.String(), "以父之名") {
		t.Errorf("admin 应看到自己的书签: %s", w.Body.String())
	}
}
