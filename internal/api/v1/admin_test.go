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
)

type adminEnv struct {
	users   *auth.UserStore
	router  http.Handler
	token   string
	adminID string
}

func mustHash(t *testing.T, pw string) string {
	t.Helper()
	hash, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	set := auth.NewSettingsStore(d)
	admin, _ := us.Create("admin", mustHash(t, "adminpw"), true)
	token, _ := ss.Create(admin.ID, time.Hour)
	h := NewAdminHandler(us, set)

	r := chi.NewRouter()
	r.Route("/admin", func(r chi.Router) {
		r.Use(middleware.SessionAuth(ss, us, false))
		r.Use(middleware.RequireAdmin)
		r.Get("/users", h.ListUsers)
		r.Post("/users", h.CreateUser)
		r.Delete("/users/{id}", h.DeleteUser)
		r.Post("/users/{id}/password", h.ResetPassword)
		r.Post("/users/{id}/role", h.SetRole)
		r.Get("/settings", h.GetSettings)
		r.Post("/settings", h.SetSettings)
	})
	return &adminEnv{users: us, router: r, token: token, adminID: admin.ID}
}

func (e *adminEnv) do(method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: e.token})
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func TestAdmin_CreateListDeleteUser(t *testing.T) {
	e := newAdminEnv(t)
	w := e.do("POST", "/admin/users", `{"username":"alice","password":"alicepw","isAdmin":false}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"username":"alice"`) {
		t.Fatalf("建用户应成功: %d %s", w.Code, w.Body.String())
	}
	w = e.do("GET", "/admin/users", "")
	if !strings.Contains(w.Body.String(), `"alice"`) || !strings.Contains(w.Body.String(), `"admin"`) {
		t.Errorf("列表应含 alice 与 admin: %s", w.Body.String())
	}
	alice, _ := e.users.ByUsername("alice")
	w = e.do("DELETE", "/admin/users/"+alice.ID, "")
	if w.Code != 200 {
		t.Errorf("删 alice 应成功: %d %s", w.Code, w.Body.String())
	}
	if _, err := e.users.ByUsername("alice"); err == nil {
		t.Error("alice 应已删除")
	}
}

func TestAdmin_CreateUser_DuplicateAndShortPw(t *testing.T) {
	e := newAdminEnv(t)
	if e.do("POST", "/admin/users", `{"username":"admin","password":"whatever"}`).Code != http.StatusConflict {
		t.Error("重名应 409")
	}
	if e.do("POST", "/admin/users", `{"username":"x","password":"1"}`).Code != http.StatusBadRequest {
		t.Error("短密码应 400")
	}
}

func TestAdmin_DeleteSelfAndDemoteLastAdminBlocked(t *testing.T) {
	e := newAdminEnv(t)
	if e.do("DELETE", "/admin/users/"+e.adminID, "").Code != http.StatusBadRequest {
		t.Error("删自己应 400")
	}
	if e.do("POST", "/admin/users/"+e.adminID+"/role", `{"isAdmin":false}`).Code != http.StatusBadRequest {
		t.Error("降级最后一个管理员应 400")
	}
}

func TestAdmin_ResetPasswordAndRole(t *testing.T) {
	e := newAdminEnv(t)
	bob, _ := e.users.Create("bob", mustHash(t, "bobpw"), false)
	if e.do("POST", "/admin/users/"+bob.ID+"/password", `{"password":"newpw123"}`).Code != 200 {
		t.Error("重置密码应成功")
	}
	got, _ := e.users.ByID(bob.ID)
	if !auth.CheckPassword(got.PasswordHash, "newpw123") {
		t.Error("新密码应生效")
	}
	if e.do("POST", "/admin/users/"+bob.ID+"/role", `{"isAdmin":true}`).Code != 200 {
		t.Error("升级 bob 应成功")
	}
	got, _ = e.users.ByID(bob.ID)
	if !got.IsAdmin {
		t.Error("bob 应已是管理员")
	}
	if e.do("POST", "/admin/users/nope/password", `{"password":"xxxx"}`).Code != http.StatusNotFound {
		t.Error("重置不存在用户应 404")
	}
}

func TestAdmin_Settings(t *testing.T) {
	e := newAdminEnv(t)
	if !strings.Contains(e.do("GET", "/admin/settings", "").Body.String(), `"allowRegistration":false`) {
		t.Error("默认 allowRegistration 应 false")
	}
	if e.do("POST", "/admin/settings", `{"allowRegistration":true}`).Code != 200 {
		t.Error("设置应成功")
	}
	if !strings.Contains(e.do("GET", "/admin/settings", "").Body.String(), `"allowRegistration":true`) {
		t.Error("切换后应 true")
	}
}
