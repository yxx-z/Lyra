ALTER TABLE albums ADD COLUMN scrape_status TEXT DEFAULT 'pending';
CREATE INDEX idx_albums_scrape_status ON albums(scrape_status);
