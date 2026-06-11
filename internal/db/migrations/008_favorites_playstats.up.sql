-- 收藏（per-user，多态：歌曲/专辑/歌手）
CREATE TABLE starred (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_type  TEXT NOT NULL,
    item_id    TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, item_type, item_id)
);
CREATE INDEX idx_starred_user_type ON starred(user_id, item_type);

-- 播放统计（per-user）
CREATE TABLE play_stats (
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id       TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    play_count     INTEGER NOT NULL DEFAULT 0,
    last_played_at DATETIME,
    PRIMARY KEY (user_id, track_id)
);
CREATE INDEX idx_play_stats_user_count  ON play_stats(user_id, play_count DESC);
CREATE INDEX idx_play_stats_user_recent ON play_stats(user_id, last_played_at DESC);
