ALTER TABLE lyrics ADD COLUMN sync_checked INTEGER DEFAULT 0;
CREATE INDEX idx_lyrics_sync_checked ON lyrics(sync_checked);
