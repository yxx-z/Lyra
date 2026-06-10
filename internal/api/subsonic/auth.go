package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/yxx-z/lyra/internal/auth"
)

// authenticate 按用户名查库、解密 Subsonic 密码并校验；通过返回 *auth.User，否则 *Error。
func (h *Handler) authenticate(q url.Values) (*auth.User, *Error) {
	if !h.cfg.Subsonic.Enabled {
		return nil, &Error{Code: 40, Message: "Subsonic 未启用"}
	}
	u, err := h.users.ByUsername(q.Get("u"))
	if err != nil || len(u.SubsonicPW) == 0 {
		return nil, &Error{Code: 40, Message: "用户名或密码错误"}
	}
	pw, err := auth.Decrypt(h.key, u.SubsonicPW)
	if err != nil {
		return nil, &Error{Code: 40, Message: "用户名或密码错误"}
	}
	if p := q.Get("p"); p != "" {
		if strings.HasPrefix(p, "enc:") {
			if dec, err := hex.DecodeString(strings.TrimPrefix(p, "enc:")); err == nil {
				p = string(dec)
			}
		}
		if p == pw {
			return u, nil
		}
		return nil, &Error{Code: 40, Message: "用户名或密码错误"}
	}
	if tok, salt := q.Get("t"), q.Get("s"); tok != "" && salt != "" {
		sum := md5.Sum([]byte(pw + salt))
		if hex.EncodeToString(sum[:]) == tok {
			return u, nil
		}
		return nil, &Error{Code: 40, Message: "用户名或密码错误"}
	}
	return nil, &Error{Code: 10, Message: "缺少认证参数"}
}
