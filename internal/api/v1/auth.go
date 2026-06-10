// internal/api/v1/auth.go
package v1

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
)

const AuthCookieName = "lyra_auth"
const sessionTTL = 30 * 24 * time.Hour

// AuthHandler 处理 /api/v1/auth/* 端点，基于 users/sessions 表。
type AuthHandler struct {
	users    *auth.UserStore
	sessions *auth.SessionStore
}

func NewAuthHandler(users *auth.UserStore, sessions *auth.SessionStore) *AuthHandler {
	return &AuthHandler{users: users, sessions: sessions}
}

// Login 处理 POST /api/v1/auth/login。
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	u, err := h.users.ByUsername(req.Username)
	if err != nil || !auth.CheckPassword(u.PasswordHash, req.Password) {
		writeJSONError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	token, err := h.sessions.Create(u.ID, sessionTTL)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}
	setAuthCookie(w, token)
	writeJSON(w, map[string]string{"token": token})
}

// Logout 处理 POST /api/v1/auth/logout：删会话行 + 清 cookie。
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(AuthCookieName); err == nil {
		_ = h.sessions.Delete(c.Value)
	}
	clearAuthCookie(w)
	writeJSON(w, map[string]bool{"ok": true})
}

// Session 处理 POST /api/v1/auth/session：刷新会话有效期。
func (h *AuthHandler) Session(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(AuthCookieName); err == nil {
		_ = h.sessions.Refresh(c.Value, sessionTTL)
		setAuthCookie(w, c.Value)
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// Me 处理 GET /api/v1/auth/me：返回当前登录用户。
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	writeJSON(w, map[string]any{"username": u.Username, "isAdmin": u.IsAdmin})
}

func setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: AuthCookieName, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: AuthCookieName, Value: "", Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Expires: time.Unix(0, 0), MaxAge: -1,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
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
