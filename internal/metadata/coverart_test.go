package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCoverFetch_HitWithRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/release/mbid-1/front" {
			http.Redirect(w, r, "/img.jpg", http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("\xff\xd8\xff JPEGDATA"))
	}))
	defer srv.Close()

	c := NewCoverArtClient(srv.URL, srv.Client())
	data, mime, err := c.FetchFront(context.Background(), "mbid-1")
	if err != nil {
		t.Fatalf("FetchFront err: %v", err)
	}
	if len(data) == 0 {
		t.Error("应返回图片字节")
	}
	if mime != "image/jpeg" {
		t.Errorf("mime = %q", mime)
	}
}

func TestCoverFetch_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := NewCoverArtClient(srv.URL, srv.Client())
	_, _, err := c.FetchFront(context.Background(), "mbid-x")
	if !errors.Is(err, ErrNoCover) {
		t.Errorf("404 应返回 ErrNoCover，得到 %v", err)
	}
}
