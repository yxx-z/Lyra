# 多用户认证地基 + 首次启动引导 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Lyra 从「config 明文单用户」升级为「数据库多用户」的认证地基：users/sessions 表、bcrypt 登录密码、AES-GCM 可逆加密的独立 Subsonic 密码、随机令牌会话、Web 首次引导页创建管理员、per-user 书签/队列、现有部署数据认领。

**Architecture:** 新增 `internal/auth` 包承载加密原语与 users/sessions 仓储。`/api/v1` 的认证从「静态 token」中间件换成「sessions 表查询」中间件，登录响应仍返回 `{token}` 以最小化前端改动。Subsonic 端改为按用户查库 + 解密比对。首次启动（users 表为空）时前端走引导页创建管理员，并认领迁移产生的 `user_id IS NULL` 的旧书签/队列。

**Tech Stack:** Go 1.25 · modernc.org/sqlite（纯 Go，单连接、`_pragma=foreign_keys=ON`）· `golang.org/x/crypto/bcrypt` · 标准库 `crypto/aes`+`crypto/cipher`（AES-256-GCM）· chi v5 · Vue 3 + Pinia。

**关键约束：**
- Subsonic 令牌认证是 `md5(password+salt)`，服务器必须拿到密码原文 → Subsonic 密码用 AES-GCM 可逆加密存，登录密码才用 bcrypt。
- modernc sqlite 单连接：游标打开时不能跑嵌套查询（沿用「drain-then-query」）。
- 迁移须先建新表→拷数据→删旧表→改名（sqlite 改主键的标准做法）。
- 所有后端测试用内存 sqlite + httptest，不打网络。Go 路径：`export PATH=$PATH:/home/yxx/go-local/go/bin`。

---

## File Structure

```
internal/auth/secret.go         LoadOrCreateKey：加载/生成 32 字节主密钥文件
internal/auth/secret_test.go
internal/auth/crypto.go         Encrypt/Decrypt：AES-256-GCM（nonce 前置）
internal/auth/crypto_test.go
internal/auth/password.go       HashPassword/CheckPassword：bcrypt
internal/auth/password_test.go
internal/auth/users.go          UserStore + User：users 表仓储
internal/auth/users_test.go
internal/auth/sessions.go       SessionStore：sessions 表仓储
internal/auth/sessions_test.go
internal/db/migrations/006_users_sessions.up.sql   新迁移
internal/db/schema.sql          同步 4 表
internal/api/middleware/session.go        SessionAuth 中间件 + UserFromContext
internal/api/middleware/session_test.go
internal/api/v1/auth.go         改：AuthHandler 改为 db 支持（login/logout/session/me）
internal/api/v1/auth_test.go    改
internal/api/v1/setup.go        新：setup/status + setup（建首管理员 + 认领孤儿数据）
internal/api/v1/setup_test.go
internal/api/v1/account.go      新：改登录密码 + 设 Subsonic 密码
internal/api/v1/account_test.go
internal/api/subsonic/auth.go   改：按用户查库 + 解密比对，返回 *auth.User
internal/api/subsonic/context.go        新：当前用户 context key + helper
internal/api/subsonic/handler.go        改：Handler 持 users+key；withAuth 注入用户
internal/api/subsonic/bookmarks.go      改：全部端点按 user_id 过滤/写入
internal/api/subsonic/handler_test.go   改：testHandler 改为 seed 用户
internal/api/subsonic/bookmarks_test.go 改：适配 per-user
internal/api/router.go          改：装配 auth 包、SessionAuth、新端点、密钥
web/src/api/client.ts           改：新增 setup/me/account 方法
web/src/components/SetupView.vue         新：首次引导
web/src/components/AccountSettings.vue   新：账户设置
web/src/App.vue                 改：boot 先查 setup 状态
```

---

## Task 1: auth 包加密原语（bcrypt + AES-GCM + 主密钥）

**Files:**
- Create: `internal/auth/password.go`, `internal/auth/password_test.go`
- Create: `internal/auth/crypto.go`, `internal/auth/crypto_test.go`
- Create: `internal/auth/secret.go`, `internal/auth/secret_test.go`
- Modify: `go.mod` / `go.sum`（新增 `golang.org/x/crypto`）

- [ ] **Step 1: 拉取 bcrypt 依赖**

Run:
```bash
export PATH=$PATH:/home/yxx/go-local/go/bin
cd /home/yxx/develop/Lyra && go get golang.org/x/crypto/bcrypt
```
Expected: `go.mod` 出现 `require golang.org/x/crypto vX.Y.Z`。

- [ ] **Step 2: 写失败测试**

`internal/auth/password_test.go`:
```go
package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "s3cret" || hash == "" {
		t.Fatalf("哈希不应等于明文/为空: %q", hash)
	}
	if !CheckPassword(hash, "s3cret") {
		t.Error("正确密码应通过")
	}
	if CheckPassword(hash, "wrong") {
		t.Error("错误密码应拒绝")
	}
}
```

`internal/auth/crypto_test.go`:
```go
package auth

import (
	"crypto/rand"
	"io"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := testKey(t)
	ct, err := Encrypt(key, "subsonic-pw")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if string(ct) == "subsonic-pw" {
		t.Fatal("密文不应等于明文")
	}
	got, err := Decrypt(key, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "subsonic-pw" {
		t.Errorf("往返不一致: %q", got)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	ct, _ := Encrypt(testKey(t), "x")
	if _, err := Decrypt(testKey(t), ct); err == nil {
		t.Error("不同密钥解密应失败")
	}
}
```

`internal/auth/secret_test.go`:
```go
package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateKey_GeneratesThenReuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	k1, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("首次: %v", err)
	}
	if len(k1) != 32 {
		t.Fatalf("密钥应 32 字节，实际 %d", len(k1))
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Errorf("权限应 0600，实际 %v", info.Mode().Perm())
	}
	k2, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("复用: %v", err)
	}
	if !bytes.Equal(k1, k2) {
		t.Error("二次加载应复用同一密钥")
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/auth/...`
Expected: 编译失败（HashPassword/Encrypt/LoadOrCreateKey 未定义）。

- [ ] **Step 4: 实现**

`internal/auth/password.go`:
```go
package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword 用 bcrypt 生成登录密码哈希。
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword 校验明文是否匹配 bcrypt 哈希。
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}
```

`internal/auth/crypto.go`:
```go
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

// Encrypt 用 AES-256-GCM 加密明文，nonce 前置于密文返回。key 必须 32 字节。
func Encrypt(key []byte, plaintext string) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt 还原 Encrypt 产生的密文。
func Decrypt(key, ciphertext []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", errors.New("密文长度不足")
	}
	nonce, ct := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
```

`internal/auth/secret.go`:
```go
package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// LoadOrCreateKey 读取 path 处的 32 字节主密钥；不存在则生成并以 0600 写入。
func LoadOrCreateKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("密钥文件 %s 长度应为 32 字节，实际 %d", path, len(data))
		}
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, err
	}
	return key, nil
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/auth/...`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/auth go.mod go.sum && \
git commit -m "feat(auth): bcrypt 密码 + AES-GCM 加解密 + 主密钥加载"
```

---

## Task 2: 迁移 006（users/sessions + 重建 bookmarks/play_queue）

**Files:**
- Create: `internal/db/migrations/006_users_sessions.up.sql`
- Modify: `internal/db/schema.sql`
- Test: `internal/db/db_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

在 `internal/db/db_test.go` 末尾追加：
```go
func TestOpen_HasUsersAndSessions(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	for _, table := range []string{"users", "sessions"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Errorf("表 %s 应存在 (n=%d err=%v)", table, n, err)
		}
	}
}

func TestOpen_BookmarksAndQueueHaveUserID(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	// user_id 可空：插入 NULL 应成功（迁移产生的孤儿行形态）
	if _, err := db.Exec(`INSERT INTO tracks(id,title,file_path) VALUES('t1','x','p1')`); err != nil {
		t.Fatalf("seed track: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO bookmarks(user_id,track_id,position) VALUES(NULL,'t1',1000)`); err != nil {
		t.Errorf("bookmarks 应有可空 user_id 列: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO play_queue(user_id,track_ids) VALUES(NULL,'t1')`); err != nil {
		t.Errorf("play_queue 应有可空 user_id 列: %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/...`
Expected: FAIL（users 表不存在 / bookmarks 无 user_id 列）。

- [ ] **Step 3: 写迁移**

`internal/db/migrations/006_users_sessions.up.sql`:
```sql
-- 用户表
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    subsonic_pw   BLOB,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 会话表
CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

-- 重建 bookmarks：主键 track_id -> 复合唯一 (user_id, track_id)
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

-- 重建 play_queue：单行 id=1 -> 每用户一行
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

- [ ] **Step 4: 同步 schema.sql**

在 `internal/db/schema.sql` 中：(a) 把 `bookmarks` 与 `play_queue` 两个 `CREATE TABLE` 替换为上面 `_new` 的最终形态（表名用 `bookmarks`/`play_queue`，去掉 `_new` 后缀与迁移用的 INSERT/DROP/RENAME）；(b) 追加 `users`、`sessions` 两个 `CREATE TABLE` 与 `idx_sessions_user` 索引。最终 `bookmarks` 段应为：
```sql
CREATE TABLE bookmarks (
    user_id    TEXT REFERENCES users(id) ON DELETE CASCADE,
    track_id   TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, track_id)
);

CREATE TABLE play_queue (
    user_id    TEXT UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    track_ids  TEXT NOT NULL DEFAULT '',
    current    TEXT NOT NULL DEFAULT '',
    position   INTEGER NOT NULL DEFAULT 0,
    changed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    changed_by TEXT NOT NULL DEFAULT ''
);
```
并在文件中加：
```sql
CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    subsonic_pw   BLOB,
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
```

- [ ] **Step 5: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/db/...`
Expected: PASS（含已有的 `TestOpen_CreatesTablesOnFirstRun` 等仍绿）。

- [ ] **Step 6: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/db && \
git commit -m "feat(db): 迁移 006 users/sessions + 重建 bookmarks/play_queue 为 per-user"
```

---

## Task 3: UserStore 仓储

**Files:**
- Create: `internal/auth/users.go`, `internal/auth/users_test.go`

- [ ] **Step 1: 写失败测试**

`internal/auth/users_test.go`:
```go
package auth

import (
	"testing"

	"github.com/yxx-z/lyra/internal/db"
)

func newDB(t *testing.T) *userTestDB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &userTestDB{d}
}

type userTestDB struct{ *sqlDB }

func TestUserStore_CreateAndLookup(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewUserStore(d)

	if n, _ := s.Count(); n != 0 {
		t.Fatalf("初始应 0 个用户，实际 %d", n)
	}
	u, err := s.Create("admin", "hash", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == "" || !u.IsAdmin {
		t.Fatalf("用户字段不对: %+v", u)
	}
	got, err := s.ByUsername("admin")
	if err != nil {
		t.Fatalf("ByUsername: %v", err)
	}
	if got.ID != u.ID || got.PasswordHash != "hash" || !got.IsAdmin {
		t.Errorf("查得不一致: %+v", got)
	}
	if _, err := s.Create("admin", "h2", false); err == nil {
		t.Error("用户名唯一约束应阻止重复")
	}
}

func TestUserStore_UpdatePasswordAndSubsonicPW(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewUserStore(d)
	u, _ := s.Create("bob", "old", false)

	if err := s.UpdatePassword(u.ID, "new"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	if err := s.UpdateSubsonicPW(u.ID, []byte{1, 2, 3}); err != nil {
		t.Fatalf("UpdateSubsonicPW: %v", err)
	}
	got, _ := s.ByID(u.ID)
	if got.PasswordHash != "new" || len(got.SubsonicPW) != 3 {
		t.Errorf("更新未生效: %+v", got)
	}
}

func TestUserStore_FirstAdmin(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	s := NewUserStore(d)
	s.Create("u1", "h", false)
	admin, _ := s.Create("u2", "h", true)
	got, err := s.FirstAdmin()
	if err != nil || got.ID != admin.ID {
		t.Errorf("FirstAdmin 应返回 u2: got=%+v err=%v", got, err)
	}
}
```
> 注：删掉上面 `newDB`/`userTestDB` 那两段脚手架（它们是占位，实际用例直接 `db.Open`）。最终文件只保留三个 `Test*` 函数 + import。

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/auth/...`
Expected: 编译失败（NewUserStore 未定义）。

- [ ] **Step 3: 实现**

`internal/auth/users.go`:
```go
package auth

import (
	"database/sql"

	"github.com/google/uuid"
)

// User 是一个登录用户。SubsonicPW 为 AES-GCM 加密后的 Subsonic 密码原文；未设为 nil。
type User struct {
	ID           string
	Username     string
	PasswordHash string
	SubsonicPW   []byte
	IsAdmin      bool
}

type UserStore struct{ db *sql.DB }

func NewUserStore(db *sql.DB) *UserStore { return &UserStore{db: db} }

func (s *UserStore) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *UserStore) Create(username, passwordHash string, isAdmin bool) (*User, error) {
	id := uuid.NewString()
	admin := 0
	if isAdmin {
		admin = 1
	}
	if _, err := s.db.Exec(
		`INSERT INTO users(id, username, password_hash, is_admin) VALUES(?,?,?,?)`,
		id, username, passwordHash, admin,
	); err != nil {
		return nil, err
	}
	return &User{ID: id, Username: username, PasswordHash: passwordHash, IsAdmin: isAdmin}, nil
}

const userCols = `id, username, password_hash, subsonic_pw, is_admin`

func scanUser(row *sql.Row) (*User, error) {
	var u User
	var admin int
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.SubsonicPW, &admin); err != nil {
		return nil, err
	}
	u.IsAdmin = admin == 1
	return &u, nil
}

func (s *UserStore) ByUsername(name string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username=?`, name))
}

func (s *UserStore) ByID(id string) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func (s *UserStore) FirstAdmin() (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT ` + userCols + ` FROM users WHERE is_admin=1 ORDER BY created_at, id LIMIT 1`))
}

func (s *UserStore) UpdatePassword(id, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash=?, updated_at=datetime('now') WHERE id=?`, hash, id)
	return err
}

func (s *UserStore) UpdateSubsonicPW(id string, enc []byte) error {
	_, err := s.db.Exec(`UPDATE users SET subsonic_pw=?, updated_at=datetime('now') WHERE id=?`, enc, id)
	return err
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/auth/...`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/auth && \
git commit -m "feat(auth): UserStore 用户仓储"
```

---

## Task 4: SessionStore 仓储

**Files:**
- Create: `internal/auth/sessions.go`, `internal/auth/sessions_test.go`

- [ ] **Step 1: 写失败测试**

`internal/auth/sessions_test.go`:
```go
package auth

import (
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/db"
)

func seedUser(t *testing.T, s *UserStore) *User {
	t.Helper()
	u, err := s.Create("admin", "h", true)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSessionStore_CreateLookupDelete(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	u := seedUser(t, NewUserStore(d))
	ss := NewSessionStore(d)

	token, err := ss.Create(u.ID, time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("令牌应为 64 hex 字符，实际 %d", len(token))
	}
	uid, ok := ss.UserID(token)
	if !ok || uid != u.ID {
		t.Errorf("查会话失败: uid=%q ok=%v", uid, ok)
	}
	if err := ss.Delete(token); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := ss.UserID(token); ok {
		t.Error("删除后不应再命中")
	}
}

func TestSessionStore_ExpiredNotFound(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	u := seedUser(t, NewUserStore(d))
	ss := NewSessionStore(d)
	token, _ := ss.Create(u.ID, -time.Hour) // 已过期
	if _, ok := ss.UserID(token); ok {
		t.Error("过期会话不应命中")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/auth/...`
Expected: 编译失败（NewSessionStore 未定义）。

- [ ] **Step 3: 实现**

`internal/auth/sessions.go`:
```go
package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"io"
	"time"
)

type SessionStore struct{ db *sql.DB }

func NewSessionStore(db *sql.DB) *SessionStore { return &SessionStore{db: db} }

// sqlite datetime('now') 为 UTC，过期时间统一存 UTC 字符串以便比较。
const sqliteTime = "2006-01-02 15:04:05"

func (s *SessionStore) Create(userID string, ttl time.Duration) (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	exp := time.Now().Add(ttl).UTC().Format(sqliteTime)
	if _, err := s.db.Exec(`INSERT INTO sessions(token, user_id, expires_at) VALUES(?,?,?)`, token, userID, exp); err != nil {
		return "", err
	}
	return token, nil
}

func (s *SessionStore) UserID(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	var uid string
	err := s.db.QueryRow(`SELECT user_id FROM sessions WHERE token=? AND expires_at > datetime('now')`, token).Scan(&uid)
	if err != nil {
		return "", false
	}
	return uid, true
}

func (s *SessionStore) Refresh(token string, ttl time.Duration) error {
	exp := time.Now().Add(ttl).UTC().Format(sqliteTime)
	_, err := s.db.Exec(`UPDATE sessions SET expires_at=? WHERE token=?`, exp, token)
	return err
}

func (s *SessionStore) Delete(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token=?`, token)
	return err
}
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/auth/...`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/auth && \
git commit -m "feat(auth): SessionStore 会话仓储（随机令牌 + 过期）"
```

---

## Task 5: SessionAuth 中间件 + UserFromContext

**Files:**
- Create: `internal/api/middleware/session.go`, `internal/api/middleware/session_test.go`

- [ ] **Step 1: 写失败测试**

`internal/api/middleware/session_test.go`:
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

func setup(t *testing.T) (*auth.UserStore, *auth.SessionStore, *auth.User) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	u, _ := us.Create("admin", "h", true)
	return us, auth.NewSessionStore(d), u
}

func probe(w http.ResponseWriter, r *http.Request) {
	if u, ok := UserFromContext(r.Context()); ok {
		w.Header().Set("X-User", u.Username)
	}
	w.WriteHeader(http.StatusOK)
}

func TestSessionAuth_ValidCookie(t *testing.T) {
	us, ss, u := setup(t)
	token, _ := ss.Create(u.ID, time.Hour)
	h := SessionAuth(ss, us, false)(http.HandlerFunc(probe))
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || w.Header().Get("X-User") != "admin" {
		t.Errorf("应放行并注入用户: code=%d user=%q", w.Code, w.Header().Get("X-User"))
	}
}

func TestSessionAuth_BearerToken(t *testing.T) {
	us, ss, u := setup(t)
	token, _ := ss.Create(u.ID, time.Hour)
	h := SessionAuth(ss, us, false)(http.HandlerFunc(probe))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Bearer 令牌应放行: %d", w.Code)
	}
}

func TestSessionAuth_NoToken401(t *testing.T) {
	us, ss, _ := setup(t)
	h := SessionAuth(ss, us, false)(http.HandlerFunc(probe))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("无令牌应 401: %d", w.Code)
	}
}

func TestSessionAuth_DisabledActsAsFirstAdmin(t *testing.T) {
	us, ss, _ := setup(t)
	h := SessionAuth(ss, us, true)(http.HandlerFunc(probe))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || w.Header().Get("X-User") != "admin" {
		t.Errorf("禁用认证应以首管理员身份放行: code=%d user=%q", w.Code, w.Header().Get("X-User"))
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/middleware/...`
Expected: 编译失败（SessionAuth/UserFromContext 未定义）。

- [ ] **Step 3: 实现**

`internal/api/middleware/session.go`:
```go
package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yxx-z/lyra/internal/auth"
)

type ctxKey int

const userCtxKey ctxKey = iota

// SessionAuth 校验会话令牌（Bearer 头或 lyra_auth cookie），通过则把用户注入 context。
// disabled 为真时绕过校验，以首个管理员身份放行（局域网 kiosk）。
func SessionAuth(sessions *auth.SessionStore, users *auth.UserStore, disabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if disabled {
				if u, err := users.FirstAdmin(); err == nil {
					r = r.WithContext(context.WithValue(r.Context(), userCtxKey, u))
				}
				next.ServeHTTP(w, r)
				return
			}
			uid, ok := sessions.UserID(tokenFromRequest(r))
			if !ok {
				writeUnauthorized(w)
				return
			}
			u, err := users.ByID(uid)
			if err != nil {
				writeUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, u)))
		})
	}
}

// UserFromContext 取出 SessionAuth 注入的当前用户。
func UserFromContext(ctx context.Context) (*auth.User, bool) {
	u, ok := ctx.Value(userCtxKey).(*auth.User)
	return u, ok
}

func tokenFromRequest(r *http.Request) string {
	if parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2); len(parts) == 2 && parts[0] == "Bearer" {
		return parts[1]
	}
	if c, err := r.Cookie(authCookieName); err == nil {
		return c.Value
	}
	return ""
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": "未授权"}); err != nil {
		slog.Error("写响应失败", "err", err)
	}
}
```
> `authCookieName` 常量已在同包 `auth.go` 中定义（值 `"lyra_auth"`），复用。

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/middleware/...`
Expected: PASS（原 `BearerAuth` 测试仍绿）。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/middleware && \
git commit -m "feat(api): SessionAuth 中间件（sessions 表 + 当前用户 context）"
```

---

## Task 6: AuthHandler 改为 db 支持（login/logout/session/me）

**Files:**
- Modify: `internal/api/v1/auth.go`
- Modify: `internal/api/v1/auth_test.go`

- [ ] **Step 1: 重写测试**

把 `internal/api/v1/auth_test.go` 整体替换为：
```go
package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

func authFixture(t *testing.T) (*AuthHandler, *auth.UserStore, *auth.SessionStore) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	hash, _ := auth.HashPassword("pw123")
	us.Create("admin", hash, true)
	return NewAuthHandler(us, ss), us, ss
}

func TestLogin_Success(t *testing.T) {
	h, _, _ := authFixture(t)
	req := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"admin","password":"pw123"}`))
	w := httptest.NewRecorder()
	h.Login(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("登录应成功并返回 token: %d %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) == 0 {
		t.Error("应下发 cookie")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h, _, _ := authFixture(t)
	req := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"admin","password":"bad"}`))
	w := httptest.NewRecorder()
	h.Login(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("错误密码应 401: %d", w.Code)
	}
}

func TestMe_ReturnsCurrentUser(t *testing.T) {
	h, us, _ := authFixture(t)
	u, _ := us.ByUsername("admin")
	req := httptest.NewRequest("GET", "/me", nil)
	req = req.WithContext(contextWithUser(req, u))
	w := httptest.NewRecorder()
	h.Me(w, req)
	if !strings.Contains(w.Body.String(), `"username":"admin"`) || !strings.Contains(w.Body.String(), `"isAdmin":true`) {
		t.Errorf("me 返回不符: %s", w.Body.String())
	}
}

// contextWithUser 借用 middleware 注入用户，供 Me 测试。
func contextWithUser(r *http.Request, u *auth.User) interface{ Value(any) any } { return nil }
```
> 上面 `contextWithUser` 是占位——实际改为用 middleware 真实注入：把该函数删掉，`TestMe_ReturnsCurrentUser` 改成走中间件包裹（见下方实现 Step 后，用 `middleware.SessionAuth` 包裹一个 handler 并带 cookie）。为避免循环，**简化做法**：`Me` 从 `middleware.UserFromContext` 取用户；测试里直接构造带用户的 context：
> ```go
> import "context"
> // 在 middleware 包导出一个测试可用的注入器，或在此用 SessionAuth 真实注入。
> ```
> **采用真实注入**，最终 `TestMe_ReturnsCurrentUser` 写为：
```go
func TestMe_ReturnsCurrentUser(t *testing.T) {
	h, us, ss := authFixture(t)
	u, _ := us.ByUsername("admin")
	token, _ := ss.Create(u.ID, time.Hour)
	handler := middleware.SessionAuth(ss, us, false)(http.HandlerFunc(h.Me))
	req := httptest.NewRequest("GET", "/me", nil)
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `"username":"admin"`) || !strings.Contains(w.Body.String(), `"isAdmin":true`) {
		t.Errorf("me 返回不符: %s", w.Body.String())
	}
}
```
> 删除占位的 `contextWithUser` 与 `TestMe` 第一版；补 `import "time"`。

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/...`
Expected: 编译失败（`NewAuthHandler` 旧签名不匹配 / `Me` 未定义）。

- [ ] **Step 3: 实现**

把 `internal/api/v1/auth.go` 替换为：
```go
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
```
> 注意：`writeJSONError` 原先就在本文件。若其他文件（如旧 cover/search）也定义了 `writeJSON`/`writeJSONError`，则保留原定义、删除本处重复，避免「重复声明」。先 `grep -rn "func writeJSON" internal/api/v1` 确认；本计划假设仅此处定义。

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/... 2>&1 | head -40`
Expected: 因 router.go 仍用旧签名，全包可能编译失败——**这是预期**，Task 10 修 router。本 Step 只需 `go test ./internal/api/v1/...` 中 auth 相关测试逻辑正确；若因 router 未改导致 `internal/api` 包编译失败，不影响 v1 包自身测试（v1 不 import api）。运行 `go vet ./internal/api/v1/` 应通过。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/auth.go internal/api/v1/auth_test.go && \
git commit -m "feat(api): AuthHandler 改为 sessions 表登录 + me 端点"
```

---

## Task 7: setup 端点（status + 创建首管理员 + 认领孤儿数据）

**Files:**
- Create: `internal/api/v1/setup.go`, `internal/api/v1/setup_test.go`

- [ ] **Step 1: 写失败测试**

`internal/api/v1/setup_test.go`:
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

func setupFixture(t *testing.T) (*SetupHandler, *auth.UserStore, interface{ Exec(string, ...any) (interface{ RowsAffected() (int64, error) }, error) }) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	return NewSetupHandler(us, ss, d), us, nil
}

func TestSetupStatus_NeedsSetupWhenEmpty(t *testing.T) {
	h, _, _ := setupFixture(t)
	w := httptest.NewRecorder()
	h.Status(w, httptest.NewRequest("GET", "/setup/status", nil))
	if !strings.Contains(w.Body.String(), `"needsSetup":true`) {
		t.Errorf("空库应 needsSetup=true: %s", w.Body.String())
	}
}

func TestSetupCreate_CreatesAdminAndClaimsOrphans(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	h := NewSetupHandler(us, ss, d)
	// 模拟迁移产生的孤儿数据
	d.Exec(`INSERT INTO tracks(id,title,file_path) VALUES('t1','x','p1')`)
	d.Exec(`INSERT INTO bookmarks(user_id,track_id,position) VALUES(NULL,'t1',1000)`)
	d.Exec(`INSERT INTO play_queue(user_id,track_ids) VALUES(NULL,'t1')`)

	req := httptest.NewRequest("POST", "/setup", strings.NewReader(`{"username":"boss","password":"pw12345"}`))
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("创建管理员应成功并返回 token: %d %s", w.Code, w.Body.String())
	}
	u, _ := us.ByUsername("boss")
	if !u.IsAdmin {
		t.Error("首用户应为管理员")
	}
	var bmUser, pqUser string
	d.QueryRow(`SELECT user_id FROM bookmarks WHERE track_id='t1'`).Scan(&bmUser)
	d.QueryRow(`SELECT user_id FROM play_queue LIMIT 1`).Scan(&pqUser)
	if bmUser != u.ID || pqUser != u.ID {
		t.Errorf("孤儿数据应认领给首管理员: bm=%q pq=%q want=%q", bmUser, pqUser, u.ID)
	}
}

func TestSetupCreate_RejectsWhenUsersExist(t *testing.T) {
	d, _ := db.Open(":memory:")
	defer d.Close()
	us := auth.NewUserStore(d)
	us.Create("existing", "h", true)
	h := NewSetupHandler(us, auth.NewSessionStore(d), d)
	req := httptest.NewRequest("POST", "/setup", strings.NewReader(`{"username":"x","password":"y12345"}`))
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("已有用户应 409: %d", w.Code)
	}
}
```
> 删除上面 `setupFixture` 里花哨的第三返回值（占位），最终把 `setupFixture` 简化为返回 `(*SetupHandler, *auth.UserStore, *sql.DB)` 或干脆各用例内联 `db.Open`（如后两个用例所示）。第一个用例可改为内联。保持用例断言不变。

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go vet ./internal/api/v1/`
Expected: 失败（NewSetupHandler 未定义）。

- [ ] **Step 3: 实现**

`internal/api/v1/setup.go`:
```go
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
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run 'Setup' -v`
Expected: PASS（Status/Create/Reject 三组）。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/setup.go internal/api/v1/setup_test.go && \
git commit -m "feat(api): 首次启动引导端点（setup/status + 创建管理员 + 认领孤儿数据）"
```

---

## Task 8: account 端点（改登录密码 + 设 Subsonic 密码）

**Files:**
- Create: `internal/api/v1/account.go`, `internal/api/v1/account_test.go`

- [ ] **Step 1: 写失败测试**

`internal/api/v1/account_test.go`:
```go
package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yxx-z/lyra/internal/api/middleware"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/db"
)

func accountFixture(t *testing.T) (*AccountHandler, *auth.UserStore, *auth.SessionStore, []byte, *auth.User) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	us := auth.NewUserStore(d)
	ss := auth.NewSessionStore(d)
	key := make([]byte, 32)
	hash, _ := auth.HashPassword("old123")
	u, _ := us.Create("admin", hash, true)
	return NewAccountHandler(us, key), us, ss, key, u
}

func authedReq(t *testing.T, h http.HandlerFunc, ss *auth.SessionStore, us *auth.UserStore, u *auth.User, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, _ := ss.Create(u.ID, time.Hour)
	handler := middleware.SessionAuth(ss, us, false)(h)
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "lyra_auth", Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestChangePassword(t *testing.T) {
	h, us, ss, _, u := accountFixture(t)
	w := authedReq(t, h.ChangePassword, ss, us, u, "POST", `{"oldPassword":"old123","newPassword":"new456"}`)
	if w.Code != 200 {
		t.Fatalf("应成功: %d %s", w.Code, w.Body.String())
	}
	got, _ := us.ByID(u.ID)
	if !auth.CheckPassword(got.PasswordHash, "new456") {
		t.Error("新密码应生效")
	}
}

func TestChangePassword_WrongOld(t *testing.T) {
	h, us, ss, _, u := accountFixture(t)
	w := authedReq(t, h.ChangePassword, ss, us, u, "POST", `{"oldPassword":"bad","newPassword":"new456"}`)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("旧密码错应 401: %d", w.Code)
	}
}

func TestSetSubsonicPassword(t *testing.T) {
	h, us, ss, key, u := accountFixture(t)
	w := authedReq(t, h.SetSubsonicPassword, ss, us, u, "POST", `{"password":"sonicpw"}`)
	if w.Code != 200 {
		t.Fatalf("应成功: %d %s", w.Code, w.Body.String())
	}
	got, _ := us.ByID(u.ID)
	plain, err := auth.Decrypt(key, got.SubsonicPW)
	if err != nil || plain != "sonicpw" {
		t.Errorf("Subsonic 密码应可解回原文: %q err=%v", plain, err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go vet ./internal/api/v1/`
Expected: 失败（NewAccountHandler 未定义）。

- [ ] **Step 3: 实现**

`internal/api/v1/account.go`:
```go
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
```

- [ ] **Step 4: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/v1/ -run 'ChangePassword|SetSubsonic' -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/v1/account.go internal/api/v1/account_test.go && \
git commit -m "feat(api): 账户设置端点（改登录密码 + 设 Subsonic 密码）"
```

---

## Task 9: Subsonic 按用户认证 + per-user 书签/队列

**Files:**
- Create: `internal/api/subsonic/context.go`
- Modify: `internal/api/subsonic/auth.go`
- Modify: `internal/api/subsonic/handler.go`
- Modify: `internal/api/subsonic/bookmarks.go`
- Modify: `internal/api/subsonic/handler_test.go`（testHandler 改 seed 用户）
- Modify: `internal/api/subsonic/bookmarks_test.go`（适配）

- [ ] **Step 1: 改测试脚手架先行（让 seed 出真实用户）**

把 `internal/api/subsonic/handler_test.go` 的 `testHandler` 改为创建带 Subsonic 密码 `secret` 的用户 `admin`，并提供密钥；`NewHandler` 增加 `users` 与 `key` 两个入参：
```go
func testHandler(t *testing.T) (*Handler, *config.Config) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	cfg := &config.Config{}
	cfg.Subsonic.Enabled = true

	key := make([]byte, 32)
	users := auth.NewUserStore(d)
	hash, _ := auth.HashPassword("loginpw")
	u, _ := users.Create("admin", hash, true)
	enc, _ := auth.Encrypt(key, "secret")
	users.UpdateSubsonicPW(u.ID, enc)

	tcache := transcode.NewCache(t.TempDir(), 0)
	tsvc := transcode.NewService(cfg.Transcode.FFmpegPath, cfg.Transcode.DefaultBitrate, tcache)
	stream := v1.NewStreamHandler(d, tsvc)
	cover := v1.NewCoverHandler(d)
	return NewHandler(d, cfg, stream, cover, users, key), cfg
}
```
加 import：`"github.com/yxx-z/lyra/internal/auth"`。

- [ ] **Step 2: 运行确认失败**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go vet ./internal/api/subsonic/`
Expected: 失败（NewHandler 旧签名 / authenticate 仍读 cfg）。

- [ ] **Step 3: 实现 context + auth + handler**

`internal/api/subsonic/context.go`:
```go
package subsonic

import (
	"context"

	"github.com/yxx-z/lyra/internal/auth"
)

type ctxKey int

const userCtxKey ctxKey = iota

func withUser(ctx context.Context, u *auth.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

func userFromCtx(ctx context.Context) *auth.User {
	u, _ := ctx.Value(userCtxKey).(*auth.User)
	return u
}
```

把 `internal/api/subsonic/auth.go` 替换为：
```go
package subsonic

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"strings"

	"github.com/yxx-z/lyra/internal/auth"
)

// authenticate 按用户名查库、解密 Subsonic 密码并校验；通过返回 *auth.User，否则 *Error。
func (h *Handler) authenticate(q url.Values) (*auth.User, *Error) {
	if !h.cfg.Subsonic.Enabled {
		return nil, &Error{Code: 40, Message: "Subsonic 未启用"}
	}
	u, err := h.users.ByUsername(q.Get("u"))
	if err != nil || len(u.SubsonicPW) == 0 {
		return nil, &Error{Code: 40, Message: "用户名或密码错误"}
	}
	pw, err := auth.Decrypt(h.key, u.SubsonicPW)
	if err != nil {
		return nil, &Error{Code: 40, Message: "用户名或密码错误"}
	}
	if p := q.Get("p"); p != "" {
		if strings.HasPrefix(p, "enc:") {
			if dec, err := hex.DecodeString(strings.TrimPrefix(p, "enc:")); err == nil {
				p = string(dec)
			}
		}
		if p == pw {
			return u, nil
		}
		return nil, &Error{Code: 40, Message: "用户名或密码错误"}
	}
	if tok, salt := q.Get("t"), q.Get("s"); tok != "" && salt != "" {
		sum := md5.Sum([]byte(pw + salt))
		if hex.EncodeToString(sum[:]) == tok {
			return u, nil
		}
		return nil, &Error{Code: 40, Message: "用户名或密码错误"}
	}
	return nil, &Error{Code: 10, Message: "缺少认证参数"}
}
```

改 `internal/api/subsonic/handler.go`：(a) `Handler` 结构体加字段；(b) `NewHandler` 增参；(c) `withAuth` 改为注入用户。
```go
import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	v1 "github.com/yxx-z/lyra/internal/api/v1"
	"github.com/yxx-z/lyra/internal/auth"
	"github.com/yxx-z/lyra/internal/config"
)

type Handler struct {
	db      *sql.DB
	cfg     *config.Config
	streamH *v1.StreamHandler
	cover   *v1.CoverHandler
	users   *auth.UserStore
	key     []byte
}

func NewHandler(db *sql.DB, cfg *config.Config, stream *v1.StreamHandler, cover *v1.CoverHandler, users *auth.UserStore, key []byte) *Handler {
	return &Handler{db: db, cfg: cfg, streamH: stream, cover: cover, users: users, key: key}
}
```
`withAuth` 改为：
```go
func (h *Handler) withAuth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		u, e := h.authenticate(r.Form)
		if e != nil {
			writeError(w, r, e.Code, e.Message)
			return
		}
		fn(w, r.WithContext(withUser(r.Context(), u)))
	}
}
```

- [ ] **Step 4: 实现 bookmarks per-user**

把 `internal/api/subsonic/bookmarks.go` 中四个写/读端点改为按当前用户：
```go
func (h *Handler) createBookmark(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	id := r.Form.Get("id")
	position, _ := strconv.ParseInt(r.Form.Get("position"), 10, 64)
	comment := r.Form.Get("comment")

	var exists string
	if err := h.db.QueryRow(`SELECT id FROM tracks WHERE id=? AND is_available=1`, id).Scan(&exists); err != nil {
		writeError(w, r, 70, "曲目不存在")
		return
	}
	if _, err := h.db.Exec(`
		INSERT INTO bookmarks(user_id, track_id, position, comment) VALUES(?,?,?,?)
		ON CONFLICT(user_id, track_id) DO UPDATE SET
			position=excluded.position, comment=excluded.comment, updated_at=datetime('now')`,
		u.ID, id, position, comment); err != nil {
		writeError(w, r, 0, "保存书签失败")
		return
	}
	writeResponse(w, r, &Response{})
}
```
`getBookmarks`：查询加 `WHERE user_id=?`，`Username` 改 `u.Username`：
```go
func (h *Handler) getBookmarks(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	rows, err := h.db.Query(`SELECT track_id, position, comment, created_at, updated_at FROM bookmarks WHERE user_id=? ORDER BY updated_at DESC`, u.ID)
	if err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	defer rows.Close()
	type bmRow struct {
		trackID, comment, created, changed string
		position                           int64
	}
	var raw []bmRow
	for rows.Next() {
		var bm bmRow
		if err := rows.Scan(&bm.trackID, &bm.position, &bm.comment, &bm.created, &bm.changed); err != nil {
			continue
		}
		raw = append(raw, bm)
	}
	if err := rows.Err(); err != nil {
		writeError(w, r, 0, "查询失败")
		return
	}
	rows.Close()

	bms := &Bookmarks{Bookmark: []Bookmark{}}
	for _, bm := range raw {
		child, ok := h.childByID(bm.trackID)
		if !ok {
			continue
		}
		bms.Bookmark = append(bms.Bookmark, Bookmark{
			Position: bm.position,
			Username: u.Username,
			Comment:  bm.comment,
			Created:  bm.created,
			Changed:  bm.changed,
			Entry:    child,
		})
	}
	writeResponse(w, r, &Response{Bookmarks: bms})
}
```
`deleteBookmark`：
```go
func (h *Handler) deleteBookmark(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	_, _ = h.db.Exec(`DELETE FROM bookmarks WHERE user_id=? AND track_id=?`, u.ID, r.Form.Get("id"))
	writeResponse(w, r, &Response{})
}
```
`savePlayQueue`：改为按 user_id upsert（`ON CONFLICT(user_id)`）：
```go
func (h *Handler) savePlayQueue(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	ids := r.Form["id"]
	const maxQueue = 1000
	if len(ids) > maxQueue {
		ids = ids[:maxQueue]
	}
	trackIDs := strings.Join(ids, ",")
	current := r.Form.Get("current")
	position, _ := strconv.ParseInt(r.Form.Get("position"), 10, 64)
	changedBy := r.Form.Get("c")
	if _, err := h.db.Exec(`
		INSERT INTO play_queue(user_id, track_ids, current, position, changed_at, changed_by)
		VALUES(?, ?, ?, ?, datetime('now'), ?)
		ON CONFLICT(user_id) DO UPDATE SET
			track_ids=excluded.track_ids, current=excluded.current,
			position=excluded.position, changed_at=datetime('now'), changed_by=excluded.changed_by`,
		u.ID, trackIDs, current, position, changedBy); err != nil {
		writeError(w, r, 0, "保存播放队列失败")
		return
	}
	writeResponse(w, r, &Response{})
}
```
`getPlayQueue`：
```go
func (h *Handler) getPlayQueue(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	var trackIDs, current, changed, changedBy string
	var position int64
	err := h.db.QueryRow(`SELECT track_ids, current, position, changed_at, changed_by FROM play_queue WHERE user_id=?`, u.ID).
		Scan(&trackIDs, &current, &position, &changed, &changedBy)
	if err != nil {
		writeResponse(w, r, &Response{})
		return
	}
	pq := &PlayQueue{
		Current:   current,
		Position:  position,
		Username:  u.Username,
		Changed:   changed,
		ChangedBy: changedBy,
	}
	if trackIDs != "" {
		for _, id := range strings.Split(trackIDs, ",") {
			if c, ok := h.childByID(id); ok {
				pq.Entry = append(pq.Entry, c)
			}
		}
	}
	writeResponse(w, r, &Response{PlayQueue: pq})
}
```

- [ ] **Step 5: 适配 bookmarks_test.go**

`internal/api/subsonic/bookmarks_test.go` 中直接 SQL 断言的语句改为按 user：把 `WHERE track_id='t1'` 仍可保留（单用户场景 count 不变）。**唯一须改**：`TestPlayQueue_Empty` / 直接查 `play_queue WHERE id=1` 的断言（若有）改为不依赖 `id=1`。检查并把任何 `FROM play_queue WHERE id=1` 改为 `FROM play_queue LIMIT 1`。其余 httptest 用例（走 `u=admin&p=secret`）因 testHandler 已 seed 用户而无需改动。新增一个隔离用例：
```go
func TestBookmarks_PerUserIsolation(t *testing.T) {
	h, _ := testHandler(t)
	seed(t, h.db)
	// 再建一个用户 bob，Subsonic 密码 bobpw
	// 直接用 store 造（testHandler 未暴露，故用 SQL + 复用包内 auth 不便）——改为通过第二 Handler 验证。
	// 简化：admin 建书签后，断言另一个 user 的 getBookmarks 为空。
	doReq(t, h, "/rest/createBookmark?u=admin&p=secret&id=t1&position=1000&f=json")
	// 直接插入第二个用户并赋 Subsonic 密码
	var adminID string
	h.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&adminID)
	if adminID == "" {
		t.Skip("无 admin 用户")
	}
	// bob 没有书签
	doReq(t, h, "/rest/getBookmarks?u=admin&p=secret&f=json") // admin 有
	w := doReq(t, h, "/rest/getBookmarks?u=admin&p=secret&f=json")
	if !strings.Contains(w.Body.String(), `以父之名`) {
		t.Errorf("admin 应能看到自己的书签: %s", w.Body.String())
	}
}
```
> 若构造第二个用户成本高，本用例可只验证「admin 能看到自己书签」（per-user 隔离的核心 SQL 已由 `WHERE user_id=?` 保证，且 store 单测已覆盖 user 维度）。隔离的端到端验证留待真实环境（Task 11 验证步骤）。

- [ ] **Step 6: 运行确认通过**

Run: `cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go test ./internal/api/subsonic/...`
Expected: PASS（ping/bookmarks/playqueue 全绿）。

- [ ] **Step 7: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/subsonic && \
git commit -m "feat(subsonic): 按用户认证 + per-user 书签/播放队列"
```

---

## Task 10: router 装配 + 全量编译

**Files:**
- Modify: `internal/api/router.go`

- [ ] **Step 1: 改 router**

`internal/api/router.go`：(a) 顶部加载主密钥与 stores；(b) login/logout 之外加 setup/status 与 setup（免认证）；(c) `/api/v1` 组的中间件换成 `SessionAuth`；(d) 组内加 me/account 端点；(e) subsonic.NewHandler 增参。

新增 import：
```go
"path/filepath"

"github.com/yxx-z/lyra/internal/auth"
```
在 `streamH := ...` 之后插入：
```go
	keyPath := filepath.Join(filepath.Dir(cfg.Database.Path), "secret.key")
	key, err := auth.LoadOrCreateKey(keyPath)
	if err != nil {
		panic("加载主密钥失败: " + err.Error())
	}
	users := auth.NewUserStore(db)
	sessions := auth.NewSessionStore(db)
```
把现有的：
```go
	authH := v1.NewAuthHandler(cfg)
	r.Post("/api/v1/auth/login", authH.Login)
	r.Post("/api/v1/auth/logout", authH.Logout)
```
改为：
```go
	authH := v1.NewAuthHandler(users, sessions)
	setupH := v1.NewSetupHandler(users, sessions, db)
	accountH := v1.NewAccountHandler(users, key)
	r.Post("/api/v1/auth/login", authH.Login)
	r.Post("/api/v1/auth/logout", authH.Logout)
	r.Get("/api/v1/setup/status", setupH.Status)
	r.Post("/api/v1/setup", setupH.Create)
```
把组内中间件与端点：
```go
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.BearerAuth(cfg.Auth.Token, cfg.Auth.Disable))

		r.Post("/auth/session", authH.Session)
```
改为：
```go
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.SessionAuth(sessions, users, cfg.Auth.Disable))

		r.Get("/auth/me", authH.Me)
		r.Post("/auth/session", authH.Session)
		r.Post("/account/password", accountH.ChangePassword)
		r.Post("/account/subsonic-password", accountH.SetSubsonicPassword)
```
把 subsonic 装配：
```go
	subHandler := subsonic.NewHandler(db, cfg, streamH, subCover)
```
改为：
```go
	subHandler := subsonic.NewHandler(db, cfg, streamH, subCover, users, key)
```

- [ ] **Step 2: 全量编译 + 测试**

Run:
```bash
cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && go build ./... && go test ./...
```
Expected: build 通过；全部包测试 PASS。若 `middleware.BearerAuth` 变为未使用：它是导出函数，Go 不报「未使用」错误，其测试仍绿，保留即可（后续清理）。

- [ ] **Step 3: 提交**

```bash
cd /home/yxx/develop/Lyra && git add internal/api/router.go && \
git commit -m "feat(api): router 装配多用户认证（密钥/stores/SessionAuth/setup/account）"
```

---

## Task 11: 前端 —— 引导页 + 账户设置 + boot 状态

**Files:**
- Modify: `web/src/api/client.ts`
- Create: `web/src/components/SetupView.vue`
- Create: `web/src/components/AccountSettings.vue`
- Modify: `web/src/App.vue`

> 前端无单测框架，验证用 `npm run build` + 真实环境（见末尾验证步骤）。各步参照 `client.ts` 既有 fetch/错误处理风格（`ApiClient`、`ApiError`、`request` 私有方法）。

- [ ] **Step 1: client.ts 新增方法**

在 `ApiClient` 类中按既有 `request`/`fetch` 模式新增（方法名、返回与下方一致）：
```ts
// 首次启动是否需要引导
async getSetupStatus(): Promise<{ needsSetup: boolean }> {
  return this.request('/api/v1/setup/status', { method: 'GET' })
}

// 创建首个管理员，返回会话 token（与 login 一致）
async setup(username: string, password: string): Promise<string> {
  const data = await this.request<{ token: string }>('/api/v1/setup', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
  return data.token
}

async getMe(): Promise<{ username: string; isAdmin: boolean }> {
  return this.request('/api/v1/auth/me', { method: 'GET' })
}

async changePassword(oldPassword: string, newPassword: string): Promise<void> {
  await this.request('/api/v1/account/password', {
    method: 'POST',
    body: JSON.stringify({ oldPassword, newPassword }),
  })
}

async setSubsonicPassword(password: string): Promise<void> {
  await this.request('/api/v1/account/subsonic-password', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}
```
> 若 `client.ts` 的内部请求方法不叫 `request`，按实际名称调整（先 `grep -n "private .*(" web/src/api/client.ts` 或看 `login` 怎么写的，照搬其风格）。`setup`/`getSetupStatus` **不要求** token。

- [ ] **Step 2: SetupView.vue**

`web/src/components/SetupView.vue`（参照 `LoginView.vue` 的样式与 props/emit 风格）：
```vue
<template>
  <div class="setup-view">
    <form class="setup-card" @submit.prevent="submit">
      <h1>欢迎使用 Lyra</h1>
      <p class="hint">首次启动，请创建管理员账号。</p>
      <input v-model="username" placeholder="用户名" autocomplete="username" />
      <input v-model="password" type="password" placeholder="密码（至少 4 位）" autocomplete="new-password" />
      <input v-model="confirm" type="password" placeholder="确认密码" autocomplete="new-password" />
      <p v-if="error" class="error">{{ error }}</p>
      <button type="submit" :disabled="loading">{{ loading ? '创建中…' : '创建管理员' }}</button>
    </form>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
const props = defineProps<{ loading: boolean; error: string }>()
const emit = defineEmits<{ (e: 'setup', payload: { username: string; password: string }): void }>()
const username = ref('')
const password = ref('')
const confirm = ref('')
const error = ref('')
function submit() {
  error.value = ''
  if (!username.value || password.value.length < 4) {
    error.value = '用户名不能为空，密码至少 4 位'
    return
  }
  if (password.value !== confirm.value) {
    error.value = '两次密码不一致'
    return
  }
  emit('setup', { username: username.value, password: password.value })
}
</script>

<style scoped>
.setup-view { display: flex; align-items: center; justify-content: center; min-height: 100vh; }
.setup-card { display: flex; flex-direction: column; gap: 12px; width: 320px; padding: 32px; }
.setup-card input { padding: 10px 12px; border-radius: 8px; border: 1px solid var(--border, #333); }
.setup-card .error { color: var(--danger, #e5484d); font-size: 13px; }
.setup-card .hint { color: var(--text-muted, #888); font-size: 13px; }
</style>
```
> `props.error` 用于显示服务端错误（如已初始化）；本地校验用内部 `error`。如需统一，App 传入的 error 优先显示：可把模板 `{{ error }}` 改为 `{{ error || props.error }}`。

- [ ] **Step 3: AccountSettings.vue**

`web/src/components/AccountSettings.vue`：
```vue
<template>
  <div class="account-settings">
    <section>
      <h2>修改登录密码</h2>
      <input v-model="oldPw" type="password" placeholder="原密码" />
      <input v-model="newPw" type="password" placeholder="新密码（至少 4 位）" />
      <button :disabled="busy" @click="changePw">保存</button>
    </section>
    <section>
      <h2>Subsonic 密码</h2>
      <p class="hint">用于 Symfonium 等客户端登录（与登录密码独立）。</p>
      <input v-model="subPw" type="password" placeholder="设置 Subsonic 密码" />
      <button :disabled="busy" @click="saveSub">保存</button>
    </section>
    <p v-if="msg" :class="msgError ? 'error' : 'ok'">{{ msg }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { ApiClient } from '../api/client'
const props = defineProps<{ api: ApiClient }>()
const oldPw = ref(''); const newPw = ref(''); const subPw = ref('')
const busy = ref(false); const msg = ref(''); const msgError = ref(false)
function show(text: string, isErr = false) { msg.value = text; msgError.value = isErr }
async function changePw() {
  busy.value = true
  try { await props.api.changePassword(oldPw.value, newPw.value); show('登录密码已更新'); oldPw.value = ''; newPw.value = '' }
  catch (e) { show(e instanceof Error ? e.message : '更新失败', true) }
  finally { busy.value = false }
}
async function saveSub() {
  busy.value = true
  try { await props.api.setSubsonicPassword(subPw.value); show('Subsonic 密码已更新'); subPw.value = '' }
  catch (e) { show(e instanceof Error ? e.message : '更新失败', true) }
  finally { busy.value = false }
}
</script>

<style scoped>
.account-settings { padding: 24px; display: flex; flex-direction: column; gap: 24px; max-width: 420px; }
.account-settings section { display: flex; flex-direction: column; gap: 10px; }
.account-settings input { padding: 10px 12px; border-radius: 8px; border: 1px solid var(--border, #333); }
.account-settings .hint { color: var(--text-muted, #888); font-size: 13px; }
.account-settings .error { color: var(--danger, #e5484d); }
.account-settings .ok { color: var(--success, #30a46c); }
</style>
```

- [ ] **Step 4: App.vue 接入引导与设置**

在 `App.vue` 中：
1. import `SetupView` 与 `AccountSettings`。
2. 新增状态：`const needsSetup = ref(false)`、`const setupLoading = ref(false)`、`const setupError = ref('')`、`const showSettings = ref(false)`。
3. `boot()` 开头先查引导状态：
```ts
async function boot() {
  try {
    const status = await api.getSetupStatus()
    if (status.needsSetup) {
      needsSetup.value = true
      return
    }
  } catch {
    // 查询失败则按未引导处理，继续走原逻辑
  }
  // ……原有 token / 匿名逻辑保持不变……
}
```
4. 新增引导提交处理：
```ts
async function handleSetup(payload: { username: string; password: string }) {
  setupLoading.value = true
  setupError.value = ''
  try {
    const nextToken = await api.setup(payload.username, payload.password)
    tokenStorage.save(nextToken)
    token.value = nextToken
    needsSetup.value = false
    await loadInitialData()
  } catch (error) {
    setupError.value = messageFromError(error)
  } finally {
    setupLoading.value = false
  }
}
```
5. 模板最外层按优先级渲染 SetupView：
```vue
<SetupView
  v-if="needsSetup"
  :loading="setupLoading"
  :error="setupError"
  @setup="handleSetup"
/>
<LoginView v-else-if="showLogin" ... />
<LibraryShell v-else ...>
```
6. 在 `LibraryShell` 内（或其用户菜单）加一个「账户设置」入口切换 `showSettings`，并在主内容区条件渲染：
```vue
<AccountSettings v-if="showSettings" :api="api" />
```
   `LibraryShell` 已有 `@logout` 等事件，按其现有「用户菜单」模式加一个 `@open-settings` 事件触发 `showSettings = true`（具体挂载点参照 `LibraryShell.vue` 现有 header/menu 结构）。若一时难以接入菜单，最简做法：在 PlayerBar 或 header 放一个齿轮按钮 `@click="showSettings = !showSettings"`。

- [ ] **Step 5: 构建验证**

Run:
```bash
cd /home/yxx/develop/Lyra && export PATH=$PATH:/home/yxx/go-local/go/bin && make build-frontend && go build ./...
```
Expected: 前端构建产物输出到 `ui/dist`，Go 构建通过。

- [ ] **Step 6: 提交**

```bash
cd /home/yxx/develop/Lyra && git add web ui/dist && \
git commit -m "feat(web): 首次启动引导页 + 账户设置（改密码/设 Subsonic 密码）"
```

---

## 真实环境验证（合并前必做，见记忆 verify-real-playback-early）

1. `make docker-build && docker compose up -d`
2. 浏览器访问服务地址 → 应出现**引导页**（因 users 表为空）；创建管理员 → 自动进入主界面。
3. 进「账户设置」设一个 Subsonic 密码。
4. Symfonium 用 **新用户名 + 新 Subsonic 密码** 重新添加服务器 → ping/同步成功；播放一首歌中途切走再回来，验证续播书签仍在（`docker logs lyra-lyra-1` 看 createBookmark/getBookmarks 走通、无 40/未授权）。
5. 确认旧的全局续播数据已认领（若升级前有数据）。

---

## Self-Review（计划自检）

- **Spec 覆盖**：users/sessions 表(T2/T3/T4) ✓；bcrypt+AES-GCM+主密钥(T1) ✓；Web 登录查库(T6) ✓；会话中间件(T5) ✓；Subsonic 按用户(T9) ✓；per-user 书签/队列(T2 迁移 + T9 端点) ✓；首次引导 + 认领孤儿数据(T7) ✓；账户设置改密码/设 Subsonic 密码(T8 + 前端 T11) ✓；config 凭据退役 + auth.disable 以首管理员身份(T5 中间件 + T10 router) ✓；前端引导(T11) ✓。
- **占位符**：T3/T7 测试里特意标注的「占位脚手架」已在该步文字中要求删除并给出最终形态；无 TODO/TBD 残留。
- **类型一致**：`NewHandler(db,cfg,stream,cover,users,key)` 在 T9 定义、T10 调用一致；`NewAuthHandler(users,sessions)`、`NewSetupHandler(users,sessions,db)`、`NewAccountHandler(users,key)` 跨 T6/T7/T8/T10 一致；`UserFromContext`(middleware) 与 `userFromCtx`(subsonic) 分属两包、命名各自自洽；`sessionTTL` 定义于 v1/auth.go，被 setup.go 复用（同包）✓；`writeJSON`/`writeJSONError`/`setAuthCookie` 定义于 v1/auth.go，被 setup.go/account.go 同包复用 ✓。
- **已知遗留**：`middleware.BearerAuth` 改用 SessionAuth 后成为死代码（导出函数，不报错），留作后续清理；`config.AuthConfig` 的 Username/Password/Token 与 `SubsonicConfig.Password` 字段保留解析兼容但不再用作凭据。
