package lyrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPickMatch(t *testing.T) {
	songs := []neteaseSong{
		{ID: 1, Name: "晴天 (Live)", DurationMS: 200000},
		{ID: 2, Name: "晴天", DurationMS: 269000},
		{ID: 3, Name: "其它歌", DurationMS: 269000},
	}

	got, ok := pickMatch(songs, "晴天", 269)
	if !ok {
		t.Fatal("应匹配到 id=2")
	}
	if got.ID != 2 {
		t.Errorf("匹配 id = %d, want 2", got.ID)
	}
}

func TestPickMatch_DurationTooFar(t *testing.T) {
	songs := []neteaseSong{{ID: 1, Name: "晴天", DurationMS: 200000}}
	if _, ok := pickMatch(songs, "晴天", 269); ok {
		t.Error("时长差 >3s 不应匹配")
	}
}

func TestPickMatch_TitleNotContained(t *testing.T) {
	songs := []neteaseSong{{ID: 1, Name: "完全不同", DurationMS: 269000}}
	if _, ok := pickMatch(songs, "晴天", 269); ok {
		t.Error("标题不互相包含不应匹配")
	}
}

func TestNormalizeText(t *testing.T) {
	if got := normalizeText("　Ｈｅｌｌｏ   World "); got != "hello world" {
		t.Errorf("normalizeText = %q, want %q", got, "hello world")
	}
}

// newTestProvider 返回指向 httptest server 的 NeteaseProvider。
func newTestProvider(srv *httptest.Server) *NeteaseProvider {
	p := NewNeteaseProvider(srv.Client())
	p.baseURL = srv.URL
	return p
}

func TestNeteaseFetch_Hit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/eapi/cloudsearch/pc"):
			w.Write([]byte(`{"result":{"songs":[{"id":2,"name":"晴天","dt":269000,"ar":[{"name":"周杰伦"}]}]}}`))
		case strings.Contains(r.URL.Path, "/eapi/song/lyric/v1"):
			w.Write([]byte(`{"lrc":{"lyric":"[00:01.00]普通歌词"},"yrc":{"lyric":"[1000,1000](1000,1000,0)字"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newTestProvider(srv)
	res, err := p.Fetch(context.Background(), Query{TrackName: "晴天", ArtistName: "周杰伦", Duration: 269})
	if err != nil {
		t.Fatalf("Fetch err: %v", err)
	}
	if res.Source != "netease" {
		t.Errorf("Source = %q, want netease", res.Source)
	}
	if !strings.Contains(res.LRCContent, "普通歌词") {
		t.Errorf("LRCContent 缺失: %q", res.LRCContent)
	}
	if !strings.Contains(res.YRCContent, `"words"`) {
		t.Errorf("YRCContent 应为归一化 JSON: %q", res.YRCContent)
	}
}

func TestNeteaseFetch_NoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":{"songs":[{"id":9,"name":"别的歌","dt":100000,"ar":[{"name":"X"}]}]}}`))
	}))
	defer srv.Close()
	p := newTestProvider(srv)
	_, err := p.Fetch(context.Background(), Query{TrackName: "晴天", ArtistName: "周杰伦", Duration: 269})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("无匹配应返回 ErrNotFound，得到 %v", err)
	}
}

func TestNeteaseFetch_EmptyLyric(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/eapi/cloudsearch/pc"):
			w.Write([]byte(`{"result":{"songs":[{"id":2,"name":"晴天","dt":269000,"ar":[{"name":"周杰伦"}]}]}}`))
		default:
			w.Write([]byte(`{"lrc":{"lyric":""},"yrc":{"lyric":""}}`))
		}
	}))
	defer srv.Close()
	p := newTestProvider(srv)
	_, err := p.Fetch(context.Background(), Query{TrackName: "晴天", ArtistName: "周杰伦", Duration: 269})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("空歌词应返回 ErrNotFound，得到 %v", err)
	}
}

func TestNeteaseFetch_InvalidQuery(t *testing.T) {
	p := NewNeteaseProvider(nil)
	_, err := p.Fetch(context.Background(), Query{TrackName: "", ArtistName: "x", Duration: 100})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("缺曲名应返回 ErrInvalidQuery，得到 %v", err)
	}
}

func TestNeteaseFetch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := newTestProvider(srv)
	_, err := p.Fetch(context.Background(), Query{TrackName: "晴天", ArtistName: "周杰伦", Duration: 269})
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("服务端 500 应返回普通 error（非 ErrNotFound），得到 %v", err)
	}
}
