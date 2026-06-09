package acoustid

import "testing"

func TestPickResult_Hit(t *testing.T) {
	rs := []acoustResult{
		{ID: "aid-1", Score: 0.97, Recordings: []recordingRef{{ID: "mbid-1"}, {ID: "mbid-2"}}},
	}
	got, ok := pickResult(rs)
	if !ok {
		t.Fatal("0.97 应命中")
	}
	if got.AcoustID != "aid-1" || got.MBID != "mbid-1" {
		t.Errorf("got %+v", got)
	}
}

func TestPickResult_BelowThreshold(t *testing.T) {
	rs := []acoustResult{{ID: "x", Score: 0.85, Recordings: []recordingRef{{ID: "m"}}}}
	if _, ok := pickResult(rs); ok {
		t.Error("score<0.9 不应命中")
	}
}

func TestPickResult_Empty(t *testing.T) {
	if _, ok := pickResult(nil); ok {
		t.Error("无结果不应命中")
	}
}

func TestPickResult_HitNoRecordings(t *testing.T) {
	rs := []acoustResult{{ID: "aid-2", Score: 0.95}}
	got, ok := pickResult(rs)
	if !ok || got.AcoustID != "aid-2" || got.MBID != "" {
		t.Errorf("命中但无 recordings 应只 AcoustID、MBID 空，得到 %+v ok=%v", got, ok)
	}
}
