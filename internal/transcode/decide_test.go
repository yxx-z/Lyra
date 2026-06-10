package transcode

import "testing"

func TestPlan(t *testing.T) {
	const def = 192
	tests := []struct {
		name      string
		src       Source
		p         Params
		wantPass  bool
		wantCodec string
		wantBr    int
	}{
		{"raw 强制直传", Source{Format: "flac", Bitrate: 1000}, Params{Format: "raw"}, true, "", 0},
		{"无参数直传", Source{Format: "flac"}, Params{}, true, "", 0},
		{"有损源在预算内直传", Source{Format: "mp3", Bitrate: 128}, Params{MaxBitRate: 192}, true, "", 0},
		{"有损源超预算转mp3并封顶", Source{Format: "mp3", Bitrate: 320}, Params{MaxBitRate: 128}, false, "mp3", 128},
		{"无损源限码率转mp3", Source{Format: "flac"}, Params{MaxBitRate: 256}, false, "mp3", 256},
		{"指定opus转码", Source{Format: "flac"}, Params{Format: "opus"}, false, "opus", def},
		{"指定opus带码率", Source{Format: "mp3", Bitrate: 320}, Params{Format: "opus", MaxBitRate: 96}, false, "opus", 96},
		{"同编码同预算直传", Source{Format: "mp3", Bitrate: 128}, Params{Format: "mp3", MaxBitRate: 192}, true, "", 0},
		{"绝不升码率", Source{Format: "mp3", Bitrate: 128}, Params{Format: "mp3", MaxBitRate: 320}, true, "", 0},
		{"未知format回退mp3转码", Source{Format: "flac"}, Params{Format: "weird"}, false, "mp3", def},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Plan(tc.src, tc.p, def)
			if d.Passthrough != tc.wantPass {
				t.Fatalf("Passthrough=%v want %v (%+v)", d.Passthrough, tc.wantPass, d)
			}
			if !tc.wantPass && (d.Codec != tc.wantCodec || d.Bitrate != tc.wantBr) {
				t.Errorf("Codec/Bitrate=%s/%d want %s/%d", d.Codec, d.Bitrate, tc.wantCodec, tc.wantBr)
			}
		})
	}
}

func TestPlan_ContentType(t *testing.T) {
	if d := Plan(Source{Format: "flac"}, Params{}, 192); d.ContentType != "audio/flac" {
		t.Errorf("直传 flac 应 audio/flac，得到 %q", d.ContentType)
	}
	if d := Plan(Source{Format: "flac"}, Params{Format: "opus"}, 192); d.ContentType != "audio/ogg" || d.Ext != "opus" {
		t.Errorf("转 opus 应 audio/ogg/.opus，得到 %q/%q", d.ContentType, d.Ext)
	}
}
