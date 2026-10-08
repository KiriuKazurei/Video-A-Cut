// Package tts defines shared console/template configuration, independent of video assets.
package tts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var Identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

type Settings struct {
	Engine         string  `json:"engine"`
	Endpoint       string  `json:"endpoint,omitempty"`
	Model          string  `json:"model,omitempty"`
	TokenEnv       string  `json:"token_env,omitempty"`
	Voice          string  `json:"voice"`
	Language       string  `json:"language"`
	Speed          float64 `json:"speed"`
	Volume         float64 `json:"volume"`
	PitchSemitones float64 `json:"pitch_semitones"`
	SAPIRate       int     `json:"sapi_rate"`
	Instruction    string  `json:"instruction"`
}

func (s Settings) External() bool {
	if s.Engine != "stepfun" {
		return false
	}
	u, e := url.Parse(s.Endpoint)
	if e != nil {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return !strings.EqualFold(u.Hostname(), "localhost") && (ip == nil || !ip.IsLoopback())
}
func ValidText(s string, max int, required bool) bool {
	return (!required || strings.TrimSpace(s) != "") && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, 0)
}
func (s Settings) Validate() error {
	if (s.Engine != "sapi" && s.Engine != "stepfun") || !ValidText(s.Voice, 200, true) {
		return fmt.Errorf("请选择引擎和有效音色")
	}
	for _, v := range []struct {
		n            string
		v, low, high float64
	}{{"speed", s.Speed, .5, 2}, {"volume", s.Volume, .1, 2}, {"pitch_semitones", s.PitchSemitones, -12, 12}} {
		if math.IsNaN(v.v) || math.IsInf(v.v, 0) || v.v < v.low || v.v > v.high {
			return fmt.Errorf("参数 %s 超出范围", v.n)
		}
	}
	if s.SAPIRate < -10 || s.SAPIRate > 10 || !ValidText(s.Instruction, 200, false) {
		return fmt.Errorf("语速或表达指令无效；表达指令上限 200 字符")
	}
	switch s.Language {
	case "zh", "en", "ja", "ko", "fr", "es":
	default:
		return fmt.Errorf("不支持的语言")
	}
	if s.Engine == "sapi" {
		if s.Endpoint != "" || s.Model != "" || s.TokenEnv != "" || s.Instruction != "" || s.Speed != 1 || s.Volume > 1 {
			return fmt.Errorf("SAPI 不支持云端参数或表达指令")
		}
		return nil
	}
	if s.Model != "stepaudio-2.5-tts" || !envName.MatchString(s.TokenEnv) {
		return fmt.Errorf("请选择 stepaudio-2.5-tts 和有效凭证变量")
	}
	u, e := url.Parse(s.Endpoint)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !ValidText(s.Endpoint, 2048, true) {
		return fmt.Errorf("无效的 TTS 地址")
	}
	ip := net.ParseIP(u.Hostname())
	local := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return fmt.Errorf("TTS 地址须为 HTTPS 或本机 HTTP")
	}
	return nil
}
func Hash(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type Template struct {
	TemplateID    string    `json:"template_id"`
	Revision      int       `json:"revision"`
	Name          string    `json:"name"`
	Settings      Settings  `json:"settings"`
	ReferenceText string    `json:"reference_text"`
	SHA256        string    `json:"sha256"`
	Archived      bool      `json:"archived"`
	CreatedAt     time.Time `json:"created_at"`
}

// Binding is resolved by the server at profile save, never from a moving latest pointer.
type Binding struct {
	TemplateID string    `json:"template_id"`
	Revision   int       `json:"revision"`
	SHA256     string    `json:"sha256,omitempty"`
	Settings   *Settings `json:"settings,omitempty"`
}

func (b Binding) Validate() error {
	if !Identifier.MatchString(b.TemplateID) || b.Revision < 1 || len(b.SHA256) != 64 || b.Settings == nil {
		return fmt.Errorf("模板绑定须含固定版本、摘要和配置快照")
	}
	if _, e := hex.DecodeString(b.SHA256); e != nil {
		return fmt.Errorf("无效模板摘要")
	}
	return b.Settings.Validate()
}

type SaveRequest struct {
	TemplateID       string   `json:"template_id"`
	Name             string   `json:"name"`
	Settings         Settings `json:"settings"`
	ReferenceText    string   `json:"reference_text"`
	ExpectedRevision int      `json:"expected_revision"`
	IdempotencyKey   string   `json:"idempotency_key"`
}
type PreviewRequest struct {
	Text           string   `json:"text"`
	Settings       Settings `json:"settings"`
	IdempotencyKey string   `json:"idempotency_key"`
}
type Preview struct {
	JobID           string    `json:"job_id"`
	InputSHA256     string    `json:"input_sha256"`
	Status          string    `json:"status"`
	Message         string    `json:"message,omitempty"`
	Text            string    `json:"text"`
	Settings        Settings  `json:"settings"`
	DurationSeconds float64   `json:"duration_seconds"`
	SizeBytes       int64     `json:"size_bytes"`
	Expired         bool      `json:"expired"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type Voice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var StepVoices = []Voice{{"vibrant-youth", "元气男声"}, {"lively-girl", "活泼女声"}, {"soft-spoken-gentleman", "温柔男声"}, {"magnetic-voiced-male", "磁性男声"}, {"zixinnansheng", "自信男声"}, {"elegantgentle-female", "温婉女声"}, {"livelybreezy-female", "轻快女声"}}
