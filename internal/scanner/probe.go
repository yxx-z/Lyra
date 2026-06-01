// internal/scanner/probe.go
package scanner

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// probeTimeout bounds how long a single ffprobe invocation may run, so a
// hung/pathological file cannot wedge a scan worker forever. Overridable in tests.
var probeTimeout = 30 * time.Second

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
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		ffprobePath,
		"-v", "error",
		"-print_format", "json",
		"-show_entries", "format=duration,bit_rate:stream=sample_rate,channels",
		filePath,
	)
	// 超时/取消后，即便有残留子进程仍持有 I/O 管道，也在宽限期后强制关闭管道返回，
	// 防止 cmd.Output() 永久阻塞。
	cmd.WaitDelay = 5 * time.Second
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
