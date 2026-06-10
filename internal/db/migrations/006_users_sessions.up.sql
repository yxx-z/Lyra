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
