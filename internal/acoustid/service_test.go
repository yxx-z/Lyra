package acoustid

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

// fakeFP 是测试用指纹器。
type fakeFP struct {
	dur int
	fp  string
	err error
}

func (f fakeFP) Calc(ctx context.Context, path string) (int, string, error) {
	return f.dur, f.fp, f.err
}

// openTrackDB 建内存库并插入一首曲目，返回 *sql.DB。
func openTrackDB(t *testing.T, trackID, filePath string) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.Exec(`INSERT INTO tracks(id,title,file_path,is_available) VALUES(?,?,?,1)`, trackID, "曲", filePath); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestIdentifyTrack_Hit(t *testing.T) {
	database := openTrackDB(t, "tr1", "/m/a.flac")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","results":[{"id":"aid-1","score":0.97,"recordings":[{"id":"mbid-1"}]}]}`))
	}))
	defer srv.Close()
	svc := NewFingerprintService(database, fakeFP{dur: 269, fp: "FP"}, NewAcoustIDClient(srv.URL, "k", srv.Client()))

	out, err := svc.IdentifyTrack(context.Background(), "tr1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "identified" || out.AcoustID != "aid-1" || out.MBID != "mbid-1" {
		t.Fatalf("out=%+v", out)
	}
	var aid, mbid string
	database.QueryRow(`SELECT COALESCE(acoustid,''),COALESCE(mbid,'') FROM tracks WHERE id='tr1'`).Scan(&aid, &mbid)
	if aid != "aid-1" || mbid != "mbid-1" {
		t.Errorf("落库 acoustid=%q mbid=%q", aid, mbid)
	}
}

func TestIdentifyTrack_NoMatch(t *testing.T) {
	database := openTrackDB(t, "tr1", "/m/a.flac")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()
	svc := NewFingerprintService(database, fakeFP{dur: 269, fp: "FP"}, NewAcoustIDClient(srv.URL, "k", srv.Client()))

	out, err := svc.IdentifyTrack(context.Background(), "tr1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "nomatch" {
		t.Errorf("应 nomatch，得到 %q", out.Status)
	}
	var aid sql.NullString
	database.QueryRow(`SELECT acoustid FROM tracks WHERE id='tr1'`).Scan(&aid)
	if !aid.Valid || aid.String != "" {
		t.Errorf("nomatch 应置 acoustid=''（已尝试），得到 valid=%v %q", aid.Valid, aid.String)
	}
}

func TestIdentifyTrack_FpcalcError(t *testing.T) {
	database := openTrackDB(t, "tr1", "/m/a.flac")
	svc := NewFingerprintService(database, fakeFP{err: errors.New("fpcalc boom")}, NewAcoustIDClient("http://unused", "k", nil))
	_, err := svc.IdentifyTrack(context.Background(), "tr1")
	if err == nil {
		t.Fatal("fpcalc 错误应返回 error")
	}
	var aid sql.NullString
	database.QueryRow(`SELECT acoustid FROM tracks WHERE id='tr1'`).Scan(&aid)
	if aid.Valid {
		t.Errorf("瞬时错误应保持 acoustid NULL，得到 %q", aid.String)
	}
}

func TestIdentifyTrack_NotFound(t *testing.T) {
	database := openTrackDB(t, "tr1", "/m/a.flac")
	svc := NewFingerprintService(database, fakeFP{dur: 1, fp: "x"}, NewAcoustIDClient("http://unused", "k", nil))
	if _, err := svc.IdentifyTrack(context.Background(), "missing"); !errors.Is(err, ErrTrackNotFound) {
		t.Errorf("不存在曲目应 ErrTrackNotFound，得到 %v", err)
	}
}
