package userdata

import (
	"database/sql"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

// seedFav 预置用户 u1/u2 与曲目 t1/t2，满足 starred/play_stats 的外键。
func seedFav(t *testing.T, d *sql.DB) {
	t.Helper()
	for _, u := range []string{"u1", "u2"} {
		if _, err := d.Exec(`INSERT INTO users(id,username,password_hash) VALUES(?,?,?)`, u, u, "h"); err != nil {
			t.Fatalf("seed user %s: %v", u, err)
		}
	}
	for _, tr := range []string{"t1", "t2"} {
		if _, err := d.Exec(`INSERT INTO tracks(id,title,file_path) VALUES(?,?,?)`, tr, tr, tr); err != nil {
			t.Fatalf("seed track %s: %v", tr, err)
		}
	}
}

func TestStore_StarUnstarIsStarred(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seedFav(t, d)
	s := NewStore(d)

	if err := s.Star("u1", TypeSong, "t1"); err != nil {
		t.Fatalf("Star: %v", err)
	}
	if err := s.Star("u1", TypeSong, "t1"); err != nil {
		t.Fatalf("重复 Star 应幂等: %v", err)
	}
	ok, _ := s.IsStarred("u1", TypeSong, "t1")
	if !ok {
		t.Error("应已收藏")
	}
	ids, _ := s.StarredIDs("u1", TypeSong)
	if len(ids) != 1 || ids[0] != "t1" {
		t.Errorf("StarredIDs 不符: %v", ids)
	}
	if err := s.Unstar("u1", TypeSong, "t1"); err != nil {
		t.Fatal(err)
	}
	ok, _ = s.IsStarred("u1", TypeSong, "t1")
	if ok {
		t.Error("取消后不应收藏")
	}
}

func TestStore_StarredMapAndIsolation(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seedFav(t, d)
	s := NewStore(d)
	// 专辑收藏 item_id 无外键，可用任意字符串
	s.Star("u1", TypeAlbum, "a1")
	s.Star("u1", TypeAlbum, "a2")
	s.Star("u2", TypeAlbum, "a3")

	m, _ := s.StarredMap("u1", TypeAlbum)
	if len(m) != 2 || m["a1"] == "" || m["a2"] == "" {
		t.Errorf("u1 StarredMap 不符: %v", m)
	}
	if _, ok := m["a3"]; ok {
		t.Error("不应含 u2 的收藏（per-user 隔离）")
	}
}

func TestStore_RecordPlayAndOrder(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seedFav(t, d)
	s := NewStore(d)
	s.RecordPlay("u1", "t1")
	for i := 0; i < 3; i++ {
		s.RecordPlay("u1", "t2")
	}
	freq, _ := s.FrequentTrackIDs("u1", 10)
	if len(freq) != 2 || freq[0] != "t2" {
		t.Errorf("FrequentTrackIDs 应 t2 在前: %v", freq)
	}
	recent, _ := s.RecentTrackIDs("u1", 10)
	if len(recent) != 2 {
		t.Errorf("RecentTrackIDs 应有 2 条: %v", recent)
	}
	s.RecordPlay("u1", "t1")
	recent, _ = s.RecentTrackIDs("u1", 10)
	if recent[0] != "t1" {
		t.Errorf("RecentTrackIDs 应 t1 在前: %v", recent)
	}
}

func TestStore_RecordPlayUpsert(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seedFav(t, d)
	s := NewStore(d)
	s.RecordPlay("u1", "t1")
	s.RecordPlay("u1", "t1")
	freq, _ := s.FrequentTrackIDs("u1", 10)
	if len(freq) != 1 {
		t.Fatalf("应只有 1 行: %v", freq)
	}
	var cnt int
	d.QueryRow(`SELECT play_count FROM play_stats WHERE user_id='u1' AND track_id='t1'`).Scan(&cnt)
	if cnt != 2 {
		t.Errorf("play_count 应为 2，实际 %d", cnt)
	}
}

func TestStore_DeleteUserCascades(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seedFav(t, d)
	s := NewStore(d)
	s.Star("u1", TypeSong, "t1")
	s.RecordPlay("u1", "t1")
	if _, err := d.Exec(`DELETE FROM users WHERE id='u1'`); err != nil {
		t.Fatal(err)
	}
	var sc, pc int
	d.QueryRow(`SELECT COUNT(*) FROM starred WHERE user_id='u1'`).Scan(&sc)
	d.QueryRow(`SELECT COUNT(*) FROM play_stats WHERE user_id='u1'`).Scan(&pc)
	if sc != 0 || pc != 0 {
		t.Errorf("删用户应级联清理收藏/统计: starred=%d play_stats=%d", sc, pc)
	}
}
