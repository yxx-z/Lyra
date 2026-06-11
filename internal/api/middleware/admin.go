package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// RequireAdmin 必须挂在 SessionAuth 之后：校验当前用户为管理员，否则 403（无用户 401）。
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := UserFromContext(r.Context())
		if !ok {
			writeUnauthorized(w)
			return
		}
		if !u.IsAdmin {
			writeForbidden(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": "需要管理员权限"}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
