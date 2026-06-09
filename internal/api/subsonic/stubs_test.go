package subsonic

import (
	"strings"
	"testing"
)

func TestStubEndpoints_EmptyOK(t *testing.T) {
	h, _ := testHandler(t)
	cases := map[string]string{
		"/rest/getGenres?u=admin&p=secret&f=json":    `"genres"`,
		"/rest/getStarred2?u=admin&p=secret&f=json":  `"starred2"`,
		"/rest/getBookmarks?u=admin&p=secret&f=json": `"bookmarks"`,
	}
	for target, want := range cases {
		w := doReq(t, h, target)
		b := w.Body.String()
		if !strings.Contains(b, `"status":"ok"`) || !strings.Contains(b, want) {
			t.Errorf("%s 应返回含 %s 的 ok 响应: %s", target, want, b)
		}
	}
}

// TestUnknownEndpoint_SubsonicEnvelope 验证未实现端点返回可解析的 Subsonic 封套，
// 而非 chi 默认的纯文本 404（这是 Symfonium 同步中断的根因）。
func TestUnknownEndpoint_SubsonicEnvelope(t *testing.T) {
	h, _ := testHandler(t)
	w := doReq(t, h, "/rest/getArtistInfo2?u=admin&p=secret&f=json")
	b := w.Body.String()
	if !strings.Contains(b, `"subsonic-response"`) || !strings.Contains(b, `"status":"failed"`) {
		t.Errorf("未实现端点应返回 Subsonic 失败封套（而非纯文本 404）: %s", b)
	}
}
