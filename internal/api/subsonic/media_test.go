package subsonic

import (
	"strings"
	"testing"
)

func TestScrobble(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/scrobble?u=admin&p=secret&id=t1&f=json")
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("scrobble: %s", w.Body.String())
	}
	var pc int
	h.db.QueryRow(`SELECT play_count FROM tracks WHERE id='t1'`).Scan(&pc)
	if pc != 1 {
		t.Errorf("play_count 应为 1，得到 %d", pc)
	}
}

func TestStream_NotFound(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/stream?u=admin&p=secret&id=nope&f=json")
	// 不存在曲目 → v1 StreamByID 写 404（http.NotFound），主体非 subsonic 封套；只验状态码
	if w.Code != 404 {
		t.Errorf("不存在曲目 stream 应 404，得到 %d", w.Code)
	}
}

func TestGetCoverArt_NotFound(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// al1 无内嵌/本地/cover_path 封面 → 404
	w := doReq(t, h, "/rest/getCoverArt?u=admin&p=secret&id=al1&f=json")
	if w.Code != 404 {
		t.Errorf("无封面应 404，得到 %d", w.Code)
	}
}
