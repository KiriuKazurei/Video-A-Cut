package preparation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ProviderModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProviderDiagnostic struct {
	OK        bool            `json:"ok"`
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	LatencyMS int64           `json:"latency_ms"`
	Models    []ProviderModel `json:"models,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
}

// DiagnosticClient never follows redirects or persists credentials/provider text.
type DiagnosticClient struct{ Client *http.Client }

func (d DiagnosticClient) request(ctx context.Context, p Provider, token, method, endpoint string, body any) (map[string]any, string) {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, "invalid_endpoint"
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch p.APIFormat {
	case "openai":
		req.Header.Set("Authorization", "Bearer "+token)
	case "anthropic":
		req.Header.Set("x-api-key", token)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "gemini":
		req.Header.Set("x-goog-api-key", token)
	}
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if d.Client != nil {
		copy := *d.Client
		copy.CheckRedirect = client.CheckRedirect
		copy.Timeout = client.Timeout
		client = &copy
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "timeout"
		}
		if e, ok := err.(*url.Error); ok && e.Timeout() {
			return nil, "timeout"
		}
		return nil, "connection_error"
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, "redirect_refused"
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, "authentication_failed"
	}
	if resp.StatusCode == 404 {
		return nil, "endpoint_or_model_not_found"
	}
	if resp.StatusCode == 429 {
		return nil, "rate_limited"
	}
	if resp.StatusCode == 400 && p.APIFormat == "openai" && body != nil {
		if params, ok := body.(map[string]any); ok {
			if limit, exists := params["max_completion_tokens"]; exists {
				raw, _ := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
				var rejected struct {
					Error struct {
						Param   string `json:"param"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if len(raw) <= 1<<20 && json.Unmarshal(raw, &rejected) == nil && (rejected.Error.Param == "max_completion_tokens" || strings.Contains(rejected.Error.Message, "max_completion_tokens")) {
					_ = resp.Body.Close()
					legacy := make(map[string]any, len(params))
					for key, value := range params {
						if key != "max_completion_tokens" {
							legacy[key] = value
						}
					}
					legacy["max_tokens"] = limit
					return d.request(ctx, p, token, method, endpoint, legacy)
				}
			}
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "provider_error"
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, "connection_error"
	}
	if len(raw) > 1<<20 {
		return nil, "response_too_large"
	}
	var data map[string]any
	if json.Unmarshal(raw, &data) != nil || data == nil {
		return nil, "invalid_response"
	}
	return data, ""
}

func diagnosticMessage(code string) string {
	switch code {
	case "authentication_failed":
		return "鉴权失败，请核对 API key 和访问权限"
	case "endpoint_or_model_not_found":
		return "接口或模型不存在，请核对基础地址和模型 ID"
	case "rate_limited":
		return "服务商限流或额度不足，请稍后重试并检查额度"
	case "timeout":
		return "请求超时，请检查服务地址及网络"
	case "redirect_refused":
		return "服务地址发生重定向，请配置最终 API 基础地址"
	case "connection_error":
		return "无法连接服务，请检查网络、TLS 和基础地址"
	case "response_too_large":
		return "服务响应超过大小限制"
	case "invalid_response":
		return "响应不符合所选协议，或输出为空、受阻、被截断"
	case "models_unsupported":
		return "服务未返回该格式的模型列表，可手动填写模型 ID 后测试连接"
	default:
		return "服务商请求失败，请检查模型、参数兼容性和服务状态"
	}
}

// Run is an explicit synthetic diagnostic; it never reads a recording or alters a profile.
func (d DiagnosticClient) Run(ctx context.Context, p Provider, token, operation string) ProviderDiagnostic {
	started := time.Now()
	r := ProviderDiagnostic{Code: "passed"}
	finish := func(code string) ProviderDiagnostic {
		r.LatencyMS = time.Since(started).Milliseconds()
		if code != "" {
			r.Code = code
			r.Message = diagnosticMessage(code)
			return r
		}
		r.OK = true
		return r
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if operation == "test" {
		probe := "This is a connection test. Reply with the word OK."
		encoded := ""
		if p.Adapter == "vision" {
			img := image.NewRGBA(image.Rect(0, 0, 32, 32))
			for y := 0; y < 32; y++ {
				for x := 0; x < 32; x++ {
					img.Set(x, y, color.RGBA{R: 32, G: 160, B: 96, A: 255})
				}
			}
			var buf bytes.Buffer
			_ = png.Encode(&buf, img)
			encoded = base64.StdEncoding.EncodeToString(buf.Bytes())
			probe = "This is a connection test with a synthetic image. Briefly describe its color."
		}
		endpoint, body := providerGeneration(p, probe, encoded)
		data, code := d.request(ctx, p, token, "POST", endpoint, body)
		if code != "" {
			return finish(code)
		}
		if _, err := providerText(p.APIFormat, data); err != nil {
			return finish("invalid_response")
		}
		r.Message = "连接测试通过：模型返回了可读取的生成结果"
		if p.Adapter == "vision" {
			r.Message = "连接测试通过：图片请求被接受并返回生成结果；内容质量仍需实际验收"
		}
		return finish("")
	}
	r.Models = []ProviderModel{}
	seenModels, seenPages := map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		endpoint := providerBase(p) + "/models"
		query := url.Values{}
		if p.APIFormat == "anthropic" {
			query.Set("limit", "100")
			if cursor != "" {
				query.Set("after_id", cursor)
			}
		}
		if p.APIFormat == "gemini" {
			query.Set("pageSize", "100")
			if cursor != "" {
				query.Set("pageToken", cursor)
			}
		}
		if len(query) > 0 {
			endpoint += "?" + query.Encode()
		}
		data, code := d.request(ctx, p, token, "GET", endpoint, nil)
		if code != "" {
			return finish(code)
		}
		key := "data"
		if p.APIFormat == "gemini" {
			key = "models"
		}
		list, ok := data[key].([]any)
		if !ok {
			return finish("models_unsupported")
		}
		for _, raw := range list {
			item, ok := raw.(map[string]any)
			if !ok {
				return finish("invalid_response")
			}
			id, _ := item["id"].(string)
			name, _ := item["display_name"].(string)
			if p.APIFormat == "gemini" {
				id, _ = item["name"].(string)
				id = strings.TrimPrefix(id, "models/")
				name, _ = item["displayName"].(string)
				if methods, ok := item["supportedGenerationMethods"].([]any); ok {
					supported := false
					for _, m := range methods {
						if m == "generateContent" {
							supported = true
						}
					}
					if !supported {
						continue
					}
				}
			}
			if !textOK(id, 100) || strings.Contains(id, token) || seenModels[id] {
				continue
			}
			if !textOK(name, 200) || strings.Contains(name, token) {
				name = id
			}
			seenModels[id] = true
			r.Models = append(r.Models, ProviderModel{ID: id, Name: name})
			if len(r.Models) >= 500 {
				r.Truncated = true
				break
			}
		}
		next := ""
		if p.APIFormat == "gemini" {
			next, _ = data["nextPageToken"].(string)
		}
		if p.APIFormat == "anthropic" && data["has_more"] == true {
			next, _ = data["last_id"].(string)
			if next == "" {
				return finish("invalid_response")
			}
		}
		if next == "" || r.Truncated {
			break
		}
		if !textOK(next, 2048) || seenPages[next] {
			return finish("invalid_response")
		}
		seenPages[next] = true
		cursor = next
		if page == 9 {
			r.Truncated = true
		}
	}
	r.Message = "已读取服务商模型列表；模型用途与图片支持请通过连接测试确认"
	if r.Truncated {
		r.Message = "模型列表达到分页或数量上限，当前显示部分结果，可手动填写其他模型 ID"
	}
	return finish("")
}
