package acoustid

import (
	"context"
	"os/exec"
	"testing"
)

func TestParseFpcalcJSON(t *testing.T) {
	dur, fp, err := parseFpcalcJSON([]byte(`{"duration": 269.00, "fingerprint": "AQADtABC"}`))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if dur != 269 {
		t.Errorf("duration = %d, want 269", dur)
	}
	if fp != "AQADtABC" {
		t.Errorf("fingerprint = %q", fp)
	}
}

func TestParseFpcalcJSON_Bad(t *testing.T) {
	if _, _, err := parseFpcalcJSON([]byte(`not json`)); err == nil {
		t.Error("坏 JSON 应返回 error")
	}
}

func TestExecFingerprinter_SkipsWithoutBinary(t *testing.T) {
	if _, err := exec.LookPath("fpcalc"); err != nil {
		t.Skip("无 fpcalc，跳过真实指纹测试")
	}
	f := NewExecFingerprinter("fpcalc")
	if _, _, err := f.Calc(context.Background(), "/nonexistent/file.flac"); err == nil {
		t.Error("对不存在文件应返回 error")
	}
}
