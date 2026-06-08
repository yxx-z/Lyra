// internal/scanner/scanner_test.go
package scanner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/lyrics"
	"github.com/yxx-z/lyra/internal/metadata"
)

func newTestScanner(t *testing.T, paths []string) *Scanner {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return NewScanner(d, config.LibraryConfig{Paths: paths}, "", nil, nil, false)
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
	s := NewScanner(d, config.LibraryConfig{Paths: []string{dir}}, "", svc, nil, true)
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

func TestScanStatus_HasAlbumsScraped(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewScanner(d, config.LibraryConfig{}, "", nil, nil, false)
	st := s.Status()
	if st.AlbumsScraped != 0 {
		t.Errorf("初始 AlbumsScraped 应为 0，得到 %d", st.AlbumsScraped)
	}
}

func TestScrapeAlbumsPending_CountsDoneAlbums(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	// 插入一个艺术家、一张 pending 专辑和一条可用曲目（供 EnrichAlbum 的 track-count 子查询）
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('ar1','测试艺术家')`); err != nil {
		t.Fatalf("insert artist: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id,scrape_status) VALUES('al1','测试专辑','ar1','pending')`); err != nil {
		t.Fatalf("insert album: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,album_id,artist_id,file_path,is_available) VALUES('tr1','曲1','al1','ar1','/m/a.flac',1)`); err != nil {
		t.Fatalf("insert track: %v", err)
	}

	// MB 服务端：返回 score>=90、track-count=1 的 release
	mb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"releases":[{"id":"mbid-x","score":100,"date":"2003","track-count":1}]}`))
	}))
	t.Cleanup(mb.Close)

	// CAA 服务端：返回 404（无封面），EnrichAlbum 仍会置 status="done"
	caa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(caa.Close)

	metaSvc := metadata.NewMetadataService(
		d,
		metadata.NewMusicBrainzClient(mb.URL, "T/0.1", nil),
		metadata.NewCoverArtClient(caa.URL, nil),
		t.TempDir(),
	)

	// 直接构造 scanner，不需要文件系统路径
	s := NewScanner(d, config.LibraryConfig{}, "", nil, metaSvc, true)

	// 直接调用元数据阶段（同包可访问未导出方法），跳过文件扫描
	s.scrapeAlbumsPending(context.Background())

	// 断言计数
	if got := s.Status().AlbumsScraped; got != 1 {
		t.Errorf("AlbumsScraped = %d, want 1", got)
	}

	// 断言 DB 中专辑状态已更新为 done
	var status string
	d.QueryRow(`SELECT scrape_status FROM albums WHERE id='al1'`).Scan(&status)
	if status != "done" {
		t.Errorf("scrape_status = %q, want done", status)
	}
}
