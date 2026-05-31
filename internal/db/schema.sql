-- internal/db/schema.sql
-- Schema 参考文件，变更时同步写一个新的 migrations/*.up.sql

CREATE TABLE artists (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    sort_name   TEXT,
    biography   TEXT,
    image_url   TEXT,
    mbid        TEXT UNIQUE,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE albums (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    artist_id    TEXT REFERENCES artists(id),
    release_date TEXT,
    genre        TEXT,
    cover_path   TEXT,
    mbid         TEXT UNIQUE,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tracks (
    id             TEXT PRIMARY KEY,
    title          TEXT NOT NULL,
    artist_id      TEXT REFERENCES artists(id),
    album_id       TEXT REFERENCES albums(id),
    track_number   INTEGER,
    disc_number    INTEGER DEFAULT 1,
    duration       INTEGER,
    file_path      TEXT NOT NULL UNIQUE,
    file_size      INTEGER,
    format         TEXT,
    bitrate        INTEGER,
    sample_rate    INTEGER,
    channels       INTEGER,
    mbid           TEXT UNIQUE,
    acoustid       TEXT,
    scrape_status  TEXT DEFAULT 'pending',
    play_count     INTEGER DEFAULT 0,
    last_played_at DATETIME,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE lyrics (
    track_id    TEXT PRIMARY KEY REFERENCES tracks(id),
    lrc_content TEXT,
    yrc_content TEXT,
    source      TEXT,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE playlists (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE playlist_tracks (
    id          INTEGER PRIMARY KEY,
    playlist_id TEXT NOT NULL REFERENCES playlists(id),
    track_id    TEXT NOT NULL REFERENCES tracks(id),
    position    INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_playlist_tracks_pos ON playlist_tracks(playlist_id, position);

CREATE INDEX idx_tracks_album         ON tracks(album_id);
CREATE INDEX idx_tracks_artist        ON tracks(artist_id);
CREATE INDEX idx_tracks_scrape_status ON tracks(scrape_status);
CREATE INDEX idx_albums_artist        ON albums(artist_id);
