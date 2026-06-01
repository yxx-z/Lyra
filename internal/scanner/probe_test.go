// internal/scanner/probe_test.go
package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseProbeOutput_FullData(t *testing.T) {
	jsonOut := `{
		"streams": [{"sample_rate": "44100", "channels": 2}],
		"format": {"duration": "245.760000", "bit_rate": "320000"}
	}`
	props, err := parseProbeOutput([]byte(jsonOut))
	if err != nil {
		t.Fatalf("parseProbeOutput: %v", err)
	}
	if props.Duration != 245 {
		t.Errorf("Duration = %d, want 245", props.Duration)
	}
	if props.Bitrate != 320 {
		t.Errorf("Bitrate = %d, want 320", props.Bitrate)
	}
	if props.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want 44100", props.SampleRate)
	}
	if props.Channels != 2 {
		t.Errorf("Channels = %d, want 2", props.Channels)
	}
}

func TestParseProbeOutput_MissingFields(t *testing.T) {
	jsonOut := `{"streams": [], "format": {}}`
	props, err := parseProbeOutput([]byte(jsonOut))
	if err != nil {
		t.Fatalf("parseProbeOutput: %v", err)
	}
	if props.Duration != 0 || props.Bitrate != 0 || props.SampleRate != 0 || props.Channels != 0 {
		t.Errorf("want all zero, got %+v", props)
	}
}

func TestParseProbeOutput_BadJSON(t *testing.T) {
	_, err := parseProbeOutput([]byte("not json"))
	if err == nil {
		t.Error("want error for bad JSON")
	}
}

func TestProbe_FfprobeNotFound(t *testing.T) {
	_, err := Probe("/nonexistent/ffprobe", "/tmp/whatever.mp3")
	if err == nil {
		t.Error("want error when ffprobe binary missing")
	}
}

func TestProbe_TimeoutReturnsError(t *testing.T) {
	dir := t.TempDir()
	// 假 ffprobe：exec 成单进程 sleep（真实模拟单进程的 ffprobe 卡住，
	// 无孙进程持有管道），被 SIGKILL 后管道立即关闭。
	fake := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 30\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// 把超时压到 200ms，断言 Probe 不会一直等
	old := probeTimeout
	probeTimeout = 200 * time.Millisecond
	defer func() { probeTimeout = old }()

	start := time.Now()
	_, err := Probe(fake, filepath.Join(dir, "whatever.mp3"))
	elapsed := time.Since(start)

	if err == nil {
		t.Error("want error when ffprobe exceeds timeout")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Probe waited %v, should have timed out near 200ms", elapsed)
	}
}
