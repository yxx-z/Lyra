package transcode

import "strings"

// Source 来自 tracks 表的一行。
type Source struct {
	ID      string
	Path    string
	Format  string // 小写最佳；为空时调用方应已用扩展名兜底
	Bitrate int    // kbps，0 = 未知
}

// Params 来自客户端请求。
type Params struct {
	Format     string // raw|mp3|opus|aac|""（未指定）
	MaxBitRate int    // kbps，0 = 未指定
}

// Decision 决定直传或转码及其参数。
type Decision struct {
	Passthrough bool
	Codec       string // 转码时 mp3|opus|aac
	Bitrate     int    // 转码时 kbps
	ContentType string
	Ext         string // 转码时缓存扩展名
}

var losslessFormats = map[string]bool{"flac": true, "wav": true, "alac": true, "ape": true}

func isLossless(format string) bool { return losslessFormats[strings.ToLower(format)] }

// Plan 按源与客户端参数决定直传还是转码。defaultBitrate 为配置默认码率(kbps)。
func Plan(src Source, p Params, defaultBitrate int) Decision {
	sf := strings.ToLower(src.Format)
	pf := strings.ToLower(p.Format)

	pass := Decision{Passthrough: true, ContentType: contentTypeForSource(sf)}

	// 1. raw → 直传
	if pf == "raw" {
		return pass
	}
	// 2. 未指定 format
	if pf == "" {
		if p.MaxBitRate == 0 {
			return pass
		}
		if !isLossless(sf) && src.Bitrate > 0 && src.Bitrate <= p.MaxBitRate {
			return pass
		}
		return transcodeDecision("mp3", p, src, defaultBitrate)
	}
	// 3/4. 指定 format（未知值回退 mp3）
	target := pf
	if _, ok := codecs[target]; !ok {
		target = "mp3"
	}
	if target == sf {
		if p.MaxBitRate == 0 || (!isLossless(sf) && src.Bitrate > 0 && src.Bitrate <= p.MaxBitRate) {
			return pass
		}
	}
	return transcodeDecision(target, p, src, defaultBitrate)
}

func transcodeDecision(codecName string, p Params, src Source, defaultBitrate int) Decision {
	c := codecFor(codecName)
	br := defaultBitrate
	if p.MaxBitRate > 0 {
		br = p.MaxBitRate
	}
	if !isLossless(src.Format) && src.Bitrate > 0 && src.Bitrate < br {
		br = src.Bitrate // 绝不升码率
	}
	return Decision{Codec: c.Name, Bitrate: br, ContentType: c.ContentType, Ext: c.Ext}
}
