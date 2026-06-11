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
	"github.com/yxx-z/lyra/internal/playlists"
)

func plFixture(t *testing.T) (http.Handler, *auth.User, *playlists.Store, string) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	pl := playlists.NewStore(d)
	u, _ := us.Create("admin", mustHashFav(t, "pw"), true)
	d.Exec(`INSERT INTO tracks(id,title,file_path,duration) VALUES('t1','歌一','p1',100)`)
	d.Exec(`INSERT INTO tracks(id,title,file_path,duration) VALUES('t2','歌二','p2',100)`)
	h := NewPlaylistHandler(d, pl)
	token, _ := ss.Create(u.ID, time.Hour)

	r := chi.NewRouter()
	r.Use(middleware.SessionAuth(ss, us, false))
	r.Get("/playlists", h.List)
	r.Post("/playlists", h.Create)
	r.Get("/playlists/{id}", h.Get)
	r.Patch("/playlists/{id}", h.Update)
	r.Delete("/playlists/{id}", h.Delete)
	r.Post("/playlists/{id}/tracks", h.AddTracks)
	r.Put("/playlists/{id}/tracks", h.ReplaceTracks)
	return r, u, pl, token
}

func plDo(t *testing.T, r http.Handler, token, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestV1Playlist_CRUDAndTracks(t *testing.T) {
	r, u, pl, token := plFixture(t)
	if plDo(t, r, token, "POST", "/playlists", `{"name":"晨间"}`).Code != 200 {
		t.Fatal("新建失败")
	}
	list, _ := pl.List(u.ID)
	if len(list) != 1 {
		t.Fatalf("应有 1 个歌单: %d", len(list))
	}
	id := list[0].ID
	if plDo(t, r, token, "POST", "/playlists/"+id+"/tracks", `{"trackIds":["t1","t2"]}`).Code != 200 {
		t.Fatal("追加失败")
	}
	w := plDo(t, r, token, "GET", "/playlists/"+id, "")
	if !strings.Contains(w.Body.String(), "歌一") || !strings.Contains(w.Body.String(), "歌二") {
		t.Errorf("详情应含曲目: %s", w.Body.String())
	}
	if plDo(t, r, token, "PUT", "/playlists/"+id+"/tracks", `{"trackIds":["t2","t1"]}`).Code != 200 {
		t.Fatal("替换失败")
	}
	ids, _ := pl.TrackIDs(u.ID, id)
	if ids[0] != "t2" {
		t.Errorf("替换后 t2 应在前: %v", ids)
	}
	plDo(t, r, token, "PATCH", "/playlists/"+id, `{"name":"新名"}`)
	p, _ := pl.Get(u.ID, id)
	if p.Name != "新名" {
		t.Errorf("改名未生效: %+v", p)
	}
	if plDo(t, r, token, "DELETE", "/playlists/"+id, "").Code != 200 {
		t.Fatal("删除失败")
	}
	if list, _ := pl.List(u.ID); len(list) != 0 {
		t.Errorf("删除后列表应空")
	}
}

func TestV1Playlist_NotFound404(t *testing.T) {
	r, _, _, token := plFixture(t)
	w := plDo(t, r, token, "GET", "/playlists/nonexistent", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("不存在歌单应 404: %d", w.Code)
	}
}
