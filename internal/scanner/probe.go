// internal/scanner/probe.go
package scanner

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// AudioProps holds audio properties extracted via ffprobe.
type AudioProps struct {
	Duration   int // seconds
	Bitrate    int // kbps
	SampleRate int // Hz
	Channels   int
}

// Probe runs ffprobe on filePath and returns its audio properties.
// Returns an error if ffprobe is unavailable or fails; callers should
// degrade to zero values without aborting the scan.
func Probe(ffprobePath, filePath string) (AudioProps, error) {
	cmd := exec.Command(
		ffprobePath,
		"-v", "error",
		"-print_format", "json",
		"-show_entries", "format=duration,bit_rate:stream=sample_rate,channels",
		filePath,
	)
	out, err := cmd.Output()
	if err != nil {
		return AudioProps{}, err
	}
	return parseProbeOutput(out)
}

// parseProbeOutput parses ffprobe JSON into AudioProps. Missing or malformed
// numeric fields degrade to 0 individually (only a JSON syntax error fails).
func parseProbeOutput(data []byte) (AudioProps, error) {
	var raw struct {
		Streams []struct {
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
		} `json:"format"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return AudioProps{}, err
	}

	var props AudioProps
	if d, err := strconv.ParseFloat(strings.TrimSpace(raw.Format.Duration), 64); err == nil {
		props.Duration = int(d)
	}
	if br, err := strconv.Atoi(strings.TrimSpace(raw.Format.BitRate)); err == nil {
		props.Bitrate = br / 1000
	}
	if len(raw.Streams) > 0 {
		if sr, err := strconv.Atoi(strings.TrimSpace(raw.Streams[0].SampleRate)); err == nil {
			props.SampleRate = sr
		}
		props.Channels = raw.Streams[0].Channels
	}
	return props, nil
}
