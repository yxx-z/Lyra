package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

func setup(t *testing.T) (*auth.UserStore, *auth.SessionStore, *auth.User) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	u, _ := us.Create("admin", "h", true)
	return us, auth.NewSessionStore(d), u
}

func probe(w http.ResponseWriter, r *http.Request) {
	if u, ok := UserFromContext(r.Context()); ok {
		w.Header().Set("X-User", u.Username)
	}
	w.WriteHeader(http.StatusOK)
}

func TestSessionAuth_ValidCookie(t *testing.T) {
	us, ss, u := setup(t)
	token, _ := ss.Create(u.ID, time.Hour)
	h := SessionAuth(ss, us, false)(http.HandlerFunc(probe))
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || w.Header().Get("X-User") != "admin" {
		t.Errorf("应放行并注入用户: code=%d user=%q", w.Code, w.Header().Get("X-User"))
	}
}

func TestSessionAuth_BearerToken(t *testing.T) {
	us, ss, u := setup(t)
	token, _ := ss.Create(u.ID, time.Hour)
	h := SessionAuth(ss, us, false)(http.HandlerFunc(probe))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Bearer 令牌应放行: %d", w.Code)
	}
}

func TestSessionAuth_NoToken401(t *testing.T) {
	us, ss, _ := setup(t)
	h := SessionAuth(ss, us, false)(http.HandlerFunc(probe))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("无令牌应 401: %d", w.Code)
	}
}

func TestSessionAuth_DisabledActsAsFirstAdmin(t *testing.T) {
	us, ss, _ := setup(t)
	h := SessionAuth(ss, us, true)(http.HandlerFunc(probe))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || w.Header().Get("X-User") != "admin" {
		t.Errorf("禁用认证应以首管理员身份放行: code=%d user=%q", w.Code, w.Header().Get("X-User"))
	}
}
