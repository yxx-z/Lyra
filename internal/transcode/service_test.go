package transcode

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// fakeFFmpeg 写一个假 ffmpeg 脚本：最后一个参数若为 pipe:1 则输出到 stdout，否则写到该文件。
// body 为输出内容；sleep 秒数用于测试取消。
func fakeFFmpeg(t *testing.T, body string, sleepSec int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\n"
	if sleepSec > 0 {
		// exec 让 sleep 取代 shell（同 PID），ctx 取消时 SIGKILL 直接命中它 → 立即结束，
		// 真正验证"客户端断开即终止"，并避免孤儿 sleep 持有管道拖慢测试。
		script += "exec sleep " + strconv.Itoa(sleepSec) + "\n"
	}
	script += `out=""
for a in "$@"; do out="$a"; done
if [ "$out" = "pipe:1" ]; then printf '` + body + `'; else printf '` + body + `' > "$out"; fi
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func newSource(t *testing.T, format, data string) Source {
	t.Helper()
	f := filepath.Join(t.TempDir(), "in."+format)
	if err := os.WriteFile(f, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return Source{ID: "t1", Path: f, Format: format}
}

func TestServe_Passthrough(t *testing.T) {
	svc := NewService(fakeFFmpeg(t, "X", 0), 192, NewCache(t.TempDir(), 0))
	src := newSource(t, "mp3", "ORIGINALMP3")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream", nil) // 无参数 → 直传
	svc.Serve(w, r, src)
	if w.Code != 200 || w.Body.String() != "ORIGINALMP3" {
		t.Errorf("直传应原样返回文件: %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type=%q", ct)
	}
}

func TestServe_PipeAndCache(t *testing.T) {
	cache := NewCache(t.TempDir(), 0)
	svc := NewService(fakeFFmpeg(t, "TRANSCODED", 0), 192, cache)
	src := newSource(t, "flac", "FLACDATA")
	// 指定 format=opus 触发转码
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/stream?format=opus", nil)
	svc.Serve(w, r, src)
	if w.Body.String() != "TRANSCODED" {
		t.Errorf("管道应输出转码字节: %q", w.Body.String())
	}
	// 缓存文件应被提升
	cp := cache.Path("t1", "opus", 192)
	if _, err := os.Stat(cp); err != nil {
		t.Errorf("缓存文件应存在: %v", err)
	}
	// 再次请求命中缓存
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/stream?format=opus", nil)
	svc.Serve(w2, r2, src)
	if w2.Body.String() != "TRANSCODED" {
		t.Errorf("命中缓存应返回缓存内容: %q", w2.Body.String())
	}
}

func TestServe_ClientCancel(t *testing.T) {
	cache := NewCache(t.TempDir(), 0)
	svc := NewService(fakeFFmpeg(t, "LATE", 3), 192, cache) // ffmpeg 睡 3s
	src := newSource(t, "flac", "FLACDATA")
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/stream?format=opus", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	svc.Serve(w, r, src) // 应在 ffmpeg 被取消后返回
	// 取消后不应留下被提升的缓存文件
	if _, err := os.Stat(cache.Path("t1", "opus", 192)); err == nil {
		t.Errorf("取消后不应提升缓存")
	}
}
