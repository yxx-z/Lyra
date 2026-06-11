// internal/api/v1/albums_test.go
package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/userdata"
)

func TestListAlbums_ReturnsAlbums(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewAlbumsHandler(d, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums", nil)
	w := httptest.NewRecorder()
	h.ListAlbums(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Albums []map[string]interface{} `json:"albums"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Albums) != 1 {
		t.Fatalf("want 1 album, got %d", len(resp.Albums))
	}
	if resp.Albums[0]["title"] != "金片子" {
		t.Errorf("want title=金片子, got %v", resp.Albums[0]["title"])
	}
	if resp.Albums[0]["artist"] != "蔡琴" {
		t.Errorf("want artist=蔡琴, got %v", resp.Albums[0]["artist"])
	}
	if resp.Albums[0]["track_count"].(float64) != 2 {
		t.Errorf("want track_count=2, got %v", resp.Albums[0]["track_count"])
	}
}

func TestGetAlbum_ReturnsTracks(t *testing.T) {
	d := newTestDB(t)
	insertTestData(t, d)
	h := NewAlbumsHandler(d, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums/al1", nil)
	w := httptest.NewRecorder()
	h.getAlbum(w, req, "al1")

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["title"] != "金片子" {
		t.Errorf("want title=金片子, got %v", resp["title"])
	}
	tracks := resp["tracks"].([]interface{})
	if len(tracks) != 2 {
		t.Fatalf("want 2 tracks, got %d", len(tracks))
	}
}

func TestGetAlbum_NotFound(t *testing.T) {
	d := newTestDB(t)
	h := NewAlbumsHandler(d, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums/nonexistent", nil)
	h.getAlbum(w, req, "nonexistent")

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestGetAlbum_ReturnsGenreAndReleaseDate(t *testing.T) {
	d := newTestDB(t)

	if _, err := d.Exec(`INSERT INTO artists(id,name) VALUES('ar','周杰伦')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO albums(id,title,artist_id,release_date,genre) VALUES('al','叶惠美','ar','2003-07-31','Mandopop')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO tracks(id,title,artist_id,album_id,file_path,format,is_available,scrape_status) VALUES('t','晴天','ar','al','/m/a.flac','',1,'done')`); err != nil {
		t.Fatal(err)
	}

	h := NewAlbumsHandler(d, nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/albums/al", nil)
	h.getAlbum(w, req, "al")

	if w.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d", w.Code)
	}
	var resp AlbumDetail
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Genre != "Mandopop" {
		t.Errorf("Genre = %q, want Mandopop", resp.Genre)
	}
	if resp.ReleaseDate != "2003-07-31" {
		t.Errorf("ReleaseDate = %q, want 2003-07-31", resp.ReleaseDate)
	}
	if resp.Year != 2003 {
		t.Errorf("Year = %d, want 2003（应能从完整日期派生）", resp.Year)
	}
}

func TestAlbums_StarredFlag(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO albums(id,title) VALUES('al1','专辑')`)
	store := userdata.NewStore(d)
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	u, _ := us.Create("admin", mustHashFav(t, "pw"), true)
	store.Star(u.ID, userdata.TypeAlbum, "al1")

	h := NewAlbumsHandler(d, store)
	token, _ := ss.Create(u.ID, time.Hour)
	r := chi.NewRouter()
	r.Use(middleware.SessionAuth(ss, us, false))
	r.Get("/albums", h.ListAlbums)
	req := httptest.NewRequest("GET", "/albums", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `"starred":true`) {
		t.Errorf("已收藏专辑应含 starred:true: %s", w.Body.String())
	}
}
