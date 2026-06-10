package transcode

import "strings"

// Codec 描述一种输出编码的 ffmpeg 参数与 HTTP 元数据。
type Codec struct {
	Name        string   // mp3|opus|aac
	Args        []string // ffmpeg 编码/容器参数（置于 -i 之后、码率/输出之前）
	ContentType string
	Ext         string
}

// 输出编码注册表。
var codecs = map[string]Codec{
	"mp3":  {Name: "mp3", Args: []string{"-c:a", "libmp3lame", "-f", "mp3"}, ContentType: "audio/mpeg", Ext: "mp3"},
	"opus": {Name: "opus", Args: []string{"-c:a", "libopus", "-f", "ogg"}, ContentType: "audio/ogg", Ext: "opus"},
	"aac":  {Name: "aac", Args: []string{"-c:a", "aac", "-f", "adts"}, ContentType: "audio/aac", Ext: "aac"},
}

// codecFor 返回目标编码；未知名回退 mp3。
func codecFor(name string) Codec {
	if c, ok := codecs[strings.ToLower(name)]; ok {
		return c
	}
	return codecs["mp3"]
}

// 直传时按源格式推 Content-Type。
var sourceContentTypes = map[string]string{
	"mp3": "audio/mpeg", "flac": "audio/flac", "m4a": "audio/mp4", "aac": "audio/aac",
	"ogg": "audio/ogg", "opus": "audio/ogg", "wav": "audio/wav", "alac": "audio/mp4", "ape": "audio/x-ape",
}

// contentTypeForSource 直传时按源格式推 Content-Type；未知回退 application/octet-stream。
func contentTypeForSource(format string) string {
	if ct, ok := sourceContentTypes[strings.ToLower(format)]; ok {
		return ct
	}
	return "application/octet-stream"
}
