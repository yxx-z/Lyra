package lyrics

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

var lrcTimestampRe = regexp.MustCompile(`\[\d+:\d+(\.\d+)?\]`)

// hasTimestamps 判断 LRC 文本是否含 [mm:ss] 时间轴（即同步歌词）。
func hasTimestamps(lrc string) bool {
	return lrcTimestampRe.MatchString(lrc)
}

// UpgradeOutcome 报告同步歌词升级结果。
type UpgradeOutcome struct {
	Status string // "upgraded" | "no_synced"
	Source string
}

// UpgradeToSynced 跳过 embedded，向网络源查同步版歌词；仅在找到同步版时替换。
func (s *LyricsService) UpgradeToSynced(ctx context.Context, trackID string) (UpgradeOutcome, error) {
	track, err := s.loadTrack(trackID)
	if err != nil {
		return UpgradeOutcome{}, err
	}
	q := Query{
		TrackName:  track.Title,
		ArtistName: track.Artist,
		AlbumName:  track.Album,
		Duration:   track.Duration,
		FilePath:   track.FilePath,
	}

	for _, p := range s.providers {
		if p.Name() == "embedded" {
			continue // embedded 给的正是要替换的纯文本，跳过
		}
		res, ferr := p.Fetch(ctx, q)
		if ferr != nil {
			if errors.Is(ferr, ErrNotFound) || errors.Is(ferr, ErrInvalidQuery) {
				continue
			}
			return UpgradeOutcome{}, ferr
		}
		// 仅接受同步结果（YRC 或带时间轴的 LRC）
		if strings.TrimSpace(res.YRCContent) != "" || hasTimestamps(res.LRCContent) {
			if err := s.saveLyrics(trackID, res); err != nil {
				return UpgradeOutcome{}, err
			}
			if err := s.updateStatus(trackID, "done"); err != nil {
				return UpgradeOutcome{}, err
			}
			return UpgradeOutcome{Status: "upgraded", Source: res.Source}, nil
		}
		// 命中但仍是纯文本 → 试下一个
	}
	return UpgradeOutcome{Status: "no_synced"}, nil
}

// markSyncChecked 标记某曲目已做过自动同步升级检查。
func (s *LyricsService) markSyncChecked(trackID string) {
	if _, err := s.db.Exec(`UPDATE lyrics SET sync_checked=1 WHERE track_id=?`, trackID); err != nil {
		slog.Warn("标记 sync_checked 失败", "track", trackID, "err", err)
	}
}

// UpgradeStaleLyrics 批量把未检查的纯文本歌词升级为同步版，返回成功升级数。
// 已同步的候选只标记不联网；无同步版也标记（避免反复查询）；瞬时错误不标记（下次重试）。
func (s *LyricsService) UpgradeStaleLyrics(ctx context.Context) (int, error) {
	rows, err := s.db.Query(`
		SELECT track_id, COALESCE(lrc_content,'') FROM lyrics
		WHERE sync_checked=0 AND COALESCE(yrc_content,'')='' AND TRIM(COALESCE(lrc_content,''))<>''`)
	if err != nil {
		return 0, err
	}
	type cand struct{ id, lrc string }
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.lrc); err == nil {
			cands = append(cands, c)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(cands) == 0 {
		return 0, nil
	}
	slog.Info("开始后台同步歌词升级", "待检查", len(cands))

	upgraded := 0
	for _, c := range cands {
		select {
		case <-ctx.Done():
			return upgraded, nil
		default:
		}
		if hasTimestamps(c.lrc) {
			// 已是同步歌词：只标记，不联网
			s.markSyncChecked(c.id)
			continue
		}
		out, err := s.UpgradeToSynced(ctx, c.id)
		if err != nil {
			// 瞬时错误（网络/provider 异常）：不标记，下次扫描重试（但仍节流——已消耗一次网络请求）
			slog.Warn("同步歌词升级失败", "track", c.id, "err", err)
		} else {
			s.markSyncChecked(c.id)
			if out.Status == "upgraded" {
				upgraded++
			}
		}
		// 任何真实网络尝试后都节流（无论成败），对 LRCLIB 礼貌
		select {
		case <-time.After(800 * time.Millisecond):
		case <-ctx.Done():
			return upgraded, nil
		}
	}
	slog.Info("后台同步歌词升级结束", "升级", upgraded)
	return upgraded, nil
}
