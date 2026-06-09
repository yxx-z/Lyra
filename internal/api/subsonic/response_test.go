package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteResponse_XML(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/rest/ping", nil)
	writeResponse(w, r, &Response{License: &License{Valid: true}})

	body := w.Body.String()
	if !strings.Contains(body, `<subsonic-response`) || !strings.Contains(body, `status="ok"`) ||
		!strings.Contains(body, `version="1.16.1"`) || !strings.Contains(body, `<license valid="true"`) {
		t.Errorf("XML 输出不符: %s", body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "xml") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestWriteResponse_JSON(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/rest/ping?f=json", nil)
	writeResponse(w, r, &Response{})

	var got map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("非合法 JSON: %v (%s)", err, w.Body.String())
	}
	sr := got["subsonic-response"]
	if sr["status"] != "ok" || sr["version"] != "1.16.1" {
		t.Errorf("JSON 封套不符: %v", sr)
	}
}

// TestWriteResponse_OpenSubsonicFields 验证响应携带 OpenSubsonic 识别字段
// （Symfonium 等客户端据此识别服务器，否则报“未识别的服务器”）。
func TestWriteResponse_OpenSubsonicFields(t *testing.T) {
	// JSON
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/rest/ping?f=json", nil)
	writeResponse(w, r, &Response{})
	var got map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("非合法 JSON: %v", err)
	}
	sr := got["subsonic-response"]
	if sr["openSubsonic"] != true || sr["type"] != "lyra" || sr["serverVersion"] != "0.1.0" {
		t.Errorf("缺少 OpenSubsonic 字段: %v", sr)
	}

	// XML
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/rest/ping", nil)
	writeResponse(w2, r2, &Response{})
	if b := w2.Body.String(); !strings.Contains(b, `openSubsonic="true"`) ||
		!strings.Contains(b, `type="lyra"`) || !strings.Contains(b, `serverVersion="0.1.0"`) {
		t.Errorf("XML 缺少 OpenSubsonic 字段: %s", b)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/rest/ping?f=json", nil)
	writeError(w, r, 40, "用户名或密码错误")

	var got map[string]map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	sr := got["subsonic-response"]
	if sr["status"] != "failed" {
		t.Errorf("应 failed: %v", sr)
	}
	e, _ := sr["error"].(map[string]any)
	if e == nil || e["code"].(float64) != 40 {
		t.Errorf("error 字段不符: %v", sr)
	}
}

func TestWriteResponse_JSONFromPOSTBody(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/rest/ping", strings.NewReader("f=json"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	writeResponse(w, r, &Response{})
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("POST body f=json 应得 JSON，Content-Type=%q body=%s", ct, w.Body.String())
	}
}

func TestResponse_AlbumListXMLArray(t *testing.T) {
	resp := &Response{AlbumList2: &AlbumList2{Album: []AlbumID3{{ID: "a1", Name: "X"}, {ID: "a2", Name: "Y"}}}}
	out, _ := xml.Marshal(resp)
	if strings.Count(string(out), "<album ") != 2 {
		t.Errorf("应有 2 个 <album> 元素: %s", out)
	}
}
