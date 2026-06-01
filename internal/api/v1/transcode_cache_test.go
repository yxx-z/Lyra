// internal/api/v1/transcode_cache_test.go
package v1

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscodeCache_Path(t *testing.T) {
	c := NewTranscodeCache("/cache")
	p := c.Path("abc123", "mp3", 192)
	want := filepath.Join("/cache", "abc123_mp3_192k.mp3")
	if p != want {
		t.Errorf("Path = %q, want %q", p, want)
	}
}

func TestTranscodeCache_PathSanitizesID(t *testing.T) {
	c := NewTranscodeCache("/cache")
	// trackID 理论上是 UUID，不含路径分隔符；防御性测试确保不逃逸目录
	p := c.Path("../etc/passwd", "mp3", 192)
	if strings.Contains(p, "..") {
		t.Errorf("Path must not contain '..': %q", p)
	}
}

func TestTranscodeCache_LockForSameKeySameMutex(t *testing.T) {
	c := NewTranscodeCache("/cache")
	l1 := c.lockFor("k1")
	l2 := c.lockFor("k1")
	if l1 != l2 {
		t.Error("lockFor same key should return same mutex")
	}
	l3 := c.lockFor("k2")
	if l1 == l3 {
		t.Error("lockFor different keys should return different mutexes")
	}
}
