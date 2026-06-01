// internal/lyrics/service_test.go
package lyrics

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

type fakeProvider struct {
	name   string
	result Result
	err    error
	calls  *int
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Fetch(_ context.Context, _ Query) (Result, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.result, f.err
}

func newServiceTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('a1','艺术家')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id) VALUES('al1','专辑','a1')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,duration,is_available,scrape_status) VALUES('t1','歌名','a1','al1','/tmp/x.mp3','mp3',200,1,'pending')`); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestScrapeTrack_TrackNotFound(t *testing.T) {
	d := newServiceTestDB(t)
	svc := NewLyricsService(d)
	_, err := svc.ScrapeTrack(context.Background(), "nope")
	if !errors.Is(err, ErrTrackNotFound) {
		t.Errorf("want ErrTrackNotFound, got %v", err)
	}
}

func TestScrapeTrack_FirstProviderWins_ShortCircuits(t *testing.T) {
	d := newServiceTestDB(t)
	lrclibCalls := 0
	embedded := &fakeProvider{name: "embedded", result: Result{LRCContent: "[00:01.00]hi", Source: "embedded"}}
	lrclib := &fakeProvider{name: "lrclib", result: Result{}, err: ErrNotFound, calls: &lrclibCalls}
	svc := NewLyricsService(d, embedded, lrclib)

	out, err := svc.ScrapeTrack(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ScrapeTrack: %v", err)
	}
	if out.Status != "done" || out.Source != "embedded" {
		t.Errorf("got %+v, want done/embedded", out)
	}
	if lrclibCalls != 0 {
		t.Errorf("lrclib 不应被调用，实际 %d 次", lrclibCalls)
	}
	var lrc string
	d.QueryRow(`SELECT lrc_content FROM lyrics WHERE track_id='t1'`).Scan(&lrc)
	if lrc != "[00:01.00]hi" {
		t.Errorf("lrc_content = %q", lrc)
	}
	var st string
	d.QueryRow(`SELECT scrape_status FROM tracks WHERE id='t1'`).Scan(&st)
	if st != "done" {
		t.Errorf("scrape_status = %q, want done", st)
	}
}

func TestScrapeTrack_FallsThroughToSecond(t *testing.T) {
	d := newServiceTestDB(t)
	embedded := &fakeProvider{name: "embedded", err: ErrNotFound}
	lrclib := &fakeProvider{name: "lrclib", result: Result{LRCContent: "[00:02.00]yo", Source: "lrclib"}}
	svc := NewLyricsService(d, embedded, lrclib)

	out, err := svc.ScrapeTrack(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ScrapeTrack: %v", err)
	}
	if out.Status != "done" || out.Source != "lrclib" {
		t.Errorf("got %+v, want done/lrclib", out)
	}
}

func TestScrapeTrack_AllNotFound_Failed(t *testing.T) {
	d := newServiceTestDB(t)
	embedded := &fakeProvider{name: "embedded", err: ErrNotFound}
	lrclib := &fakeProvider{name: "lrclib", err: ErrNotFound}
	svc := NewLyricsService(d, embedded, lrclib)

	out, err := svc.ScrapeTrack(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ScrapeTrack should not error: %v", err)
	}
	if out.Status != "failed" {
		t.Errorf("Status = %q, want failed", out.Status)
	}
	var st string
	d.QueryRow(`SELECT scrape_status FROM tracks WHERE id='t1'`).Scan(&st)
	if st != "failed" {
		t.Errorf("scrape_status = %q, want failed", st)
	}
}

func TestScrapeTrack_AlreadyHasLyrics_Skipped(t *testing.T) {
	d := newServiceTestDB(t)
	d.Exec(`INSERT INTO lyrics(track_id,lrc_content,source) VALUES('t1','[00:00.00]exist','manual')`)
	called := 0
	embedded := &fakeProvider{name: "embedded", err: ErrNotFound, calls: &called}
	svc := NewLyricsService(d, embedded)

	out, err := svc.ScrapeTrack(context.Background(), "t1")
	if err != nil {
		t.Fatalf("ScrapeTrack: %v", err)
	}
	if out.Status != "skipped" {
		t.Errorf("Status = %q, want skipped", out.Status)
	}
	if called != 0 {
		t.Errorf("已有歌词不应调 provider，实际 %d 次", called)
	}
}

func TestScrapeTrack_ProviderError_Propagates(t *testing.T) {
	d := newServiceTestDB(t)
	boom := errors.New("network boom")
	lrclib := &fakeProvider{name: "lrclib", err: boom}
	svc := NewLyricsService(d, lrclib)

	_, err := svc.ScrapeTrack(context.Background(), "t1")
	if err == nil {
		t.Error("provider 非 ErrNotFound 错误应透传")
	}
}
