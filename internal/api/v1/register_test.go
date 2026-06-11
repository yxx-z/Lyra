package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

func regFixture(t *testing.T) (*RegisterHandler, *auth.UserStore, *auth.SettingsStore) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	set := auth.NewSettingsStore(d)
	return NewRegisterHandler(us, ss, set), us, set
}

func TestRegister_DisabledByDefault(t *testing.T) {
	h, _, _ := regFixture(t)
	req := httptest.NewRequest("POST", "/register", strings.NewReader(`{"username":"alice","password":"alicepw"}`))
	w := httptest.NewRecorder()
	h.Register(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("默认未开放，应 403: %d", w.Code)
	}
}

func TestRegister_EnabledCreatesNormalUserAndLogsIn(t *testing.T) {
	h, us, set := regFixture(t)
	set.SetAllowRegistration(true)
	req := httptest.NewRequest("POST", "/register", strings.NewReader(`{"username":"alice","password":"alicepw"}`))
	w := httptest.NewRecorder()
	h.Register(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("注册应成功并返回 token: %d %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) == 0 {
		t.Error("应下发 cookie")
	}
	u, err := us.ByUsername("alice")
	if err != nil || u.IsAdmin {
		t.Errorf("应创建普通用户: %+v err=%v", u, err)
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	h, us, set := regFixture(t)
	set.SetAllowRegistration(true)
	us.Create("alice", "h", false)
	req := httptest.NewRequest("POST", "/register", strings.NewReader(`{"username":"alice","password":"alicepw"}`))
	w := httptest.NewRecorder()
	h.Register(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("重名应 409: %d", w.Code)
	}
}

func TestRegister_Status(t *testing.T) {
	h, _, set := regFixture(t)
	w := httptest.NewRecorder()
	h.Status(w, httptest.NewRequest("GET", "/register/status", nil))
	if !strings.Contains(w.Body.String(), `"allowRegistration":false`) {
		t.Errorf("默认应 false: %s", w.Body.String())
	}
	set.SetAllowRegistration(true)
	w = httptest.NewRecorder()
	h.Status(w, httptest.NewRequest("GET", "/register/status", nil))
	if !strings.Contains(w.Body.String(), `"allowRegistration":true`) {
		t.Errorf("开启后应 true: %s", w.Body.String())
	}
}
