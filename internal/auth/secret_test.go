package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateKey_GeneratesThenReuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	k1, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("首次: %v", err)
	}
	if len(k1) != 32 {
		t.Fatalf("密钥应 32 字节，实际 %d", len(k1))
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Errorf("权限应 0600，实际 %v", info.Mode().Perm())
	}
	k2, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("复用: %v", err)
	}
	if !bytes.Equal(k1, k2) {
		t.Error("二次加载应复用同一密钥")
	}
}
