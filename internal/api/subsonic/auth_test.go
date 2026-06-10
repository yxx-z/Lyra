package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"testing"

	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/db"
)

// newAuthHandler 创建一个仅含认证相关字段的 Handler，用于单元测试 authenticate 方法。
func newAuthHandler(t *testing.T, subsonicPW string, enabled bool) *Handler {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	cfg := &config.Config{}
	cfg.Subsonic.Enabled = enabled

	key := make([]byte, 32)
	users := auth.NewUserStore(d)
	hash, _ := auth.HashPassword("loginpw")
	u, _ := users.Create("admin", hash, true)
	if subsonicPW != "" {
		enc, _ := auth.Encrypt(key, subsonicPW)
		users.UpdateSubsonicPW(u.ID, enc)
	}
	return &Handler{db: d, cfg: cfg, users: users, key: key}
}

func TestAuth_PlainPassword(t *testing.T) {
	h := newAuthHandler(t, "secret", true)
	q := url.Values{"u": {"admin"}, "p": {"secret"}}
	if _, e := h.authenticate(q); e != nil {
		t.Errorf("正确明文密码应通过，得到 %+v", e)
	}
	q2 := url.Values{"u": {"admin"}, "p": {"wrong"}}
	if _, e := h.authenticate(q2); e == nil || e.Code != 40 {
		t.Errorf("错误密码应 40，得到 %+v", e)
	}
}

func TestAuth_EncPassword(t *testing.T) {
	h := newAuthHandler(t, "secret", true)
	enc := "enc:" + hex.EncodeToString([]byte("secret"))
	q := url.Values{"u": {"admin"}, "p": {enc}}
	if _, e := h.authenticate(q); e != nil {
		t.Errorf("enc: 密码应通过，得到 %+v", e)
	}
}

func TestAuth_TokenSalt(t *testing.T) {
	h := newAuthHandler(t, "secret", true)
	salt := "abc"
	sum := md5.Sum([]byte("secret" + salt))
	tok := hex.EncodeToString(sum[:])
	q := url.Values{"u": {"admin"}, "t": {tok}, "s": {salt}}
	if _, e := h.authenticate(q); e != nil {
		t.Errorf("正确 token 应通过，得到 %+v", e)
	}
	q2 := url.Values{"u": {"admin"}, "t": {"deadbeef"}, "s": {salt}}
	if _, e := h.authenticate(q2); e == nil || e.Code != 40 {
		t.Errorf("错误 token 应 40，得到 %+v", e)
	}
}

func TestAuth_WrongUser(t *testing.T) {
	h := newAuthHandler(t, "secret", true)
	q := url.Values{"u": {"bob"}, "p": {"secret"}}
	if _, e := h.authenticate(q); e == nil || e.Code != 40 {
		t.Errorf("错误用户名应 40，得到 %+v", e)
	}
}

func TestAuth_MissingParams(t *testing.T) {
	h := newAuthHandler(t, "secret", true)
	q := url.Values{"u": {"admin"}}
	if _, e := h.authenticate(q); e == nil || e.Code != 10 {
		t.Errorf("缺认证参数应 10，得到 %+v", e)
	}
}

func TestAuth_EmptyPasswordOrDisabled(t *testing.T) {
	// 未设置 SubsonicPW（空密码）应 40
	hEmpty := newAuthHandler(t, "", true)
	q := url.Values{"u": {"admin"}, "p": {"secret"}}
	if _, e := hEmpty.authenticate(q); e == nil || e.Code != 40 {
		t.Errorf("空密码应 40，得到 %+v", e)
	}
	// 禁用 Subsonic 应 40
	hDisabled := newAuthHandler(t, "secret", false)
	if _, e := hDisabled.authenticate(q); e == nil || e.Code != 40 {
		t.Errorf("禁用应 40，得到 %+v", e)
	}
}
