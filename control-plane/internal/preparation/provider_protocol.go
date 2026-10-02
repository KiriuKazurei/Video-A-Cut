package preparation

import (
	"fmt"
	"net/url"
	"strings"
)

func providerBase(p Provider) string {
	u, _ := url.Parse(p.Endpoint) // validated before use
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	for _, suffix := range []string{"/chat/completions", "/messages"} {
		u.Path = strings.TrimSuffix(u.Path, suffix)
	}
	if u.Path == "" {
		u.Path = "/v1"
		if p.APIFormat == "gemini" {
			u.Path = "/v1beta"
		}
	}
	return u.String()
}

func providerGeneration(p Provider, text, image string) (string, map[string]any) {
	base := providerBase(p)
	switch p.APIFormat {
	case "openai":
		content := []any{map[string]any{"type": "text", "text": text}}
		if image != "" {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + image}})
		}
		return base + "/chat/completions", map[string]any{"model": p.Model, "messages": []any{map[string]any{"role": "user", "content": content}}, "max_completion_tokens": 512, "stream": false}
	case "anthropic":
		content := []any{map[string]any{"type": "text", "text": text}}
		if image != "" {
			content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": image}})
		}
		return base + "/messages", map[string]any{"model": p.Model, "max_tokens": 512, "messages": []any{map[string]any{"role": "user", "content": content}}}
	default:
		parts := []any{map[string]any{"text": text}}
		if image != "" {
			parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": image}})
		}
		return base + "/models/" + strings.TrimPrefix(p.Model, "models/") + ":generateContent", map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}, "generationConfig": map[string]any{"maxOutputTokens": 512}}
	}
}

func providerText(format string, data map[string]any) (string, error) {
	var text strings.Builder
	blocks := func(value any, key string) {
		list, _ := value.([]any)
		for _, value := range list {
			b, _ := value.(map[string]any)
			if b["thought"] == true {
				continue
			}
			if t, ok := b[key].(string); ok {
				text.WriteString(t)
			}
		}
	}
	switch format {
	case "openai":
		choices, _ := data["choices"].([]any)
		if len(choices) == 0 {
			break
		}
		choice, _ := choices[0].(map[string]any)
		if choice["finish_reason"] == "length" || choice["finish_reason"] == "content_filter" {
			return "", fmt.Errorf("truncated")
		}
		message, _ := choice["message"].(map[string]any)
		if v, ok := message["refusal"].(string); ok && v != "" {
			return "", fmt.Errorf("refused")
		}
		if t, ok := message["content"].(string); ok {
			text.WriteString(t)
		} else {
			blocks(message["content"], "text")
		}
	case "anthropic":
		if data["stop_reason"] == "max_tokens" || data["stop_reason"] == "refusal" {
			return "", fmt.Errorf("truncated")
		}
		blocks(data["content"], "text")
	case "gemini":
		list, _ := data["candidates"].([]any)
		if len(list) == 0 {
			break
		}
		candidate, _ := list[0].(map[string]any)
		if reason, ok := candidate["finishReason"].(string); ok && reason != "STOP" {
			return "", fmt.Errorf("blocked")
		}
		content, _ := candidate["content"].(map[string]any)
		blocks(content["parts"], "text")
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", fmt.Errorf("empty response")
	}
	return text.String(), nil
}
