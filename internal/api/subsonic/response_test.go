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

func TestBookmarkDTO_XMLJSON(t *testing.T) {
	resp := &Response{Bookmarks: &Bookmarks{Bookmark: []Bookmark{
		{Position: 42000, Username: "admin", Comment: "hi", Created: "2026-06-10 00:00:00", Changed: "2026-06-10 00:01:00", Entry: Child{ID: "t1", Title: "X"}},
	}}}
	out, _ := xml.Marshal(resp)
	if !strings.Contains(string(out), `<bookmark position="42000"`) || !strings.Contains(string(out), `<entry id="t1"`) {
		t.Errorf("书签 XML 不符: %s", out)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/x?f=json", nil)
	writeResponse(w, r, resp)
	b := w.Body.String()
	if !strings.Contains(b, `"position":42000`) || !strings.Contains(b, `"username":"admin"`) || !strings.Contains(b, `"comment":"hi"`) {
		t.Errorf("书签 JSON 不符: %s", b)
	}
}

func TestPlayQueueDTO_Order(t *testing.T) {
	resp := &Response{PlayQueue: &PlayQueue{
		Current: "t2", Position: 5000, Username: "admin", Changed: "2026-06-10 00:00:00", ChangedBy: "Symfonium",
		Entry: []Child{{ID: "t1", Title: "A"}, {ID: "t2", Title: "B"}},
	}}
	out, _ := xml.Marshal(resp)
	s := string(out)
	if !strings.Contains(s, `<playQueue current="t2"`) || strings.Index(s, `id="t1"`) > strings.Index(s, `id="t2"`) {
		t.Errorf("播放队列 XML 顺序/属性不符: %s", s)
	}
}
