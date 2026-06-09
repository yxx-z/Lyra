package lyrics

import (
	"context"
	"errors"
	"regexp"
	"strings"
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
