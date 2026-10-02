package preparation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderProtocolsDiscoveryAndImageConnection(t *testing.T) {
	for _, format := range []string{"openai", "anthropic", "gemini"} {
		t.Run(format, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				header := "Authorization"
				expected := "Bearer test-secret"
				if format == "anthropic" {
					header = "x-api-key"
					expected = "test-secret"
					if r.Header.Get("anthropic-version") != "2023-06-01" {
						t.Error("missing version")
					}
				}
				if format == "gemini" {
					header = "x-goog-api-key"
					expected = "test-secret"
				}
				if r.Header.Get(header) != expected || strings.Contains(r.URL.RawQuery, "test-secret") {
					t.Error("bad auth")
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					if r.URL.Path != "/v1/models" && r.URL.Path != "/v1beta/models" {
						t.Errorf("bad model path %s", r.URL.Path)
					}
					switch format {
					case "openai":
						w.Write([]byte(`{"data":[{"id":"model-one"},{"id":"model-one"},{"id":"model-two"}]}`))
					case "anthropic":
						if r.URL.Query().Get("after_id") == "model-one" {
							w.Write([]byte(`{"data":[{"id":"model-two","display_name":"Two"}],"has_more":false}`))
						} else {
							w.Write([]byte(`{"data":[{"id":"model-one","display_name":"One"}],"has_more":true,"last_id":"model-one"}`))
						}
					case "gemini":
						if r.URL.Query().Get("pageToken") == "page-two" {
							w.Write([]byte(`{"models":[{"name":"models/model-two","supportedGenerationMethods":["generateContent"]}]}`))
						} else {
							w.Write([]byte(`{"models":[{"name":"models/model-one","displayName":"One","supportedGenerationMethods":["generateContent"]},{"name":"models/embed","supportedGenerationMethods":["embedContent"]}],"nextPageToken":"page-two"}`))
						}
					}
					return
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid body")
				}
				encoded := ""
				if format == "gemini" {
					if r.URL.Path != "/v1beta/models/model-one:generateContent" {
						t.Errorf("bad generation path %s", r.URL.Path)
					}
					part := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)[1].(map[string]any)
					encoded = part["inlineData"].(map[string]any)["data"].(string)
					w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"green"}]},"finishReason":"STOP"}]}`))
				} else {
					if body["model"] != "model-one" {
						t.Error("model not passed")
					}
					item := body["messages"].([]any)[0].(map[string]any)["content"].([]any)[1].(map[string]any)
					if format == "openai" {
						if r.URL.Path != "/v1/chat/completions" {
							t.Error("bad chat path")
						}
						encoded = strings.TrimPrefix(item["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,")
						w.Write([]byte(`{"choices":[{"message":{"content":"green"},"finish_reason":"stop"}]}`))
					} else {
						if r.URL.Path != "/v1/messages" {
							t.Error("bad messages path")
						}
						encoded = item["source"].(map[string]any)["data"].(string)
						w.Write([]byte(`{"content":[{"type":"text","text":"green"}],"stop_reason":"end_turn"}`))
					}
				}
				raw, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					t.Error(err)
					return
				}
				img, err := png.Decode(strings.NewReader(string(raw)))
				if err != nil || img.Bounds().Dx() != 32 {
					t.Error("synthetic image invalid")
				}
			}))
			defer srv.Close()
			p := Provider{Adapter: "vision", APIFormat: format, Endpoint: srv.URL, TokenEnv: "TEST_PROVIDER", Model: "model-one"}
			if err := p.Validate(true); err != nil {
				t.Fatal(err)
			}
			client := DiagnosticClient{}
			found := client.Run(t.Context(), p, "test-secret", "models")
			if !found.OK || len(found.Models) != 2 || found.Truncated {
				t.Fatalf("models %+v", found)
			}
			result := client.Run(t.Context(), p, "test-secret", "test")
			if !result.OK || calls < 2 {
				t.Fatalf("connection %+v", result)
			}
		})
	}
}

func TestDiagnosticsFailuresAreBoundedAndDoNotEchoCredentials(t *testing.T) {
	for _, row := range []struct {
		status     int
		body, code string
	}{
		{401, `{"error":"secret-marker"}`, "authentication_failed"},
		{403, "secret-marker", "authentication_failed"},
		{404, "secret-marker", "endpoint_or_model_not_found"},
		{429, "secret-marker", "rate_limited"},
		{500, "secret-marker", "provider_error"},
		{200, "not-json secret-marker", "invalid_response"},
		{200, `{"choices":[{"message":{"content":""}}]}`, "invalid_response"},
		{200, `{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}]}`, "invalid_response"},
		{200, strings.Repeat("x", (1<<20)+1), "response_too_large"},
	} {
		t.Run(row.code+string(rune(row.status)), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(row.status); w.Write([]byte(row.body)) }))
			defer srv.Close()
			p := Provider{Adapter: "narration", APIFormat: "openai", Endpoint: srv.URL, Model: "m", TokenEnv: "T"}
			r := (DiagnosticClient{}).Run(t.Context(), p, "secret-marker", "test")
			raw, _ := json.Marshal(r)
			if r.OK || r.Code != row.code || strings.Contains(string(raw), "secret-marker") {
				t.Fatalf("bad result %s", raw)
			}
		})
	}
	hits := 0
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer sink.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, sink.URL, 307) }))
	defer redirect.Close()
	p := Provider{Adapter: "narration", APIFormat: "openai", Endpoint: redirect.URL, Model: "m", TokenEnv: "T"}
	if r := (DiagnosticClient{}).Run(t.Context(), p, "secret-marker", "test"); r.Code != "redirect_refused" || hits != 0 {
		t.Fatalf("redirect followed %+v", r)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if r := (DiagnosticClient{}).Run(ctx, p, "secret-marker", "test"); r.Code != "timeout" {
		t.Fatalf("cancel ignored %+v", r)
	}
}

func TestProviderFormatValidationAndHistoricalFingerprint(t *testing.T) {
	p := fixture()
	old, _ := p.Fingerprint()
	encoded, _ := json.Marshal(p)
	if strings.Contains(string(encoded), "api_format") {
		t.Fatal("old profile changed")
	}
	p.Vision.APIFormat = "openai"
	if p.Validate() == nil {
		t.Fatal("builtin format accepted")
	}
	p = fixture()
	p.ContentMode = "configured"
	p.Vision = Provider{Adapter: "vision", Endpoint: "https://example.com/v1", Model: "m", TokenEnv: "T"}
	p.Narration = Provider{Adapter: "narration", Endpoint: "https://example.com/v1", Model: "m", TokenEnv: "T"}
	custom, _ := p.Fingerprint()
	p.Vision.APIFormat = "openai"
	standard, err := p.Fingerprint()
	if err != nil || standard == custom || standard == old {
		t.Fatal("format not fingerprinted")
	}
	p.Vision.APIFormat = "unknown"
	if p.Validate() == nil {
		t.Fatal("unsupported format accepted")
	}
	p.Vision.APIFormat = "gemini"
	p.Vision.Model = "../../other?key=secret"
	if p.Validate() == nil {
		t.Fatal("unsafe Gemini ID accepted")
	}
}

func TestOpenAILegacyTokenLimitFallback(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if calls == 1 {
			if body["max_completion_tokens"] == nil {
				t.Error("missing token cap")
			}
			w.WriteHeader(400)
			w.Write([]byte(`{"error":{"param":"max_completion_tokens"}}`))
			return
		}
		if body["max_tokens"] != float64(512) || body["max_completion_tokens"] != nil {
			t.Error("invalid legacy fallback")
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer srv.Close()
	p := Provider{Adapter: "narration", APIFormat: "openai", Endpoint: srv.URL, Model: "m", TokenEnv: "T"}
	if result := (DiagnosticClient{}).Run(t.Context(), p, "test-secret", "test"); !result.OK || calls != 2 {
		t.Fatalf("fallback %+v %d", result, calls)
	}
}
