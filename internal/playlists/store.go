// Package playlists 提供 per-user 私人歌单仓储，供 Subsonic 与 Web 两端复用。
package playlists

import (
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// ErrNotFound 表示歌单不存在或不属于该用户（私有，越权一律视为不存在）。
var ErrNotFound = errors.New("歌单不存在")

type Playlist struct {
	ID        string
	Name      string
	Comment   string
	Created   string
	Changed   string
	SongCount int
	Duration  int
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) ensureOwner(userID, id string) error {
	var owner sql.NullString
	err := s.db.QueryRow(`SELECT user_id FROM playlists WHERE id=?`, id).Scan(&owner)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !owner.Valid || owner.String != userID {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Create(userID, name string) (string, error) {
	id := uuid.NewString()
	_, err := s.db.Exec(`INSERT INTO playlists(id, user_id, name) VALUES(?,?,?)`, id, userID, name)
	if err != nil {
		return "", err
	}
	return id, nil
}

const playlistCols = `p.id, p.name, p.comment, p.created_at, p.updated_at,
	(SELECT COUNT(*) FROM playlist_tracks pt WHERE pt.playlist_id=p.id),
	(SELECT COALESCE(SUM(t.duration),0) FROM playlist_tracks pt JOIN tracks t ON t.id=pt.track_id WHERE pt.playlist_id=p.id)`

func scanPlaylist(scan func(...any) error) (Playlist, error) {
	var p Playlist
	err := scan(&p.ID, &p.Name, &p.Comment, &p.Created, &p.Changed, &p.SongCount, &p.Duration)
	return p, err
}

func (s *Store) List(userID string) ([]Playlist, error) {
	rows, err := s.db.Query(`SELECT `+playlistCols+` FROM playlists p WHERE p.user_id=? ORDER BY p.updated_at DESC, p.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		p, err := scanPlaylist(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Get(userID, id string) (Playlist, error) {
	p, err := scanPlaylist(s.db.QueryRow(`SELECT `+playlistCols+` FROM playlists p WHERE p.id=? AND p.user_id=?`, id, userID).Scan)
	if err == sql.ErrNoRows {
		return Playlist{}, ErrNotFound
	}
	return p, err
}

func (s *Store) TrackIDs(userID, id string) ([]string, error) {
	if err := s.ensureOwner(userID, id); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT track_id FROM playlist_tracks WHERE playlist_id=? ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil, err
		}
		ids = append(ids, tid)
	}
	return ids, rows.Err()
}

func (s *Store) UpdateMeta(userID, id, name, comment string) error {
	if err := s.ensureOwner(userID, id); err != nil {
		return err
	}
	// 空字符串视为「不修改该字段」（清空备注属罕见，按不改处理）。
	_, err := s.db.Exec(`UPDATE playlists SET
		name=CASE WHEN ?='' THEN name ELSE ? END,
		comment=CASE WHEN ?='' THEN comment ELSE ? END,
		updated_at=datetime('now') WHERE id=?`, name, name, comment, comment, id)
	return err
}

func (s *Store) Delete(userID, id string) error {
	if err := s.ensureOwner(userID, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM playlists WHERE id=?`, id) // playlist_tracks 经 FK 级联
	return err
}

func (s *Store) AddTracks(userID, id string, trackIDs []string) error {
	if err := s.ensureOwner(userID, id); err != nil {
		return err
	}
	if len(trackIDs) == 0 {
		return nil
	}
	var maxPos sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(position) FROM playlist_tracks WHERE playlist_id=?`, id).Scan(&maxPos); err != nil {
		return err
	}
	start := 0
	if maxPos.Valid {
		start = int(maxPos.Int64) + 1
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for i, tid := range trackIDs {
		if _, err := tx.Exec(`INSERT INTO playlist_tracks(playlist_id, track_id, position) VALUES(?,?,?)`, id, tid, start+i); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE playlists SET updated_at=datetime('now') WHERE id=?`, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) ReplaceTracks(userID, id string, trackIDs []string) error {
	if err := s.ensureOwner(userID, id); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM playlist_tracks WHERE playlist_id=?`, id); err != nil {
		tx.Rollback()
		return err
	}
	for i, tid := range trackIDs {
		if _, err := tx.Exec(`INSERT INTO playlist_tracks(playlist_id, track_id, position) VALUES(?,?,?)`, id, tid, i); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE playlists SET updated_at=datetime('now') WHERE id=?`, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) RemoveByIndices(userID, id string, indices []int) error {
	ids, err := s.TrackIDs(userID, id) // 含属主校验
	if err != nil {
		return err
	}
	skip := make(map[int]bool, len(indices))
	for _, idx := range indices {
		skip[idx] = true
	}
	kept := make([]string, 0, len(ids))
	for i, tid := range ids {
		if !skip[i] {
			kept = append(kept, tid)
		}
	}
	return s.ReplaceTracks(userID, id, kept)
}
