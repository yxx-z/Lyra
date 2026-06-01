// internal/lyrics/service.go
package lyrics

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrTrackNotFound is returned when the track id does not exist / is unavailable.
var ErrTrackNotFound = errors.New("曲目不存在")

// ScrapeOutcome reports the result of scraping a single track.
type ScrapeOutcome struct {
	Status string // "done" | "skipped" | "failed"
	Source string
}

// LyricsService orchestrates lyrics lookup across providers and persistence.
type LyricsService struct {
	db        *sql.DB
	providers []Provider
}

// NewLyricsService creates a service with providers tried in the given order.
func NewLyricsService(db *sql.DB, providers ...Provider) *LyricsService {
	return &LyricsService{db: db, providers: providers}
}

type trackInfo struct {
	Title    string
	Artist   string
	Album    string
	Duration int
	FilePath string
}

// ScrapeTrack runs the full lyrics scrape pipeline for one track.
func (s *LyricsService) ScrapeTrack(ctx context.Context, trackID string) (ScrapeOutcome, error) {
	track, err := s.loadTrack(trackID)
	if err != nil {
		return ScrapeOutcome{}, err
	}

	has, err := s.hasLyrics(trackID)
	if err != nil {
		return ScrapeOutcome{}, err
	}
	if has {
		_ = s.updateStatus(trackID, "done")
		return ScrapeOutcome{Status: "skipped"}, nil
	}

	q := Query{
		TrackName:  track.Title,
		ArtistName: track.Artist,
		AlbumName:  track.Album,
		Duration:   track.Duration,
		FilePath:   track.FilePath,
	}

	for _, p := range s.providers {
		res, ferr := p.Fetch(ctx, q)
		if ferr == nil {
			if err := s.saveLyrics(trackID, res); err != nil {
				return ScrapeOutcome{}, err
			}
			if err := s.updateStatus(trackID, "done"); err != nil {
				return ScrapeOutcome{}, err
			}
			return ScrapeOutcome{Status: "done", Source: res.Source}, nil
		}
		if errors.Is(ferr, ErrNotFound) || errors.Is(ferr, ErrInvalidQuery) {
			continue
		}
		return ScrapeOutcome{}, ferr
	}

	_ = s.updateStatus(trackID, "failed")
	return ScrapeOutcome{Status: "failed"}, nil
}

func (s *LyricsService) loadTrack(trackID string) (trackInfo, error) {
	var t trackInfo
	err := s.db.QueryRow(`
		SELECT tr.title, COALESCE(ar.name,''), COALESCE(al.title,''),
		       COALESCE(tr.duration,0), tr.file_path
		FROM tracks tr
		LEFT JOIN artists ar ON ar.id = tr.artist_id
		LEFT JOIN albums al ON al.id = tr.album_id
		WHERE tr.id=? AND tr.is_available=1`, trackID).
		Scan(&t.Title, &t.Artist, &t.Album, &t.Duration, &t.FilePath)
	if errors.Is(err, sql.ErrNoRows) {
		return trackInfo{}, ErrTrackNotFound
	}
	return t, err
}

func (s *LyricsService) hasLyrics(trackID string) (bool, error) {
	var one int
	err := s.db.QueryRow(`
		SELECT 1 FROM lyrics
		WHERE track_id=?
		  AND (trim(COALESCE(lrc_content,'')) <> '' OR trim(COALESCE(yrc_content,'')) <> '')`,
		trackID).Scan(&one)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func (s *LyricsService) saveLyrics(trackID string, res Result) error {
	source := strings.TrimSpace(res.Source)
	if source == "" {
		source = "unknown"
	}
	_, err := s.db.Exec(`
		INSERT INTO lyrics(track_id,lrc_content,yrc_content,source,updated_at)
		VALUES(?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(track_id) DO UPDATE SET
			lrc_content=excluded.lrc_content,
			yrc_content=excluded.yrc_content,
			source=excluded.source,
			updated_at=CURRENT_TIMESTAMP`,
		trackID, res.LRCContent, res.YRCContent, source)
	return err
}

func (s *LyricsService) updateStatus(trackID, status string) error {
	_, err := s.db.Exec(`UPDATE tracks SET scrape_status=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, trackID)
	return err
}
