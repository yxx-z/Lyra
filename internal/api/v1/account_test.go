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

func accountFixture(t *testing.T) (*AccountHandler, *auth.UserStore, *auth.SessionStore, []byte, *auth.User) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	key := make([]byte, 32)
	hash, _ := auth.HashPassword("old123")
	u, _ := us.Create("admin", hash, true)
	return NewAccountHandler(us, key), us, ss, key, u
}

func authedReq(t *testing.T, h http.HandlerFunc, ss *auth.SessionStore, us *auth.UserStore, u *auth.User, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, _ := ss.Create(u.ID, time.Hour)
	handler := middleware.SessionAuth(ss, us, false)(h)
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestChangePassword(t *testing.T) {
	h, us, ss, _, u := accountFixture(t)
	w := authedReq(t, h.ChangePassword, ss, us, u, "POST", `{"oldPassword":"old123","newPassword":"new456"}`)
	if w.Code != 200 {
		t.Fatalf("应成功: %d %s", w.Code, w.Body.String())
	}
	got, _ := us.ByID(u.ID)
	if !auth.CheckPassword(got.PasswordHash, "new456") {
		t.Error("新密码应生效")
	}
}

func TestChangePassword_WrongOld(t *testing.T) {
	h, us, ss, _, u := accountFixture(t)
	w := authedReq(t, h.ChangePassword, ss, us, u, "POST", `{"oldPassword":"bad","newPassword":"new456"}`)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("旧密码错应 401: %d", w.Code)
	}
}

func TestSetSubsonicPassword(t *testing.T) {
	h, us, ss, key, u := accountFixture(t)
	w := authedReq(t, h.SetSubsonicPassword, ss, us, u, "POST", `{"password":"sonicpw"}`)
	if w.Code != 200 {
		t.Fatalf("应成功: %d %s", w.Code, w.Body.String())
	}
	got, _ := us.ByID(u.ID)
	plain, err := auth.Decrypt(key, got.SubsonicPW)
	if err != nil || plain != "sonicpw" {
		t.Errorf("Subsonic 密码应可解回原文: %q err=%v", plain, err)
	}
}
