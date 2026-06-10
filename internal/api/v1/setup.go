// internal/api/v1/setup.go
package v1

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
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
	// 建管理员与认领孤儿数据放在同一事务，避免中途崩溃导致孤儿行永久无主。
	userID := uuid.NewString()
	tx, err := h.db.Begin()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	if _, err := tx.Exec(`INSERT INTO users(id, username, password_hash, is_admin) VALUES(?,?,?,1)`, userID, req.Username, hash); err != nil {
		tx.Rollback()
		writeJSONError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}
	if _, err := tx.Exec(`UPDATE bookmarks SET user_id=? WHERE user_id IS NULL`, userID); err != nil {
		tx.Rollback()
		writeJSONError(w, http.StatusInternalServerError, "认领数据失败")
		return
	}
	if _, err := tx.Exec(`UPDATE play_queue SET user_id=? WHERE user_id IS NULL`, userID); err != nil {
		tx.Rollback()
		writeJSONError(w, http.StatusInternalServerError, "认领数据失败")
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建用户失败")
		return
	}

	token, err := h.sessions.Create(userID, sessionTTL)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}
	setAuthCookie(w, token)
	writeJSON(w, map[string]string{"token": token})
}
