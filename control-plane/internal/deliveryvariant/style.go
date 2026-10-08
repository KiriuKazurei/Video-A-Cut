package deliveryvariant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

const EditableSpecID = "ass-basic-v2"

type SubtitleStyle struct {
	FontFamily   string  `json:"font_family"`
	FontSize     int     `json:"font_size"`
	Bold         bool    `json:"bold"`
	Italic       bool    `json:"italic"`
	Underline    bool    `json:"underline"`
	Outline      float64 `json:"outline"`
	Shadow       float64 `json:"shadow"`
	PrimaryColor string  `json:"primary_color"`
	OutlineColor string  `json:"outline_color"`
	Alignment    int     `json:"alignment"`
	MarginL      int     `json:"margin_l"`
	MarginR      int     `json:"margin_r"`
	MarginV      int     `json:"margin_v"`
	FadeInMS     int64   `json:"fade_in_ms"`
	FadeOutMS    int64   `json:"fade_out_ms"`
}
type SubtitleCueOverride struct {
	CueID   int                        `json:"cue_id"`
	StartMS *int64                     `json:"start_ms,omitempty"`
	EndMS   *int64                     `json:"end_ms,omitempty"`
	Style   map[string]json.RawMessage `json:"style,omitempty"`
}
type StyleDocument struct {
	SourceSRTSHA256 string                `json:"source_srt_sha256"`
	PlayResX        int                   `json:"play_res_x"`
	PlayResY        int                   `json:"play_res_y"`
	Style           SubtitleStyle         `json:"style"`
	Overrides       []SubtitleCueOverride `json:"overrides"`
}
type SubtitleStyleDraft struct {
	AssetID      string        `json:"asset_id"`
	RunID        string        `json:"run_id"`
	RevisionID   string        `json:"revision_id"`
	DraftVersion int           `json:"draft_version"`
	StyleSHA256  string        `json:"style_sha256"`
	DurationMS   int64         `json:"duration_ms"`
	Cues         []SourceCue   `json:"cues"`
	Document     StyleDocument `json:"document"`
}
type SourceCue struct {
	CueID   int    `json:"cue_id"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
}
type SaveStyleRequest struct {
	RevisionID           string        `json:"revision_id"`
	ExpectedDraftVersion int           `json:"expected_draft_version"`
	Document             StyleDocument `json:"document"`
}
type FontFamily struct {
	Family string `json:"family"`
	SHA256 string `json:"sha256"`
}

func DefaultStyle() SubtitleStyle {
	return SubtitleStyle{FontFamily: "Microsoft YaHei", FontSize: 36, Outline: 2, Shadow: 1, PrimaryColor: "#FFFFFF", OutlineColor: "#000000", Alignment: 2, MarginL: 64, MarginR: 64, MarginV: 36}
}

var rgbColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func (s SubtitleStyle) Validate(x, y int) error {
	for _, r := range s.FontFamily {
		if r < 32 || r == 127 {
			return fmt.Errorf("字体名称含控制字符")
		}
	}
	if strings.TrimSpace(s.FontFamily) == "" || len(s.FontFamily) > 128 || strings.ContainsAny(s.FontFamily, ",\n\r\\{}") || s.FontSize < 12 || s.FontSize > 120 || math.IsNaN(s.Outline) || math.IsInf(s.Outline, 0) || s.Outline < 0 || s.Outline > 10 || s.Outline != math.Round(s.Outline*100)/100 || math.IsNaN(s.Shadow) || math.IsInf(s.Shadow, 0) || s.Shadow < 0 || s.Shadow > 10 || s.Shadow != math.Round(s.Shadow*100)/100 || !rgbColor.MatchString(s.PrimaryColor) || !rgbColor.MatchString(s.OutlineColor) || s.Alignment < 1 || s.Alignment > 9 || s.MarginL < 0 || s.MarginR < 0 || s.MarginL+s.MarginR >= x || s.MarginV < 0 || s.MarginV >= y || s.FadeInMS < 0 || s.FadeOutMS < 0 || s.FadeInMS > 9007199254740991 || s.FadeOutMS > 9007199254740991 {
		return fmt.Errorf("字体、字号、颜色、位置或边距无效")
	}
	return nil
}
func ResolveStyle(base SubtitleStyle, override map[string]json.RawMessage) (SubtitleStyle, error) {
	raw, _ := json.Marshal(base)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for k, v := range override {
		if _, ok := fields[k]; !ok {
			return base, fmt.Errorf("unknown style field %s", k)
		}
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return base, fmt.Errorf("null style field %s", k)
		}
		fields[k] = v
	}
	raw, _ = json.Marshal(fields)
	var out SubtitleStyle
	err := json.Unmarshal(raw, &out)
	return out, err
}
func EffectiveCue(cue Cue, id int, d StyleDocument) (Cue, SubtitleStyle, error) {
	style := d.Style
	for _, o := range d.Overrides {
		if o.CueID == id {
			var e error
			style, e = ResolveStyle(style, o.Style)
			if e != nil {
				return cue, style, e
			}
			if o.StartMS != nil {
				cue.Start = (*o.StartMS + 5) / 10
			}
			if o.EndMS != nil {
				cue.End = (*o.EndMS + 5) / 10
			}
		}
	}
	return cue, style, nil
}
func ValidateStyleDocument(d StyleDocument, cues []Cue, duration int64) error {
	if d.Overrides == nil || !shaPattern.MatchString(d.SourceSRTSHA256) || d.PlayResY != 720 || d.PlayResX < 180 || d.PlayResX > 10000 || len(d.Overrides) > len(cues) {
		return fmt.Errorf("invalid style document")
	}
	if e := d.Style.Validate(d.PlayResX, d.PlayResY); e != nil {
		return e
	}
	seen := map[int]bool{}
	for _, o := range d.Overrides {
		if o.CueID < 1 || o.CueID > len(cues) || seen[o.CueID] {
			return fmt.Errorf("invalid or duplicate cue id")
		}
		seen[o.CueID] = true
		for _, ms := range []*int64{o.StartMS, o.EndMS} {
			if ms != nil && (*ms < 0 || *ms > duration) {
				return fmt.Errorf("第 %d 条字幕时间超出成片范围", o.CueID)
			}
		}
	}
	for i, c := range cues {
		c, s, e := EffectiveCue(c, i+1, d)
		if e != nil {
			return e
		}
		if e = s.Validate(d.PlayResX, d.PlayResY); e != nil {
			return fmt.Errorf("第 %d 条: %w", i+1, e)
		}
		if c.Start < 0 || c.End <= c.Start || c.End > (duration+5)/10 || s.FadeInMS+s.FadeOutMS > (c.End-c.Start)*10 {
			return fmt.Errorf("第 %d 条字幕时间或渐入渐出时长无效", i+1)
		}
	}
	return nil
}

// Canonical JSON sorts object keys and leaves Unicode/HTML literal, shared with Node.
func StyleDocumentBytes(d StyleDocument) []byte {
	raw, _ := json.Marshal(d)
	var value any
	_ = json.Unmarshal(raw, &value)
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(value)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}
func StyleHash(d StyleDocument) string { return digest(StyleDocumentBytes(d)) }
func StyledASS(cues []Cue, d StyleDocument) string {
	var out strings.Builder
	fmt.Fprintf(&out, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 0\nScaledBorderAndShadow: yes\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n", d.PlayResX, d.PlayResY)
	flag := func(b bool) int {
		if b {
			return -1
		}
		return 0
	}
	color := func(s string) string { return "&H00" + strings.ToUpper(s[5:7]+s[3:5]+s[1:3]) }
	num := func(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
	for i, c := range cues {
		_, s, _ := EffectiveCue(c, i+1, d)
		fmt.Fprintf(&out, "Style: Cue_%d,%s,%d,%s,%s,%s,&H80000000,%d,%d,%d,0,100,100,0,0,1,%s,%s,%d,%d,%d,%d,1\n", i+1, s.FontFamily, s.FontSize, color(s.PrimaryColor), color(s.PrimaryColor), color(s.OutlineColor), flag(s.Bold), flag(s.Italic), flag(s.Underline), num(s.Outline), num(s.Shadow), s.Alignment, s.MarginL, s.MarginR, s.MarginV)
	}
	out.WriteString("\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for i, c := range cues {
		c, s, _ := EffectiveCue(c, i+1, d)
		limit := max(2, int(math.Floor(float64(d.PlayResX-s.MarginL-s.MarginR-20)/float64(s.FontSize)))*2)
		text := escapeASS(wrapASSLimit(c.Text, limit))
		effect := ""
		if s.FadeInMS > 0 || s.FadeOutMS > 0 {
			effect = fmt.Sprintf("{\\fad(%d,%d)}", s.FadeInMS, s.FadeOutMS)
		}
		tm := func(v int64) string {
			return fmt.Sprintf("%d:%02d:%02d.%02d", v/360000, (v/6000)%60, (v/100)%60, v%100)
		}
		fmt.Fprintf(&out, "Dialogue: 0,%s,%s,Cue_%d,,0,0,0,,%s%s\n", tm(c.Start), tm(c.End), i+1, effect, text)
	}
	return out.String()
}
func escapeASS(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch r {
		case '\\':
			b.WriteString("\\\u2060")
		case '{':
			b.WriteString("\\{")
		case '}':
			b.WriteString("\\}")
		case '\n':
			b.WriteString("\\N")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
