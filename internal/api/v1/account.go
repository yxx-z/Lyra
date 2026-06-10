// internal/api/v1/account.go
package v1

import (
	"encoding/json"
	"net/http"

	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
)

// AccountHandler 处理当前登录用户的账户设置。
type AccountHandler struct {
	users *auth.UserStore
	key   []byte
}

func NewAccountHandler(users *auth.UserStore, key []byte) *AccountHandler {
	return &AccountHandler{users: users, key: key}
}

// ChangePassword 处理 POST /api/v1/account/password。
func (h *AccountHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, req.OldPassword) {
		writeJSONError(w, http.StatusUnauthorized, "原密码错误")
		return
	}
	if len(req.NewPassword) < 4 {
		writeJSONError(w, http.StatusBadRequest, "新密码至少 4 位")
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	if err := h.users.UpdatePassword(u.ID, hash); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// SetSubsonicPassword 处理 POST /api/v1/account/subsonic-password。
func (h *AccountHandler) SetSubsonicPassword(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, "密码不能为空")
		return
	}
	enc, err := auth.Encrypt(h.key, req.Password)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "加密失败")
		return
	}
	if err := h.users.UpdateSubsonicPW(u.ID, enc); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
