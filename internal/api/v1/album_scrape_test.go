package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/metadata"
)

func mkMetaSvc(t *testing.T, mbBody string, caaStatus int) (*metadata.MetadataService, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	mb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(mbBody)) }))
	t.Cleanup(mb.Close)
	caa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if caaStatus == http.StatusOK {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("\xff\xd8\xffJPEG"))
			return
		}
		w.WriteHeader(caaStatus)
	}))
	t.Cleanup(caa.Close)
	svc := metadata.NewMetadataService(database, metadata.NewMusicBrainzClient(mb.URL, "T/0.1", nil), metadata.NewCoverArtClient(caa.URL, nil), dir)
	return svc, database
}

func doScrapeReq(h *AlbumScrapeHandler, albumID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/albums/"+albumID+"/scrape", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", albumID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.ScrapeAlbum(rec, req)
	return rec
}

func TestAlbumScrape_Done(t *testing.T) {
	svc, d := mkMetaSvc(t, `{"releases":[{"id":"mbid-1","score":100,"date":"2003","track-count":0}]}`, http.StatusOK)
	d.Exec(`INSERT INTO artists(id,name) VALUES('ar','A')`)
	d.Exec(`INSERT INTO albums(id,title,artist_id,scrape_status) VALUES('al','T','ar','pending')`)

	rec := doScrapeReq(NewAlbumScrapeHandler(svc), "al")
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d", rec.Code)
	}
	var resp AlbumScrapeResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Status != "done" || resp.MBID != "mbid-1" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestAlbumScrape_NotFound(t *testing.T) {
	svc, _ := mkMetaSvc(t, `{"releases":[]}`, http.StatusNotFound)
	rec := doScrapeReq(NewAlbumScrapeHandler(svc), "nonexistent")
	if rec.Code != http.StatusNotFound {
		t.Errorf("不存在专辑应 404，得到 %d", rec.Code)
	}
}

func TestAlbumScrape_NoMatch(t *testing.T) {
	svc, d := mkMetaSvc(t, `{"releases":[{"id":"x","score":10,"track-count":1}]}`, http.StatusNotFound)
	d.Exec(`INSERT INTO artists(id,name) VALUES('ar','A')`)
	d.Exec(`INSERT INTO albums(id,title,artist_id,scrape_status) VALUES('al','T','ar','pending')`)
	rec := doScrapeReq(NewAlbumScrapeHandler(svc), "al")
	if rec.Code != http.StatusNotFound {
		t.Errorf("无匹配应 404，得到 %d", rec.Code)
	}
}
