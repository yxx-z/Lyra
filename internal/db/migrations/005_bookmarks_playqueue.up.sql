CREATE TABLE bookmarks (
    track_id   TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    position   INTEGER NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE play_queue (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    track_ids  TEXT NOT NULL DEFAULT '',
    current    TEXT NOT NULL DEFAULT '',
    position   INTEGER NOT NULL DEFAULT 0,
    changed_at TEXT NOT NULL DEFAULT (datetime('now')),
    changed_by TEXT NOT NULL DEFAULT ''
);
