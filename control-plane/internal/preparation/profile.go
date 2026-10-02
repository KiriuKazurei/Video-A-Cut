package preparation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func textOK(value string, max int) bool {
	if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// Validate returns field names, never echoes a potentially secret input value.
func (p Profile) Validate() error {
	if p.SchemaVersion != 1 || !identifier.MatchString(p.ProfileID) || p.Revision < 1 || !textOK(p.Name, 100) {
		return fmt.Errorf("invalid profile identity or schema")
	}
	if p.ContentMode != "builtin" && p.ContentMode != "configured" {
		return fmt.Errorf("invalid content_mode")
	}
	if p.ExportTarget != "premiere" {
		return fmt.Errorf("export_target must be premiere in phase 6")
	}
	if !textOK(p.TTSVoice, 100) {
		return fmt.Errorf("tts_voice is required")
	}
	if p.Sampling.MaxFrames < 1 || p.Sampling.MaxFrames > 48 || p.Sampling.MaxBytes < 1 || p.Sampling.MaxBytes > 48*1024*1024 || p.Sampling.TimeoutSeconds < 1 || p.Sampling.TimeoutSeconds > 120 {
		return fmt.Errorf("sampling exceeds bounded limits")
	}
	for _, spec := range []struct {
		name, adapter string
		value         Provider
	}{{"vision", "vision", p.Vision}, {"narration", "narration", p.Narration}} {
		v := spec.value
		if p.ContentMode == "builtin" {
			if v.Adapter != "builtin" || v.Endpoint != "" || v.Model != "" || v.TokenEnv != "" || v.APIFormat != "" {
				return fmt.Errorf("%s must be explicitly builtin without external settings", spec.name)
			}
			continue
		}
		if v.Adapter != spec.adapter {
			return fmt.Errorf("invalid %s adapter, model or token_env", spec.name)
		}
		if err := v.Validate(true); err != nil {
			return fmt.Errorf("invalid %s provider: %v", spec.name, err)
		}
	}
	return nil
}

// Validate checks a provider without transmitting anything. Discovery can omit model.
func (p Provider) Validate(requireModel bool) error {
	if p.Adapter != "vision" && p.Adapter != "narration" {
		return fmt.Errorf("adapter must be vision or narration")
	}
	if p.APIFormat != "" && p.APIFormat != "openai" && p.APIFormat != "anthropic" && p.APIFormat != "gemini" {
		return fmt.Errorf("unsupported api_format")
	}
	if (requireModel || p.Model != "") && !textOK(p.Model, 100) || !envName.MatchString(p.TokenEnv) {
		return fmt.Errorf("invalid model or token_env")
	}
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || len(p.Endpoint) > 2048 || !textOK(p.Endpoint, 2048) {
		return fmt.Errorf("invalid endpoint; credentials and queries are forbidden")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && localHost(u.Hostname())) {
		return fmt.Errorf("endpoint requires HTTPS or loopback HTTP")
	}
	if p.APIFormat == "gemini" && requireModel && !regexp.MustCompile(`^(models/)?[A-Za-z0-9_.-]+$`).MatchString(p.Model) {
		return fmt.Errorf("invalid Gemini model identifier")
	}
	return nil
}

func localHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (p Provider) External() bool {
	u, err := url.Parse(p.Endpoint)
	return err == nil && u.Hostname() != "" && !localHost(u.Hostname())
}

func (p Profile) Fingerprint() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
