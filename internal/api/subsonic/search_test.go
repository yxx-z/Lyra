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

// TestSearch3_MatchAll 验证 Symfonium 风格的“取全部”查询：
// 空串、"*"、带引号的 ""（URL 编码 %22%22）都应返回全部内容。
func TestSearch3_MatchAll(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	for _, q := range []string{"", "*", "%22%22"} {
		w := doReq(t, h, "/rest/search3?u=admin&p=secret&query="+q+"&f=json")
		b := w.Body.String()
		if !strings.Contains(b, `以父之名`) || !strings.Contains(b, `晴天`) {
			t.Errorf("query=%q 应返回全部曲目: %s", q, b)
		}
	}
}

// TestSearch3_SongOffset 验证 songOffset 分页：seed 有 3 首（按 title 排序：七里香、以父之名、晴天）。
func TestSearch3_SongOffset(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// 第一页：songCount=2 offset=0 → 前两首
	w := doReq(t, h, "/rest/search3?u=admin&p=secret&query=&songCount=2&songOffset=0&artistCount=0&albumCount=0&f=json")
	first := w.Body.String()
	// 第二页：offset=2 → 不同的一首
	w2 := doReq(t, h, "/rest/search3?u=admin&p=secret&query=&songCount=2&songOffset=2&artistCount=0&albumCount=0&f=json")
	second := w2.Body.String()
	if first == second {
		t.Errorf("offset 分页应返回不同结果，两页相同: %s", first)
	}
	// 两页合起来应覆盖全部 3 首歌
	combined := first + second
	if !strings.Contains(combined, `晴天`) || !strings.Contains(combined, `以父之名`) || !strings.Contains(combined, `七里香`) {
		t.Errorf("两页应覆盖全部曲目: 页1=%s 页2=%s", first, second)
	}
}
