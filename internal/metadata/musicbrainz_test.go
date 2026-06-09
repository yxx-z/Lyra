package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMBSearch_QueryUnquotedAndEscaped(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		w.Write([]byte(`{"releases":[]}`))
	}))
	defer srv.Close()

	// 标题含冒号：验证不再用短语引号、且 Lucene 保留字被转义
	_, _ = newTestMB(srv).Search(context.Background(), AlbumQuery{AlbumTitle: "金片子: 贰", ArtistName: "蔡琴", TrackCount: 12})

	if strings.Contains(gotQuery, `"`) {
		t.Errorf("查询不应含短语引号: %q", gotQuery)
	}
	if !strings.Contains(gotQuery, `artist:蔡琴`) {
		t.Errorf("应为裸词 artist:蔡琴, 得到 %q", gotQuery)
	}
	if !strings.Contains(gotQuery, `release:金片子\: 贰`) {
		t.Errorf("标题中的冒号应被转义为 \\: , 得到 %q", gotQuery)
	}
}

func TestPickRelease_ClosestTrackCount(t *testing.T) {
	rs := []mbRelease{
		{ID: "a", Score: 100, TrackCount: 22},
		{ID: "b", Score: 100, TrackCount: 11},
		{ID: "c", Score: 100, TrackCount: 14},
	}
	got, ok := pickRelease(rs, 11)
	if !ok || got.ID != "b" {
		t.Fatalf("应选 b，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickRelease_FiltersLowScore(t *testing.T) {
	rs := []mbRelease{
		{ID: "x", Score: 39, TrackCount: 11},
		{ID: "y", Score: 91, TrackCount: 99},
	}
	got, ok := pickRelease(rs, 11)
	if !ok || got.ID != "y" {
		t.Fatalf("应只在 score>=90 里选，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickRelease_AllBelowThreshold(t *testing.T) {
	rs := []mbRelease{{ID: "x", Score: 50, TrackCount: 11}}
	if _, ok := pickRelease(rs, 11); ok {
		t.Error("全部 score<90 应不命中")
	}
}

func TestPickRelease_UnknownLocalCountTakesFirst(t *testing.T) {
	rs := []mbRelease{
		{ID: "first", Score: 100, TrackCount: 20},
		{ID: "second", Score: 95, TrackCount: 11},
	}
	got, ok := pickRelease(rs, 0)
	if !ok || got.ID != "first" {
		t.Fatalf("localCount=0 应取靠前者，得到 %q", got.ID)
	}
}

func newTestMB(srv *httptest.Server) *MusicBrainzClient {
	c := NewMusicBrainzClient("", "Lyra-Test/0.1", srv.Client())
	c.baseURL = srv.URL
	c.minInterval = 0 // 测试不节流
	return c
}

func TestMBSearch_Hit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[
			{"id":"mbid-11","score":100,"title":"叶惠美","date":"2003-07-31","track-count":11},
			{"id":"mbid-22","score":100,"title":"叶惠美","date":"2008-01-23","track-count":22}
		]}`))
	}))
	defer srv.Close()

	m, err := newTestMB(srv).Search(context.Background(), AlbumQuery{AlbumTitle: "叶惠美", ArtistName: "周杰伦", TrackCount: 11})
	if err != nil {
		t.Fatalf("Search err: %v", err)
	}
	if m.MBID != "mbid-11" {
		t.Errorf("应选 11 首的 release，得到 %q", m.MBID)
	}
	if m.ReleaseDate != "2003-07-31" {
		t.Errorf("ReleaseDate = %q", m.ReleaseDate)
	}
}

func TestMBSearch_NoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[{"id":"x","score":40,"title":"别的","track-count":5}]}`))
	}))
	defer srv.Close()
	_, err := newTestMB(srv).Search(context.Background(), AlbumQuery{AlbumTitle: "叶惠美", ArtistName: "周杰伦", TrackCount: 11})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("低 score 应返回 ErrNotFound，得到 %v", err)
	}
}

func TestMBSearch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := newTestMB(srv).Search(context.Background(), AlbumQuery{AlbumTitle: "x", ArtistName: "y", TrackCount: 1})
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("500 应返回普通 error，得到 %v", err)
	}
}

func TestMB_Throttle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[]}`))
	}))
	defer srv.Close()
	c := NewMusicBrainzClient(srv.URL, "T", srv.Client())
	c.minInterval = 80 * time.Millisecond

	start := time.Now()
	_, _ = c.Search(context.Background(), AlbumQuery{AlbumTitle: "a", ArtistName: "b"})
	_, _ = c.Search(context.Background(), AlbumQuery{AlbumTitle: "a", ArtistName: "b"})
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Errorf("两次请求应间隔≥80ms（节流），实际 %v", elapsed)
	}
}

func TestPickByVote_MaxCoverage(t *testing.T) {
	in := [][]string{{"relA", "relB"}, {"relA", "relC"}, {"relA"}}
	got, ok := pickByVote(in)
	if !ok || got != "relA" {
		t.Fatalf("应选覆盖最多的 relA，得到 %q ok=%v", got, ok)
	}
}

func TestPickByVote_TieFirstSeen(t *testing.T) {
	in := [][]string{{"relX"}, {"relY"}} // 各 1 票，relX 先出现
	got, ok := pickByVote(in)
	if !ok || got != "relX" {
		t.Fatalf("并列应取先出现的 relX，得到 %q", got)
	}
}

func TestPickByVote_Empty(t *testing.T) {
	if _, ok := pickByVote(nil); ok {
		t.Error("空应 false")
	}
	if _, ok := pickByVote([][]string{{}}); ok {
		t.Error("全空应 false")
	}
}

func TestRecordingReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"releases":[{"id":"rel-1"},{"id":"rel-2"}]}`))
	}))
	defer srv.Close()
	ids, err := newTestMB(srv).RecordingReleases(context.Background(), "rec-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ids) != 2 || ids[0] != "rel-1" || ids[1] != "rel-2" {
		t.Errorf("ids = %v", ids)
	}
}

func TestRecordingReleases_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := newTestMB(srv).RecordingReleases(context.Background(), "rec-1"); err == nil {
		t.Error("500 应返回 error")
	}
}

func TestReleaseDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"rel-1","date":"2003-07-31"}`))
	}))
	defer srv.Close()
	d, err := newTestMB(srv).ReleaseDate(context.Background(), "rel-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d != "2003-07-31" {
		t.Errorf("date = %q", d)
	}
}
