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

// UserSummary 是用户列表中暴露给前端的精简视图（不含密码哈希/密文）。
type UserSummary struct {
	ID                  string `json:"id"`
	Username            string `json:"username"`
	IsAdmin             bool   `json:"isAdmin"`
	HasSubsonicPassword bool   `json:"hasSubsonicPassword"`
	CreatedAt           string `json:"createdAt"`
}

func (s *UserStore) List() ([]UserSummary, error) {
	rows, err := s.db.Query(`SELECT id, username, is_admin, (subsonic_pw IS NOT NULL AND length(subsonic_pw) > 0), created_at FROM users ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserSummary
	for rows.Next() {
		var u UserSummary
		var admin, hasPW int
		if err := rows.Scan(&u.ID, &u.Username, &admin, &hasPW, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.IsAdmin = admin == 1
		u.HasSubsonicPassword = hasPW == 1
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *UserStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *UserStore) UpdateRole(id string, isAdmin bool) error {
	admin := 0
	if isAdmin {
		admin = 1
	}
	_, err := s.db.Exec(`UPDATE users SET is_admin=?, updated_at=datetime('now') WHERE id=?`, admin, id)
	return err
}

func (s *UserStore) AdminCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin=1`).Scan(&n)
	return n, err
}
