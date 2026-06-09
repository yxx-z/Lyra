package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"strings"

	"net/url"

	"github.com/yxx-z/lyra/internal/config"
)

// authenticate 校验 Subsonic 请求参数；通过返回 nil，否则返回 *Error。
func authenticate(q url.Values, cfg *config.Config) *Error {
	if !cfg.Subsonic.Enabled {
		return &Error{Code: 40, Message: "Subsonic 未启用"}
	}
	pw := cfg.Subsonic.Password
	if pw == "" || q.Get("u") != cfg.Auth.Username {
		return &Error{Code: 40, Message: "用户名或密码错误"}
	}
	if p := q.Get("p"); p != "" {
		if strings.HasPrefix(p, "enc:") {
			if dec, err := hex.DecodeString(strings.TrimPrefix(p, "enc:")); err == nil {
				p = string(dec)
			}
		}
		if p == pw {
			return nil
		}
		return &Error{Code: 40, Message: "用户名或密码错误"}
	}
	if tok, salt := q.Get("t"), q.Get("s"); tok != "" && salt != "" {
		sum := md5.Sum([]byte(pw + salt))
		if hex.EncodeToString(sum[:]) == tok {
			return nil
		}
		return &Error{Code: 40, Message: "用户名或密码错误"}
	}
	return &Error{Code: 10, Message: "缺少认证参数"}
}
