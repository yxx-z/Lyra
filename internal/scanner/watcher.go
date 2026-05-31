// internal/scanner/watcher.go
package scanner

import (
	"io/fs"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debouncer coalesces rapid-fire events for the same key into a single call.
type debouncer struct {
	delay  time.Duration
	timers map[string]*time.Timer
	mu     sync.Mutex
}

func newDebouncer(delay time.Duration) *debouncer {
	return &debouncer{delay: delay, timers: make(map[string]*time.Timer)}
}

func (d *debouncer) trigger(key string, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[key]; ok {
		t.Reset(d.delay)
		return
	}
	d.timers[key] = time.AfterFunc(d.delay, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()
		fn()
	})
}

func startWatcher(s *Scanner) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, p := range s.cfg.Paths {
		if err := addDirsRecursive(w, p); err != nil {
			w.Close()
			return err
		}
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer w.Close()
		runWatchLoop(s, w)
	}()
	return nil
}

func addDirsRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		return w.Add(path)
	})
}

func runWatchLoop(s *Scanner, w *fsnotify.Watcher) {
	db := newDebouncer(500 * time.Millisecond)
	for {
		select {
		case <-s.stopCh:
			return
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			handleFSEvent(s, db, event)
		case <-w.Errors:
			// non-fatal; continue
		}
	}
}

func handleFSEvent(s *Scanner, db *debouncer, event fsnotify.Event) {
	path := event.Name
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		if IsAudioFile(path) {
			s.ing.MarkUnavailable(path)
		}
		return
	}
	if !IsAudioFile(path) {
		return
	}
	db.trigger(path, func() {
		meta, err := Read(path, s.cfg.Paths)
		if err != nil {
			return
		}
		s.ing.Ingest(meta)
	})
}
