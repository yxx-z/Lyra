ALTER TABLE tracks ADD COLUMN is_available INTEGER NOT NULL DEFAULT 1;
CREATE INDEX idx_tracks_available ON tracks(is_available);
