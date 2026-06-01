// internal/api/v1/transcode_cache.go
package v1

import (
	"fmt"
	"path/filepath"
	"sync"
)

// TranscodeCache manages on-disk cached transcode outputs and per-key locks
// that prevent two requests from transcoding the same track concurrently.
type TranscodeCache struct {
	dir      string
	mu       sync.Mutex
	// inflight maps cache key → per-key lock. It grows bounded by the number of
	// distinct (trackID, format, bitrate) combinations (≈ library size), not by
	// request count; entries are never removed, which is acceptable at this scale.
	inflight map[string]*sync.Mutex
}

// NewTranscodeCache creates a cache rooted at dir.
func NewTranscodeCache(dir string) *TranscodeCache {
	return &TranscodeCache{
		dir:      dir,
		inflight: make(map[string]*sync.Mutex),
	}
}

// Path returns the cache file path for a track in the given format and bitrate.
// The trackID is sanitised to its base name so it cannot escape the cache dir.
func (c *TranscodeCache) Path(trackID, format string, bitrate int) string {
	safeID := filepath.Base(trackID)
	name := fmt.Sprintf("%s_%s_%dk.%s", safeID, format, bitrate, format)
	return filepath.Join(c.dir, name)
}

// key derives the lock key from track parameters.
func (c *TranscodeCache) key(trackID, format string, bitrate int) string {
	return fmt.Sprintf("%s_%s_%dk", filepath.Base(trackID), format, bitrate)
}

// lockFor returns the mutex guarding a cache key, creating it lazily.
func (c *TranscodeCache) lockFor(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.inflight[key]; ok {
		return m
	}
	m := &sync.Mutex{}
	c.inflight[key] = m
	return m
}
