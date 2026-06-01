// internal/scanner/tag_reader.go
package scanner

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true,
	".ogg": true, ".opus": true, ".wav": true,
	".aiff": true, ".aif": true, ".wma": true,
}

var (
	reYear    = regexp.MustCompile(`\s*\(\d{4}\)\s*$`)
	reFormat  = regexp.MustCompile(`(?i)\s*-\s*(WEB|FLAC|MP3|AAC|ALAC|DL|CDRip|320k|16bit|24bit|SACD|HDCD).*$`)
	reBracket = regexp.MustCompile(`\s*\[.*?\]\s*$`)
	reTrackNo = regexp.MustCompile(`^(\d+)[\.\s\-_]+`)
)

// TrackMeta holds normalised metadata for a single audio file.
type TrackMeta struct {
	FilePath    string
	FileSize    int64
	Format      string
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	TrackNumber int
	DiscNumber  int
	Year        int
	Genre       string
	Duration    int // seconds (0 if ffprobe unavailable)
	Bitrate     int // kbps   (0 if ffprobe unavailable)
	SampleRate  int // Hz     (0 if ffprobe unavailable)
	Channels    int //        (0 if ffprobe unavailable)
}

// IsAudioFile reports whether path has a supported audio extension.
func IsAudioFile(path string) bool {
	return audioExts[strings.ToLower(filepath.Ext(path))]
}

// Read extracts metadata from path, falling back to path inference then defaults.
func Read(path string, libraryPaths []string, ffprobePath string) (TrackMeta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return TrackMeta{}, err
	}

	ext := strings.ToLower(filepath.Ext(path))
	meta := TrackMeta{
		FilePath: path,
		FileSize: info.Size(),
		Format:   strings.TrimPrefix(ext, "."),
	}

	// Priority 1: embedded tags
	if f, err := os.Open(path); err == nil {
		if t, err := tag.ReadFrom(f); err == nil {
			meta.Title = t.Title()
			meta.Artist = t.Artist()
			meta.AlbumArtist = t.AlbumArtist()
			meta.Album = t.Album()
			meta.Year = t.Year()
			meta.Genre = t.Genre()
			n, _ := t.Track()
			meta.TrackNumber = n
			d, _ := t.Disc()
			meta.DiscNumber = d
		}
		f.Close()
	}

	// Priority 2 & 3: path and filename inference for missing fields
	meta = inferFromPath(meta, libraryPaths)

	// Priority 4: final defaults
	base := filepath.Base(path)
	nameNoExt := strings.TrimSuffix(base, filepath.Ext(base))
	if meta.Title == "" {
		title := reTrackNo.ReplaceAllString(nameNoExt, "")
		meta.Title = strings.TrimSpace(title)
		if meta.Title == "" {
			meta.Title = nameNoExt
		}
	}
	if meta.Artist == "" {
		meta.Artist = "未知艺术家"
	}
	if meta.Album == "" {
		meta.Album = "未知专辑"
	}

	// ffprobe 提取音频属性（失败保持 0，不阻断扫描）
	if ffprobePath != "" {
		if props, err := Probe(ffprobePath, path); err == nil {
			meta.Duration = props.Duration
			meta.Bitrate = props.Bitrate
			meta.SampleRate = props.SampleRate
			meta.Channels = props.Channels
		}
	}

	return meta, nil
}

// inferFromPath fills missing Artist/Album/TrackNumber using directory structure.
func inferFromPath(meta TrackMeta, libraryPaths []string) TrackMeta {
	dir := filepath.Dir(meta.FilePath)
	depth := dirDepth(dir, libraryPaths)

	switch {
	case depth >= 2:
		parent := filepath.Base(dir)
		grandparent := filepath.Base(filepath.Dir(dir))
		if meta.Artist == "" {
			meta.Artist = grandparent
		}
		if meta.Album == "" {
			meta.Album = parent
		}
	case depth == 1:
		parent := filepath.Base(dir)
		artist, album := parseArtistAlbum(parent)
		if meta.Artist == "" {
			meta.Artist = artist
		}
		if meta.Album == "" {
			meta.Album = album
		}
	}

	if meta.TrackNumber == 0 {
		meta.TrackNumber = parseTrackNumber(filepath.Base(meta.FilePath))
	}

	return meta
}

// dirDepth returns how many directory levels dir is below one of the library roots.
func dirDepth(dir string, roots []string) int {
	for _, root := range roots {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			continue
		}
		if rel == "." {
			return 0
		}
		if strings.HasPrefix(rel, "..") {
			continue
		}
		return len(strings.Split(rel, string(filepath.Separator)))
	}
	return -1
}

// parseArtistAlbum splits "Artist - Album (Year) - Format" into artist and cleaned album.
func parseArtistAlbum(name string) (artist, album string) {
	idx := strings.Index(name, " - ")
	if idx <= 0 {
		return "", cleanAlbumName(name)
	}
	artist = strings.TrimSpace(name[:idx])
	album = cleanAlbumName(strings.TrimSpace(name[idx+3:]))
	if album == "" {
		return "", cleanAlbumName(name)
	}
	return
}

func cleanAlbumName(s string) string {
	s = reFormat.ReplaceAllString(s, "")
	s = reBracket.ReplaceAllString(s, "")
	s = reYear.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

var reNumOnly = regexp.MustCompile(`^(\d+)$`)

func parseTrackNumber(filename string) int {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	if m := reTrackNo.FindStringSubmatch(name); len(m) >= 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	// 兜底：文件名去扩展名后纯为数字（如 "01.flac"）
	if m := reNumOnly.FindStringSubmatch(name); len(m) >= 2 {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}
