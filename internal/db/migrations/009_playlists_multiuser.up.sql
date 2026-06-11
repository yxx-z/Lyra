-- 歌单：加 user_id 归属 + comment/updated_at
CREATE TABLE playlists_new (
    id         TEXT PRIMARY KEY,
    user_id    TEXT REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO playlists_new (id, user_id, name, created_at)
    SELECT id, NULL, name, created_at FROM playlists;
DROP TABLE playlists;
ALTER TABLE playlists_new RENAME TO playlists;

-- 歌单曲目：FK 加 ON DELETE CASCADE
CREATE TABLE playlist_tracks_new (
    id          INTEGER PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    track_id    TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL
);
INSERT INTO playlist_tracks_new (id, playlist_id, track_id, position)
    SELECT id, playlist_id, track_id, position FROM playlist_tracks;
DROP TABLE playlist_tracks;
ALTER TABLE playlist_tracks_new RENAME TO playlist_tracks;
CREATE UNIQUE INDEX idx_playlist_tracks_pos ON playlist_tracks(playlist_id, position);
