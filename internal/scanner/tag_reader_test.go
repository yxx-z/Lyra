// internal/scanner/tag_reader_test.go
package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsAudioFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"song.mp3", true},
		{"song.FLAC", true},
		{"song.m4a", true},
		{"song.ogg", true},
		{"song.opus", true},
		{"song.wav", true},
		{"song.aiff", true},
		{"song.wma", true},
		{"song.txt", false},
		{"song.jpg", false},
		{"song.mp4", false},
	}
	for _, c := range cases {
		if got := IsAudioFile(c.path); got != c.want {
			t.Errorf("IsAudioFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestParseArtistAlbum(t *testing.T) {
	cases := []struct {
		input      string
		wantArtist string
		wantAlbum  string
	}{
		{
			"蔡琴 - 金片子 贰・魂萦旧梦 (2015) - WEB-DL - 16bit ALAC-HHWEB",
			"蔡琴",
			"金片子 贰・魂萦旧梦",
		},
		{
			"周杰伦 - 叶惠美 (2003)",
			"周杰伦",
			"叶惠美",
		},
		{
			"The Beatles - Abbey Road (1969) [FLAC]",
			"The Beatles",
			"Abbey Road",
		},
		{
			"NoSeparatorAlbum",
			"",
			"NoSeparatorAlbum",
		},
		{
			"周杰伦 - 七里香 (2004) - FLAC 24bit 96kHz",
			"周杰伦",
			"七里香",
		},
	}
	for _, c := range cases {
		gotArtist, gotAlbum := parseArtistAlbum(c.input)
		if gotArtist != c.wantArtist {
			t.Errorf("parseArtistAlbum(%q).artist = %q, want %q", c.input, gotArtist, c.wantArtist)
		}
		if gotAlbum != c.wantAlbum {
			t.Errorf("parseArtistAlbum(%q).album = %q, want %q", c.input, gotAlbum, c.wantAlbum)
		}
	}
}

func TestParseTrackNumber(t *testing.T) {
	cases := []struct {
		filename string
		want     int
	}{
		{"01. 渡口.flac", 1},
		{"02 - 被遗忘的时光.flac", 2},
		{"10_天涯.mp3", 10},
		{"无编号.flac", 0},
	}
	for _, c := range cases {
		if got := parseTrackNumber(c.filename); got != c.want {
			t.Errorf("parseTrackNumber(%q) = %d, want %d", c.filename, got, c.want)
		}
	}
}

func TestInferFromPath_DoubleLayer(t *testing.T) {
	// /music/蔡琴/金片子/01.flac → artist=蔡琴, album=金片子
	meta := TrackMeta{FilePath: filepath.Join("/music", "蔡琴", "金片子", "01.flac")}
	result := inferFromPath(meta, []string{"/music"})
	if result.Artist != "蔡琴" {
		t.Errorf("artist = %q, want 蔡琴", result.Artist)
	}
	if result.Album != "金片子" {
		t.Errorf("album = %q, want 金片子", result.Album)
	}
}

func TestInferFromPath_SingleLayerWithFormat(t *testing.T) {
	// /music/蔡琴 - 金片子 (2015) - WEB-DL/01.flac → artist=蔡琴, album=金片子
	meta := TrackMeta{FilePath: filepath.Join("/music", "蔡琴 - 金片子 (2015) - WEB-DL", "01.flac")}
	result := inferFromPath(meta, []string{"/music"})
	if result.Artist != "蔡琴" {
		t.Errorf("artist = %q, want 蔡琴", result.Artist)
	}
	if result.Album != "金片子" {
		t.Errorf("album = %q, want 金片子", result.Album)
	}
}

func TestInferFromPath_SingleLayerNoSeparator(t *testing.T) {
	// /music/杂项/song.flac → artist 空，album=杂项
	meta := TrackMeta{FilePath: filepath.Join("/music", "杂项", "song.flac")}
	result := inferFromPath(meta, []string{"/music"})
	if result.Artist != "" {
		t.Errorf("artist = %q, want empty", result.Artist)
	}
	if result.Album != "杂项" {
		t.Errorf("album = %q, want 杂项", result.Album)
	}
}

func TestInferFromPath_DirectlyInRoot(t *testing.T) {
	// /music/song.flac → 无目录推断
	meta := TrackMeta{FilePath: filepath.Join("/music", "song.flac")}
	result := inferFromPath(meta, []string{"/music"})
	if result.Artist != "" {
		t.Errorf("artist should be empty for root-level file, got %q", result.Artist)
	}
	if result.Album != "" {
		t.Errorf("album should be empty for root-level file, got %q", result.Album)
	}
}

func TestRead_FfprobeUnavailable_DurationZero(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(f, []byte("notreal"), 0644); err != nil {
		t.Fatal(err)
	}
	// ffprobe 路径无效 → duration 应保持 0，且不报错
	meta, err := Read(f, []string{dir}, "/nonexistent/ffprobe")
	if err != nil {
		t.Fatalf("Read should not fail when ffprobe missing: %v", err)
	}
	if meta.Duration != 0 {
		t.Errorf("Duration = %d, want 0 (ffprobe unavailable)", meta.Duration)
	}
}
