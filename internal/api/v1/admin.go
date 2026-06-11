// internal/api/v1/admin.go
package v1

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
)

// AdminHandler 处理 /api/v1/admin/* 管理员端点。
type AdminHandler struct {
	users    *auth.UserStore
	settings *auth.SettingsStore
}

func NewAdminHandler(users *auth.UserStore, settings *auth.SettingsStore) *AdminHandler {
	return &AdminHandler{users: users, settings: settings}
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := h.users.List()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if list == nil {
		list = []auth.UserSummary{}
	}
	writeJSON(w, map[string]any{"users": list})
}

func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"isAdmin"`
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
	u, err := h.users.Create(req.Username, hash, req.IsAdmin)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	writeJSON(w, map[string]any{"id": u.ID, "username": u.Username, "isAdmin": u.IsAdmin})
}

func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	current, _ := middleware.UserFromContext(r.Context())
	id := chi.URLParam(r, "id")
	if current != nil && current.ID == id {
		writeJSONError(w, http.StatusBadRequest, "不能删除自己")
		return
	}
	target, err := h.users.ByID(id)
	if err != nil {
		writeJSON(w, map[string]bool{"ok": true}) // 不存在视为已删（幂等）
		return
	}
	if target.IsAdmin {
		n, err := h.users.AdminCount()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		if n <= 1 {
			writeJSONError(w, http.StatusBadRequest, "不能删除最后一个管理员")
			return
		}
	}
	if err := h.users.Delete(id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *AdminHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(req.Password) < 4 {
		writeJSONError(w, http.StatusBadRequest, "密码至少 4 位")
		return
	}
	if _, err := h.users.ByID(id); err != nil {
		writeJSONError(w, http.StatusNotFound, "用户不存在")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	if err := h.users.UpdatePassword(id, hash); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *AdminHandler) SetRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		IsAdmin bool `json:"isAdmin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	target, err := h.users.ByID(id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if target.IsAdmin && !req.IsAdmin {
		n, err := h.users.AdminCount()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		if n <= 1 {
			writeJSONError(w, http.StatusBadRequest, "不能降级最后一个管理员")
			return
		}
	}
	if err := h.users.UpdateRole(id, req.IsAdmin); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *AdminHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"allowRegistration": h.settings.AllowRegistration()})
}

func (h *AdminHandler) SetSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AllowRegistration bool `json:"allowRegistration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := h.settings.SetAllowRegistration(req.AllowRegistration); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
