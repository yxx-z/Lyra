package metadata

import "errors"

// ErrNotFound 表示未匹配到合适的 release。
var ErrNotFound = errors.New("未匹配到专辑")

// mbRelease 是 MusicBrainz release 搜索结果中我们关心的字段。
type mbRelease struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Score      int    `json:"score"`
	Date       string `json:"date"`
	TrackCount int    `json:"track-count"`
}

// AlbumQuery 是元数据查询输入。
type AlbumQuery struct {
	AlbumTitle string
	ArtistName string
	TrackCount int // 本地该专辑曲目数，用于在多个 release 中择优
}

// ReleaseMatch 是选中的 release。
type ReleaseMatch struct {
	MBID        string
	Title       string
	ReleaseDate string
	Genre       string // 本轮恒为空（MB search 无可靠 genre 字段）
}

// pickRelease 过滤 score>=90，在剩余里选曲目数最接近 localTrackCount 的；
// 并列取靠前者；localTrackCount<=0 时取靠前者（MB 已按 score 降序）。
func pickRelease(releases []mbRelease, localTrackCount int) (mbRelease, bool) {
	bestIdx := -1
	bestDiff := 0
	for i, r := range releases {
		if r.Score < 90 {
			continue
		}
		if bestIdx == -1 {
			bestIdx = i
			bestDiff = absDiff(r.TrackCount, localTrackCount)
			if localTrackCount <= 0 {
				return r, true // 取首个满足阈值者
			}
			continue
		}
		if localTrackCount > 0 {
			if d := absDiff(r.TrackCount, localTrackCount); d < bestDiff {
				bestIdx = i
				bestDiff = d
			}
		}
	}
	if bestIdx == -1 {
		return mbRelease{}, false
	}
	return releases[bestIdx], true
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
