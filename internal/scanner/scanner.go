// internal/scanner/scanner.go
package scanner

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yxx-z/lyra/internal/acoustid"
	"github.com/yxx-z/lyra/internal/config"
	"github.com/yxx-z/lyra/internal/lyrics"
	"github.com/yxx-z/lyra/internal/metadata"
)

// ErrScanInProgress is returned by TriggerScan when a scan is already running.
var ErrScanInProgress = errors.New("扫描正在进行中")

// ScrapeServices 聚合后台刮削/识别服务，任一可为 nil。
type ScrapeServices struct {
	Lyrics      *lyrics.LyricsService
	Metadata    *metadata.MetadataService
	Fingerprint *acoustid.FingerprintService
}

// ScanStatus is a point-in-time snapshot of scanner progress.
type ScanStatus struct {
	Running       bool      `json:"running"`
	Total         int64     `json:"total"`
	Processed     int64     `json:"processed"`
	Errors        int64     `json:"errors"`
	StartedAt     time.Time `json:"started_at"`
	Phase         string    `json:"phase"`
	LyricsScraped int64     `json:"lyrics_scraped"`
	AlbumsScraped int64     `json:"albums_scraped"`
	Fingerprinted int64     `json:"fingerprinted"`
}

// Scanner orchestrates directory walking, tag reading, and DB ingestion.
type Scanner struct {
	db          *sql.DB
	cfg         config.LibraryConfig
	ing         *Ingester
	ffprobePath string

	services      ScrapeServices
	scrapeEnabled bool

	running   atomic.Bool
	total     atomic.Int64
	processed atomic.Int64
	errors    atomic.Int64

	lyricsScraped atomic.Int64
	albumsScraped atomic.Int64
	fingerprinted atomic.Int64
	phase         atomic.Value // string

	mu             sync.RWMutex
	startedAt      time.Time
	watcherStarted bool

	stopCh chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
}

// NewScanner creates a Scanner. Call Start to begin scanning.
func NewScanner(db *sql.DB, cfg config.LibraryConfig, ffprobePath string, services ScrapeServices, scrapeEnabled bool) *Scanner {
	s := &Scanner{
		db:            db,
		cfg:           cfg,
		ing:           NewIngester(db),
		ffprobePath:   ffprobePath,
		services:      services,
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
		AlbumsScraped: s.albumsScraped.Load(),
		Fingerprinted: s.fingerprinted.Load(),
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
	s.albumsScraped.Store(0)
	s.fingerprinted.Store(0)
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
	if s.scrapeEnabled && s.services.Lyrics != nil {
		s.phase.Store("scraping")
		s.scrapePending(ctx)
	}
	if s.scrapeEnabled && s.services.Metadata != nil {
		s.phase.Store("metadata")
		s.scrapeAlbumsPending(ctx)
	}
	if s.scrapeEnabled && s.services.Fingerprint != nil {
		s.phase.Store("fingerprint")
		s.fingerprintPending(ctx)
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
	// 修复 1：检查行遍历是否出错
	if err := rows.Err(); err != nil {
		slog.Warn("刮削阶段遍历待处理曲目出错", "err", err)
	}

	if len(ids) == 0 {
		return
	}
	// 修复 3：起始日志
	slog.Info("开始后台歌词刮削", "待处理", len(ids))

	for _, id := range ids {
		select {
		case <-ctx.Done():
			return
		default:
		}
		outcome, err := s.services.Lyrics.ScrapeTrack(ctx, id)
		if err != nil || outcome.Status == "failed" {
			s.errors.Add(1)
		} else if outcome.Status == "done" {
			s.lyricsScraped.Add(1)
		}
		// 修复 2：err 路径也限流
		shouldThrottle := err != nil || outcome.Status == "failed" || (outcome.Status == "done" && outcome.Source != "embedded")
		if shouldThrottle {
			select {
			case <-time.After(800 * time.Millisecond):
			case <-ctx.Done():
				return
			}
		}
	}

	// 修复 3：结束日志
	slog.Info("后台歌词刮削结束", "成功", s.lyricsScraped.Load())
}

func (s *Scanner) scrapeAlbumsPending(ctx context.Context) {
	rows, err := s.db.Query(`SELECT id FROM albums WHERE scrape_status='pending'`)
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
	if err := rows.Err(); err != nil {
		slog.Warn("元数据阶段遍历待处理专辑出错", "err", err)
	}
	if len(ids) == 0 {
		return
	}
	slog.Info("开始后台专辑元数据刮削", "待处理", len(ids))

	for _, id := range ids {
		select {
		case <-ctx.Done():
			return
		default:
		}
		outcome, err := s.services.Metadata.EnrichAlbum(ctx, id)
		// EnrichAlbum 透传 MB 瞬时错误（网络/5xx）时不改 scrape_status，
		// 专辑保持 'pending'，下次扫描自动重试；仅 "failed"（无匹配）会被持久化。
		if err != nil || outcome.Status == "failed" {
			s.errors.Add(1)
		} else if outcome.Status == "done" {
			s.albumsScraped.Add(1)
		}
		// MusicBrainz 限速 1 req/s：每张专辑后固定等待 ≥1s（可被 ctx 中断）。
		select {
		case <-time.After(1100 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
	slog.Info("后台专辑元数据刮削结束", "成功", s.albumsScraped.Load())
}

func (s *Scanner) fingerprintPending(ctx context.Context) {
	rows, err := s.db.Query(`SELECT id FROM tracks WHERE acoustid IS NULL AND is_available=1`)
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
	if err := rows.Err(); err != nil {
		slog.Warn("指纹阶段遍历待识别曲目出错", "err", err)
	}
	if len(ids) == 0 {
		return
	}
	slog.Info("开始后台指纹识别", "待处理", len(ids))

	for _, id := range ids {
		select {
		case <-ctx.Done():
			return
		default:
		}
		outcome, err := s.services.Fingerprint.IdentifyTrack(ctx, id)
		if err != nil {
			s.errors.Add(1) // 瞬时错误（fpcalc/网络）：acoustid 留 NULL，下次重试
		} else if outcome.Status == "identified" {
			s.fingerprinted.Add(1)
		}
		// AcoustID 限速：每曲后等待 ~350ms（可被 ctx 中断）
		select {
		case <-time.After(350 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
	slog.Info("后台指纹识别结束", "成功", s.fingerprinted.Load())
}

