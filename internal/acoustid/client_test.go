package acoustid

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPickResult_Hit(t *testing.T) {
	rs := []acoustResult{
		{ID: "aid-1", Score: 0.97, Recordings: []recordingRef{{ID: "mbid-1"}, {ID: "mbid-2"}}},
	}
	got, ok := pickResult(rs)
	if !ok {
		t.Fatal("0.97 应命中")
	}
	if got.AcoustID != "aid-1" || got.MBID != "mbid-1" {
		t.Errorf("got %+v", got)
	}
}

func TestPickResult_BelowThreshold(t *testing.T) {
	rs := []acoustResult{{ID: "x", Score: 0.85, Recordings: []recordingRef{{ID: "m"}}}}
	if _, ok := pickResult(rs); ok {
		t.Error("score<0.9 不应命中")
	}
}

func TestPickResult_Empty(t *testing.T) {
	if _, ok := pickResult(nil); ok {
		t.Error("无结果不应命中")
	}
}

func TestPickResult_HitNoRecordings(t *testing.T) {
	rs := []acoustResult{{ID: "aid-2", Score: 0.95}}
	got, ok := pickResult(rs)
	if !ok || got.AcoustID != "aid-2" || got.MBID != "" {
		t.Errorf("命中但无 recordings 应只 AcoustID、MBID 空，得到 %+v ok=%v", got, ok)
	}
}

func newTestClient(srv *httptest.Server) *AcoustIDClient {
	return NewAcoustIDClient(srv.URL, "testkey", srv.Client())
}

func TestLookup_Hit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","results":[{"id":"aid-1","score":0.97,"recordings":[{"id":"mbid-1","title":"晴天"}]}]}`))
	}))
	defer srv.Close()
	res, err := newTestClient(srv).Lookup(context.Background(), 269, "FP")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if res.AcoustID != "aid-1" || res.MBID != "mbid-1" {
		t.Errorf("got %+v", res)
	}
}

func TestLookup_NoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()
	_, err := newTestClient(srv).Lookup(context.Background(), 269, "FP")
	if !errors.Is(err, ErrNoMatch) {
		t.Errorf("空结果应 ErrNoMatch，得到 %v", err)
	}
}

func TestLookup_StatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"error","error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()
	_, err := newTestClient(srv).Lookup(context.Background(), 269, "FP")
	if err == nil || errors.Is(err, ErrNoMatch) {
		t.Errorf("status!=ok 应普通 error，得到 %v", err)
	}
}

func TestLookup_HTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := newTestClient(srv).Lookup(context.Background(), 269, "FP")
	if err == nil || errors.Is(err, ErrNoMatch) {
		t.Errorf("500 应普通 error，得到 %v", err)
	}
}
