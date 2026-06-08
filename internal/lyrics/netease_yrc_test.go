package lyrics

import (
	"encoding/json"
	"testing"
)

func TestParseYRC_Basic(t *testing.T) {
	raw := "[12100,3000](12100,300,0)作(12400,300,0)词(12700,400,0)人\n[15100,2000](15100,500,0)歌(15600,500,0)手"

	out, err := parseYRC(raw)
	if err != nil {
		t.Fatalf("parseYRC err: %v", err)
	}

	var doc yrcDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("输出非合法 JSON: %v (%s)", err, out)
	}
	if len(doc.Lines) != 2 {
		t.Fatalf("应解析出 2 行，得到 %d", len(doc.Lines))
	}
	l0 := doc.Lines[0]
	if l0.Start != 12.1 || l0.End != 15.1 {
		t.Errorf("行0 时间 = %v~%v, want 12.1~15.1", l0.Start, l0.End)
	}
	if len(l0.Words) != 3 {
		t.Fatalf("行0 应 3 字，得到 %d", len(l0.Words))
	}
	if l0.Words[0].Text != "作" || l0.Words[0].Start != 12.1 || l0.Words[0].End != 12.4 {
		t.Errorf("行0字0 = %+v, want {作 12.1 12.4}", l0.Words[0])
	}
}

func TestParseYRC_SkipsMetadataLines(t *testing.T) {
	raw := "{\"t\":0,\"c\":[{\"tx\":\"作词: 某人\"}]}\n[1000,1000](1000,1000,0)字"

	out, err := parseYRC(raw)
	if err != nil {
		t.Fatalf("parseYRC err: %v", err)
	}
	var doc yrcDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("输出非合法 JSON: %v", err)
	}
	if len(doc.Lines) != 1 {
		t.Errorf("元信息行应被丢弃，仅留 1 行，得到 %d", len(doc.Lines))
	}
}

func TestParseYRC_EmptyReturnsEmptyString(t *testing.T) {
	out, err := parseYRC("{\"t\":0,\"c\":[]}\n\n")
	if err != nil {
		t.Fatalf("parseYRC err: %v", err)
	}
	if out != "" {
		t.Errorf("无有效歌词行应返回空串，得到 %q", out)
	}
}
