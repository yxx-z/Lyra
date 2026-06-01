// internal/scanner/ingester.go
package scanner

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	trackID, err := ing.upsertTrack(meta, trackArtistID, albumID)
	if err != nil {
		return err
	}
	if err := ing.importSidecarLyrics(trackID, meta.FilePath); err != nil {
		return fmt.Errorf("lyrics: %w", err)
	}
	return nil
}

// MarkUnavailable sets is_available=0 for the track at filePath.
func (ing *Ingester) MarkUnavailable(filePath string) error {
	_, err := ing.db.Exec(
		`UPDATE tracks SET is_available=0, updated_at=? WHERE file_path=?`,
		time.Now(), filePath,
	)
	return err
}

// normalize lowercases and trims leading/trailing whitespace for dedup comparisons.
// Internal spaces are preserved: "AC DC" and "ACDC" remain distinct.
func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (ing *Ingester) findOrCreateArtist(name string) (string, error) {
	var id string
	err := ing.db.QueryRow(
		`SELECT id FROM artists WHERE lower(trim(name))=?`, normalize(name),
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
		`INSERT INTO artists(id,name,created_at,updated_at) VALUES(?,?,?,?)`,
		id, name, now, now,
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

func (ing *Ingester) upsertTrack(meta TrackMeta, artistID, albumID string) (string, error) {
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
	if err != nil {
		return "", err
	}

	var id string
	if err := ing.db.QueryRow(`SELECT id FROM tracks WHERE file_path=?`, meta.FilePath).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (ing *Ingester) importSidecarLyrics(trackID, audioPath string) error {
	lrcPath := strings.TrimSuffix(audioPath, filepath.Ext(audioPath)) + ".lrc"
	content, err := os.ReadFile(lrcPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(string(content)) == "" {
		return nil
	}

	_, err = ing.db.Exec(`
		INSERT INTO lyrics(track_id,lrc_content,yrc_content,source,updated_at)
		VALUES(?,?,'','sidecar',CURRENT_TIMESTAMP)
		ON CONFLICT(track_id) DO UPDATE SET
			lrc_content=excluded.lrc_content,
			source=excluded.source,
			updated_at=CURRENT_TIMESTAMP
		WHERE lyrics.source = 'sidecar'`,
		trackID, string(content),
	)
	return err
}
