package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"testing"

	"github.com/yxx-z/lyra/internal/config"
)

func cfgWith(pw string, enabled bool) *config.Config {
	c := &config.Config{}
	c.Auth.Username = "admin"
	c.Subsonic.Password = pw
	c.Subsonic.Enabled = enabled
	return c
}

func TestAuth_PlainPassword(t *testing.T) {
	q := url.Values{"u": {"admin"}, "p": {"secret"}}
	if e := authenticate(q, cfgWith("secret", true)); e != nil {
		t.Errorf("正确明文密码应通过，得到 %+v", e)
	}
	q2 := url.Values{"u": {"admin"}, "p": {"wrong"}}
	if e := authenticate(q2, cfgWith("secret", true)); e == nil || e.Code != 40 {
		t.Errorf("错误密码应 40，得到 %+v", e)
	}
}

func TestAuth_EncPassword(t *testing.T) {
	enc := "enc:" + hex.EncodeToString([]byte("secret"))
	q := url.Values{"u": {"admin"}, "p": {enc}}
	if e := authenticate(q, cfgWith("secret", true)); e != nil {
		t.Errorf("enc: 密码应通过，得到 %+v", e)
	}
}

func TestAuth_TokenSalt(t *testing.T) {
	salt := "abc"
	sum := md5.Sum([]byte("secret" + salt))
	tok := hex.EncodeToString(sum[:])
	q := url.Values{"u": {"admin"}, "t": {tok}, "s": {salt}}
	if e := authenticate(q, cfgWith("secret", true)); e != nil {
		t.Errorf("正确 token 应通过，得到 %+v", e)
	}
	q2 := url.Values{"u": {"admin"}, "t": {"deadbeef"}, "s": {salt}}
	if e := authenticate(q2, cfgWith("secret", true)); e == nil || e.Code != 40 {
		t.Errorf("错误 token 应 40，得到 %+v", e)
	}
}

func TestAuth_WrongUser(t *testing.T) {
	q := url.Values{"u": {"bob"}, "p": {"secret"}}
	if e := authenticate(q, cfgWith("secret", true)); e == nil || e.Code != 40 {
		t.Errorf("错误用户名应 40，得到 %+v", e)
	}
}

func TestAuth_MissingParams(t *testing.T) {
	q := url.Values{"u": {"admin"}}
	if e := authenticate(q, cfgWith("secret", true)); e == nil || e.Code != 10 {
		t.Errorf("缺认证参数应 10，得到 %+v", e)
	}
}

func TestAuth_EmptyPasswordOrDisabled(t *testing.T) {
	q := url.Values{"u": {"admin"}, "p": {"secret"}}
	if e := authenticate(q, cfgWith("", true)); e == nil || e.Code != 40 {
		t.Errorf("空密码应 40，得到 %+v", e)
	}
	if e := authenticate(q, cfgWith("secret", false)); e == nil || e.Code != 40 {
		t.Errorf("禁用应 40，得到 %+v", e)
	}
}
