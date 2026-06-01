// internal/scanner/probe_test.go
package scanner

import "testing"

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
