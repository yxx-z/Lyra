// internal/scanner/scanner.go
package scanner

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/lyrics"
)

// ErrScanInProgress is returned by TriggerScan when a scan is already running.
var ErrScanInProgress = errors.New("扫描正在进行中")

// ScanStatus is a point-in-time snapshot of scanner progress.
type ScanStatus struct {
	Running       bool      `json:"running"`
	Total         int64     `json:"total"`
	Processed     int64     `json:"processed"`
	Errors        int64     `json:"errors"`
	StartedAt     time.Time `json:"started_at"`
	Phase         string    `json:"phase"`
	LyricsScraped int64     `json:"lyrics_scraped"`
}

// Scanner orchestrates directory walking, tag reading, and DB ingestion.
type Scanner struct {
	db          *sql.DB
	cfg         config.LibraryConfig
	ing         *Ingester
	ffprobePath string

	lyricsService *lyrics.LyricsService
	scrapeEnabled bool

	running   atomic.Bool
	total     atomic.Int64
	processed atomic.Int64
	errors    atomic.Int64

	lyricsScraped atomic.Int64
	phase         atomic.Value // string

	mu             sync.RWMutex
	startedAt      time.Time
	watcherStarted bool

	stopCh chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
}

// NewScanner creates a Scanner. Call Start to begin scanning.
func NewScanner(db *sql.DB, cfg config.LibraryConfig, ffprobePath string, lyricsService *lyrics.LyricsService, scrapeEnabled bool) *Scanner {
	s := &Scanner{
		db:            db,
		cfg:           cfg,
		ing:           NewIngester(db),
		ffprobePath:   ffprobePath,
		lyricsService: lyricsService,
		scrapeEnabled: scrapeEnabled,
		stopCh:        make(chan struct{}),
	}
	s.phase.Store("idle")
	return s
}

// Start begins an initial full scan (if paths configured) and starts fsnotify watcher (if cfg.Watch).
func (s *Scanner) Start() error {
	hasPaths := len(s.cfg.Paths) > 0
	if hasPaths {
		if s.running.CompareAndSwap(false, true) {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.doScan()
			}()
		}
	}
	if s.cfg.Watch && hasPaths {
		s.mu.Lock()
		alreadyStarted := s.watcherStarted
		if !alreadyStarted {
			s.watcherStarted = true
		}
		s.mu.Unlock()
		if !alreadyStarted {
			if err := startWatcher(s); err != nil {
				// watcher failure is non-fatal
				_ = err
			}
		}
	}
	return nil
}

// TriggerScan starts a full scan in the background. Returns ErrScanInProgress if already running.
func (s *Scanner) TriggerScan() error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrScanInProgress
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.doScan()
	}()
	return nil
}

// Status returns a snapshot of current scan progress.
func (s *Scanner) Status() ScanStatus {
	s.mu.RLock()
	startedAt := s.startedAt
	s.mu.RUnlock()
	phase, _ := s.phase.Load().(string)
	if phase == "" {
		phase = "idle"
	}
	return ScanStatus{
		Running:       s.running.Load(),
		Total:         s.total.Load(),
		Processed:     s.processed.Load(),
		Errors:        s.errors.Load(),
		StartedAt:     startedAt,
		Phase:         phase,
		LyricsScraped: s.lyricsScraped.Load(),
	}
}

// Stop signals the scanner to halt and waits for all goroutines to exit.
// Safe to call multiple times — subsequent calls are no-ops.
func (s *Scanner) Stop() {
	s.once.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

// doScan performs the actual scan. Caller must set running=true before calling.
func (s *Scanner) doScan() {
	defer s.running.Store(false)

	s.total.Store(0)
	s.processed.Store(0)
	s.errors.Store(0)
	s.lyricsScraped.Store(0)
	s.phase.Store("scanning")
	s.mu.Lock()
	s.startedAt = time.Now()
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Track the cancel-goroutine in wg so Stop() waits for it to exit cleanly.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-s.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	paths := Walk(ctx, s.cfg.Paths)

	type result struct {
		meta TrackMeta
		err  error
	}
	results := make(chan result, 8)

	const numWorkers = 4
	var workerWg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		workerWg.Add(1)
		go func() {
			defer workerWg.Done()
			for path := range paths {
				s.total.Add(1)
				meta, err := Read(path, s.cfg.Paths, s.ffprobePath)
				results <- result{meta, err}
			}
		}()
	}

	go func() {
		workerWg.Wait()
		close(results)
	}()

	for r := range results {
		if r.err != nil {
			s.errors.Add(1)
			continue
		}
		if err := s.ing.Ingest(r.meta); err != nil {
			s.errors.Add(1)
		} else {
			s.processed.Add(1)
		}
	}

	// 刮削阶段：为缺歌词的曲目串行刮削（受 scraper.enabled 控制，可被 ctx 中断）
	if s.scrapeEnabled && s.lyricsService != nil {
		s.phase.Store("scraping")
		s.scrapePending(ctx)
	}
	s.phase.Store("idle")
}

func (s *Scanner) scrapePending(ctx context.Context) {
	rows, err := s.db.Query(`SELECT id FROM tracks WHERE scrape_status='pending' AND is_available=1`)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()

	for _, id := range ids {
		select {
		case <-ctx.Done():
			return
		default:
		}
		outcome, err := s.lyricsService.ScrapeTrack(ctx, id)
		if err != nil || outcome.Status == "failed" {
			s.errors.Add(1)
		} else if outcome.Status == "done" {
			s.lyricsScraped.Add(1)
		}
		if outcome.Status == "failed" || (outcome.Status == "done" && outcome.Source != "embedded") {
			select {
			case <-time.After(800 * time.Millisecond):
			case <-ctx.Done():
				return
			}
		}
	}
}

