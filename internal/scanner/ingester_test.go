// internal/scanner/ingester_test.go
package scanner

import (
	"database/sql"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestIngest_CreatesArtistAlbumTrack(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)

	meta := TrackMeta{
		FilePath: "/music/蔡琴/金片子/01.flac",
		FileSize: 10000,
		Format:   "flac",
		Title:    "渡口",
		Artist:   "蔡琴",
		Album:    "金片子",
		Year:     1984,
	}
	if err := ing.Ingest(meta); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	var trackCount, artistCount, albumCount int
	d.QueryRow(`SELECT count(*) FROM tracks`).Scan(&trackCount)
	d.QueryRow(`SELECT count(*) FROM artists`).Scan(&artistCount)
	d.QueryRow(`SELECT count(*) FROM albums`).Scan(&albumCount)

	if trackCount != 1 {
		t.Errorf("tracks: want 1, got %d", trackCount)
	}
	if artistCount != 1 {
		t.Errorf("artists: want 1, got %d", artistCount)
	}
	if albumCount != 1 {
		t.Errorf("albums: want 1, got %d", albumCount)
	}
}

func TestIngest_Dedup_SameArtistCaseInsensitive(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)

	m1 := TrackMeta{FilePath: "/music/a.flac", Title: "A", Artist: "蔡琴", Album: "X"}
	m2 := TrackMeta{FilePath: "/music/b.flac", Title: "B", Artist: "蔡 琴 ", Album: "X"} // 空格变体
	ing.Ingest(m1)
	ing.Ingest(m2)

	var count int
	d.QueryRow(`SELECT count(*) FROM artists`).Scan(&count)
	if count != 1 {
		t.Errorf("artists: want 1 (deduped), got %d", count)
	}
}

func TestIngest_Upsert_SameFilePath(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)

	m := TrackMeta{FilePath: "/music/a.flac", Title: "旧标题", Artist: "A", Album: "B"}
	ing.Ingest(m)

	m.Title = "新标题"
	if err := ing.Ingest(m); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	var count int
	d.QueryRow(`SELECT count(*) FROM tracks`).Scan(&count)
	if count != 1 {
		t.Errorf("tracks: want 1 (upserted), got %d", count)
	}

	var title string
	d.QueryRow(`SELECT title FROM tracks WHERE file_path=?`, m.FilePath).Scan(&title)
	if title != "新标题" {
		t.Errorf("title: want 新标题, got %q", title)
	}
}

func TestMarkUnavailable(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)

	m := TrackMeta{FilePath: "/music/a.flac", Title: "T", Artist: "A", Album: "B"}
	ing.Ingest(m)

	if err := ing.MarkUnavailable("/music/a.flac"); err != nil {
		t.Fatalf("MarkUnavailable: %v", err)
	}

	var avail int
	d.QueryRow(`SELECT is_available FROM tracks WHERE file_path=?`, m.FilePath).Scan(&avail)
	if avail != 0 {
		t.Errorf("is_available: want 0, got %d", avail)
	}
}
