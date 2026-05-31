// internal/db/db.go
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations
var migrationsFS embed.FS

func Open(path string) (*sql.DB, error) {
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("创建数据库目录: %w", err)
			}
		}
	}
	dsn := path
	if path != ":memory:" {
		dsn = path + "?_pragma=foreign_keys%3DON"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite: %w", err)
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&journalMode); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 WAL 模式: %w", err)
	}
	// :memory: 不支持 WAL，忽略；文件数据库若无法设为 WAL 则记录警告
	if path != ":memory:" && journalMode != "wal" {
		db.Close()
		return nil, fmt.Errorf("无法启用 WAL 模式，当前模式: %s（网络文件系统不支持 WAL）", journalMode)
	}
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("执行迁移: %w", err)
	}
	return db, nil
}

func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at DATETIME DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		var applied int
		if err := db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version=?`, e.Name()).Scan(&applied); err != nil {
			return fmt.Errorf("查询迁移状态 %s: %w", e.Name(), err)
		}
		if applied > 0 {
			continue
		}
		content, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("开启事务 %s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("应用迁移 %s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, e.Name()); err != nil {
			tx.Rollback()
			return fmt.Errorf("记录迁移版本 %s: %w", e.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交迁移 %s: %w", e.Name(), err)
		}
	}
	return nil
}
