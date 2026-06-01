// internal/scanner/ingester_test.go
package scanner

import (
	"database/sql"
	"os"
	"path/filepath"
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
	m2 := TrackMeta{FilePath: "/music/b.flac", Title: "B", Artist: "蔡琴 ", Album: "X"} // 尾部空格变体
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

func TestIngest_ImportsSidecarLyrics(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "a.flac")
	lrcContent := "[00:01.00]渡口\n[00:03.00]让我与你握别"
	if err := os.WriteFile(filepath.Join(dir, "a.lrc"), []byte(lrcContent), 0o644); err != nil {
		t.Fatalf("write lrc: %v", err)
	}

	meta := TrackMeta{FilePath: audioPath, Title: "渡口", Artist: "蔡琴", Album: "金片子"}
	if err := ing.Ingest(meta); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	var savedContent, source string
	if err := d.QueryRow(`
		SELECT l.lrc_content, l.source
		FROM lyrics l
		JOIN tracks t ON t.id = l.track_id
		WHERE t.file_path=?`, audioPath).Scan(&savedContent, &source); err != nil {
		t.Fatalf("query imported lyrics: %v", err)
	}
	if savedContent != lrcContent {
		t.Errorf("lrc_content: want %q, got %q", lrcContent, savedContent)
	}
	if source != "sidecar" {
		t.Errorf("source: want sidecar, got %q", source)
	}
}

func TestIngest_DoesNotDeleteLyricsWhenSidecarMissing(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "a.flac")
	lrcPath := filepath.Join(dir, "a.lrc")
	if err := os.WriteFile(lrcPath, []byte("[00:01.00]旧歌词"), 0o644); err != nil {
		t.Fatalf("write lrc: %v", err)
	}

	meta := TrackMeta{FilePath: audioPath, Title: "渡口", Artist: "蔡琴", Album: "金片子"}
	if err := ing.Ingest(meta); err != nil {
		t.Fatalf("first Ingest: %v", err)
	}
	if err := os.Remove(lrcPath); err != nil {
		t.Fatalf("remove lrc: %v", err)
	}
	if err := ing.Ingest(meta); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	var savedContent string
	if err := d.QueryRow(`
		SELECT l.lrc_content
		FROM lyrics l
		JOIN tracks t ON t.id = l.track_id
		WHERE t.file_path=?`, audioPath).Scan(&savedContent); err != nil {
		t.Fatalf("query lyrics: %v", err)
	}
	if savedContent != "[00:01.00]旧歌词" {
		t.Errorf("lrc_content: want old lyrics preserved, got %q", savedContent)
	}
}

func TestIngest_DoesNotOverwriteManualLyricsWithSidecar(t *testing.T) {
	d := newTestDB(t)
	ing := NewIngester(d)
	dir := t.TempDir()
	audioPath := filepath.Join(dir, "a.flac")
	if err := os.WriteFile(filepath.Join(dir, "a.lrc"), []byte("[00:01.00]旁挂歌词"), 0o644); err != nil {
		t.Fatalf("write lrc: %v", err)
	}

	meta := TrackMeta{FilePath: audioPath, Title: "渡口", Artist: "蔡琴", Album: "金片子"}
	if err := ing.Ingest(meta); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if _, err := d.Exec(`
		UPDATE lyrics
		SET lrc_content='[00:01.00]手工歌词', source='manual'
		WHERE track_id=(SELECT id FROM tracks WHERE file_path=?)`, audioPath); err != nil {
		t.Fatalf("set manual lyrics: %v", err)
	}
	if err := ing.Ingest(meta); err != nil {
		t.Fatalf("second Ingest: %v", err)
	}

	var savedContent, source string
	if err := d.QueryRow(`
		SELECT l.lrc_content, l.source
		FROM lyrics l
		JOIN tracks t ON t.id = l.track_id
		WHERE t.file_path=?`, audioPath).Scan(&savedContent, &source); err != nil {
		t.Fatalf("query lyrics: %v", err)
	}
	if savedContent != "[00:01.00]手工歌词" {
		t.Errorf("lrc_content: want manual lyrics preserved, got %q", savedContent)
	}
	if source != "manual" {
		t.Errorf("source: want manual, got %q", source)
	}
}
