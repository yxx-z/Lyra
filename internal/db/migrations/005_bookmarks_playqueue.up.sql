CREATE TABLE bookmarks (
    track_id   TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE play_queue (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    track_ids  TEXT NOT NULL DEFAULT '',
    current    TEXT NOT NULL DEFAULT '',
    position   INTEGER NOT NULL DEFAULT 0,
    changed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    changed_by TEXT NOT NULL DEFAULT ''
);
