package db

import (
	"testing"
)

func TestOpen_CreatesTablesOnFirstRun(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer db.Close()

	tables := []string{"artists", "albums", "tracks", "lyrics", "playlists", "playlist_tracks"}
	for _, table := range tables {
		var count int
		err := db.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&count)
		if err != nil {
			t.Fatalf("查询表 %s 失败: %v", table, err)
		}
		if count != 1 {
			t.Errorf("期望表 %s 存在，实际未找到", table)
		}
	}
}

func TestOpen_IdempotentMigrations(t *testing.T) {
	tmp := t.TempDir() + "/test.db"
	db1, err := Open(tmp)
	if err != nil {
		t.Fatalf("第一次 Open 失败: %v", err)
	}
	db1.Close()

	// 第二次打开同一文件，迁移应幂等，不报错
	db2, err := Open(tmp)
	if err != nil {
		t.Fatalf("第二次 Open（幂等性测试）失败: %v", err)
	}
	db2.Close()
}

func TestOpen_TracksHasIsAvailableColumn(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// 若列不存在，INSERT 会失败
	_, err = db.Exec(
		`INSERT INTO tracks(id,title,file_path,is_available) VALUES('x','t','p',1)`,
	)
	if err != nil {
		t.Errorf("is_available 列不存在: %v", err)
	}
}

func TestOpen_AlbumsHasScrapeStatusColumn(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT name FROM pragma_table_info('albums') WHERE name='scrape_status'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("albums 表应有 scrape_status 列")
	}
}

func TestOpen_LyricsHasSyncCheckedColumn(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT name FROM pragma_table_info('lyrics') WHERE name='sync_checked'`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("lyrics 表应有 sync_checked 列")
	}
}
