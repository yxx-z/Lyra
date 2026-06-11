package playlists

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func seed(t *testing.T, d *sql.DB) {
	t.Helper()
	for _, u := range []string{"u1", "u2"} {
		if _, err := d.Exec(`INSERT INTO users(id,username,password_hash) VALUES(?,?,?)`, u, u, "h"); err != nil {
			t.Fatal(err)
		}
	}
	for _, tr := range []string{"t1", "t2", "t3"} {
		if _, err := d.Exec(`INSERT INTO tracks(id,title,file_path,duration) VALUES(?,?,?,100)`, tr, tr, tr); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStore_CreateListGet(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seed(t, d)
	s := NewStore(d)

	id, err := s.Create("u1", "晨间")
	if err != nil || id == "" {
		t.Fatalf("Create: %v id=%q", err, id)
	}
	s.AddTracks("u1", id, []string{"t1", "t2"})

	list, err := s.List("u1")
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v len=%d", err, len(list))
	}
	if list[0].SongCount != 2 || list[0].Duration != 200 {
		t.Errorf("聚合不符: %+v", list[0])
	}
	p, err := s.Get("u1", id)
	if err != nil || p.Name != "晨间" {
		t.Errorf("Get: %v %+v", err, p)
	}
	ids, _ := s.TrackIDs("u1", id)
	if len(ids) != 2 || ids[0] != "t1" || ids[1] != "t2" {
		t.Errorf("TrackIDs 顺序: %v", ids)
	}
}

func TestStore_OwnerIsolation(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seed(t, d)
	s := NewStore(d)
	id, _ := s.Create("u1", "私人")

	if _, err := s.Get("u2", id); !errors.Is(err, ErrNotFound) {
		t.Errorf("u2 不应看到 u1 的歌单: %v", err)
	}
	if err := s.Delete("u2", id); !errors.Is(err, ErrNotFound) {
		t.Errorf("u2 不应能删 u1 的歌单: %v", err)
	}
	if list, _ := s.List("u2"); len(list) != 0 {
		t.Errorf("u2 列表应为空: %v", list)
	}
}

func TestStore_AddRemoveReplace(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seed(t, d)
	s := NewStore(d)
	id, _ := s.Create("u1", "x")

	s.AddTracks("u1", id, []string{"t1", "t2", "t3"})
	if err := s.RemoveByIndices("u1", id, []int{1}); err != nil {
		t.Fatal(err)
	}
	ids, _ := s.TrackIDs("u1", id)
	if len(ids) != 2 || ids[0] != "t1" || ids[1] != "t3" {
		t.Errorf("移除后顺序: %v", ids)
	}
	if err := s.ReplaceTracks("u1", id, []string{"t3", "t1", "t2"}); err != nil {
		t.Fatal(err)
	}
	ids, _ = s.TrackIDs("u1", id)
	if len(ids) != 3 || ids[0] != "t3" || ids[2] != "t2" {
		t.Errorf("替换后顺序: %v", ids)
	}
}

func TestStore_UpdateMetaAndDeleteCascade(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	seed(t, d)
	s := NewStore(d)
	id, _ := s.Create("u1", "旧名")
	s.AddTracks("u1", id, []string{"t1"})

	if err := s.UpdateMeta("u1", id, "新名", "备注"); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Get("u1", id)
	if p.Name != "新名" || p.Comment != "备注" {
		t.Errorf("改名/备注未生效: %+v", p)
	}
	if err := s.Delete("u1", id); err != nil {
		t.Fatal(err)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id=?`, id).Scan(&n)
	if n != 0 {
		t.Errorf("删歌单应级联清曲目，剩 %d", n)
	}
}
