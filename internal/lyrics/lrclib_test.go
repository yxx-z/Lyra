package lyrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLRCLIBClientFetch_ReturnsSyncedLyrics(t *testing.T) {
	var sawUserAgent bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/get" {
			t.Fatalf("path: want /api/get, got %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("track_name") != "渡口" {
			t.Errorf("track_name: got %q", q.Get("track_name"))
		}
		if q.Get("artist_name") != "蔡琴" {
			t.Errorf("artist_name: got %q", q.Get("artist_name"))
		}
		if q.Get("album_name") != "金片子" {
			t.Errorf("album_name: got %q", q.Get("album_name"))
		}
		if q.Get("duration") != "245" {
			t.Errorf("duration: got %q", q.Get("duration"))
		}
		if r.UserAgent() == "Lyra/0.1" {
			sawUserAgent = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 1,
			"trackName": "渡口",
			"artistName": "蔡琴",
			"albumName": "金片子",
			"duration": 245,
			"instrumental": false,
			"plainLyrics": "让我与你握别",
			"syncedLyrics": "[00:01.00]让我与你握别"
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewLRCLIBClient(server.URL, "Lyra/0.1", server.Client())
	result, err := client.Fetch(context.Background(), Query{
		TrackName:  "渡口",
		ArtistName: "蔡琴",
		AlbumName:  "金片子",
		Duration:   245,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.LRCContent != "[00:01.00]让我与你握别" {
		t.Errorf("LRCContent mismatch: %q", result.LRCContent)
	}
	if result.PlainContent != "让我与你握别" {
		t.Errorf("PlainContent mismatch: %q", result.PlainContent)
	}
	if result.Source != "lrclib" {
		t.Errorf("Source: want lrclib, got %q", result.Source)
	}
	if !sawUserAgent {
		t.Error("expected User-Agent header")
	}
}

func TestLRCLIBClientFetch_FallsBackToPlainLyrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 2,
			"trackName": "Plain",
			"artistName": "Artist",
			"albumName": "Album",
			"duration": 100,
			"instrumental": false,
			"plainLyrics": "line one\nline two",
			"syncedLyrics": ""
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewLRCLIBClient(server.URL, "Lyra/0.1", server.Client())
	result, err := client.Fetch(context.Background(), Query{
		TrackName:  "Plain",
		ArtistName: "Artist",
		AlbumName:  "Album",
		Duration:   100,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.LRCContent != "line one\nline two" {
		t.Errorf("LRCContent should fall back to plain lyrics, got %q", result.LRCContent)
	}
}

func TestLRCLIBClientFetch_ReturnsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":404,"name":"TrackNotFound"}`))
	}))
	t.Cleanup(server.Close)

	client := NewLRCLIBClient(server.URL, "Lyra/0.1", server.Client())
	_, err := client.Fetch(context.Background(), Query{
		TrackName:  "missing",
		ArtistName: "artist",
		AlbumName:  "album",
		Duration:   120,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestLRCLIBClientFetch_RejectsMissingDuration(t *testing.T) {
	client := NewLRCLIBClient("http://example.test", "Lyra/0.1", http.DefaultClient)
	_, err := client.Fetch(context.Background(), Query{
		TrackName:  "missing duration",
		ArtistName: "artist",
		AlbumName:  "album",
	})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("want ErrInvalidQuery, got %v", err)
	}
}

func TestLRCLIBClientSearch_ReturnsCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			t.Fatalf("path: want /api/search, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("track_name") != "罗刹海市" {
			t.Errorf("track_name: got %q", r.URL.Query().Get("track_name"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":1,"trackName":"罗刹海市","artistName":"刀郎","albumName":"山歌寥哉","duration":268,"instrumental":false,"plainLyrics":"那马户又鸟","syncedLyrics":"[00:01.00]那马户又鸟"},
			{"id":2,"trackName":"罗刹海市(伴奏)","artistName":"刀郎","albumName":"山歌寥哉","duration":268,"instrumental":true,"plainLyrics":"","syncedLyrics":""}
		]`))
	}))
	defer server.Close()

	c := NewLRCLIBClient(server.URL, "", nil)
	cands, err := c.Search(context.Background(), "罗刹海市", "刀郎", "山歌寥哉")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(cands))
	}
	if cands[0].TrackName != "罗刹海市" || cands[0].Duration != 268 || cands[0].SyncedLyrics == "" {
		t.Errorf("候选0 解析不符: %+v", cands[0])
	}
	if !cands[1].Instrumental {
		t.Errorf("候选1 应为器乐")
	}
}

func TestLRCLIBClientSearch_EmptyQuery(t *testing.T) {
	c := NewLRCLIBClient("http://example.invalid", "", nil)
	if _, err := c.Search(context.Background(), "", "", ""); !errors.Is(err, ErrInvalidQuery) {
		t.Errorf("空参应 ErrInvalidQuery, got %v", err)
	}
}
