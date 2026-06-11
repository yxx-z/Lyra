package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/userdata"
)

func mustHashFav(t *testing.T, pw string) string {
	t.Helper()
	h, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func favFixture(t *testing.T) (*StarHandler, *auth.UserStore, *auth.SessionStore, *auth.User, *userdata.Store) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	store := userdata.NewStore(d)
	u, _ := us.Create("admin", mustHashFav(t, "pw"), true)
	d.Exec(`INSERT INTO albums(id,title) VALUES('al1','专辑')`)
	d.Exec(`INSERT INTO tracks(id,title,album_id,file_path) VALUES('t1','歌一','al1','p1')`)
	return NewStarHandler(d, store), us, ss, u, store
}

func favReq(t *testing.T, fn http.HandlerFunc, ss *auth.SessionStore, us *auth.UserStore, u *auth.User, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, _ := ss.Create(u.ID, time.Hour)
	handler := middleware.SessionAuth(ss, us, false)(fn)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestV1_StarAndFavorites(t *testing.T) {
	h, us, ss, u, _ := favFixture(t)
	w := favReq(t, h.Star, ss, us, u, "POST", "/star", `{"type":"song","id":"t1"}`)
	if w.Code != 200 {
		t.Fatalf("star 应成功: %d %s", w.Code, w.Body.String())
	}
	w = favReq(t, h.Favorites, ss, us, u, "GET", "/favorites", "")
	if !strings.Contains(w.Body.String(), "歌一") {
		t.Errorf("favorites 应含已收藏歌曲: %s", w.Body.String())
	}
}

func TestV1_ScrobbleAndRecent(t *testing.T) {
	h, us, ss, u, store := favFixture(t)
	token, _ := ss.Create(u.ID, time.Hour)
	r := chi.NewRouter()
	r.Use(middleware.SessionAuth(ss, us, false))
	r.Post("/tracks/{id}/scrobble", h.Scrobble)
	r.Get("/recently-played", h.RecentlyPlayed)

	req := httptest.NewRequest("POST", "/tracks/t1/scrobble", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("scrobble 应成功: %d", w.Code)
	}
	ids, _ := store.RecentTrackIDs(u.ID, 10)
	if len(ids) != 1 || ids[0] != "t1" {
		t.Errorf("应记录最近播放 t1: %v", ids)
	}
	req = httptest.NewRequest("GET", "/recently-played", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "歌一") {
		t.Errorf("recently-played 应含 t1: %s", w.Body.String())
	}
}

func TestV1_StarStatus(t *testing.T) {
	h, us, ss, u, _ := favFixture(t)
	// 未收藏时应 false
	w := favReq(t, h.StarStatus, ss, us, u, "GET", "/star?type=song&id=t1", "")
	if !strings.Contains(w.Body.String(), `"starred":false`) {
		t.Fatalf("未收藏应 starred:false: %s", w.Body.String())
	}
	// 收藏后应 true
	favReq(t, h.Star, ss, us, u, "POST", "/star", `{"type":"song","id":"t1"}`)
	w = favReq(t, h.StarStatus, ss, us, u, "GET", "/star?type=song&id=t1", "")
	if !strings.Contains(w.Body.String(), `"starred":true`) {
		t.Errorf("收藏后应 starred:true: %s", w.Body.String())
	}
}
