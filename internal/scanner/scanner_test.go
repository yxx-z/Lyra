// internal/scanner/scanner_test.go
package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/lyrics"
)

func newTestScanner(t *testing.T, paths []string) *Scanner {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return NewScanner(d, config.LibraryConfig{Paths: paths}, "", nil, false)
}

func TestNewScanner_NotRunning(t *testing.T) {
	s := newTestScanner(t, nil)
	defer s.Stop()
	if s.Status().Running {
		t.Error("新建的 Scanner 不应处于运行状态")
	}
}

func TestTriggerScan_ReturnsBusyError(t *testing.T) {
	s := newTestScanner(t, nil)
	defer s.Stop()

	// 直接将 running 置为 true，避免依赖时序
	s.running.Store(true)
	defer s.running.Store(false)

	if err := s.TriggerScan(); !errors.Is(err, ErrScanInProgress) {
		t.Errorf("want ErrScanInProgress, got %v", err)
	}
}

func TestTriggerScan_SetsTotal(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.mp3", i)), []byte{}, 0644)
	}
	// 非音频文件不计入
	os.WriteFile(filepath.Join(dir, "cover.jpg"), []byte{}, 0644)

	s := newTestScanner(t, []string{dir})
	defer s.Stop()
	s.TriggerScan()

	// 等待扫描完成（最多 3 秒）
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !s.Status().Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := s.Status()
	if st.Total != 5 {
		t.Errorf("Total = %d, want 5", st.Total)
	}
	// 空文件读取标签会失败，计入 Errors；但 Total = Processed + Errors 必须成立
	if st.Total != st.Processed+st.Errors {
		t.Errorf("Total(%d) != Processed(%d)+Errors(%d)", st.Total, st.Processed, st.Errors)
	}
}

func TestStop_DoesNotHang(t *testing.T) {
	s := newTestScanner(t, nil)
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Stop() 超时")
	}
}

func TestDoScan_ScrapePhase_MarksDone(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(f, []byte("notreal"), 0644); err != nil {
		t.Fatal(err)
	}

	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	svc := lyrics.NewLyricsService(d, scanStubProvider{res: lyrics.Result{LRCContent: "[00:01.00]hi", Source: "lrclib"}})
	s := NewScanner(d, config.LibraryConfig{Paths: []string{dir}}, "", svc, true)
	defer s.Stop()

	s.TriggerScan()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !s.Status().Running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	st := s.Status()
	if st.Running {
		t.Fatal("扫描应已结束")
	}
	if st.LyricsScraped < 1 {
		t.Errorf("LyricsScraped = %d, want >=1", st.LyricsScraped)
	}
	if st.Phase != "idle" {
		t.Errorf("Phase = %q, want idle", st.Phase)
	}
}

type scanStubProvider struct {
	res lyrics.Result
	err error
}

func (p scanStubProvider) Name() string { return "stub" }
func (p scanStubProvider) Fetch(_ context.Context, _ lyrics.Query) (lyrics.Result, error) {
	return p.res, p.err
}
