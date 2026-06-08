package lyrics

import "testing"

func TestPickMatch(t *testing.T) {
	songs := []neteaseSong{
		{ID: 1, Name: "晴天 (Live)", DurationMS: 200000},
		{ID: 2, Name: "晴天", DurationMS: 269000},
		{ID: 3, Name: "其它歌", DurationMS: 269000},
	}

	got, ok := pickMatch(songs, "晴天", 269)
	if !ok {
		t.Fatal("应匹配到 id=2")
	}
	if got.ID != 2 {
		t.Errorf("匹配 id = %d, want 2", got.ID)
	}
}

func TestPickMatch_DurationTooFar(t *testing.T) {
	songs := []neteaseSong{{ID: 1, Name: "晴天", DurationMS: 200000}}
	if _, ok := pickMatch(songs, "晴天", 269); ok {
		t.Error("时长差 >3s 不应匹配")
	}
}

func TestPickMatch_TitleNotContained(t *testing.T) {
	songs := []neteaseSong{{ID: 1, Name: "完全不同", DurationMS: 269000}}
	if _, ok := pickMatch(songs, "晴天", 269); ok {
		t.Error("标题不互相包含不应匹配")
	}
}

func TestNormalizeText(t *testing.T) {
	if got := normalizeText("　Ｈｅｌｌｏ   World "); got != "hello world" {
		t.Errorf("normalizeText = %q, want %q", got, "hello world")
	}
}
