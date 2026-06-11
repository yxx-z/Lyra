// internal/api/v1/register.go
package v1

import (
	"encoding/json"
	"net/http"

	"github.com/yxx-z/lyra/internal/auth"
)

// RegisterHandler 处理公开自助注册（受 allow_registration 开关限制）。
type RegisterHandler struct {
	users    *auth.UserStore
	sessions *auth.SessionStore
	settings *auth.SettingsStore
}

func NewRegisterHandler(users *auth.UserStore, sessions *auth.SessionStore, settings *auth.SettingsStore) *RegisterHandler {
	return &RegisterHandler{users: users, sessions: sessions, settings: settings}
}

// Status 处理 GET /api/v1/register/status（免认证）。
func (h *RegisterHandler) Status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"allowRegistration": h.settings.AllowRegistration()})
}

// Register 处理 POST /api/v1/register（免认证，受开关限制）。
func (h *RegisterHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.settings.AllowRegistration() {
		writeJSONError(w, http.StatusForbidden, "未开放注册")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Username == "" || len(req.Password) < 4 {
		writeJSONError(w, http.StatusBadRequest, "用户名不能为空，密码至少 4 位")
		return
	}
	if _, err := h.users.ByUsername(req.Username); err == nil {
		writeJSONError(w, http.StatusConflict, "用户名已存在")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	u, err := h.users.Create(req.Username, hash, false)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建用户失败")
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
