// internal/api/v1/auth.go
package v1

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/yxx-z/lyra/internal/config"
)

// AuthHandler handles /api/v1/auth/* endpoints.
type AuthHandler struct {
	cfg *config.Config
}

// NewAuthHandler creates an AuthHandler backed by cfg.
func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{cfg: cfg}
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if h.cfg.Auth.Password == "" || req.Username != h.cfg.Auth.Username || req.Password != h.cfg.Auth.Password {
		writeJSONError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"token": h.cfg.Auth.Token}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
