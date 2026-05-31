// internal/scanner/walker_test.go
package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestWalk_FindsAudioFiles(t *testing.T) {
	dir := t.TempDir()
	// 创建混合文件
	files := map[string]bool{
		"song.mp3":         true,
		"album/track.flac": true,
		"image.jpg":        false,
		"readme.txt":       false,
		"sub/deep/a.ogg":   true,
	}
	for name := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte{}, 0644)
	}

	ctx := context.Background()
	ch := Walk(ctx, []string{dir})

	found := map[string]bool{}
	for p := range ch {
		rel, _ := filepath.Rel(dir, p)
		found[filepath.ToSlash(rel)] = true
	}

	for name, wantFound := range files {
		if found[name] != wantFound {
			t.Errorf("Walk(%q): found=%v, want=%v", name, found[name], wantFound)
		}
	}
}

func TestWalk_MultipleRoots(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir1, "a.mp3"), []byte{}, 0644)
	os.WriteFile(filepath.Join(dir2, "b.flac"), []byte{}, 0644)

	ctx := context.Background()
	var paths []string
	for p := range Walk(ctx, []string{dir1, dir2}) {
		paths = append(paths, filepath.Base(p))
	}
	sort.Strings(paths)
	if len(paths) != 2 || paths[0] != "a.mp3" || paths[1] != "b.flac" {
		t.Errorf("got %v", paths)
	}
}

func TestWalk_ContextCancel(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub", "sub2", "sub3")
	os.MkdirAll(subDir, 0755)
	for i := 0; i < 100; i++ {
		os.WriteFile(filepath.Join(subDir, fmt.Sprintf("%d.mp3", i)), []byte{}, 0644)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch := Walk(ctx, []string{dir})
	// 读取一个后取消
	<-ch
	cancel()
	// 排空 channel，确保不 hang
	for range ch {
	}
}
