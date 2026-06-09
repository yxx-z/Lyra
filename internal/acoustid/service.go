package acoustid

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
)

// ErrTrackNotFound 表示数据库无此曲目。
var ErrTrackNotFound = errors.New("曲目不存在")

// IdentifyOutcome 是 IdentifyTrack 的结果。
type IdentifyOutcome struct {
	Status   string // "identified" | "nomatch"
	AcoustID string
	MBID     string
}

// FingerprintService 编排单曲指纹识别。
type FingerprintService struct {
	db     *sql.DB
	fp     Fingerprinter
	client *AcoustIDClient
}

// NewFingerprintService 创建服务。
func NewFingerprintService(db *sql.DB, fp Fingerprinter, client *AcoustIDClient) *FingerprintService {
	return &FingerprintService{db: db, fp: fp, client: client}
}

// IdentifyTrack 为单曲算指纹并经 AcoustID 识别，落库 acoustid/mbid。
func (s *FingerprintService) IdentifyTrack(ctx context.Context, trackID string) (IdentifyOutcome, error) {
	var filePath string
	err := s.db.QueryRowContext(ctx, `SELECT file_path FROM tracks WHERE id=?`, trackID).Scan(&filePath)
	if errors.Is(err, sql.ErrNoRows) {
		return IdentifyOutcome{}, ErrTrackNotFound
	}
	if err != nil {
		return IdentifyOutcome{}, err
	}

	dur, fingerprint, err := s.fp.Calc(ctx, filePath)
	if err != nil {
		return IdentifyOutcome{}, err // 瞬时：acoustid 保持 NULL，下次重试
	}

	res, err := s.client.Lookup(ctx, dur, fingerprint)
	if errors.Is(err, ErrNoMatch) {
		if _, e := s.db.ExecContext(ctx, `UPDATE tracks SET acoustid='' WHERE id=?`, trackID); e != nil {
			slog.Warn("标记 acoustid 空失败", "track", trackID, "err", e)
		}
		return IdentifyOutcome{Status: "nomatch"}, nil
	}
	if err != nil {
		return IdentifyOutcome{}, err // 瞬时
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE tracks SET acoustid=? WHERE id=?`, res.AcoustID, trackID); err != nil {
		return IdentifyOutcome{}, err
	}
	if res.MBID != "" {
		// mbid UNIQUE：冲突仅 warn 不致命
		if _, err := s.db.ExecContext(ctx, `UPDATE tracks SET mbid=? WHERE id=?`, res.MBID, trackID); err != nil {
			slog.Warn("设置曲目 mbid 失败（可能 UNIQUE 冲突）", "track", trackID, "mbid", res.MBID, "err", err)
		}
	}
	return IdentifyOutcome{Status: "identified", AcoustID: res.AcoustID, MBID: res.MBID}, nil
}
