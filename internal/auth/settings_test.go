package auth

import (
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func TestSettingsStore_AllowRegistration(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewSettingsStore(d)

	if s.AllowRegistration() {
		t.Error("默认应为关闭（无行）")
	}
	if err := s.SetAllowRegistration(true); err != nil {
		t.Fatalf("SetAllowRegistration: %v", err)
	}
	if !s.AllowRegistration() {
		t.Error("开启后应为 true")
	}
	if err := s.SetAllowRegistration(false); err != nil {
		t.Fatalf("SetAllowRegistration: %v", err)
	}
	if s.AllowRegistration() {
		t.Error("关闭后应为 false")
	}
}

func TestSettingsStore_GetSet(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewSettingsStore(d)
	if _, ok := s.Get("missing"); ok {
		t.Error("不存在的键应返回 ok=false")
	}
	if err := s.Set("k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("k", "v2"); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.Get("k"); !ok || v != "v2" {
		t.Errorf("upsert 后应为 v2: %q ok=%v", v, ok)
	}
}
