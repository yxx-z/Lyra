package transcode

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachePathAndKey(t *testing.T) {
	c := NewCache("/tmp/x", 0)
	if p := c.Path("t1", "opus", 128); p != filepath.Join("/tmp/x", "t1_opus_128k.opus") {
		t.Errorf("Path=%q", p)
	}
	if k := c.key("t1", "opus", 128); k != "t1_opus_128k" {
		t.Errorf("key=%q", k)
	}
	// trackID 不能越出缓存目录
	if p := c.Path("../../etc/passwd", "mp3", 192); filepath.Dir(p) != "/tmp/x" {
		t.Errorf("越界路径未净化: %q", p)
	}
}

func TestCacheEvict_LRU(t *testing.T) {
	dir := t.TempDir()
	// 三个 1MB 文件，mtime 由旧到新：old < mid < new
	write := func(name string, ageMin int) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, 1024*1024), 0o644); err != nil {
			t.Fatal(err)
		}
		tm := time.Now().Add(time.Duration(-ageMin) * time.Minute)
		if err := os.Chtimes(p, tm, tm); err != nil {
			t.Fatal(err)
		}
		return p
	}
	oldF := write("old.mp3", 30)
	midF := write("mid.mp3", 20)
	newF := write("new.mp3", 10)

	// 上限 2MB → 需删到 ≤2MB，最旧的 old 先删；keep=newF 永不删
	c := NewCache(dir, 2)
	c.evict(newF)

	if _, err := os.Stat(oldF); !os.IsNotExist(err) {
		t.Errorf("最旧文件应被删除")
	}
	if _, err := os.Stat(midF); err != nil {
		t.Errorf("mid 不应被删（删 old 后已达标）")
	}
	if _, err := os.Stat(newF); err != nil {
		t.Errorf("keep 文件不应被删")
	}
}

func TestCacheEvict_Unlimited(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.mp3")
	os.WriteFile(p, make([]byte, 1024), 0o644)
	NewCache(dir, 0).evict("") // 0 = 不限，不删任何东西
	if _, err := os.Stat(p); err != nil {
		t.Errorf("不限容量时不应删除")
	}
}

func TestCacheEvict_SkipsTmp(t *testing.T) {
	dir := t.TempDir()
	// 一个 2MB 的 .tmp（正在转码）+ 一个 2MB 正式文件，上限 1MB
	for _, name := range []string{"x.mp3.tmp", "y.mp3"} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, 2*1024*1024), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCache(dir, 1)
	c.evict("")
	// .tmp 不应被删除（即便超限）
	if _, err := os.Stat(filepath.Join(dir, "x.mp3.tmp")); err != nil {
		t.Errorf(".tmp 文件不应被 evict 删除")
	}
}
