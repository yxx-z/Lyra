// internal/config/config_test.go
package config

import (
	"os"
	"testing"
)

func TestDefault_Defaults(t *testing.T) {
	cfg := Default()
	if cfg.Server.Port != 4533 {
		t.Errorf("期望端口 4533，实际 %d", cfg.Server.Port)
	}
	if cfg.Transcode.DefaultFormat != "mp3" {
		t.Errorf("期望 mp3，实际 %s", cfg.Transcode.DefaultFormat)
	}
	if cfg.Transcode.DefaultBitrate != 192 {
		t.Errorf("期望码率 192，实际 %d", cfg.Transcode.DefaultBitrate)
	}
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	cfg, err := Load("does-not-exist.yaml")
	if err != nil {
		t.Fatalf("不应报错，实际: %v", err)
	}
	if cfg.Server.Port != 4533 {
		t.Errorf("期望默认端口 4533，实际 %d", cfg.Server.Port)
	}
}

func TestLoad_OverridesPort(t *testing.T) {
	f, err := os.CreateTemp("", "lyra-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString("server:\n  port: 9090\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := Load(f.Name())
	if err != nil {
		t.Fatalf("不应报错，实际: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("期望端口 9090，实际 %d", cfg.Server.Port)
	}
}

func TestDefault_AuthUsernameIsAdmin(t *testing.T) {
	cfg := Default()
	if cfg.Auth.Username != "admin" {
		t.Errorf("want username=admin, got %q", cfg.Auth.Username)
	}
}
