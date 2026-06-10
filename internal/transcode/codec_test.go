package transcode

import "testing"

func TestCodecFor(t *testing.T) {
	if c := codecFor("opus"); c.Name != "opus" || c.ContentType != "audio/ogg" || c.Ext != "opus" {
		t.Errorf("opus 映射不符: %+v", c)
	}
	if c := codecFor("aac"); c.ContentType != "audio/aac" || c.Ext != "aac" {
		t.Errorf("aac 映射不符: %+v", c)
	}
	// 未知名回退 mp3
	if c := codecFor("flac"); c.Name != "mp3" {
		t.Errorf("未知编码应回退 mp3，得到 %s", c.Name)
	}
}

func TestContentTypeForSource(t *testing.T) {
	cases := map[string]string{"mp3": "audio/mpeg", "flac": "audio/flac", "m4a": "audio/mp4", "FLAC": "audio/flac"}
	for in, want := range cases {
		if got := contentTypeForSource(in); got != want {
			t.Errorf("contentTypeForSource(%q)=%q want %q", in, got, want)
		}
	}
	if got := contentTypeForSource("xyz"); got != "application/octet-stream" {
		t.Errorf("未知格式应回退 octet-stream，得到 %q", got)
	}
}
