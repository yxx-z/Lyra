# 用户管理 + 注册 Implementation Plan（多用户 auth 子项目 2）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在已完成的多用户认证地基之上补齐「管理」面：管理员后台增删用户、重置他人密码、升降级角色，以及 DB 存储、可运行时切换的自助注册开关与公开注册端点。

**Architecture:** 新增 `app_settings` 键值表 + `SettingsStore`（承载 `allow_registration`）；`UserStore` 增 List/Delete/UpdateRole/AdminCount；新 `middleware.RequireAdmin` 守卫 `/api/v1/admin/*`；新 `AdminHandler`（用户/设置端点）与 `RegisterHandler`（公开注册，受开关限制）；前端把 `getMe` 接入 boot 识别角色，新增 RegisterView 与管理员 UserManagement 面板。

**Tech Stack:** Go 1.25 · modernc.org/sqlite（单连接、`_pragma=foreign_keys=ON`）· chi v5 · `golang.org/x/crypto/bcrypt`（已在用）· Vue 3 + Pinia。

**关键约束：**
- 已就绪可复用：`auth.UserStore`、`auth.SessionStore`、`auth.HashPassword`、`auth.User`、`middleware.SessionAuth`/`UserFromContext`、v1 的 `writeJSON`/`writeJSONError`/`setAuthCookie`/`sessionTTL`、`SetupHandler` 自动登录范式。
- 角色变更即时生效：`SessionAuth` 每请求用 `ByID` 重载用户。
- 删用户经 FK `ON DELETE CASCADE` 自动清理 sessions/bookmarks/play_queue。
- Go 路径：`export PATH=$PATH:/home/yxx/go-local/go/bin`。后端测试用内存 sqlite + httptest，不打网络。

---

## File Structure

```
internal/db/migrations/007_app_settings.up.sql   新迁移
internal/db/schema.sql                            改：加 app_settings
internal/auth/settings.go                         新：SettingsStore
internal/auth/settings_test.go
internal/auth/users.go                            改：List/Delete/UpdateRole/AdminCount + UserSummary
internal/auth/users_test.go                       改：追加测试
internal/api/middleware/admin.go                  新：RequireAdmin + writeForbidden
internal/api/middleware/admin_test.go
internal/api/v1/admin.go                          新：AdminHandler
internal/api/v1/admin_test.go
internal/api/v1/register.go                       新：RegisterHandler
internal/api/v1/register_test.go
internal/api/router.go                            改：装配 settings/admin/register
web/src/api/client.ts                             改：register/admin 方法
web/src/components/RegisterView.vue               新
web/src/components/UserManagement.vue             新
web/src/components/LoginView.vue                  改：注册入口
web/src/components/LibraryShell.vue               改：管理员「用户管理」入口
web/src/App.vue                                    改：getMe + currentUser + 注册/用户管理装配
```

---

## Task 1: 迁移 007（app_settings）

**Files:**
- Create: `internal/db/migrations/007_app_settings.up.sql`
- Modify: `internal/db/schema.sql`
- Test: `internal/db/db_test.go`（追加）

- [ ] **Step 1: 写失败测试**

在 `internal/db/db_test.go` 末尾追加：
```go
func TestOpen_HasAppSettings(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='app_settings'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("app_settings 表应存在 (n=%d err=%v)", n, err)
	}
	if _, err := db.Exec(`INSERT INTO app_settings(key,value) VALUES('allow_registration','1')`); err != nil {
		t.Errorf("写 app_settings 失败: %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/...`
Expected: FAIL（app_settings 表不存在）。

- [ ] **Step 3: 写迁移**

`internal/db/migrations/007_app_settings.up.sql`:
```sql
-- 运行时应用设置（键值），目前承载 allow_registration
CREATE TABLE app_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

- [ ] **Step 4: 同步 schema.sql**

在 `internal/db/schema.sql` 中（紧随 sessions 表之后）追加：
```sql
CREATE TABLE app_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

- [ ] **Step 5: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/...`
Expected: PASS（含原有用例）。

- [ ] **Step 6: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/db && git commit -m "feat(db): 迁移 007 app_settings 键值表"
```

---

## Task 2: SettingsStore

**Files:**
- Create: `internal/auth/settings.go`, `internal/auth/settings_test.go`

- [ ] **Step 1: 写失败测试**

`internal/auth/settings_test.go`:
```go
package auth

import (
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func TestSettingsStore_AllowRegistration(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewSettingsStore(d)

	if s.AllowRegistration() {
		t.Error("默认应为关闭（无行）")
	}
	if err := s.SetAllowRegistration(true); err != nil {
		t.Fatalf("SetAllowRegistration: %v", err)
	}
	if !s.AllowRegistration() {
		t.Error("开启后应为 true")
	}
	if err := s.SetAllowRegistration(false); err != nil {
		t.Fatalf("SetAllowRegistration: %v", err)
	}
	if s.AllowRegistration() {
		t.Error("关闭后应为 false")
	}
}

func TestSettingsStore_GetSet(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewSettingsStore(d)
	if _, ok := s.Get("missing"); ok {
		t.Error("不存在的键应返回 ok=false")
	}
	if err := s.Set("k", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("k", "v2"); err != nil { // upsert 覆盖
		t.Fatal(err)
	}
	if v, ok := s.Get("k"); !ok || v != "v2" {
		t.Errorf("upsert 后应为 v2: %q ok=%v", v, ok)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/auth/...`
Expected: 编译失败（NewSettingsStore 未定义）。

- [ ] **Step 3: 实现**

`internal/auth/settings.go`:
```go
package auth

import "database/sql"

type SettingsStore struct{ db *sql.DB }

func NewSettingsStore(db *sql.DB) *SettingsStore { return &SettingsStore{db: db} }

func (s *SettingsStore) Get(key string) (string, bool) {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key=?`, key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func (s *SettingsStore) Set(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO app_settings(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

const keyAllowRegistration = "allow_registration"

func (s *SettingsStore) AllowRegistration() bool {
	v, _ := s.Get(keyAllowRegistration)
	return v == "1"
}

func (s *SettingsStore) SetAllowRegistration(allow bool) error {
	v := "0"
	if allow {
		v = "1"
	}
	return s.Set(keyAllowRegistration, v)
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/auth/...`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/auth && git commit -m "feat(auth): SettingsStore（app_settings 键值 + allow_registration）"
```

---

## Task 3: UserStore 新增 List/Delete/UpdateRole/AdminCount

**Files:**
- Modify: `internal/auth/users.go`
- Modify: `internal/auth/users_test.go`（追加）

- [ ] **Step 1: 写失败测试**

在 `internal/auth/users_test.go` 末尾追加：
```go
func TestUserStore_ListDeleteRoleAdminCount(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewUserStore(d)
	a, _ := s.Create("admin", "h", true)
	b, _ := s.Create("bob", "h", false)
	// bob 设一个 subsonic_pw
	if err := s.UpdateSubsonicPW(b.ID, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("应有 2 个用户，实际 %d", len(list))
	}
	// 找到 bob，校验 HasSubsonicPassword=true、IsAdmin=false
	var bobSum *UserSummary
	for i := range list {
		if list[i].Username == "bob" {
			bobSum = &list[i]
		}
	}
	if bobSum == nil || bobSum.IsAdmin || !bobSum.HasSubsonicPassword {
		t.Errorf("bob 摘要不符: %+v", bobSum)
	}

	if n, _ := s.AdminCount(); n != 1 {
		t.Errorf("AdminCount 应为 1，实际 %d", n)
	}
	// 升级 bob
	if err := s.UpdateRole(b.ID, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.AdminCount(); n != 2 {
		t.Errorf("升级后 AdminCount 应为 2，实际 %d", n)
	}
	// 删除 bob
	if err := s.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ByID(b.ID); err == nil {
		t.Error("删除后 ByID 应失败")
	}
	_ = a
}

func TestUserStore_DeleteCascadesSessions(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	us := NewUserStore(d)
	ss := NewSessionStore(d)
	u, _ := us.Create("bob", "h", false)
	token, _ := ss.Create(u.ID, 3600*1e9) // 1h in ns
	if _, ok := ss.UserID(token); !ok {
		t.Fatal("会话应存在")
	}
	if err := us.Delete(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := ss.UserID(token); ok {
		t.Error("删用户后其会话应被级联清理")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/auth/...`
Expected: 编译失败（List/UserSummary/AdminCount/UpdateRole/Delete 未定义）。

- [ ] **Step 3: 实现**

在 `internal/auth/users.go` 末尾追加：
```go
// UserSummary 是用户列表中暴露给前端的精简视图（不含密码哈希/密文）。
type UserSummary struct {
	ID                  string `json:"id"`
	Username            string `json:"username"`
	IsAdmin             bool   `json:"isAdmin"`
	HasSubsonicPassword bool   `json:"hasSubsonicPassword"`
	CreatedAt           string `json:"createdAt"`
}

func (s *UserStore) List() ([]UserSummary, error) {
	rows, err := s.db.Query(`SELECT id, username, is_admin, (subsonic_pw IS NOT NULL AND length(subsonic_pw) > 0), created_at FROM users ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserSummary
	for rows.Next() {
		var u UserSummary
		var admin, hasPW int
		if err := rows.Scan(&u.ID, &u.Username, &admin, &hasPW, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.IsAdmin = admin == 1
		u.HasSubsonicPassword = hasPW == 1
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *UserStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *UserStore) UpdateRole(id string, isAdmin bool) error {
	admin := 0
	if isAdmin {
		admin = 1
	}
	_, err := s.db.Exec(`UPDATE users SET is_admin=?, updated_at=datetime('now') WHERE id=?`, admin, id)
	return err
}

func (s *UserStore) AdminCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin=1`).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/auth/...`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/auth && git commit -m "feat(auth): UserStore List/Delete/UpdateRole/AdminCount + UserSummary"
```

---

## Task 4: RequireAdmin 中间件

**Files:**
- Create: `internal/api/middleware/admin.go`, `internal/api/middleware/admin_test.go`

- [ ] **Step 1: 写失败测试**

`internal/api/middleware/admin_test.go`:
```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

func adminProbe(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }

func TestRequireAdmin_AdminPasses(t *testing.T) {
	d, _ := db.Open(":memory:")
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	admin, _ := us.Create("admin", "h", true)
	token, _ := ss.Create(admin.ID, time.Hour)
	h := SessionAuth(ss, us, false)(RequireAdmin(http.HandlerFunc(adminProbe)))
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("管理员应放行: %d", w.Code)
	}
}

func TestRequireAdmin_NormalForbidden(t *testing.T) {
	d, _ := db.Open(":memory:")
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	bob, _ := us.Create("bob", "h", false)
	token, _ := ss.Create(bob.ID, time.Hour)
	h := SessionAuth(ss, us, false)(RequireAdmin(http.HandlerFunc(adminProbe)))
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("普通用户应 403: %d", w.Code)
	}
}

func TestRequireAdmin_NoUser401(t *testing.T) {
	h := RequireAdmin(http.HandlerFunc(adminProbe))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("无用户应 401: %d", w.Code)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/middleware/...`
Expected: 编译失败（RequireAdmin 未定义）。

- [ ] **Step 3: 实现**

`internal/api/middleware/admin.go`:
```go
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
```
> `writeUnauthorized` 与 `UserFromContext` 已在同包 `session.go` 中定义，复用。

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/middleware/...`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/middleware && git commit -m "feat(api): RequireAdmin 中间件"
```

---

## Task 5: AdminHandler（用户管理 + 设置端点）

**Files:**
- Create: `internal/api/v1/admin.go`, `internal/api/v1/admin_test.go`

> 复用同包 `writeJSON`/`writeJSONError`（定义于 `auth.go`）。`router.go` 在 Task 7 才接线，本任务用 `go vet ./internal/api/v1/` 与定向 `go test` 验证（v1 包不 import api 包，独立编译）。

- [ ] **Step 1: 写失败测试**

`internal/api/v1/admin_test.go`:
```go
package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

// adminEnv 组装：admin 用户 + 其会话 + 走 SessionAuth+RequireAdmin 的 chi 路由。
type adminEnv struct {
	h       *AdminHandler
	users   *auth.UserStore
	router  http.Handler
	token   string
	adminID string
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	set := auth.NewSettingsStore(d)
	admin, _ := us.Create("admin", mustHash(t, "adminpw"), true)
	token, _ := ss.Create(admin.ID, time.Hour)
	h := NewAdminHandler(us, set)

	r := chi.NewRouter()
	r.Route("/admin", func(r chi.Router) {
		r.Use(middleware.SessionAuth(ss, us, false))
		r.Use(middleware.RequireAdmin)
		r.Get("/users", h.ListUsers)
		r.Post("/users", h.CreateUser)
		r.Delete("/users/{id}", h.DeleteUser)
		r.Post("/users/{id}/password", h.ResetPassword)
		r.Post("/users/{id}/role", h.SetRole)
		r.Get("/settings", h.GetSettings)
		r.Post("/settings", h.SetSettings)
	})
	return &adminEnv{h: h, users: us, router: r, token: token, adminID: admin.ID}
}

func mustHash(t *testing.T, pw string) string {
	t.Helper()
	hash, err := auth.HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func (e *adminEnv) do(method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: e.token})
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func TestAdmin_CreateListDeleteUser(t *testing.T) {
	e := newAdminEnv(t)
	w := e.do("POST", "/admin/users", `{"username":"alice","password":"alicepw","isAdmin":false}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"username":"alice"`) {
		t.Fatalf("建用户应成功: %d %s", w.Code, w.Body.String())
	}
	w = e.do("GET", "/admin/users", "")
	if !strings.Contains(w.Body.String(), `"alice"`) || !strings.Contains(w.Body.String(), `"admin"`) {
		t.Errorf("列表应含 alice 与 admin: %s", w.Body.String())
	}
	alice, _ := e.users.ByUsername("alice")
	w = e.do("DELETE", "/admin/users/"+alice.ID, "")
	if w.Code != 200 {
		t.Errorf("删 alice 应成功: %d %s", w.Code, w.Body.String())
	}
	if _, err := e.users.ByUsername("alice"); err == nil {
		t.Error("alice 应已删除")
	}
}

func TestAdmin_CreateUser_DuplicateAndShortPw(t *testing.T) {
	e := newAdminEnv(t)
	if e.do("POST", "/admin/users", `{"username":"admin","password":"whatever"}`).Code != http.StatusConflict {
		t.Error("重名应 409")
	}
	if e.do("POST", "/admin/users", `{"username":"x","password":"1"}`).Code != http.StatusBadRequest {
		t.Error("短密码应 400")
	}
}

func TestAdmin_DeleteSelfAndLastAdminBlocked(t *testing.T) {
	e := newAdminEnv(t)
	// 删自己
	if e.do("DELETE", "/admin/users/"+e.adminID, "").Code != http.StatusBadRequest {
		t.Error("删自己应 400")
	}
	// admin 是唯一管理员，造一个普通用户后删该 admin 仍应被拦（最后管理员）
	bob, _ := e.users.Create("bob", mustHash(t, "bobpw"), false)
	_ = bob
	// 直接删 e.adminID 已被“删自己”拦截，这里改为：再建一个 admin，降级后删第一个时验证最后管理员保护
	// 简化：验证 SetRole 降级最后管理员被拦
	if e.do("POST", "/admin/users/"+e.adminID+"/role", `{"isAdmin":false}`).Code != http.StatusBadRequest {
		t.Error("降级最后一个管理员应 400")
	}
}

func TestAdmin_ResetPasswordAndRole(t *testing.T) {
	e := newAdminEnv(t)
	bob, _ := e.users.Create("bob", mustHash(t, "bobpw"), false)
	if e.do("POST", "/admin/users/"+bob.ID+"/password", `{"password":"newpw123"}`).Code != 200 {
		t.Error("重置密码应成功")
	}
	got, _ := e.users.ByID(bob.ID)
	if !auth.CheckPassword(got.PasswordHash, "newpw123") {
		t.Error("新密码应生效")
	}
	if e.do("POST", "/admin/users/"+bob.ID+"/role", `{"isAdmin":true}`).Code != 200 {
		t.Error("升级 bob 应成功")
	}
	got, _ = e.users.ByID(bob.ID)
	if !got.IsAdmin {
		t.Error("bob 应已是管理员")
	}
	// 不存在的用户
	if e.do("POST", "/admin/users/nope/password", `{"password":"xxxx"}`).Code != http.StatusNotFound {
		t.Error("重置不存在用户应 404")
	}
}

func TestAdmin_Settings(t *testing.T) {
	e := newAdminEnv(t)
	if !strings.Contains(e.do("GET", "/admin/settings", "").Body.String(), `"allowRegistration":false`) {
		t.Error("默认 allowRegistration 应 false")
	}
	if e.do("POST", "/admin/settings", `{"allowRegistration":true}`).Code != 200 {
		t.Error("设置应成功")
	}
	if !strings.Contains(e.do("GET", "/admin/settings", "").Body.String(), `"allowRegistration":true`) {
		t.Error("切换后应 true")
	}
}

func TestAdmin_NormalUserForbidden(t *testing.T) {
	e := newAdminEnv(t)
	// bob 普通用户的会话
	d := e.users // 复用同库
	bob, _ := d.Create("bob", mustHash(t, "bobpw"), false)
	// 需要 bob 的会话：用 SessionStore 重建一个——通过 env 暴露不便，改用直接断言普通用户经 RequireAdmin 被拦由 middleware 测试覆盖。
	_ = bob
}
```
> 注：`TestAdmin_NormalUserForbidden` 的「普通用户经 admin 路由 403」已由 Task 4 的 `TestRequireAdmin_NormalForbidden` 覆盖；把该空用例删除，不要保留空函数。`TestAdmin_DeleteSelfAndLastAdminBlocked` 中保留两条断言（删自己 400、降级最后管理员 400），删除其中关于 bob 的无效中间步骤注释与 `_ = bob`，最终只留下这两条 `e.do(...)` 断言。

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go vet ./internal/api/v1/`
Expected: 失败（NewAdminHandler 未定义）。

- [ ] **Step 3: 实现**

`internal/api/v1/admin.go`:
```go
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
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run Admin -v && go vet ./internal/api/v1/`
Expected: PASS；vet 通过。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/admin.go internal/api/v1/admin_test.go && git commit -m "feat(api): AdminHandler（增删用户/重置密码/升降级/设置开关 + 护栏）"
```

---

## Task 6: RegisterHandler（公开注册 + 状态）

**Files:**
- Create: `internal/api/v1/register.go`, `internal/api/v1/register_test.go`

- [ ] **Step 1: 写失败测试**

`internal/api/v1/register_test.go`:
```go
package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

func regFixture(t *testing.T) (*RegisterHandler, *auth.UserStore, *auth.SettingsStore) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	set := auth.NewSettingsStore(d)
	return NewRegisterHandler(us, ss, set), us, set
}

func TestRegister_DisabledByDefault(t *testing.T) {
	h, _, _ := regFixture(t)
	req := httptest.NewRequest("POST", "/register", strings.NewReader(`{"username":"alice","password":"alicepw"}`))
	w := httptest.NewRecorder()
	h.Register(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("默认未开放，应 403: %d", w.Code)
	}
}

func TestRegister_EnabledCreatesNormalUserAndLogsIn(t *testing.T) {
	h, us, set := regFixture(t)
	set.SetAllowRegistration(true)
	req := httptest.NewRequest("POST", "/register", strings.NewReader(`{"username":"alice","password":"alicepw"}`))
	w := httptest.NewRecorder()
	h.Register(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("注册应成功并返回 token: %d %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) == 0 {
		t.Error("应下发 cookie")
	}
	u, err := us.ByUsername("alice")
	if err != nil || u.IsAdmin {
		t.Errorf("应创建普通用户: %+v err=%v", u, err)
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	h, us, set := regFixture(t)
	set.SetAllowRegistration(true)
	us.Create("alice", "h", false)
	req := httptest.NewRequest("POST", "/register", strings.NewReader(`{"username":"alice","password":"alicepw"}`))
	w := httptest.NewRecorder()
	h.Register(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("重名应 409: %d", w.Code)
	}
}

func TestRegister_Status(t *testing.T) {
	h, _, set := regFixture(t)
	w := httptest.NewRecorder()
	h.Status(w, httptest.NewRequest("GET", "/register/status", nil))
	if !strings.Contains(w.Body.String(), `"allowRegistration":false`) {
		t.Errorf("默认应 false: %s", w.Body.String())
	}
	set.SetAllowRegistration(true)
	w = httptest.NewRecorder()
	h.Status(w, httptest.NewRequest("GET", "/register/status", nil))
	if !strings.Contains(w.Body.String(), `"allowRegistration":true`) {
		t.Errorf("开启后应 true: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go vet ./internal/api/v1/`
Expected: 失败（NewRegisterHandler 未定义）。

- [ ] **Step 3: 实现**

`internal/api/v1/register.go`:
```go
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
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run Register -v && go vet ./internal/api/v1/`
Expected: PASS；vet 通过。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/register.go internal/api/v1/register_test.go && git commit -m "feat(api): RegisterHandler（公开注册 + 状态，受开关限制）"
```

---

## Task 7: router 装配 + 全量编译

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: 接线**

在 `internal/api/router.go` 中：

(1) 在 `users := auth.NewUserStore(db)` / `sessions := auth.NewSessionStore(db)` 附近，新增：
```go
	settings := auth.NewSettingsStore(db)
```
(2) 在 `accountH := v1.NewAccountHandler(users, key)` 之后，新增：
```go
	adminH := v1.NewAdminHandler(users, settings)
	registerH := v1.NewRegisterHandler(users, sessions, settings)
```
(3) 在公开端点区（`r.Post("/api/v1/setup", setupH.Create)` 之后）新增公开注册端点：
```go
	r.Get("/api/v1/register/status", registerH.Status)
	r.Post("/api/v1/register", registerH.Register)
```
(4) 在 `/api/v1` 路由组内部（已 `r.Use(middleware.SessionAuth(...))`），在 account 端点之后，新增 admin 子组：
```go
		r.Route("/admin", func(r chi.Router) {
			r.Use(middleware.RequireAdmin)
			r.Get("/users", adminH.ListUsers)
			r.Post("/users", adminH.CreateUser)
			r.Delete("/users/{id}", adminH.DeleteUser)
			r.Post("/users/{id}/password", adminH.ResetPassword)
			r.Post("/users/{id}/role", adminH.SetRole)
			r.Get("/settings", adminH.GetSettings)
			r.Post("/settings", adminH.SetSettings)
		})
```

- [ ] **Step 2: 全量编译 + 测试**

Run:
```bash
cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && gofmt -l internal/api/router.go && go build ./... && go test ./...
```
Expected: gofmt -l 无输出；build 成功；全部包 PASS。

- [ ] **Step 3: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/router.go && git commit -m "feat(api): router 装配 admin 组 + register 端点 + settings store"
```

---

## Task 8: 前端 —— 注册入口 + 管理员用户管理 + boot 识别角色

**Files:**
- Modify: `web/src/api/client.ts`
- Create: `web/src/components/RegisterView.vue`, `web/src/components/UserManagement.vue`
- Modify: `web/src/components/LoginView.vue`, `web/src/components/LibraryShell.vue`, `web/src/App.vue`

> 前端无单测；以 `make build-frontend` + `go build ./...` 验证。先读 `web/src/api/client.ts`（`request` 私有方法、`login`/`setup` 如何用 `auth:false`、`getMe` 现状）、`web/src/App.vue`（boot、handleLogin/handleSetup、模板优先级、`showSettings`/`AccountSettings` 接法）、`web/src/components/LoginView.vue`、`web/src/components/LibraryShell.vue`（`@logout`/`@open-settings` 如何 emit）。

- [ ] **Step 1: client.ts 新增方法**

在 `ApiClient` 类中按既有 `request` 风格新增（`register`/`getRegisterStatus` 用 `auth: false`，其余需鉴权）：
```ts
  // 自助注册
  getRegisterStatus(): Promise<{ allowRegistration: boolean }> {
    return this.request<{ allowRegistration: boolean }>('/api/v1/register/status', { auth: false })
  }

  async register(username: string, password: string): Promise<string> {
    const data = await this.request<{ token: string }>('/api/v1/register', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
      headers: { 'Content-Type': 'application/json' },
      auth: false,
    })
    this.token = data.token
    return data.token
  }

  // 管理员：用户管理
  listUsers(): Promise<{ users: AdminUser[] }> {
    return this.request<{ users: AdminUser[] }>('/api/v1/admin/users', { method: 'GET' })
  }
  createUser(username: string, password: string, isAdmin: boolean): Promise<void> {
    return this.request<void>('/api/v1/admin/users', {
      method: 'POST',
      body: JSON.stringify({ username, password, isAdmin }),
      headers: { 'Content-Type': 'application/json' },
    })
  }
  deleteUser(id: string): Promise<void> {
    return this.request<void>(`/api/v1/admin/users/${id}`, { method: 'DELETE' })
  }
  resetUserPassword(id: string, password: string): Promise<void> {
    return this.request<void>(`/api/v1/admin/users/${id}/password`, {
      method: 'POST',
      body: JSON.stringify({ password }),
      headers: { 'Content-Type': 'application/json' },
    })
  }
  setUserRole(id: string, isAdmin: boolean): Promise<void> {
    return this.request<void>(`/api/v1/admin/users/${id}/role`, {
      method: 'POST',
      body: JSON.stringify({ isAdmin }),
      headers: { 'Content-Type': 'application/json' },
    })
  }
  getAdminSettings(): Promise<{ allowRegistration: boolean }> {
    return this.request<{ allowRegistration: boolean }>('/api/v1/admin/settings', { method: 'GET' })
  }
  setAdminSettings(allowRegistration: boolean): Promise<void> {
    return this.request<void>('/api/v1/admin/settings', {
      method: 'POST',
      body: JSON.stringify({ allowRegistration }),
      headers: { 'Content-Type': 'application/json' },
    })
  }
```
并在文件的类型导出区新增：
```ts
export type AdminUser = {
  id: string
  username: string
  isAdmin: boolean
  hasSubsonicPassword: boolean
  createdAt: string
}
```
> 若 `request` 的选项类型不含 `auth` 字段，则它已存在（`login` 在用 `auth:false`）；`GET` 无需 `method` 时按 `login`/`getMe` 的既有写法保持一致。

- [ ] **Step 2: RegisterView.vue**

`web/src/components/RegisterView.vue`（结构同 `SetupView`，标题改「注册账号」，含「返回登录」入口）：
```vue
<template>
  <div class="login-screen">
    <form class="login-panel login-form" @submit.prevent="submit">
      <h1>注册账号</h1>
      <p class="hint">创建一个普通用户账号。</p>
      <input class="custom-input" v-model="username" placeholder="用户名" autocomplete="username" />
      <input class="custom-input" v-model="password" type="password" placeholder="密码（至少 4 位）" autocomplete="new-password" />
      <input class="custom-input" v-model="confirm" type="password" placeholder="确认密码" autocomplete="new-password" />
      <p v-if="displayError" class="custom-alert">{{ displayError }}</p>
      <button class="custom-btn-primary" type="submit" :disabled="loading">{{ loading ? '注册中…' : '注册' }}</button>
      <button class="link-btn" type="button" @click="$emit('back')">返回登录</button>
    </form>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
const props = defineProps<{ loading: boolean; error: string }>()
const emit = defineEmits<{ (e: 'register', payload: { username: string; password: string }): void; (e: 'back'): void }>()
const username = ref('')
const password = ref('')
const confirm = ref('')
const localError = ref('')
const displayError = computed(() => localError.value || props.error)
function submit() {
  localError.value = ''
  if (!username.value || password.value.length < 4) {
    localError.value = '用户名不能为空，密码至少 4 位'
    return
  }
  if (password.value !== confirm.value) {
    localError.value = '两次密码不一致'
    return
  }
  emit('register', { username: username.value, password: password.value })
}
</script>

<style scoped>
.hint { color: var(--text-muted, #888); font-size: 13px; margin: 0; }
.link-btn { background: none; border: none; color: var(--text-muted, #888); cursor: pointer; font-size: 13px; }
</style>
```
> 复用 LoginView 已有的全局类（`login-screen`/`login-panel`/`login-form`/`custom-input`/`custom-alert`/`custom-btn-primary`）。若 LoginView 用的类名不同，按其实际类名对齐。

- [ ] **Step 3: UserManagement.vue**

`web/src/components/UserManagement.vue`：
```vue
<template>
  <div class="user-mgmt">
    <header class="um-header">
      <h2>用户管理</h2>
      <button class="link-btn" @click="$emit('close')">返回</button>
    </header>

    <section class="um-reg-toggle">
      <label>
        <input type="checkbox" :checked="allowRegistration" @change="toggleReg($event)" />
        允许自助注册（开启后访客可在登录页注册普通账号）
      </label>
    </section>

    <section class="um-create">
      <h3>新建用户</h3>
      <input class="custom-input" v-model="newUsername" placeholder="用户名" />
      <input class="custom-input" v-model="newPassword" type="password" placeholder="初始密码（至少 4 位）" />
      <label><input type="checkbox" v-model="newIsAdmin" /> 管理员</label>
      <button class="custom-btn-primary" :disabled="busy" @click="create">创建</button>
    </section>

    <table class="um-table">
      <thead><tr><th>用户名</th><th>角色</th><th>Subsonic 密码</th><th>操作</th></tr></thead>
      <tbody>
        <tr v-for="u in users" :key="u.id">
          <td>{{ u.username }}</td>
          <td>{{ u.isAdmin ? '管理员' : '普通' }}</td>
          <td>{{ u.hasSubsonicPassword ? '已设' : '未设' }}</td>
          <td class="um-actions">
            <button @click="toggleRole(u)">{{ u.isAdmin ? '降为普通' : '设为管理员' }}</button>
            <button @click="resetPw(u)">重置密码</button>
            <button class="danger" @click="remove(u)">删除</button>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="msg" :class="msgError ? 'error' : 'ok'">{{ msg }}</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { AdminUser, ApiClient } from '../api/client'
const props = defineProps<{ api: ApiClient }>()
defineEmits<{ (e: 'close'): void }>()
const users = ref<AdminUser[]>([])
const allowRegistration = ref(false)
const newUsername = ref(''); const newPassword = ref(''); const newIsAdmin = ref(false)
const busy = ref(false); const msg = ref(''); const msgError = ref(false)
function show(t: string, e = false) { msg.value = t; msgError.value = e }

async function reload() {
  try {
    users.value = (await props.api.listUsers()).users
    allowRegistration.value = (await props.api.getAdminSettings()).allowRegistration
  } catch (e) { show(e instanceof Error ? e.message : '加载失败', true) }
}
onMounted(reload)

async function toggleReg(ev: Event) {
  const checked = (ev.target as HTMLInputElement).checked
  try { await props.api.setAdminSettings(checked); allowRegistration.value = checked; show('已更新注册开关') }
  catch (e) { show(e instanceof Error ? e.message : '更新失败', true); await reload() }
}
async function create() {
  busy.value = true
  try {
    await props.api.createUser(newUsername.value, newPassword.value, newIsAdmin.value)
    newUsername.value = ''; newPassword.value = ''; newIsAdmin.value = false
    show('用户已创建'); await reload()
  } catch (e) { show(e instanceof Error ? e.message : '创建失败', true) }
  finally { busy.value = false }
}
async function toggleRole(u: AdminUser) {
  try { await props.api.setUserRole(u.id, !u.isAdmin); await reload() }
  catch (e) { show(e instanceof Error ? e.message : '操作失败', true) }
}
async function resetPw(u: AdminUser) {
  const pw = window.prompt(`为 ${u.username} 设置新密码（至少 4 位）`)
  if (!pw) return
  try { await props.api.resetUserPassword(u.id, pw); show('密码已重置') }
  catch (e) { show(e instanceof Error ? e.message : '重置失败', true) }
}
async function remove(u: AdminUser) {
  if (!window.confirm(`确认删除用户 ${u.username}？其书签/续播将一并清除。`)) return
  try { await props.api.deleteUser(u.id); show('已删除'); await reload() }
  catch (e) { show(e instanceof Error ? e.message : '删除失败', true) }
}
</script>

<style scoped>
.user-mgmt { padding: 24px; display: flex; flex-direction: column; gap: 20px; max-width: 760px; }
.um-header { display: flex; align-items: center; justify-content: space-between; }
.um-create { display: flex; flex-direction: column; gap: 10px; max-width: 360px; }
.um-table { width: 100%; border-collapse: collapse; }
.um-table th, .um-table td { text-align: left; padding: 8px; border-bottom: 1px solid var(--border, #333); font-size: 14px; }
.um-actions { display: flex; gap: 8px; }
.um-actions .danger { color: var(--danger, #e5484d); }
.link-btn { background: none; border: none; color: var(--text-muted, #888); cursor: pointer; }
.error { color: var(--danger, #e5484d); }
.ok { color: var(--success, #30a46c); }
</style>
```

- [ ] **Step 4: LoginView.vue 注册入口**

在 `LoginView.vue` 增加一个可选的「注册」入口：新增 prop `allowRegistration: boolean`，在表单底部当其为真时显示一个按钮 emit `register`：
```vue
<!-- 在登录按钮下方追加 -->
<button v-if="allowRegistration" class="link-btn" type="button" @click="$emit('register')">没有账号？注册</button>
```
对应在 `<script setup>` 的 `defineProps` 增加 `allowRegistration: boolean`（给默认 false），`defineEmits` 增加 `(e: 'register'): void`。若 LoginView 现有 props/emits 是对象式或类型式，按其现状对齐添加。补一个 `.link-btn` scoped 样式（background:none;border:none;color:var(--text-muted,#888);cursor:pointer;font-size:13px;）。

- [ ] **Step 5: LibraryShell.vue 管理员入口**

在 `LibraryShell.vue` 的 `.logout-nav-container`（已含账户设置/登出按钮的容器）中，新增一个仅管理员可见的「用户管理」按钮：新增 prop `isAdmin: boolean`（默认 false），按钮 `v-if="isAdmin"` emit `open-users`：
```vue
<button v-if="isAdmin" class="nav-icon-btn" title="用户管理" @click="$emit('open-users')">
  <!-- 复用现有按钮的 svg/类名风格，放一个“用户组”图标或文字“用户” -->
  用户
</button>
```
在 `defineProps` 增加 `isAdmin: boolean`，`defineEmits` 增加 `(e: 'open-users'): void`。样式/类名沿用该容器内既有按钮。

- [ ] **Step 6: App.vue 装配**

在 `web/src/App.vue`：

(1) import：
```ts
import RegisterView from './components/RegisterView.vue'
import UserManagement from './components/UserManagement.vue'
```
并从 `./api/client` 的类型导入处补上 `AdminUser` 不是必须（App 不直接用）。

(2) 新增 refs：
```ts
const currentUser = ref<{ username: string; isAdmin: boolean } | null>(null)
const allowRegistration = ref(false)
const showRegister = ref(false)
const registerLoading = ref(false)
const registerError = ref('')
const showUsers = ref(false)
```

(3) `loadInitialData()` 末尾追加加载当前用户（覆盖 login/setup/register/refresh 所有已登录路径）：
```ts
async function loadInitialData() {
  await Promise.all([loadAlbums(), loadArtists(), loadScanStatus()])
  try { currentUser.value = await api.getMe() } catch { currentUser.value = null }
}
```

(4) `boot()` 的未登录分支（`try { const response = await api.listAlbums() ... }` 之前）加载注册开关：
```ts
  try { allowRegistration.value = (await api.getRegisterStatus()).allowRegistration } catch { allowRegistration.value = false }
```

(5) 新增 `handleRegister`（仿 `handleLogin`）：
```ts
async function handleRegister(payload: { username: string; password: string }) {
  registerLoading.value = true
  registerError.value = ''
  try {
    const nextToken = await api.register(payload.username, payload.password)
    tokenStorage.save(nextToken)
    token.value = nextToken
    showRegister.value = false
    await loadInitialData()
  } catch (error) {
    registerError.value = messageFromError(error)
  } finally {
    registerLoading.value = false
  }
}
```

(6) `logout()` 内追加清理：
```ts
  currentUser.value = null
  showUsers.value = false
  showRegister.value = false
```

(7) 模板调整：在登录分支处理注册视图，并把 LibraryShell 接上 `isAdmin`/`open-users` 与 UserManagement 面板。把现有 `<LoginView v-else-if="showLogin" .../>` 改为：
```vue
    <RegisterView
      v-else-if="showLogin && showRegister"
      :loading="registerLoading"
      :error="registerError"
      @register="handleRegister"
      @back="showRegister = false"
    />
    <LoginView
      v-else-if="showLogin"
      :loading="loginLoading"
      :error="loginError"
      :allow-registration="allowRegistration"
      @login="handleLogin"
      @register="showRegister = true"
    />
```
在 `<LibraryShell ...>` 标签上追加：
```vue
      :is-admin="currentUser?.isAdmin ?? false"
      @open-users="showUsers = true"
```
在 LibraryShell 内容区（与 `AccountSettings` 同级）追加面板，并让它与账户设置互斥优先展示：
```vue
      <UserManagement
        v-if="showUsers"
        :api="api"
        @close="showUsers = false"
      />
```
并把后续主内容的 `v-if="!showSettings && ..."` 同步改为 `v-if="!showSettings && !showUsers && ..."`（搜索面板与各 mode 面板的首个条件加上 `&& !showUsers`），避免用户管理面板与主内容叠加。AccountSettings 的 `v-if="showSettings"` 保持不变；打开用户管理时也应关闭账户设置：把 `@open-users="showUsers = true"` 改为 `@open-users="showUsers = true; showSettings = false"`，并在打开账户设置处 `@open-settings="showSettings = true; showUsers = false"`。

- [ ] **Step 7: 构建验证**

Run:
```bash
cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && make build-frontend && go build ./...
```
Expected: 前端无 TS 错误、产物入 `ui/dist`；Go 构建通过。修掉任何类型错误（方法签名、缺失 import、prop 类型）。

- [ ] **Step 8: 提交**

```bash
cd /home/yxx/develop/Lyra && git add web ui/dist && git commit -m "feat(web): 注册入口 + 管理员用户管理面板 + boot 识别角色"
```

---

## Self-Review（计划自检）

- **Spec 覆盖**：app_settings + SettingsStore(T1/T2) ✓；UserStore List/Delete/UpdateRole/AdminCount(T3) ✓；RequireAdmin(T4) ✓；admin 端点列/建/删/重置/升降级/设置(T5) ✓；护栏删自己/删/降最后管理员(T5) ✓；公开注册 + status + 受开关限制 + 自动登录(T6) ✓；router 装配(T7) ✓；前端 getMe/currentUser + RegisterView + UserManagement + 登录页注册入口 + LibraryShell 管理员入口(T8) ✓；删用户级联清理(T3 测试验证 + FK 既有) ✓；角色即时生效(SessionAuth 既有行为，T5 升降级后 ByID 重载) ✓。
- **占位符**：T5 测试中标注的两处需精简的用例（空的 `TestAdmin_NormalUserForbidden`、`TestAdmin_DeleteSelfAndLastAdminBlocked` 的无效中间步骤）已在该步文字中明确要求删除/精简并给出最终断言；无 TODO/TBD。
- **类型一致**：`NewAdminHandler(users, settings)`、`NewRegisterHandler(users, sessions, settings)`、`NewSettingsStore(db)` 跨 T2/T5/T6/T7 一致；`UserSummary` JSON 字段（id/username/isAdmin/hasSubsonicPassword/createdAt）与前端 `AdminUser` 类型一致；`AllowRegistration()`/`SetAllowRegistration(bool)` 跨 store/handler 一致；`writeJSON`/`writeJSONError`/`setAuthCookie`/`sessionTTL` 复用 v1/auth.go；`RequireAdmin`/`UserFromContext`/`writeUnauthorized` 复用 middleware 包。
- **已知约束**：admin 子组用 chi 嵌套 `r.Route("/admin", …)`，外层 `/api/v1` 已有 `SessionAuth`，内层叠加 `RequireAdmin`；register 与 register/status 注册在 `/api/v1` 组外（免认证）。
