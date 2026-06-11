package auth

import "database/sql"

type SettingsStore struct{ db *sql.DB }

func NewSettingsStore(db *sql.DB) *SettingsStore { return &SettingsStore{db: db} }

func (s *SettingsStore) Get(key string) (string, bool) {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key=?`, key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func (s *SettingsStore) Set(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

const keyAllowRegistration = "allow_registration"

func (s *SettingsStore) AllowRegistration() bool {
	v, _ := s.Get(keyAllowRegistration)
	return v == "1"
}

func (s *SettingsStore) SetAllowRegistration(allow bool) error {
	v := "0"
	if allow {
		v = "1"
	}
	return s.Set(keyAllowRegistration, v)
}
