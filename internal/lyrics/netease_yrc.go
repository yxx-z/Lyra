package lyrics

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// yrcWord 是一个逐字单元（时间单位=秒）。
type yrcWord struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// yrcLine 是一行逐字歌词。
type yrcLine struct {
	Start float64   `json:"start"`
	End   float64   `json:"end"`
	Words []yrcWord `json:"words"`
}

// yrcDoc 是归一化后的 YRC 文档，序列化后存入 lyrics.yrc_content。
type yrcDoc struct {
	Lines []yrcLine `json:"lines"`
}

var (
	yrcLineHead = regexp.MustCompile(`^\[(\d+),(\d+)\]`)
	yrcWordRe   = regexp.MustCompile(`\((\d+),(\d+),\d+\)([^(]*)`)
)

// parseYRC 将网易云原始 YRC 解析为归一化 JSON 字符串；无任何有效歌词行时返回空串。
// 无 [起,长] 行头的行（如 {"t":..} 元信息）一律跳过；单行解析失败不影响其它行。
func parseYRC(raw string) (string, error) {
	doc := yrcDoc{Lines: []yrcLine{}}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		head := yrcLineHead.FindStringSubmatch(line)
		if head == nil {
			continue
		}
		lineStart, _ := strconv.Atoi(head[1])
		lineDur, _ := strconv.Atoi(head[2])

		words := make([]yrcWord, 0, 8)
		for _, m := range yrcWordRe.FindAllStringSubmatch(line, -1) {
			ws, _ := strconv.Atoi(m[1])
			wd, _ := strconv.Atoi(m[2])
			text := m[3]
			if strings.TrimSpace(text) == "" {
				continue
			}
			words = append(words, yrcWord{
				Start: float64(ws) / 1000,
				End:   float64(ws+wd) / 1000,
				Text:  text,
			})
		}
		if len(words) == 0 {
			continue
		}
		doc.Lines = append(doc.Lines, yrcLine{
			Start: float64(lineStart) / 1000,
			End:   float64(lineStart+lineDur) / 1000,
			Words: words,
		})
	}
	if len(doc.Lines) == 0 {
		return "", nil
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
