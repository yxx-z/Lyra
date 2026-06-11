// Package userdata 提供 per-user 的收藏与播放统计仓储，供 Subsonic 与 Web 两端复用。
package userdata

import "database/sql"

// 收藏对象类型。
const (
	TypeSong   = "song"
	TypeAlbum  = "album"
	TypeArtist = "artist"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Star(userID, itemType, itemID string) error {
	_, err := s.db.Exec(
		`INSERT INTO starred(user_id, item_type, item_id) VALUES(?,?,?) ON CONFLICT DO NOTHING`,
		userID, itemType, itemID,
	)
	return err
}

func (s *Store) Unstar(userID, itemType, itemID string) error {
	_, err := s.db.Exec(
		`DELETE FROM starred WHERE user_id=? AND item_type=? AND item_id=?`,
		userID, itemType, itemID,
	)
	return err
}

func (s *Store) IsStarred(userID, itemType, itemID string) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM starred WHERE user_id=? AND item_type=? AND item_id=?`,
		userID, itemType, itemID,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// StarredIDs 按收藏时间倒序返回该类型已收藏的 id。
func (s *Store) StarredIDs(userID, itemType string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT item_id FROM starred WHERE user_id=? AND item_type=? ORDER BY created_at DESC, item_id`,
		userID, itemType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// StarredMap 返回 id→收藏时间（字符串）的映射，用于批量标注列表；存在即表示已收藏。
func (s *Store) StarredMap(userID, itemType string) (map[string]string, error) {
	rows, err := s.db.Query(
		`SELECT item_id, created_at FROM starred WHERE user_id=? AND item_type=?`,
		userID, itemType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var id, ts string
		if err := rows.Scan(&id, &ts); err != nil {
			return nil, err
		}
		m[id] = ts
	}
	return m, rows.Err()
}

// RecordPlay 记一次播放（upsert：次数 +1，更新最近播放时间）。
func (s *Store) RecordPlay(userID, trackID string) error {
	_, err := s.db.Exec(`
		INSERT INTO play_stats(user_id, track_id, play_count, last_played_at)
		VALUES(?, ?, 1, datetime('now'))
		ON CONFLICT(user_id, track_id) DO UPDATE SET
			play_count = play_count + 1,
			last_played_at = datetime('now')`,
		userID, trackID,
	)
	return err
}

func (s *Store) trackIDsBy(userID, kind string, limit int) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT track_id FROM play_stats WHERE user_id=? AND `+orderClause(kind)+` LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RecentTrackIDs 按最近播放倒序（仅含有播放时间的行）。
func (s *Store) RecentTrackIDs(userID string, limit int) ([]string, error) {
	return s.trackIDsBy(userID, "recent", limit)
}

// FrequentTrackIDs 按播放次数倒序。
func (s *Store) FrequentTrackIDs(userID string, limit int) ([]string, error) {
	return s.trackIDsBy(userID, "frequent", limit)
}

// orderClause 把内部排序标识映射为安全的 WHERE/ORDER 片段（不接受外部原文，杜绝注入）。
func orderClause(kind string) string {
	switch kind {
	case "frequent":
		return `1=1 ORDER BY play_count DESC, last_played_at DESC`
	default: // recent
		return `last_played_at IS NOT NULL ORDER BY last_played_at DESC`
	}
}
