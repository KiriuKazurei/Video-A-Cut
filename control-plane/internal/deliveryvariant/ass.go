package deliveryvariant

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Cue struct {
	Start, End int64
	Text       string
}

var srtTime = regexp.MustCompile(`^(\d{2,}):(\d{2}):(\d{2}),(\d{3}) --> (\d{2,}):(\d{2}):(\d{2}),(\d{3})$`)

func TimelineDuration(edl []byte) (int64, error) {
	var e struct {
		Timeline struct {
			FPS float64 `json:"fps"`
		}
		Video []struct {
			In, Out    float64
			TimelineIn float64 `json:"timeline_in"`
		}
	}
	if err := json.Unmarshal(edl, &e); err != nil {
		return 0, err
	}
	fps := e.Timeline.FPS
	if fps <= 0 || fps > 9007199254740991 || fps != math.Trunc(fps) || len(e.Video) == 0 {
		return 0, fmt.Errorf("invalid timeline")
	}
	var frames float64
	for _, v := range e.Video {
		n := math.Round(v.Out*fps) - math.Round(v.In*fps)
		end := math.Round(v.TimelineIn*fps) + n
		if v.In < 0 || v.TimelineIn < 0 || n <= 0 || end > 9007199254740991 {
			return 0, fmt.Errorf("invalid video span")
		}
		frames = math.Max(frames, end)
	}
	ms := math.Round(frames * 1000 / fps)
	if ms <= 0 || ms > 9007199254740991 {
		return 0, fmt.Errorf("invalid duration")
	}
	return int64(ms), nil
}
func ParseSRT(raw []byte, durationMs int64) ([]Cue, error) { return parseSRT(raw, durationMs, true) }
func ParseSourceSRT(raw []byte, durationMs int64) ([]Cue, error) {
	return parseSRT(raw, durationMs, false)
}
func parseSRT(raw []byte, durationMs int64, fixed bool) ([]Cue, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("SRT is not UTF-8")
	}
	text := strings.TrimPrefix(string(raw), "\ufeff")
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return []Cue{}, nil
	}
	blocks := regexp.MustCompile(`\n\n+`).Split(strings.TrimRight(text, "\n"), -1)
	cues := make([]Cue, 0, len(blocks))
	for i, block := range blocks {
		lines := strings.Split(block, "\n")
		if len(lines) < 3 || lines[0] != strconv.Itoa(i+1) {
			return nil, fmt.Errorf("invalid SRT block")
		}
		m := srtTime.FindStringSubmatch(lines[1])
		if m == nil {
			return nil, fmt.Errorf("invalid SRT timing")
		}
		values := make([]int64, 8)
		for j := range values {
			n, err := strconv.ParseInt(m[j+1], 10, 64)
			if err != nil || n > 9007199254 {
				return nil, fmt.Errorf("invalid SRT time")
			}
			values[j] = n
		}
		if values[1] > 59 || values[2] > 59 || values[5] > 59 || values[6] > 59 {
			return nil, fmt.Errorf("invalid SRT time")
		}
		start := ((values[0]*60+values[1])*60+values[2])*1000 + values[3]
		end := ((values[4]*60+values[5])*60+values[6])*1000 + values[7]
		content := strings.Join(lines[2:], "\n")
		for _, r := range content {
			if (r < 32 && r != '\n' && r != '\t') || r == 127 {
				return nil, fmt.Errorf("unsafe subtitle text")
			}
		}
		a, b := (start+5)/10, (end+5)/10
		if start > 9007199254740991 || end > 9007199254740991 || strings.TrimSpace(content) == "" || end <= start || b <= a || end > durationMs+1 {
			return nil, fmt.Errorf("empty or out-of-range subtitle")
		}
		cues = append(cues, Cue{a, b, content})
		if fixed && strings.Count(wrapASS(content), "\n") >= 8 {
			return nil, fmt.Errorf("subtitle exceeds fixed ASS line limit")
		}
	}
	if fixed && len(ASS(cues)) > 1024*1024 {
		return nil, fmt.Errorf("ASS exceeds 1 MiB")
	}
	return cues, nil
}
func ASS(cues []Cue) string {
	header := "[Script Info]\nScriptType: v4.00+\nPlayResX: 1280\nPlayResY: 720\nWrapStyle: 0\nScaledBorderAndShadow: yes\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,Microsoft YaHei,36,&H00FFFFFF,&H00FFFFFF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,2,1,2,64,64,36,1\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n"
	var out strings.Builder
	out.WriteString(header)
	time := func(cs int64) string {
		return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, (cs/6000)%60, (cs/100)%60, cs%100)
	}
	for _, c := range cues {
		var text strings.Builder
		for _, r := range wrapASS(c.Text) {
			switch r {
			case '\\':
				text.WriteString("\\\u2060")
			case '{':
				text.WriteString("\\{")
			case '}':
				text.WriteString("\\}")
			case '\n':
				text.WriteString("\\N")
			default:
				text.WriteRune(r)
			}
		}
		fmt.Fprintf(&out, "Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n", time(c.Start), time(c.End), text.String())
	}
	return out.String()
}

func wrapASS(text string) string { return wrapASSLimit(text, 56) }
func wrapASSLimit(text string, limit int) string {
	var out strings.Builder
	units := 0
	for _, r := range text {
		if r == '\n' {
			out.WriteRune(r)
			units = 0
			continue
		}
		width := 2
		if r == '\t' {
			width = 4
		}
		if r >= 0x300 && r <= 0x36f || r >= 0xfe00 && r <= 0xfe0f || r >= 0xe0100 && r <= 0xe01ef || r == 0x200d {
			width = 0
		}
		if width > 0 && units+width > limit {
			out.WriteByte('\n')
			units = 0
		}
		out.WriteRune(r)
		units += width
	}
	return out.String()
}
