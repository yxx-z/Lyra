package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

func authFixture(t *testing.T) (*AuthHandler, *auth.UserStore, *auth.SessionStore) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	hash, _ := auth.HashPassword("pw123")
	us.Create("admin", hash, true)
	return NewAuthHandler(us, ss), us, ss
}

func TestLogin_Success(t *testing.T) {
	h, _, _ := authFixture(t)
	req := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"admin","password":"pw123"}`))
	w := httptest.NewRecorder()
	h.Login(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("登录应成功并返回 token: %d %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) == 0 {
		t.Error("应下发 cookie")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h, _, _ := authFixture(t)
	req := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"admin","password":"bad"}`))
	w := httptest.NewRecorder()
	h.Login(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("错误密码应 401: %d", w.Code)
	}
}

func TestMe_ReturnsCurrentUser(t *testing.T) {
	h, us, ss := authFixture(t)
	u, _ := us.ByUsername("admin")
	token, _ := ss.Create(u.ID, time.Hour)
	handler := middleware.SessionAuth(ss, us, false)(http.HandlerFunc(h.Me))
	req := httptest.NewRequest("GET", "/me", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `"username":"admin"`) || !strings.Contains(w.Body.String(), `"isAdmin":true`) {
		t.Errorf("me 返回不符: %s", w.Body.String())
	}
}
