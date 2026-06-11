package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

func adminProbe(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

func TestRequireAdmin_AdminPasses(t *testing.T) {
	d, _ := db.Open(":memory:")
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	admin, _ := us.Create("admin", "h", true)
	token, _ := ss.Create(admin.ID, time.Hour)
	h := SessionAuth(ss, us, false)(RequireAdmin(http.HandlerFunc(adminProbe)))
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("管理员应放行: %d", w.Code)
	}
}

func TestRequireAdmin_NormalForbidden(t *testing.T) {
	d, _ := db.Open(":memory:")
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	bob, _ := us.Create("bob", "h", false)
	token, _ := ss.Create(bob.ID, time.Hour)
	h := SessionAuth(ss, us, false)(RequireAdmin(http.HandlerFunc(adminProbe)))
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("普通用户应 403: %d", w.Code)
	}
}

func TestRequireAdmin_NoUser401(t *testing.T) {
	h := RequireAdmin(http.HandlerFunc(adminProbe))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("无用户应 401: %d", w.Code)
	}
}
