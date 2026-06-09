package metadata

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrAlbumNotFound 表示数据库中无此专辑。
var ErrAlbumNotFound = errors.New("专辑不存在")

// EnrichOutcome 是 EnrichAlbum 的结果。
type EnrichOutcome struct {
	Status   string // "done" | "failed"
	MBID     string
	HasCover bool
}

// MetadataService 编排专辑元数据 + 封面刮削。
type MetadataService struct {
	db         *sql.DB
	mb         *MusicBrainzClient
	cover      *CoverArtClient
	artworkDir string
}

// NewMetadataService 创建服务。
func NewMetadataService(db *sql.DB, mb *MusicBrainzClient, cover *CoverArtClient, artworkDir string) *MetadataService {
	return &MetadataService{db: db, mb: mb, cover: cover, artworkDir: artworkDir}
}

// EnrichAlbum 为单张专辑补元数据 + 封面：指纹优先，文本搜索兜底。
func (s *MetadataService) EnrichAlbum(ctx context.Context, albumID string) (EnrichOutcome, error) {
	var title, artist string
	var trackCount int
	err := s.db.QueryRowContext(ctx, `
		SELECT a.title, COALESCE(ar.name,''),
		       (SELECT COUNT(*) FROM tracks WHERE album_id=a.id AND is_available=1)
		FROM albums a LEFT JOIN artists ar ON ar.id=a.artist_id
		WHERE a.id=?`, albumID).Scan(&title, &artist, &trackCount)
	if errors.Is(err, sql.ErrNoRows) {
		return EnrichOutcome{}, ErrAlbumNotFound
	}
	if err != nil {
		return EnrichOutcome{}, err
	}

	// 指纹路径：本专辑曲目有 recording MBID 时，投票定位权威 release。
	match, ok := s.resolveByFingerprint(ctx, albumID)
	if !ok {
		// 文本兜底
		var ferr error
		match, ferr = s.mb.Search(ctx, AlbumQuery{AlbumTitle: title, ArtistName: artist, TrackCount: trackCount})
		if errors.Is(ferr, ErrNotFound) {
			s.setStatus(ctx, albumID, "failed")
			return EnrichOutcome{Status: "failed"}, nil
		}
		if ferr != nil {
			return EnrichOutcome{}, ferr
		}
	}
	return s.applyMatch(ctx, albumID, match)
}

// resolveByFingerprint 用本专辑曲目的 recording MBID 投票定位 release；无可用指纹/无结果 → false。
func (s *MetadataService) resolveByFingerprint(ctx context.Context, albumID string) (ReleaseMatch, bool) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mbid FROM tracks
		WHERE album_id=? AND is_available=1 AND mbid IS NOT NULL AND mbid<>''
		LIMIT 5`, albumID)
	if err != nil {
		return ReleaseMatch{}, false
	}
	var recMBIDs []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err == nil {
			recMBIDs = append(recMBIDs, m)
		}
	}
	rows.Close()
	if len(recMBIDs) == 0 {
		return ReleaseMatch{}, false
	}

	var releasesPerTrack [][]string
	for _, rm := range recMBIDs {
		rels, err := s.mb.RecordingReleases(ctx, rm)
		if err != nil {
			continue // 瞬时失败：跳过该曲，靠其余曲目投票
		}
		releasesPerTrack = append(releasesPerTrack, rels)
	}
	releaseMBID, ok := pickByVote(releasesPerTrack)
	if !ok {
		return ReleaseMatch{}, false
	}
	date, _ := s.mb.ReleaseDate(ctx, releaseMBID) // best-effort，失败则 date 空
	return ReleaseMatch{MBID: releaseMBID, ReleaseDate: date}, true
}

// applyMatch 把选中的 release 落库（元数据 + mbid + 封面 + 状态 done）。
func (s *MetadataService) applyMatch(ctx context.Context, albumID string, match ReleaseMatch) (EnrichOutcome, error) {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE albums SET
			release_date=COALESCE(NULLIF(?,''),release_date),
			genre=COALESCE(NULLIF(?,''),genre),
			updated_at=?
		WHERE id=?`,
		match.ReleaseDate, match.Genre, time.Now(), albumID); err != nil {
		return EnrichOutcome{}, err
	}
	// mbid 单独 best-effort 设置：UNIQUE 冲突（别的专辑已占）时跳过，不致命。
	if _, err := s.db.ExecContext(ctx, `UPDATE albums SET mbid=? WHERE id=?`, match.MBID, albumID); err != nil {
		slog.Warn("设置专辑 mbid 失败（可能 UNIQUE 冲突）", "album", albumID, "mbid", match.MBID, "err", err)
	}
	hasCover := s.downloadCover(ctx, albumID, match.MBID)
	s.setStatus(ctx, albumID, "done")
	return EnrichOutcome{Status: "done", MBID: match.MBID, HasCover: hasCover}, nil
}

// downloadCover 下封面到 artworkDir 并写 cover_path；成功返回 true。
func (s *MetadataService) downloadCover(ctx context.Context, albumID, mbid string) bool {
	data, mime, err := s.cover.FetchFront(ctx, mbid)
	if err != nil {
		if !errors.Is(err, ErrNoCover) {
			slog.Warn("下载封面失败", "album", albumID, "err", err)
		}
		return false
	}
	ext := ".jpg"
	if strings.HasPrefix(mime, "image/png") {
		ext = ".png"
	}
	if err := os.MkdirAll(s.artworkDir, 0o755); err != nil {
		slog.Warn("创建封面目录失败", "dir", s.artworkDir, "err", err)
		return false
	}
	path := filepath.Join(s.artworkDir, albumID+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Warn("写封面文件失败", "path", path, "err", err)
		return false
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE albums SET cover_path=? WHERE id=?`, path, albumID); err != nil {
		slog.Warn("写 cover_path 失败", "album", albumID, "err", err)
		return false
	}
	return true
}

func (s *MetadataService) setStatus(ctx context.Context, albumID, status string) {
	if _, err := s.db.ExecContext(ctx, `UPDATE albums SET scrape_status=? WHERE id=?`, status, albumID); err != nil {
		slog.Warn("更新专辑 scrape_status 失败", "album", albumID, "err", err)
	}
}
