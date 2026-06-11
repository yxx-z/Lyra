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

	tables := []string{"artists", "albums", "tracks", "lyrics", "playlists", "playlist_tracks", "bookmarks", "play_queue"}
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

func TestOpen_HasUsersAndSessions(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	for _, table := range []string{"users", "sessions"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Errorf("表 %s 应存在 (n=%d err=%v)", table, n, err)
		}
	}
}

func TestOpen_BookmarksAndQueueHaveUserID(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO tracks(id,title,file_path) VALUES('t1','x','p1')`); err != nil {
		t.Fatalf("seed track: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO bookmarks(user_id,track_id,position) VALUES(NULL,'t1',1000)`); err != nil {
		t.Errorf("bookmarks 应有可空 user_id 列: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO play_queue(user_id,track_ids) VALUES(NULL,'t1')`); err != nil {
		t.Errorf("play_queue 应有可空 user_id 列: %v", err)
	}
}

func TestOpen_HasAppSettings(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='app_settings'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("app_settings 表应存在 (n=%d err=%v)", n, err)
	}
	if _, err := db.Exec(`INSERT INTO app_settings(key,value) VALUES('allow_registration','1')`); err != nil {
		t.Errorf("写 app_settings 失败: %v", err)
	}
}

func TestOpen_HasFavoritesAndPlayStats(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash) VALUES('u1','u1','h')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO tracks(id,title,file_path) VALUES('t1','x','p1')`); err != nil {
		t.Fatalf("seed track: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO starred(user_id,item_type,item_id) VALUES('u1','song','t1')`); err != nil {
		t.Errorf("starred 表应可写: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO play_stats(user_id,track_id,play_count) VALUES('u1','t1',1)`); err != nil {
		t.Errorf("play_stats 表应可写: %v", err)
	}
}

func TestOpen_PlaylistsHaveUserIDAndCascade(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	db.Exec(`INSERT INTO users(id,username,password_hash) VALUES('u1','u1','h')`)
	db.Exec(`INSERT INTO tracks(id,title,file_path) VALUES('t1','x','p1')`)
	if _, err := db.Exec(`INSERT INTO playlists(id,user_id,name) VALUES('p1','u1','我的歌单')`); err != nil {
		t.Fatalf("playlists 应有 user_id 列: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO playlist_tracks(playlist_id,track_id,position) VALUES('p1','t1',0)`); err != nil {
		t.Fatalf("playlist_tracks 写入: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM playlists WHERE id='p1'`); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id='p1'`).Scan(&n)
	if n != 0 {
		t.Errorf("删歌单应级联清曲目，剩 %d", n)
	}
}
