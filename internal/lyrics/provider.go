// internal/lyrics/provider.go
package lyrics

import (
	"context"
	"errors"
)

var (
	ErrInvalidQuery = errors.New("歌词查询参数不足")
	ErrNotFound     = errors.New("歌词不存在")
)

// Query contains track metadata used by lyrics providers.
type Query struct {
	TrackName  string
	ArtistName string
	AlbumName  string
	Duration   int
	FilePath   string // 内嵌源读取文件用
}

// Result contains provider-normalized lyrics content.
type Result struct {
	LRCContent   string
	PlainContent string
	YRCContent   string // 预留网易云逐字歌词
	Source       string // "embedded" / "lrclib" / "netease"
}

// Provider is a single lyrics source.
type Provider interface {
	Name() string
	Fetch(ctx context.Context, q Query) (Result, error)
}
