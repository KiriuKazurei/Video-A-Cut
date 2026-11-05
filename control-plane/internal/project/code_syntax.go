package project

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

type ParameterKeyframe struct {
	Kind       string   `json:"kind"`
	StartValue *float64 `json:"start_value,omitempty"`
	Frame      int64    `json:"frame"`
	Duration   int64    `json:"duration"`
	X          float64  `json:"x,omitempty"`
	Y          float64  `json:"y,omitempty"`
	Value      float64  `json:"value,omitempty"`
}

// Shared with the paragraph language: quoted subtitle braces and escaped
// literal braces must have the same meaning in both editing contexts.
func ScriptDirectiveEnd(source string, start int) (int, error) {
	quoted, escaped := false, false
	for i := start + 1; i < len(source); i++ {
		c := source[i]
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
		} else if c == '{' {
			return 0, fmt.Errorf("语法块不能嵌套")
		} else if c == '}' {
			return i, nil
		}
	}
	return 0, fmt.Errorf("语法块缺少右花括号或字符串引号未闭合")
}

func validateParameterKeyframes(keys []ParameterKeyframe, duration int64) error {
	last := map[string]int64{}
	for _, k := range keys {
		if !slices.Contains([]string{"移动", "旋转", "缩放", "透明度"}, k.Kind) || k.Frame < 0 || k.Frame >= duration || k.Duration < 0 || k.Duration > duration-k.Frame || !finite(k.X) || !finite(k.Y) || !finite(k.Value) {
			return fmt.Errorf("参数关键帧无效")
		}
		if end, ok := last[k.Kind]; ok && k.Frame < end {
			return fmt.Errorf("参数关键帧重叠或未排序")
		}
		if k.Kind == "缩放" && (k.Value <= 0 || k.Value > 8) || k.Kind == "透明度" && (k.Value < 0 || k.Value > 1 || k.StartValue != nil && (!finite(*k.StartValue) || *k.StartValue < 0 || *k.StartValue > 1)) {
			return fmt.Errorf("参数取值超出范围")
		}
		last[k.Kind] = max(k.Frame+1, k.Frame+k.Duration)
	}
	return nil
}

func parameterAt(e Effects, kind string, frame int64) (float64, float64) {
	x, y := e.X, e.Y
	if kind == "旋转" {
		x, y = e.Rotation, 0
	}
	if kind == "缩放" {
		x, y = e.Scale, 0
	}
	if kind == "透明度" {
		x, y = 1, 0
		if e.Opacity != nil {
			x = *e.Opacity
		}
	}
	for _, k := range e.ParameterKeyframes {
		if k.Kind != kind {
			continue
		}
		if frame < k.Frame {
			break
		}
		a, b := k.X, k.Y
		if kind != "移动" {
			a = k.Value
			b = 0
		}
		if k.StartValue != nil {
			x = *k.StartValue
		}
		p := 1.0
		if k.Duration > 0 {
			p = math.Min(1, float64(frame-k.Frame)/float64(k.Duration))
		}
		x += p * (a - x)
		y += p * (b - y)
		if p < 1 {
			break
		}
	}
	return x, y
}

func sliceParameters(c *Clip, offset int64) {
	old := clone(c.Effects)
	c.Effects.X, c.Effects.Y = parameterAt(old, "移动", offset)
	c.Effects.Rotation, _ = parameterAt(old, "旋转", offset)
	c.Effects.Scale, _ = parameterAt(old, "缩放", offset)
	if old.Opacity != nil || slices.ContainsFunc(old.ParameterKeyframes, func(k ParameterKeyframe) bool { return k.Kind == "透明度" }) {
		v, _ := parameterAt(old, "透明度", offset)
		c.Effects.Opacity = &v
	}
	c.Effects.Subtitles = nil
	for _, cue := range old.Subtitles {
		start, end := max(cue.Frame, offset), min(cue.Frame+cue.Duration, offset+c.Duration)
		if end > start {
			cue.Frame, cue.Duration = start-offset, end-start
			c.Effects.Subtitles = append(c.Effects.Subtitles, cue)
		}
	}
	c.Effects.ParameterKeyframes = nil
	for _, k := range old.ParameterKeyframes {
		end := k.Frame + k.Duration
		if end <= offset {
			continue
		}
		if k.Frame < offset {
			k.Frame = 0
			k.Duration = end - offset
			k.StartValue = nil
		} else {
			k.Frame -= offset
		}
		if k.Frame >= c.Duration {
			continue
		}
		if k.Duration > c.Duration-k.Frame {
			x, y := parameterAt(old, k.Kind, offset+c.Duration)
			if k.Kind != "移动" {
				k.Value = x
			} else {
				k.X = x
				k.Y = y
			}
		}
		k.Duration = min(k.Duration, c.Duration-k.Frame)
		c.Effects.ParameterKeyframes = append(c.Effects.ParameterKeyframes, k)
	}
}

func CodeTime(text string, rate Rate) (int64, error) {
	parts := strings.Split(text, ":")
	if len(parts) != 3 && len(parts) != 4 {
		return 0, fmt.Errorf("时间需要时:分:秒或时:分:秒:帧")
	}
	values := []int64{}
	for _, p := range parts {
		v, e := strconv.ParseInt(p, 10, 64)
		if e != nil || v < 0 {
			return 0, fmt.Errorf("时间无效")
		}
		values = append(values, v)
	}
	if values[1] >= 60 || values[2] >= 60 {
		return 0, fmt.Errorf("时间分秒超出范围")
	}
	nominal := int64(math.Round(rate.Value()))
	f := int64(0)
	if len(values) == 4 {
		f = values[3]
		if f >= nominal {
			return 0, fmt.Errorf("帧超出FPS范围")
		}
	}
	return ((values[0]*60+values[1])*60+values[2])*nominal + f, nil
}

// ParseScriptTime returns a clip-local integer frame. Legacy bare timecodes
// remain sequence time; explicit local times survive timeline moves.
func ParseScriptTime(text string, c Clip, rate Rate) (int64, error) {
	if text == "" {
		return 0, nil
	}
	local := strings.HasPrefix(text, "条内")
	text = strings.TrimPrefix(strings.TrimPrefix(text, "条内"), "序列")
	var frame int64
	var err error
	if strings.Contains(text, ":") {
		frame, err = CodeTime(text, rate)
	} else {
		frame, err = ScriptDuration(text, rate)
	}
	if err != nil {
		return 0, err
	}
	if !local {
		frame -= c.At
	}
	return frame, nil
}
func ScriptDuration(text string, rate Rate) (int64, error) {
	factor := 1.0
	unit := "s"
	if strings.HasSuffix(text, "ms") {
		unit = "ms"
		factor = .001
	} else if strings.HasSuffix(text, "f") {
		unit = "f"
	}
	if !strings.HasSuffix(text, unit) {
		return 0, fmt.Errorf("时间需要时码或s/ms/f单位")
	}
	raw := strings.TrimSuffix(text, unit)
	n, err := strconv.ParseFloat(raw, 64)
	if !scriptDurationDecimal.MatchString(raw) || err != nil || !finite(n) || n < 0 || n > 1e7 || unit == "f" && n != math.Trunc(n) {
		return 0, fmt.Errorf("时间值无效")
	}
	if unit == "f" {
		return int64(n), nil
	}
	return rate.Frames(n * factor), nil
}

type SubtitleCue struct {
	TimeBase string  `json:"time_base,omitempty"`
	Text     string  `json:"text"`
	Frame    int64   `json:"frame"`
	Duration int64   `json:"duration"`
	SizePX   float64 `json:"size_px,omitempty"`
	Font     string  `json:"font,omitempty"`
}

func (cue SubtitleCue) Validate(duration int64) error {
	if cue.TimeBase != "" {
		return fmt.Errorf("片段字幕必须使用条内整数帧")
	}
	if strings.TrimSpace(cue.Text) == "" || len([]rune(cue.Text)) > 2000 || cue.Frame < 0 || cue.Frame >= duration || cue.Duration < 1 || cue.Duration > duration-cue.Frame || cue.SizePX != 0 && (!finite(cue.SizePX) || cue.SizePX < 1 || cue.SizePX > 512) || len([]rune(cue.Font)) > 100 || strings.ContainsAny(cue.Font, "\r\n\x00") {
		return fmt.Errorf("字幕文字、样式或时间范围无效")
	}
	return nil
}
func ParseSubtitleCue(block ScriptBlock, c Clip, rate Rate) (SubtitleCue, error) {
	cue := SubtitleCue{}
	_ = json.Unmarshal([]byte(block.Args["文本"]), &cue.Text)
	_ = json.Unmarshal([]byte(block.Args["字体"]), &cue.Font)
	var err error
	cue.Frame, err = ParseScriptTime(block.Args["时间"], c, rate)
	if err != nil {
		return cue, err
	}
	cue.Duration = c.Duration - cue.Frame
	if v := block.Args["持续"]; v != "" {
		cue.Duration, err = ScriptDuration(v, rate)
		if err != nil {
			return cue, err
		}
	}
	if v := block.Args["字号"]; v != "" {
		cue.SizePX, err = ScriptNumber("字幕", "字号", v)
		if err != nil {
			return cue, err
		}
	}
	return cue, cue.Validate(c.Duration)
}

// The timeline owns parameter semantics; creative text remains plain data.
func ParseCodeDescription(text string, c Clip, rate Rate) (Effects, string, error) {
	fx := clone(c.Effects)
	keys := []ParameterKeyframe{}
	blocks, err := ScriptBlocks(text, "code")
	if err != nil {
		return fx, "", err
	}
	fx.Subtitles = nil
	if c.Code != nil && c.Code.Script != nil {
		old, _ := ScriptBlocks(*c.Code.Script, "code")
		for _, prior := range old {
			if slices.ContainsFunc(blocks, func(b ScriptBlock) bool { return b.Name == prior.Name }) {
				continue
			}
			switch prior.Name {
			case "音量":
				fx.GainDB = 0
			case "移动":
				fx.X, fx.Y = 0, 0
			case "旋转":
				fx.Rotation = 0
			case "缩放":
				fx.Scale = 1
			case "透明度":
				fx.Opacity = nil
			case "黑屏", "白屏":
				fx.FadeIn = 0
				fx.FadeColor = ""
			case "交叉溶解":
				fx.Transition = 0
			}
		}
	}
	cursor := 0
	var body strings.Builder
	for _, block := range blocks {
		body.WriteString(text[cursor:block.Start])
		cursor = block.End
		if block.Name == "@" {
			body.WriteString(text[block.Start:block.End])
			continue
		}
		fail := func(err error) (Effects, string, error) { return fx, "", ScriptError(text, block.Start, err.Error()) }
		if block.Name == "字幕" {
			cue, err := ParseSubtitleCue(block, c, rate)
			if err != nil {
				return fail(err)
			}
			fx.Subtitles = append(fx.Subtitles, cue)
			continue
		}
		if block.Name == "黑屏" || block.Name == "白屏" {
			fx.FadeColor = "black"
			if block.Name == "白屏" {
				fx.FadeColor = "white"
			}
			fx.FadeIn = min(rate.Frames(.35), c.Duration/3)
			continue
		}
		if block.Name == "交叉溶解" {
			fx.Transition = min(rate.Frames(.35), c.Duration/3)
			continue
		}
		at, err := ParseScriptTime(block.Args["时间"], c, rate)
		if err != nil {
			return fail(err)
		}
		k := ParameterKeyframe{Kind: block.Name, Frame: at}
		if v := block.Args["持续"]; v != "" {
			k.Duration, err = ScriptDuration(v, rate)
			if err != nil {
				return fail(err)
			}
		}
		if at < 0 || at >= c.Duration || k.Duration > c.Duration-at {
			return fail(fmt.Errorf("%s时间超出代码条（共%d帧）", block.Name, c.Duration))
		}
		number := func(name string) float64 { v, _ := ScriptNumber(block.Name, name, block.Args[name]); return v }
		switch k.Kind {
		case "移动":
			k.X, k.Y = number("x"), number("y")
		case "旋转":
			k.Value = number("角度")
		case "缩放":
			k.Value = number("比例") / 100
		case "透明度":
			k.Value = number("到") / 100
			if block.Args["从"] != "" {
				v := number("从") / 100
				k.StartValue = &v
			}
		case "音量":
			k.Value = number("增益")
			if block.Args["起始增益"] != "" {
				v := number("起始增益")
				k.StartValue = &v
			}
		}
		keys = append(keys, k)
	}
	body.WriteString(text[cursor:])
	slices.SortStableFunc(keys, func(a, b ParameterKeyframe) int {
		if a.Frame < b.Frame {
			return -1
		}
		if a.Frame > b.Frame {
			return 1
		}
		return 0
	})
	last := map[string]int64{}
	for _, k := range keys {
		if end, ok := last[k.Kind]; ok && k.Frame < end {
			return fx, "", fmt.Errorf("%s在第%d帧的指令重复或动画范围重叠", k.Kind, k.Frame)
		}
		last[k.Kind] = max(k.Frame+1, k.Frame+k.Duration)
	}
	fx.ParameterKeyframes = nil
	fx.GainKeyframes = nil
	for _, k := range keys {
		if k.Kind != "音量" {
			fx.ParameterKeyframes = append(fx.ParameterKeyframes, k)
			continue
		}
		start, end := float64(k.Frame)/float64(c.Duration), float64(k.Frame+k.Duration)/float64(c.Duration)
		if len(fx.GainKeyframes) == 0 && start > 0 {
			fx.GainKeyframes = append(fx.GainKeyframes, GainKeyframe{0, fx.GainDB})
		}
		value := fx.GainDB
		if len(fx.GainKeyframes) > 0 {
			value = fx.GainKeyframes[len(fx.GainKeyframes)-1].DB
		}
		if k.StartValue != nil {
			value = *k.StartValue
			if len(fx.GainKeyframes) == 0 {
				fx.GainDB = value
			}
		}
		if k.Duration > 0 {
			if len(fx.GainKeyframes) > 0 && fx.GainKeyframes[len(fx.GainKeyframes)-1].T == start {
				fx.GainKeyframes = fx.GainKeyframes[:len(fx.GainKeyframes)-1]
			}
			fx.GainKeyframes = append(fx.GainKeyframes, GainKeyframe{start, value})
		} else if k.Frame > 0 {
			fx.GainKeyframes = append(fx.GainKeyframes, GainKeyframe{float64(k.Frame-1) / float64(c.Duration), value})
		}
		if len(fx.GainKeyframes) > 0 && fx.GainKeyframes[len(fx.GainKeyframes)-1].T == end {
			fx.GainKeyframes[len(fx.GainKeyframes)-1].DB = k.Value
		} else {
			fx.GainKeyframes = append(fx.GainKeyframes, GainKeyframe{end, k.Value})
		}
	}
	if len(fx.GainKeyframes) > 0 {
		fx.GainDB = fx.GainKeyframes[0].DB
	}
	return fx, strings.TrimSpace(body.String()), nil
}
