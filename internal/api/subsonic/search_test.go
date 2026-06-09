package subsonic

import (
	"strings"
	"testing"
)

func TestSearch3(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/search3?u=admin&p=secret&query=晴天&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `晴天`) || !strings.Contains(b, `"searchResult3"`) {
		t.Errorf("search3 应命中曲目: %s", b)
	}
}

func TestSearch3_ArtistAlbum(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	w := doReq(t, h, "/rest/search3?u=admin&p=secret&query=叶惠美&f=json")
	if !strings.Contains(w.Body.String(), `"id":"al1"`) {
		t.Errorf("search3 应命中专辑: %s", w.Body.String())
	}
}
