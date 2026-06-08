package lyrics

import "strings"

// neteaseSong 是搜索候选中我们关心的字段。
type neteaseSong struct {
	ID         int64
	Name       string
	DurationMS int
}

// normalizeText 归一化文本：去首尾空格、转小写、全角转半角、折叠空白。
func normalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '　': // 全角空格
			b.WriteRune(' ')
		case r >= '！' && r <= '～': // 全角 ASCII 区
			b.WriteRune(r - 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// titleMatches 判断两个标题归一化后是否互相包含。
func titleMatches(a, b string) bool {
	na, nb := normalizeText(a), normalizeText(b)
	if na == "" || nb == "" {
		return false
	}
	return strings.Contains(na, nb) || strings.Contains(nb, na)
}

// pickMatch 从候选中选时长差 ≤3 秒且标题互含的第一首。
func pickMatch(songs []neteaseSong, wantTitle string, wantDurationSec int) (neteaseSong, bool) {
	for _, s := range songs {
		diff := wantDurationSec - s.DurationMS/1000
		if diff < 0 {
			diff = -diff
		}
		if diff <= 3 && titleMatches(s.Name, wantTitle) {
			return s, true
		}
	}
	return neteaseSong{}, false
}
