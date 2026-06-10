# 多用户认证地基 + 首次启动引导 设计文档

> 版本：1.0 · 日期：2026-06-11 · 状态：已批准

---

## 背景与目标

当前 Lyra 是**完全单用户**且**凭据明文写在 `config.yaml`**：

- `auth.username/password/token`、`subsonic.password` 都是明文，Web 登录做明文比较，Subsonic 用 `md5(password+salt)` 校验。
- 没有 `users` 表；`bookmarks`、`play_queue` 都是全局单行。
- 部署时用户须手写整个 `config.yaml`，无任何引导。

目标是把项目升级为**真正的多用户**架构。整体被拆为两个子项目：

- **子项目 1（本文档）· 认证地基 + 首次启动引导**
- **子项目 2（后续独立 spec）· 用户管理 + 注册**：管理员后台新增/删除用户、重置密码、角色细分、自助注册开关。

本文档只覆盖子项目 1。

---

## 范围

**做**：

- `users` / `sessions` 两张新表。
- 登录密码 bcrypt 单向哈希。
- 独立的 Subsonic 密码，AES-256-GCM 可逆加密存库（Subsonic 令牌认证须拿到密码原文，bcrypt 不可行）。
- 随机令牌会话（sessions 表，可撤销）。
- Web 首次启动引导页：`users` 表为空时在浏览器创建管理员。
- Web 登录改为查库（bcrypt 校验）。
- Subsonic `authenticate` 改为按用户校验。
- `bookmarks` / `play_queue` 重建为 per-user。
- 现有部署升级时，旧全局 bookmarks/play_queue 自动认领给首个创建的管理员。
- 最小账户设置页：登录用户可**改登录密码**、**设 Subsonic 密码**（不设则无法使用 Symfonium 等客户端）。

**不做**（留给子项目 2）：新增/删除其他用户、角色权限细分、自助注册开关。`users` 表预留 `is_admin` 字段，子项目 2 只补 UI 与端点。

---

## 数据模型（新迁移 `006`）

`internal/db/migrations/006_users_sessions.up.sql`，并同步更新 `internal/db/schema.sql`。

```sql
-- 用户
CREATE TABLE users (
    id            TEXT PRIMARY KEY,      -- UUID
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,         -- bcrypt
    subsonic_pw   BLOB,                  -- AES-256-GCM 加密的 Subsonic 密码原文；未设为 NULL
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 会话（随机令牌，可撤销）
CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,         -- 随机 32 字节 hex
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
```

`bookmarks` 与 `play_queue` **重建**以加入 `user_id`（可空，便于旧数据迁移；FK `ON DELETE CASCADE`）：

```sql
-- bookmarks：主键从 track_id 改为 (user_id, track_id)，允许多用户各自书签同一曲
CREATE TABLE bookmarks_new (
    user_id    TEXT REFERENCES users(id) ON DELETE CASCADE,
    track_id   TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, track_id)
);
INSERT INTO bookmarks_new (user_id, track_id, position, comment, created_at, updated_at)
    SELECT NULL, track_id, position, comment, created_at, updated_at FROM bookmarks;
DROP TABLE bookmarks;
ALTER TABLE bookmarks_new RENAME TO bookmarks;

-- play_queue：从单行 id=1 改为每用户一行
CREATE TABLE play_queue_new (
    user_id    TEXT UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    track_ids  TEXT NOT NULL DEFAULT '',
    current    TEXT NOT NULL DEFAULT '',
    position   INTEGER NOT NULL DEFAULT 0,
    changed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    changed_by TEXT NOT NULL DEFAULT ''
);
INSERT INTO play_queue_new (user_id, track_ids, current, position, changed_at, changed_by)
    SELECT NULL, track_ids, current, position, changed_at, changed_by FROM play_queue WHERE id = 1;
DROP TABLE play_queue;
ALTER TABLE play_queue_new RENAME TO play_queue;
```

**说明**：

- `user_id` 设为可空，迁移时旧行写 `NULL`。FK 仅校验非 NULL 值，故 NULL 行合法存在。首个管理员创建时做认领（见下）。之后所有写入都带非空 `user_id`。
- `play_queue.user_id UNIQUE` 而非 PRIMARY KEY：SQLite 对非整型 PK 允许 NULL 的行为不稳健，用 UNIQUE 更可靠（UNIQUE 允许多个 NULL，但迁移只产生一行 NULL，且很快被认领）。
- 迁移内若涉及外键，遵循项目既有 `_pragma=foreign_keys=ON` 约定；表重建顺序为先建新表、拷数据、删旧表、改名。

**加密主密钥**：首次启动时若 `data/secret.key` 不存在则生成 32 字节随机密钥并以 `0600` 写入（路径取 `database.path` 同级目录下的 `secret.key`，避免新增 config 项）。密钥不进 `config.yaml`、不进库。丢失则所有已存 Subsonic 密码无法解密，用户需在设置页重设（可接受的降级）。

---

## 认证与会话（后端）

### Web 登录 / 会话

- `POST /api/v1/auth/login` `{username, password}`：按 `username` 查 `users`，bcrypt 校验 `password_hash`。通过则生成随机令牌、插入 `sessions`（`expires_at` = now + 会话有效期，默认 30 天），下发 `HttpOnly` cookie（沿用 `lyra_auth`，值改为会话令牌）。
- `POST /api/v1/auth/logout`：删除当前会话行 + 清 cookie。
- `GET /api/v1/auth/session`：刷新（延长 `expires_at`）当前会话。
- `GET /api/v1/auth/me`：返回当前用户 `{username, isAdmin}`，前端据此渲染。
- **会话中间件**：读 cookie 令牌 → 查 `sessions` 未过期行 → 载入 `users` 行 → 放入 request context（供下游取当前用户）。替换现有「与静态 config token 比较」的逻辑。

### Subsonic 认证

`authenticate` 改为：按 `q.Get("u")` 查 `users` 行 → 解密 `subsonic_pw`（NULL 即未设 → 认证失败 code 40）→ 比对明文 `p`（含 `enc:` 十六进制解码）或 `md5(plainpw+salt) == t`。返回当前用户供下游 per-user 查询使用。`subsonic.enabled=false` 时仍直接拒绝（code 40）。

### 现有 per-user 端点改造

`bookmarks.go`（createBookmark/getBookmarks/deleteBookmark/savePlayQueue/getPlayQueue）全部加 `WHERE user_id = ?` / 写入当前用户 id。`username` 字段改填当前用户的 `username`（不再是 `cfg.Auth.Username`）。

---

## 首次启动引导

- `GET /api/v1/setup/status` → `{needsSetup: bool}`（`SELECT COUNT(*) FROM users = 0` 即 true）。此端点**免认证**。
- `POST /api/v1/setup` `{username, password}`：**仅当 users 表为空时允许**，否则返回 409。流程：建管理员（`is_admin=1`，bcrypt 哈希密码，`subsonic_pw=NULL`）→ 在同一事务内认领孤儿数据 `UPDATE bookmarks SET user_id=? WHERE user_id IS NULL`、`UPDATE play_queue SET user_id=? WHERE user_id IS NULL` → 自动建 session 并下发 cookie（直接登录）。此端点免认证（靠「表为空」自我保护）。
- **前端**：应用载入时先 `GET /api/v1/setup/status`；`needsSetup` 为真则渲染 `SetupView`（创建管理员表单），否则渲染 `LoginView`。

**现有部署升级路径**：升级后 `users` 表为空 → 视为首次启动 → 走引导页重建管理员。迁移已把旧全局 bookmarks/play_queue 置 `user_id=NULL`，引导时被新管理员认领，续播数据不丢。原 `config.yaml` 里的 `admin/admin` 不再生效；**Symfonium 等客户端须用新设置的 Subsonic 密码重新添加**。

---

## 账户设置（最小）

登录用户可在设置页：

- **改登录密码** `POST /api/v1/account/password` `{oldPassword, newPassword}`：校验旧密码 → 更新 bcrypt 哈希。
- **设 Subsonic 密码** `POST /api/v1/account/subsonic-password` `{password}`：AES-256-GCM 加密后写 `subsonic_pw`。

前端新增最小「账户设置」面板承载这两项。完整的「管理其他用户」属子项目 2。

---

## config 变更

- **退役**（不再作为凭据来源）：`auth.username`、`auth.password`、`auth.token`、`subsonic.password`。保留字段于结构体以兼容旧文件解析，但 Load 不再据其建凭据。
- **保留**：`subsonic.enabled`（开关）；`auth.disable`——置真时绕过会话中间件，以**首个管理员**身份处理所有请求（供纯局域网 kiosk）。
- 其余 `server/library/database/cache/scraper/transcode` 不变。
- **普通用户的 `config.yaml` 升级后无需改动即可启动**，仅首次需走浏览器引导页。

---

## 代码落点

```
internal/db/migrations/006_users_sessions.up.sql   新迁移（users/sessions + 重建 bookmarks/play_queue）
internal/db/schema.sql                              改：同步 4 表
internal/auth/                                       新包：bcrypt 哈希、AES-GCM 加解密、secret.key 加载、users/sessions 仓储
internal/api/v1/auth.go                              改：login/logout/session/me 查库 + 会话中间件
internal/api/v1/setup.go                             新：setup/status + setup（建首管理员 + 认领孤儿数据）
internal/api/v1/account.go                           新：改登录密码 + 设 Subsonic 密码
internal/api/subsonic/auth.go                        改：按用户校验 + 解密比对
internal/api/subsonic/bookmarks.go                   改：全部端点加 user_id 过滤 / 写入当前用户
internal/api/router.go                               改：装配 auth 包、会话中间件、新端点
internal/config/config.go                            改：退役凭据字段的使用（保留解析兼容）
web/src/components/SetupView.vue                      新：首次引导创建管理员
web/src/components/AccountSettings.vue                新：改密码 + 设 Subsonic 密码
web/src/components/LoginView.vue                      改：对接新登录（基本不变）
web/src/...（应用入口）                                改：载入先查 setup/status 决定渲染 Setup/Login
```

---

## 测试策略

| 测试 | 方式 |
|------|------|
| 迁移 006 可执行、schema 一致 | `go test ./internal/db/...`（内存 sqlite 跑全部迁移） |
| 旧全局 bookmarks/play_queue 迁移后为 `user_id IS NULL` | 迁移前 seed 数据，迁移后断言 |
| bcrypt 哈希：建用户后校验对/错密码 | auth 包单测 |
| 用户名唯一约束 | 插入重复 → 报错 |
| subsonic_pw AES-GCM 加解密往返 | 加密后解密等于原文；密钥不同则失败 |
| secret.key 不存在→生成、存在→复用 | 临时目录单测 |
| sessions 建/查/过期/删 | 过期行不通过中间件 |
| setup/status 反映表空与非空 | httptest |
| POST setup 建管理员 + 认领孤儿数据 + 自动登录 | httptest + 内存 sqlite，验 bookmarks.user_id 被填、cookie 下发 |
| 二次 POST setup（表非空）→ 409 | httptest |
| Web 登录：对/错密码、cookie 下发、me 返回 | httptest |
| 会话中间件：有效/过期/无 cookie | httptest |
| Subsonic per-user：`p` 路径、`t(md5)` 路径、未知用户、未设 Subsonic 密码 | httptest |
| 改登录密码：旧密码错→拒绝；对→更新 | httptest |
| 设 Subsonic 密码后可用该密码过 Subsonic 认证 | httptest 端到端 |
| per-user 隔离：用户 A 的书签不出现在用户 B 的 getBookmarks | httptest（seed 两用户） |

全部 httptest + 内存 sqlite，不打网络。前端改动按项目惯例不强制单测，依赖真实环境验证（见 `verify-real-playback-early` 记忆：构建 docker + 浏览器/Symfonium 实测引导页与续播）。

---

## 不在本次范围内（子项目 2 及以后）

- 管理员后台：新增/删除其他用户、重置他人密码、角色细分。
- 自助注册开关。
- 每用户独立的资料库视图（当前所有用户共享同一音乐库，仅 per-user 的书签/队列/未来收藏隔离）。
- 收藏（star/rating）、播放列表、播放统计——各自独立 spec，届时天然按 user_id 建。
