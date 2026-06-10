// internal/api/v1/setup.go
package v1

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/yxx-z/lyra/internal/auth"
)

// SetupHandler 处理首次启动引导：创建首个管理员。
type SetupHandler struct {
	users    *auth.UserStore
	sessions *auth.SessionStore
	db       *sql.DB
}

func NewSetupHandler(users *auth.UserStore, sessions *auth.SessionStore, db *sql.DB) *SetupHandler {
	return &SetupHandler{users: users, sessions: sessions, db: db}
}

// Status 处理 GET /api/v1/setup/status（免认证）。
func (h *SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
	n, err := h.users.Count()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, map[string]bool{"needsSetup": n == 0})
}

// Create 处理 POST /api/v1/setup（免认证，仅当 users 表为空时允许）。
func (h *SetupHandler) Create(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.users.Count()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if n > 0 {
		writeJSONError(w, http.StatusConflict, "已完成初始化")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "密码处理失败")
		return
	}
	u, err := h.users.Create(req.Username, hash, true)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	// 认领迁移产生的孤儿数据（旧全局书签/队列）
	_, _ = h.db.Exec(`UPDATE bookmarks SET user_id=? WHERE user_id IS NULL`, u.ID)
	_, _ = h.db.Exec(`UPDATE play_queue SET user_id=? WHERE user_id IS NULL`, u.ID)

	token, err := h.sessions.Create(u.ID, sessionTTL)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}
	setAuthCookie(w, token)
	writeJSON(w, map[string]string{"token": token})
}
