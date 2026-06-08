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

// EnrichAlbum 为单张专辑查 MB 补元数据并下封面。
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

	match, err := s.mb.Search(ctx, AlbumQuery{AlbumTitle: title, ArtistName: artist, TrackCount: trackCount})
	if errors.Is(err, ErrNotFound) {
		s.setStatus(ctx, albumID, "failed")
		return EnrichOutcome{Status: "failed"}, nil
	}
	if err != nil {
		return EnrichOutcome{}, err
	}

	// 元数据落库：release_date/genre 用 COALESCE(NULLIF) 仅在非空时覆盖。
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
