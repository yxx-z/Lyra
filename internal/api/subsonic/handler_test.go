package subsonic

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
	"github.com/yxx-z/lyra/internal/transcode"
)

func testHandler(t *testing.T) (*Handler, *config.Config) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	cfg := &config.Config{}
	cfg.Subsonic.Enabled = true

	key := make([]byte, 32)
	users := auth.NewUserStore(d)
	hash, _ := auth.HashPassword("loginpw")
	u, _ := users.Create("admin", hash, true)
	enc, _ := auth.Encrypt(key, "secret")
	users.UpdateSubsonicPW(u.ID, enc)

	tcache := transcode.NewCache(t.TempDir(), 0)
	tsvc := transcode.NewService(cfg.Transcode.FFmpegPath, cfg.Transcode.DefaultBitrate, tcache)
	stream := v1.NewStreamHandler(d, tsvc)
	cover := v1.NewCoverHandler(d)
	return NewHandler(d, cfg, stream, cover, users, key), cfg
}

// doReq 走完整 chi 路由（含认证中间件）。
func doReq(t *testing.T, h *Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Route("/rest", h.RegisterRoutes)
	req := httptest.NewRequest("GET", target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPing_OK(t *testing.T) {
	h, _ := testHandler(t)
	w := doReq(t, h, "/rest/ping.view?u=admin&p=secret&f=json")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("ping 失败: %d %s", w.Code, w.Body.String())
	}
}

func TestPing_AuthFail(t *testing.T) {
	h, _ := testHandler(t)
	w := doReq(t, h, "/rest/ping?u=admin&p=wrong&f=json")
	if !strings.Contains(w.Body.String(), `"status":"failed"`) || !strings.Contains(w.Body.String(), `"code":40`) {
		t.Errorf("认证失败应返回 failed/40: %s", w.Body.String())
	}
}

func TestGetLicense(t *testing.T) {
	h, _ := testHandler(t)
	w := doReq(t, h, "/rest/getLicense?u=admin&p=secret&f=json")
	if !strings.Contains(w.Body.String(), `"valid":true`) {
		t.Errorf("getLicense: %s", w.Body.String())
	}
}

func TestGetMusicFolders(t *testing.T) {
	h, _ := testHandler(t)
	w := doReq(t, h, "/rest/getMusicFolders?u=admin&p=secret&f=json")
	if !strings.Contains(w.Body.String(), `"musicFolder"`) {
		t.Errorf("getMusicFolders: %s", w.Body.String())
	}
}
