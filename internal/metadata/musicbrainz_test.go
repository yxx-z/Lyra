package metadata

import "testing"

func TestPickRelease_ClosestTrackCount(t *testing.T) {
	rs := []mbRelease{
		{ID: "a", Score: 100, TrackCount: 22},
		{ID: "b", Score: 100, TrackCount: 11},
		{ID: "c", Score: 100, TrackCount: 14},
	}
	got, ok := pickRelease(rs, 11)
	if !ok || got.ID != "b" {
		t.Fatalf("应选 b，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickRelease_FiltersLowScore(t *testing.T) {
	rs := []mbRelease{
		{ID: "x", Score: 39, TrackCount: 11},
		{ID: "y", Score: 91, TrackCount: 99},
	}
	got, ok := pickRelease(rs, 11)
	if !ok || got.ID != "y" {
		t.Fatalf("应只在 score>=90 里选，得到 %q ok=%v", got.ID, ok)
	}
}

func TestPickRelease_AllBelowThreshold(t *testing.T) {
	rs := []mbRelease{{ID: "x", Score: 50, TrackCount: 11}}
	if _, ok := pickRelease(rs, 11); ok {
		t.Error("全部 score<90 应不命中")
	}
}

func TestPickRelease_UnknownLocalCountTakesFirst(t *testing.T) {
	rs := []mbRelease{
		{ID: "first", Score: 100, TrackCount: 20},
		{ID: "second", Score: 95, TrackCount: 11},
	}
	got, ok := pickRelease(rs, 0)
	if !ok || got.ID != "first" {
		t.Fatalf("localCount=0 应取靠前者，得到 %q", got.ID)
	}
}
