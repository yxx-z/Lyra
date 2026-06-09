package acoustid

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// Fingerprinter 计算音频指纹（duration 秒 + chromaprint 指纹）。
type Fingerprinter interface {
	Calc(ctx context.Context, filePath string) (durationSec int, fingerprint string, err error)
}

// ExecFingerprinter 调用 fpcalc 二进制实现 Fingerprinter。
type ExecFingerprinter struct {
	fpcalcPath string
}

// NewExecFingerprinter 创建运行器；path 为空用 "fpcalc"。
func NewExecFingerprinter(fpcalcPath string) *ExecFingerprinter {
	if strings.TrimSpace(fpcalcPath) == "" {
		fpcalcPath = "fpcalc"
	}
	return &ExecFingerprinter{fpcalcPath: fpcalcPath}
}

// Calc 跑 `fpcalc -json <file>` 并解析。
func (f *ExecFingerprinter) Calc(ctx context.Context, filePath string) (int, string, error) {
	cmd := exec.CommandContext(ctx, f.fpcalcPath, "-json", filePath)
	cmd.WaitDelay = 5 * time.Second // 防止子进程持有管道导致 Output 永久阻塞
	out, err := cmd.Output()
	if err != nil {
		return 0, "", err
	}
	return parseFpcalcJSON(out)
}

// parseFpcalcJSON 解析 fpcalc -json 输出。
func parseFpcalcJSON(data []byte) (int, string, error) {
	var payload struct {
		Duration    float64 `json:"duration"`
		Fingerprint string  `json:"fingerprint"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, "", err
	}
	return int(payload.Duration), payload.Fingerprint, nil
}
