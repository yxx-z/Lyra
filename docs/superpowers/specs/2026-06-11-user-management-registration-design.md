# 用户管理 + 注册 设计文档（多用户 auth 子项目 2）

> 版本：1.0 · 日期：2026-06-11 · 状态：已批准

---

## 背景

多用户改造拆为两个子项目：

- **子项目 1（已完成）· 认证地基 + 首次启动引导**：`users`/`sessions` 表、bcrypt 登录密码、AES-GCM 加密的独立 Subsonic 密码、随机令牌会话、Web 引导页、per-user 书签/队列。spec：`2026-06-11-multiuser-auth-foundation-design.md`。
- **子项目 2（本文档）· 用户管理 + 注册**：在地基之上补齐多用户的「管理」面：管理员后台增删用户、重置他人密码、角色升降级，以及 DB 存储、可运行时切换的「允许自助注册」开关与公开注册端点。

地基已就绪的可复用件：`auth.UserStore`（Count/Create(username,hash,isAdmin)/ByUsername/ByID/FirstAdmin/UpdatePassword/UpdateSubsonicPW）、`auth.User{ID,Username,PasswordHash,SubsonicPW,IsAdmin}`、`auth.SessionStore`、`middleware.SessionAuth` + `middleware.UserFromContext`、v1 的 `writeJSON`/`writeJSONError`/`setAuthCookie`/`sessionTTL`、`SetupHandler` 自动登录范式、前端 `ApiClient.getMe()`（已实现但尚未在 `App.vue` 中调用）。

---

## 范围

**做**：

- 管理员专属用户管理端点：列出 / 新建 / 删除用户、重置他人密码、提升/降级角色（admin ↔ 普通）。
- `middleware.RequireAdmin`：在 `SessionAuth` 之后校验当前用户 `is_admin`。
- DB 键值设置表 `app_settings` + `SettingsStore`，承载「允许自助注册」开关（管理员后台运行时切换）。
- 公开注册端点（受开关限制）+ 注册状态查询端点。
- 安全护栏：不能删除/降级最后一个管理员；不能删除自己。
- 前端：`boot()` 接入 `getMe` 识别当前用户角色；`RegisterView`（开关允许时登录页显示注册入口）；`UserManagement` 管理员面板；`LibraryShell` 给管理员加「用户管理」入口。

**不做**（YAGNI）：

- 比 admin/普通 更细的权限分级（沿用二元 `is_admin`）。
- 邮件邀请、邮箱验证、找回密码、首次登录强制改密。
- 每用户独立资料库视图（所有用户共享同一音乐库，仅 per-user 书签/队列/未来收藏隔离 —— 与子项目 1 一致）。

---

## 角色模型

二元：`is_admin = 1` 为管理员，`0` 为普通用户。普通用户拥有除「用户管理 / 设置」外的全部能力（浏览、播放、书签、续播、改自己登录密码、设自己 Subsonic 密码）。仅 `/api/v1/admin/*` 与设置开关受 `RequireAdmin` 限制。角色变更即时生效：`SessionAuth` 每个请求都用 `ByID` 重新载入用户，无需重登。

---

## 数据模型（新迁移 `007`）

`internal/db/migrations/007_app_settings.up.sql`，并同步 `internal/db/schema.sql`：

```sql
CREATE TABLE app_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

极简键值表，便于将来扩展其它运行时设置。当前唯一键：`allow_registration`，值 `"1"` 表示开放，缺省（无行）或 `"0"` 表示关闭。

`SettingsStore`（`internal/auth/settings.go`，与其它 store 同包）：

- `Get(key string) (string, bool)`：查不到返回 `("", false)`。
- `Set(key, value string) error`：upsert（`ON CONFLICT(key) DO UPDATE`）。
- `AllowRegistration() bool`：`Get("allow_registration")` == `"1"`。
- `SetAllowRegistration(bool) error`：写 `"1"`/`"0"`。

`UserStore` 新增方法：

- `List() ([]UserSummary, error)`：返回 `UserSummary{ID, Username, IsAdmin bool, HasSubsonicPassword bool, CreatedAt string}`（不含密码哈希/密文）。SQL：`SELECT id, username, is_admin, subsonic_pw IS NOT NULL AND length(subsonic_pw)>0, created_at FROM users ORDER BY created_at, id`。
- `Delete(id string) error`：`DELETE FROM users WHERE id=?`（sessions/bookmarks/play_queue 经 FK `ON DELETE CASCADE` 自动清理）。
- `UpdateRole(id string, isAdmin bool) error`：`UPDATE users SET is_admin=?, updated_at=datetime('now') WHERE id=?`。
- `AdminCount() (int, error)`：`SELECT COUNT(*) FROM users WHERE is_admin=1`，用于「最后一个管理员」保护。

---

## 后端端点

### 管理员专属（`/api/v1/admin` 组，链 `SessionAuth` → `RequireAdmin`）

新中间件 `middleware.RequireAdmin`：取 `UserFromContext`，无用户 → 401（理论上 SessionAuth 已挡），非 admin → 403 JSON `{"error":"需要管理员权限"}`。

`AdminHandler`（`internal/api/v1/admin.go`，持 `*auth.UserStore` 与 `*auth.SettingsStore`）：

- `GET /admin/users` → `{"users":[UserSummary...]}`。
- `POST /admin/users` body `{username, password, isAdmin}`：校验 `username` 非空、`password` ≥4；bcrypt 后 `users.Create`；用户名占用 → 409；成功 → `{id,username,isAdmin}`。新用户 `subsonic_pw` 为空（其本人后续在账户设置里设）。
- `DELETE /admin/users/{id}`：取当前用户；若 `id` == 当前用户 → 400「不能删除自己」；若目标是 admin 且 `AdminCount()==1` → 400「不能删除最后一个管理员」；否则 `users.Delete(id)`。删不存在的也按成功（幂等）。
- `POST /admin/users/{id}/password` body `{password}`：校验 ≥4；目标存在性由 `ByID` 检查（不存在 → 404）；`UpdatePassword`。管理员特权，免旧密码。
- `POST /admin/users/{id}/role` body `{isAdmin}`：`ByID` 检查存在（404）；若把 admin 降为普通且 `AdminCount()==1` → 400「不能降级最后一个管理员」；否则 `UpdateRole`。
- `GET /admin/settings` → `{"allowRegistration": bool}`。
- `POST /admin/settings` body `{allowRegistration: bool}` → `SetAllowRegistration` → `{ok:true}`。

### 公开（免认证）

`RegisterHandler`（`internal/api/v1/register.go`，持 `*auth.UserStore`、`*auth.SessionStore`、`*auth.SettingsStore`）：

- `GET /register/status` → `{"allowRegistration": bool}`（登录页据此显示/隐藏注册入口）。
- `POST /register` body `{username, password}`：
  - `SettingsStore.AllowRegistration()` 为 false → 403「未开放注册」。
  - 校验 `username` 非空、`password` ≥4。
  - 用户名占用 → 409。
  - 否则建**普通用户**（`is_admin=0`），创建 session、下发 cookie、返回 `{token}`（自动登录，与 setup 一致）。

注：register 与 admin-create-user 都需要「建用户」+「占用检查」。`users.Create` 在用户名冲突时由 UNIQUE 约束返回错误，处理时映射为 409。

---

## 前端

### boot 接入角色

`App.vue`：新增 `currentUser` ref（`{username, isAdmin} | null`）。`boot()` 在确认已登录（token 有效）后调 `api.getMe()` 填充 `currentUser`；登出时清空。

### 注册入口

- `GET /register/status` 在 `boot()`（未登录、非首次引导分支）时查询，存 `allowRegistration` ref。
- `LoginView` 在 `allowRegistration` 为真时显示「注册」切换链接；点开渲染 `RegisterView.vue`（仿 `SetupView`：用户名 + 密码 + 确认，本地校验，提交 `api.register(...)` 自动登录）。

### 用户管理面板

- `UserManagement.vue`（仅管理员）：
  - 用户表格：用户名、角色（管理员/普通）、是否已设 Subsonic 密码、操作列。
  - 操作：删除（带二次确认；自己/最后管理员的按钮禁用或后端报错提示）、重置密码（弹输入新密码）、升/降级切换。
  - 新建用户表单：用户名 + 初始密码 + 是否管理员。
  - 顶部「允许自助注册」开关（读 `GET /admin/settings`，切换 `POST /admin/settings`）。
- `LibraryShell` 在 `currentUser.isAdmin` 为真时显示「用户管理」入口（在「账户设置」旁），由 `App.vue` 切换显示 `UserManagement`。

### `ApiClient` 新增方法（`web/src/api/client.ts`）

- `register(username,password): Promise<string>`（返回 token，`auth:false`）、`getRegisterStatus(): Promise<{allowRegistration:boolean}>`（`auth:false`）。
- `listUsers()`、`createUser(username,password,isAdmin)`、`deleteUser(id)`、`resetUserPassword(id,password)`、`setUserRole(id,isAdmin)`、`getAdminSettings()`、`setAdminSettings(allowRegistration)`（均需鉴权）。

---

## 代码落点

```
internal/db/migrations/007_app_settings.up.sql   新迁移
internal/db/schema.sql                            改：加 app_settings
internal/auth/settings.go                         新：SettingsStore
internal/auth/settings_test.go
internal/auth/users.go                            改：List/Delete/UpdateRole/AdminCount + UserSummary
internal/auth/users_test.go                       改：新方法测试
internal/api/middleware/admin.go                  新：RequireAdmin
internal/api/middleware/admin_test.go
internal/api/v1/admin.go                          新：AdminHandler（用户/设置端点）
internal/api/v1/admin_test.go
internal/api/v1/register.go                       新：RegisterHandler
internal/api/v1/register_test.go
internal/api/router.go                            改：装配 settings store、admin 组、register 端点
web/src/api/client.ts                             改：新增 register/admin 方法
web/src/components/RegisterView.vue               新
web/src/components/UserManagement.vue             新
web/src/components/LoginView.vue                  改：注册入口
web/src/components/LibraryShell.vue               改：管理员「用户管理」入口
web/src/App.vue                                    改：getMe + currentUser + 注册/用户管理 装配
```

---

## 测试策略

| 测试 | 方式 |
|------|------|
| 迁移 007 可执行、schema 一致 | `go test ./internal/db/...` |
| SettingsStore：缺省 false、Set 后读到、AllowRegistration 往返 | 内存 sqlite |
| UserStore.List 返回各字段（HasSubsonicPassword 正确） | seed 两用户，其一设 Subsonic 密码 |
| UserStore.Delete + 级联（删用户后其 session/bookmark 消失） | 内存 sqlite |
| UserStore.UpdateRole / AdminCount | 内存 sqlite |
| RequireAdmin：admin 放行、普通 403、无用户 401 | httptest，用 SessionAuth 包裹 |
| POST /admin/users：建普通/管理员、用户名占用 409、短密码 400 | httptest |
| DELETE /admin/users/{id}：正常删；删自己 400；删最后管理员 400 | httptest |
| POST /admin/users/{id}/password：重置成功、目标不存在 404 | httptest |
| POST /admin/users/{id}/role：升降级；降最后管理员 400 | httptest |
| GET/POST /admin/settings：读默认 false、切换后读到 true | httptest |
| 角色即时生效：降级后该用户再访问 admin 端点得 403 | httptest（重用 session） |
| GET /register/status 反映开关 | httptest |
| POST /register：开关关 403、开关开建普通用户 + 自动登录、用户名占用 409 | httptest |
| 普通用户访问 /admin/* → 403 | httptest |

全部 httptest + 内存 sqlite，不打网络。前端不强制单测，依赖构建 + 真实环境验证（见记忆 `verify-real-playback-early`：docker + 浏览器实测管理员面板与注册流程）。

---

## 不在本次范围内

- 更细的权限/角色分级。
- 邮件邀请、邮箱验证、找回密码、强制改密。
- 每用户独立资料库视图。
