// internal/api/middleware/auth.go
package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

const authCookieName = "lyra_auth"

// BearerAuth returns a middleware that validates the Authorization: Bearer header.
// If disabled is true, all requests are passed through without validation.
func BearerAuth(token string, disabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if disabled {
				next.ServeHTTP(w, r)
				return
			}
			if !hasValidToken(r, token) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				if err := json.NewEncoder(w).Encode(map[string]string{"error": "未授权"}); err != nil {
					slog.Error("写响应失败", "err", err)
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func hasValidToken(r *http.Request, token string) bool {
	parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(parts) == 2 && parts[0] == "Bearer" && parts[1] == token {
		return true
	}

	cookie, err := r.Cookie(authCookieName)
	return err == nil && cookie.Value == token
}
