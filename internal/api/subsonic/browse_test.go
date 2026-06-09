package subsonic

import (
	"database/sql"
	"strings"
	"testing"
)

// seed 插入 1 艺术家 + 1 专辑 + 2 曲目。
func seed(t *testing.T, d *sql.DB) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('ar1','周杰伦')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id,release_date,genre) VALUES('al1','叶惠美','ar1','2003-07-31','Mandopop')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,track_number,disc_number,duration,file_path,format,bitrate,is_available) VALUES('t1','以父之名','ar1','al1',1,1,342,'/m/1.m4a','m4a',320,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,track_number,disc_number,duration,file_path,format,bitrate,is_available) VALUES('t2','晴天','ar1','al1',3,1,269,'/m/3.m4a','m4a',320,1)`); err != nil {
		t.Fatal(err)
	}
}

func TestGetArtists(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getArtists?u=admin&p=secret&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"id":"ar1"`) || !strings.Contains(b, `周杰伦`) || !strings.Contains(b, `"albumCount":1`) {
		t.Errorf("getArtists: %s", b)
	}
}

func TestGetArtist(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getArtist?u=admin&p=secret&id=ar1&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"id":"al1"`) || !strings.Contains(b, `叶惠美`) {
		t.Errorf("getArtist 应含其专辑: %s", b)
	}
}

func TestGetAlbum(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getAlbum?u=admin&p=secret&id=al1&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"songCount":2`) || !strings.Contains(b, `以父之名`) || !strings.Contains(b, `晴天`) {
		t.Errorf("getAlbum 应含 2 曲: %s", b)
	}
}

func TestGetSong(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getSong?u=admin&p=secret&id=t1&f=json")
	if !strings.Contains(w.Body.String(), `以父之名`) {
		t.Errorf("getSong: %s", w.Body.String())
	}
	w2 := doReq(t, h, "/rest/getSong?u=admin&p=secret&id=nope&f=json")
	if !strings.Contains(w2.Body.String(), `"code":70`) {
		t.Errorf("不存在曲目应 70: %s", w2.Body.String())
	}
}

func TestGetAlbumList2(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/getAlbumList2?u=admin&p=secret&type=newest&f=json")
	if !strings.Contains(w.Body.String(), `"id":"al1"`) {
		t.Errorf("getAlbumList2: %s", w.Body.String())
	}
	w2 := doReq(t, h, "/rest/getAlbumList2?u=admin&p=secret&type=bogus&f=json")
	if !strings.Contains(w2.Body.String(), `"code":10`) {
		t.Errorf("未知 type 应 10: %s", w2.Body.String())
	}
}
