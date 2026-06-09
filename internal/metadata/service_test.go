package metadata

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func setupAlbum(t *testing.T, trackCount int) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir + "/t.db")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	_, _ = database.Exec(`INSERT INTO artists(id,name) VALUES('ar1','周杰伦')`)
	_, _ = database.Exec(`INSERT INTO albums(id,title,artist_id,scrape_status) VALUES('al1','叶惠美','ar1','pending')`)
	for i := 0; i < trackCount; i++ {
		_, _ = database.Exec(`INSERT INTO tracks(id,title,album_id,artist_id,file_path,is_available) VALUES(?,?,?,?,?,1)`,
			"tr"+string(rune('a'+i)), "曲", "al1", "ar1", "/m/"+string(rune('a'+i))+".flac")
	}
	return database, "al1"
}

func mbAndCaaServers(t *testing.T, mbBody string, caaStatus int) (mbURL, caaURL string) {
	t.Helper()
	mb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(mbBody))
	}))
	t.Cleanup(mb.Close)
	caa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if caaStatus == http.StatusOK {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("\xff\xd8\xffJPEG"))
			return
		}
		w.WriteHeader(caaStatus)
	}))
	t.Cleanup(caa.Close)
	return mb.URL, caa.URL
}

func newSvc(t *testing.T, database *sql.DB, mbURL, caaURL, artDir string) *MetadataService {
	mb := NewMusicBrainzClient(mbURL, "Lyra-Test/0.1", nil)
	mb.baseURL = mbURL
	cover := NewCoverArtClient(caaURL, nil)
	return NewMetadataService(database, mb, cover, artDir)
}

func TestEnrichAlbum_HitWithCover(t *testing.T) {
	database, id := setupAlbum(t, 11)
	mbURL, caaURL := mbAndCaaServers(t,
		`{"releases":[{"id":"mbid-11","score":100,"title":"叶惠美","date":"2003-07-31","track-count":11}]}`,
		http.StatusOK)
	artDir := t.TempDir()

	out, err := newSvc(t, database, mbURL, caaURL, artDir).EnrichAlbum(context.Background(), id)
	if err != nil {
		t.Fatalf("EnrichAlbum err: %v", err)
	}
	if out.Status != "done" || !out.HasCover || out.MBID != "mbid-11" {
		t.Fatalf("outcome = %+v", out)
	}

	var mbid, date, cover, status string
	database.QueryRow(`SELECT COALESCE(mbid,''),COALESCE(release_date,''),COALESCE(cover_path,''),scrape_status FROM albums WHERE id=?`, id).
		Scan(&mbid, &date, &cover, &status)
	if mbid != "mbid-11" || date != "2003-07-31" || cover == "" || status != "done" {
		t.Errorf("db 落库错误: mbid=%q date=%q cover=%q status=%q", mbid, date, cover, status)
	}
	if _, statErr := os.Stat(cover); statErr != nil {
		t.Errorf("封面文件应存在: %v", statErr)
	}
}

func TestEnrichAlbum_NoMatch(t *testing.T) {
	database, id := setupAlbum(t, 11)
	mbURL, caaURL := mbAndCaaServers(t, `{"releases":[{"id":"x","score":30,"track-count":5}]}`, http.StatusNotFound)
	out, err := newSvc(t, database, mbURL, caaURL, t.TempDir()).EnrichAlbum(context.Background(), id)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "failed" {
		t.Errorf("无匹配应 failed，得到 %q", out.Status)
	}
	var status string
	database.QueryRow(`SELECT scrape_status FROM albums WHERE id=?`, id).Scan(&status)
	if status != "failed" {
		t.Errorf("db status 应 failed，得到 %q", status)
	}
}

func TestEnrichAlbum_HitNoCover(t *testing.T) {
	database, id := setupAlbum(t, 11)
	mbURL, caaURL := mbAndCaaServers(t,
		`{"releases":[{"id":"mbid-11","score":100,"date":"2003","track-count":11}]}`,
		http.StatusNotFound)
	out, err := newSvc(t, database, mbURL, caaURL, t.TempDir()).EnrichAlbum(context.Background(), id)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "done" || out.HasCover {
		t.Errorf("无封面应 done 且 HasCover=false，得到 %+v", out)
	}
	var cover, status string
	database.QueryRow(`SELECT COALESCE(cover_path,''),scrape_status FROM albums WHERE id=?`, id).Scan(&cover, &status)
	if cover != "" || status != "done" {
		t.Errorf("cover 应空 status 应 done，得到 cover=%q status=%q", cover, status)
	}
}

func TestEnrichAlbum_AlbumNotFound(t *testing.T) {
	database, _ := setupAlbum(t, 1)
	mbURL, caaURL := mbAndCaaServers(t, `{"releases":[]}`, http.StatusNotFound)
	_, err := newSvc(t, database, mbURL, caaURL, t.TempDir()).EnrichAlbum(context.Background(), "nonexistent")
	if !errors.Is(err, ErrAlbumNotFound) {
		t.Errorf("不存在的专辑应 ErrAlbumNotFound，得到 %v", err)
	}
}

func TestEnrichAlbum_FingerprintPath(t *testing.T) {
	database, id := setupAlbum(t, 2) // 专辑 al1 + 曲目 tra/trb
	if _, err := database.Exec(`UPDATE tracks SET mbid='rec-a' WHERE id='tra'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE tracks SET mbid='rec-b' WHERE id='trb'`); err != nil {
		t.Fatal(err)
	}

	mb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/ws/2/recording/"):
			// 两首都覆盖 rel-album；其中也含 rel-comp（仅个别）
			w.Write([]byte(`{"releases":[{"id":"rel-album"},{"id":"rel-comp"}]}`))
		case strings.Contains(r.URL.Path, "/ws/2/release/"):
			w.Write([]byte(`{"id":"rel-album","date":"2003-07-31"}`))
		default:
			w.Write([]byte(`{"releases":[]}`)) // 文本兜底（不应走到）
		}
	}))
	defer mb.Close()
	caa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // 无封面，简化
	}))
	defer caa.Close()
	svc := newSvc(t, database, mb.URL, caa.URL, t.TempDir())
	svc.mb.minInterval = 0 // 消除节流延迟，加速测试

	out, err := svc.EnrichAlbum(context.Background(), id)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Status != "done" || out.MBID != "rel-album" {
		t.Fatalf("应走指纹路径选 rel-album，得到 %+v", out)
	}
	var mbid, date string
	database.QueryRow(`SELECT COALESCE(mbid,''),COALESCE(release_date,'') FROM albums WHERE id=?`, id).Scan(&mbid, &date)
	if mbid != "rel-album" || date != "2003-07-31" {
		t.Errorf("落库 mbid=%q date=%q", mbid, date)
	}
}

func TestEnrichAlbum_MBError(t *testing.T) {
	database, id := setupAlbum(t, 11)
	mb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(mb.Close)
	caa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(caa.Close)

	_, err := newSvc(t, database, mb.URL, caa.URL, t.TempDir()).EnrichAlbum(context.Background(), id)
	if err == nil {
		t.Fatal("MB 500 应返回非 nil error")
	}
	if errors.Is(err, ErrAlbumNotFound) {
		t.Error("不应是 ErrAlbumNotFound")
	}
	// 状态不应被置为 done
	var status string
	database.QueryRow(`SELECT scrape_status FROM albums WHERE id=?`, id).Scan(&status)
	if status == "done" {
		t.Error("MB 异常时状态不应为 done")
	}
}
