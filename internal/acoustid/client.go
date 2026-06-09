package acoustid

import "errors"

// ErrNoMatch 表示指纹未匹配到（无结果或低于置信阈值）。
var ErrNoMatch = errors.New("指纹未匹配")

const scoreThreshold = 0.9

type recordingRef struct {
	ID string `json:"id"`
}

type acoustResult struct {
	ID         string         `json:"id"`
	Score      float64        `json:"score"`
	Recordings []recordingRef `json:"recordings"`
}

// IdentifyResult 是识别命中后的权威标识。
type IdentifyResult struct {
	AcoustID string
	MBID     string // recording MBID，可能为空
	Score    float64
}

// pickResult 取 results[0]（AcoustID 已按 score 降序）；score≥0.9 才命中。
func pickResult(results []acoustResult) (IdentifyResult, bool) {
	if len(results) == 0 {
		return IdentifyResult{}, false
	}
	r := results[0]
	if r.Score < scoreThreshold {
		return IdentifyResult{}, false
	}
	res := IdentifyResult{AcoustID: r.ID, Score: r.Score}
	if len(r.Recordings) > 0 {
		res.MBID = r.Recordings[0].ID
	}
	return res, true
}
