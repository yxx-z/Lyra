// internal/scanner/ingester.go
package scanner

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Ingester writes TrackMeta records to the database.
type Ingester struct {
	db *sql.DB
}

// NewIngester creates an Ingester backed by db.
func NewIngester(db *sql.DB) *Ingester {
	return &Ingester{db: db}
}

// Ingest upserts artist, album, and track for the given metadata.
func (ing *Ingester) Ingest(meta TrackMeta) error {
	trackArtistID, err := ing.findOrCreateArtist(meta.Artist)
	if err != nil {
		return fmt.Errorf("artist: %w", err)
	}

	albumArtistName := meta.AlbumArtist
	if albumArtistName == "" {
		albumArtistName = meta.Artist
	}
	albumArtistID, err := ing.findOrCreateArtist(albumArtistName)
	if err != nil {
		return fmt.Errorf("album artist: %w", err)
	}

	albumID, err := ing.findOrCreateAlbum(meta.Album, albumArtistID, meta.Year)
	if err != nil {
		return fmt.Errorf("album: %w", err)
	}

	return ing.upsertTrack(meta, trackArtistID, albumID)
}

// MarkUnavailable sets is_available=0 for the track at filePath.
func (ing *Ingester) MarkUnavailable(filePath string) error {
	_, err := ing.db.Exec(
		`UPDATE tracks SET is_available=0, updated_at=? WHERE file_path=?`,
		time.Now(), filePath,
	)
	return err
}

// normalize lowercases and removes all whitespace for dedup comparisons.
func normalize(s string) string {
	s = strings.ToLower(s)
	// Remove all whitespace so "蔡 琴 " and "蔡琴" are treated as identical.
	return strings.Join(strings.Fields(s), "")
}

func (ing *Ingester) findOrCreateArtist(name string) (string, error) {
	key := normalize(name)
	var id string
	// sort_name stores the normalized (lowercased, whitespace-collapsed) form
	// so we can do exact-match dedup regardless of SQLite collation.
	err := ing.db.QueryRow(
		`SELECT id FROM artists WHERE sort_name=?`, key,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = uuid.New().String()
	now := time.Now()
	_, err = ing.db.Exec(
		`INSERT INTO artists(id,name,sort_name,created_at,updated_at) VALUES(?,?,?,?,?)`,
		id, name, key, now, now,
	)
	return id, err
}

func (ing *Ingester) findOrCreateAlbum(title, artistID string, year int) (string, error) {
	var id string
	err := ing.db.QueryRow(
		`SELECT id FROM albums WHERE lower(trim(title))=? AND artist_id=?`,
		normalize(title), artistID,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id = uuid.New().String()
	releaseDate := ""
	if year > 0 {
		releaseDate = fmt.Sprintf("%d", year)
	}
	now := time.Now()
	_, err = ing.db.Exec(
		`INSERT INTO albums(id,title,artist_id,release_date,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		id, title, artistID, releaseDate, now, now,
	)
	return id, err
}

func (ing *Ingester) upsertTrack(meta TrackMeta, artistID, albumID string) error {
	now := time.Now()
	_, err := ing.db.Exec(`
		INSERT INTO tracks(
			id,title,artist_id,album_id,track_number,disc_number,
			duration,file_path,file_size,format,bitrate,sample_rate,
			channels,scrape_status,is_available,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'pending',1,?,?)
		ON CONFLICT(file_path) DO UPDATE SET
			title=excluded.title,
			artist_id=excluded.artist_id,
			album_id=excluded.album_id,
			track_number=excluded.track_number,
			disc_number=excluded.disc_number,
			duration=excluded.duration,
			file_size=excluded.file_size,
			format=excluded.format,
			bitrate=excluded.bitrate,
			sample_rate=excluded.sample_rate,
			channels=excluded.channels,
			is_available=1,
			updated_at=excluded.updated_at`,
		uuid.New().String(),
		meta.Title, artistID, albumID,
		meta.TrackNumber, meta.DiscNumber,
		meta.Duration, meta.FilePath, meta.FileSize, meta.Format,
		meta.Bitrate, meta.SampleRate, meta.Channels,
		now, now,
	)
	return err
}
