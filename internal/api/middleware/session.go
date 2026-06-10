package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yxx-z/lyra/internal/auth"
)

type ctxKey int

const userCtxKey ctxKey = iota

// SessionAuth 校验会话令牌（Bearer 头或 lyra_auth cookie），通过则把用户注入 context。
// disabled 为真时绕过校验，以首个管理员身份放行（局域网 kiosk）。
func SessionAuth(sessions *auth.SessionStore, users *auth.UserStore, disabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if disabled {
				if u, err := users.FirstAdmin(); err == nil {
					r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
				}
				next.ServeHTTP(w, r)
				return
			}
			uid, ok := sessions.UserID(tokenFromRequest(r))
			if !ok {
				writeUnauthorized(w)
				return
			}
			u, err := users.ByID(uid)
			if err != nil {
				writeUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, u)))
		})
	}
}

// UserFromContext 取出 SessionAuth 注入的当前用户。
func UserFromContext(ctx context.Context) (*auth.User, bool) {
	u, ok := ctx.Value(userCtxKey).(*auth.User)
	return u, ok
}

func tokenFromRequest(r *http.Request) string {
	if parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2); len(parts) == 2 && parts[0] == "Bearer" {
		return parts[1]
	}
	if c, err := r.Cookie(authCookieName); err == nil {
		return c.Value
	}
	return ""
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": "未授权"}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
