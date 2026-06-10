package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"io"
	"time"
)

type SessionStore struct{ db *sql.DB }

func NewSessionStore(db *sql.DB) *SessionStore { return &SessionStore{db: db} }

// sqlite datetime('now') 为 UTC，过期时间统一存 UTC 字符串以便比较。
const sqliteTime = "2006-01-02 15:04:05"

func (s *SessionStore) Create(userID string, ttl time.Duration) (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	exp := time.Now().Add(ttl).UTC().Format(sqliteTime)
	if _, err := s.db.Exec(`INSERT INTO sessions(token, user_id, expires_at) VALUES(?,?,?)`, token, userID, exp); err != nil {
		return "", err
	}
	return token, nil
}

func (s *SessionStore) UserID(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	var uid string
	err := s.db.QueryRow(`SELECT user_id FROM sessions WHERE token=? AND expires_at > datetime('now')`, token).Scan(&uid)
	if err != nil {
		return "", false
	}
	return uid, true
}

func (s *SessionStore) Refresh(token string, ttl time.Duration) error {
	exp := time.Now().Add(ttl).UTC().Format(sqliteTime)
	_, err := s.db.Exec(`UPDATE sessions SET expires_at=? WHERE token=?`, exp, token)
	return err
}

func (s *SessionStore) Delete(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token=?`, token)
	return err
}
