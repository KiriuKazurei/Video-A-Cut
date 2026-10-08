package api

import (
	"bytes"
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTTSTemplateAPIAndConflicts(t *testing.T) {
	server := newServer(t)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		rec := httptest.NewRecorder()
		req := localRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		server.Handler().ServeHTTP(rec, req)
		return rec
	}
	spec := tts.Settings{Engine: "sapi", Voice: "test-installed-voice", Language: "zh", Speed: 1, Volume: 1}
	req := tts.SaveRequest{TemplateID: "api_voice", Name: "API 声音", Settings: spec, IdempotencyKey: "create"}
	if rec := request("POST", "/api/tts/templates", req); rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	req.ExpectedRevision = 1
	req.IdempotencyKey = "second"
	req.Settings.Volume = .5
	if rec := request("POST", "/api/tts/templates/api_voice/revisions", req); rec.Code != 201 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	req.IdempotencyKey = "stale"
	if rec := request("POST", "/api/tts/templates/api_voice/revisions", req); rec.Code != 409 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	rec := request("GET", "/api/tts/templates/api_voice/revisions/1", nil)
	var first tts.Template
	if e := json.Unmarshal(rec.Body.Bytes(), &first); e != nil || first.Settings.Volume != 1 {
		t.Fatal(rec.Body.String(), e)
	}
	if rec := request("POST", "/api/tts/templates/api_voice/archive", map[string]int{"expected_revision": 2}); rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec := request("GET", "/api/tts/templates", nil); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec := request("POST", "/api/tts/preview-jobs", tts.PreviewRequest{Text: " ", Settings: spec, IdempotencyKey: "invalid"}); rec.Code != 400 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	if rec := request("GET", "/api/tts/capabilities?token_env=bad-name", nil); rec.Code != 400 {
		t.Fatal(rec.Code, rec.Body.String())
	}
}
