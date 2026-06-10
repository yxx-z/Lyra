package auth

import (
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/db"
)

func seedUser(t *testing.T, s *UserStore) *User {
	t.Helper()
	u, err := s.Create("admin", "h", true)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSessionStore_CreateLookupDelete(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	u := seedUser(t, NewUserStore(d))
	ss := NewSessionStore(d)

	token, err := ss.Create(u.ID, time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("令牌应为 64 hex 字符，实际 %d", len(token))
	}
	uid, ok := ss.UserID(token)
	if !ok || uid != u.ID {
		t.Errorf("查会话失败: uid=%q ok=%v", uid, ok)
	}
	if err := ss.Delete(token); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := ss.UserID(token); ok {
		t.Error("删除后不应再命中")
	}
}

func TestSessionStore_ExpiredNotFound(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	u := seedUser(t, NewUserStore(d))
	ss := NewSessionStore(d)
	token, _ := ss.Create(u.ID, -time.Hour)
	if _, ok := ss.UserID(token); ok {
		t.Error("过期会话不应命中")
	}
}
