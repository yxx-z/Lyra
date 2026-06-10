package auth

import (
	"database/sql"

	"github.com/google/uuid"
)

// User 是一个登录用户。SubsonicPW 为 AES-GCM 加密后的 Subsonic 密码原文；未设为 nil。
type User struct {
	ID           string
	Username     string
	PasswordHash string
	SubsonicPW   []byte
	IsAdmin      bool
}

type UserStore struct{ db *sql.DB }

func NewUserStore(db *sql.DB) *UserStore { return &UserStore{db: db} }

func (s *UserStore) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *UserStore) Create(username, passwordHash string, isAdmin bool) (*User, error) {
	id := uuid.NewString()
	admin := 0
	if isAdmin {
		admin = 1
	}
	if _, err := s.db.Exec(
		`INSERT INTO users(id, username, password_hash, is_admin) VALUES(?,?,?,?)`,
		id, username, passwordHash, admin,
	); err != nil {
		return nil, err
	}
	return &User{ID: id, Username: username, PasswordHash: passwordHash, IsAdmin: isAdmin}, nil
}

const userCols = `id, username, password_hash, subsonic_pw, is_admin`

func scanUser(row *sql.Row) (*User, error) {
	var u User
	var admin int
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.SubsonicPW, &admin); err != nil {
		return nil, err
	}
	u.IsAdmin = admin == 1
	return &u, nil
}

func (s *UserStore) ByUsername(name string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username=?`, name))
}

func (s *UserStore) ByID(id string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func (s *UserStore) FirstAdmin() (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT ` + userCols + ` FROM users WHERE is_admin=1 ORDER BY created_at, id LIMIT 1`))
}

func (s *UserStore) UpdatePassword(id, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash=?, updated_at=datetime('now') WHERE id=?`, hash, id)
	return err
}

func (s *UserStore) UpdateSubsonicPW(id string, enc []byte) error {
	_, err := s.db.Exec(`UPDATE users SET subsonic_pw=?, updated_at=datetime('now') WHERE id=?`, enc, id)
	return err
}
