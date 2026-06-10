package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

func TestSetupStatus_NeedsSetupWhenEmpty(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	h := NewSetupHandler(auth.NewUserStore(d), auth.NewSessionStore(d), d)
	w := httptest.NewRecorder()
	h.Status(w, httptest.NewRequest("GET", "/setup/status", nil))
	if !strings.Contains(w.Body.String(), `"needsSetup":true`) {
		t.Errorf("空库应 needsSetup=true: %s", w.Body.String())
	}
}

func TestSetupCreate_CreatesAdminAndClaimsOrphans(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	h := NewSetupHandler(us, ss, d)
	d.Exec(`INSERT INTO tracks(id,title,file_path) VALUES('t1','x','p1')`)
	d.Exec(`INSERT INTO bookmarks(user_id,track_id,position) VALUES(NULL,'t1',1000)`)
	d.Exec(`INSERT INTO play_queue(user_id,track_ids) VALUES(NULL,'t1')`)

	req := httptest.NewRequest("POST", "/setup", strings.NewReader(`{"username":"boss","password":"pw12345"}`))
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("创建管理员应成功并返回 token: %d %s", w.Code, w.Body.String())
	}
	u, _ := us.ByUsername("boss")
	if !u.IsAdmin {
		t.Error("首用户应为管理员")
	}
	var bmUser, pqUser string
	d.QueryRow(`SELECT user_id FROM bookmarks WHERE track_id='t1'`).Scan(&bmUser)
	d.QueryRow(`SELECT user_id FROM play_queue LIMIT 1`).Scan(&pqUser)
	if bmUser != u.ID || pqUser != u.ID {
		t.Errorf("孤儿数据应认领给首管理员: bm=%q pq=%q want=%q", bmUser, pqUser, u.ID)
	}
}

func TestSetupCreate_RejectsWhenUsersExist(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	us := auth.NewUserStore(d)
	us.Create("existing", "h", true)
	h := NewSetupHandler(us, auth.NewSessionStore(d), d)
	req := httptest.NewRequest("POST", "/setup", strings.NewReader(`{"username":"x","password":"y12345"}`))
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("已有用户应 409: %d", w.Code)
	}
}
