package auth

import (
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func TestUserStore_CreateAndLookup(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewUserStore(d)

	if n, _ := s.Count(); n != 0 {
		t.Fatalf("初始应 0 个用户，实际 %d", n)
	}
	u, err := s.Create("admin", "hash", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == "" || !u.IsAdmin {
		t.Fatalf("用户字段不对: %+v", u)
	}
	got, err := s.ByUsername("admin")
	if err != nil {
		t.Fatalf("ByUsername: %v", err)
	}
	if got.ID != u.ID || got.PasswordHash != "hash" || !got.IsAdmin {
		t.Errorf("查得不一致: %+v", got)
	}
	if _, err := s.Create("admin", "h2", false); err == nil {
		t.Error("用户名唯一约束应阻止重复")
	}
}

func TestUserStore_UpdatePasswordAndSubsonicPW(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewUserStore(d)
	u, _ := s.Create("bob", "old", false)

	if err := s.UpdatePassword(u.ID, "new"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	if err := s.UpdateSubsonicPW(u.ID, []byte{1, 2, 3}); err != nil {
		t.Fatalf("UpdateSubsonicPW: %v", err)
	}
	got, _ := s.ByID(u.ID)
	if got.PasswordHash != "new" || len(got.SubsonicPW) != 3 {
		t.Errorf("更新未生效: %+v", got)
	}
}

func TestUserStore_FirstAdmin(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewUserStore(d)
	s.Create("u1", "h", false)
	admin, _ := s.Create("u2", "h", true)
	got, err := s.FirstAdmin()
	if err != nil || got.ID != admin.ID {
		t.Errorf("FirstAdmin 应返回 u2: got=%+v err=%v", got, err)
	}
}

func TestUserStore_ListDeleteRoleAdminCount(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewUserStore(d)
	a, _ := s.Create("admin", "h", true)
	b, _ := s.Create("bob", "h", false)
	if err := s.UpdateSubsonicPW(b.ID, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应有 2 个用户，实际 %d", len(list))
	}
	var bobSum *UserSummary
	for i := range list {
		if list[i].Username == "bob" {
			bobSum = &list[i]
		}
	}
	if bobSum == nil || bobSum.IsAdmin || !bobSum.HasSubsonicPassword {
		t.Errorf("bob 摘要不符: %+v", bobSum)
	}

	if n, _ := s.AdminCount(); n != 1 {
		t.Errorf("AdminCount 应为 1，实际 %d", n)
	}
	if err := s.UpdateRole(b.ID, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.AdminCount(); n != 2 {
		t.Errorf("升级后 AdminCount 应为 2，实际 %d", n)
	}
	if err := s.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ByID(b.ID); err == nil {
		t.Error("删除后 ByID 应失败")
	}
	_ = a
}

func TestUserStore_DeleteCascadesSessions(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	us := NewUserStore(d)
	ss := NewSessionStore(d)
	u, _ := us.Create("bob", "h", false)
	token, _ := ss.Create(u.ID, 3600*1e9) // 1h in ns
	if _, ok := ss.UserID(token); !ok {
		t.Fatal("会话应存在")
	}
	if err := us.Delete(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := ss.UserID(token); ok {
		t.Error("删用户后其会话应被级联清理")
	}
}
