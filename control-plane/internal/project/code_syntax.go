package project

import (
	"fmt"
	"math"
	"regexp"
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

var codeDirective = regexp.MustCompile(`^\{(音量|移动|旋转)([^{}]*)\}$`)

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

type codeDirectiveSpan struct {
	start, end int
	match      []string
}

func codeDirectiveSpans(text string) ([]codeDirectiveSpan, error) {
	spans := []codeDirectiveSpan{}
	for i := 0; i < len(text); {
		if text[i] == '\\' && i+1 < len(text) && strings.ContainsRune("{}\\", rune(text[i+1])) {
			i += 2
			continue
		}
		if text[i] != '{' {
			i++
			continue
		}
		end, err := ScriptDirectiveEnd(text, i)
		if err != nil {
			return nil, err
		}
		body := strings.TrimSpace(text[i+1 : end])
		match := codeDirective.FindStringSubmatch("{" + body + "}")
		if match != nil {
			spans = append(spans, codeDirectiveSpan{i, end + 1, match})
		}
		i = end + 1
	}
	return spans, nil
}

func validateParameterKeyframes(keys []ParameterKeyframe, duration int64) error {
	last := map[string]int64{}
	for _, k := range keys {
		if (k.Kind != "移动" && k.Kind != "旋转") || k.Frame < 0 || k.Frame >= duration || k.Duration < 0 || k.Duration > duration-k.Frame || !finite(k.X) || !finite(k.Y) || !finite(k.Value) {
			return fmt.Errorf("参数关键帧无效")
		}
		if end, ok := last[k.Kind]; ok && k.Frame < end {
			return fmt.Errorf("参数关键帧重叠或未排序")
		}
		last[k.Kind] = max(k.Frame+1, k.Frame+k.Duration)
	}
	return nil
}

func parameterAt(e Effects, kind string, frame int64) (float64, float64) {
	x, y := e.X, e.Y
	if kind == "旋转" {
		x = e.Rotation
		y = 0
	}
	for _, k := range e.ParameterKeyframes {
		if k.Kind != kind {
			continue
		}
		if frame < k.Frame {
			break
		}
		a, b := k.X, k.Y
		if kind == "旋转" {
			a = k.Value
			b = 0
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
	c.Effects.ParameterKeyframes = nil
	for _, k := range old.ParameterKeyframes {
		end := k.Frame + k.Duration
		if end <= offset {
			continue
		}
		if k.Frame < offset {
			k.Frame = 0
			k.Duration = end - offset
		} else {
			k.Frame -= offset
		}
		if k.Frame >= c.Duration {
			continue
		}
		if k.Duration > c.Duration-k.Frame {
			x, y := parameterAt(old, k.Kind, offset+c.Duration)
			if k.Kind == "旋转" {
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

// The timeline owns parameter semantics; creative text remains plain data.
func ParseCodeDescription(text string, c Clip, rate Rate) (Effects, string, error) {
	fx := clone(c.Effects)
	keys := []ParameterKeyframe{}
	seen := map[string]bool{}
	spans, err := codeDirectiveSpans(text)
	if err != nil {
		return fx, "", err
	}
	for _, span := range spans {
		match := span.match
		fields := strings.Split(strings.ReplaceAll(match[2], "，", ","), ",")
		args := map[string]string{}
		for _, field := range fields {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			kv := strings.SplitN(field, ":", 2)
			if len(kv) != 2 || args[strings.TrimSpace(kv[0])] != "" {
				return fx, "", fmt.Errorf("参数格式无效或重复")
			}
			args[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
		at, e := CodeTime(args["时间"], rate)
		if e != nil {
			return fx, "", e
		}
		at -= c.At
		k := ParameterKeyframe{Kind: match[1], Frame: at}
		if v := args["持续"]; v != "" {
			if !strings.HasSuffix(v, "s") {
				return fx, "", fmt.Errorf("持续时间使用s")
			}
			n, e := strconv.ParseFloat(strings.TrimSuffix(v, "s"), 64)
			if e != nil || !finite(n) || n < 0 {
				return fx, "", fmt.Errorf("持续时间无效")
			}
			k.Duration = rate.Frames(n)
		}
		if at < 0 || at >= c.Duration || k.Duration > c.Duration-at {
			return fx, "", fmt.Errorf("参数时间超出代码条")
		}
		number := func(name, unit string) (float64, error) {
			v, ok := args[name]
			if !ok {
				return 0, fmt.Errorf("缺少参数%s", name)
			}
			if unit != "" && !strings.HasSuffix(v, unit) {
				return 0, fmt.Errorf("参数%s需要%s", name, unit)
			}
			n, e := strconv.ParseFloat(strings.TrimSuffix(v, unit), 64)
			if e != nil || !finite(n) || math.Abs(n) > 100000 {
				return 0, fmt.Errorf("参数%s无效", name)
			}
			return n, nil
		}
		allowed := map[string]bool{"时间": true, "持续": true}
		switch k.Kind {
		case "移动":
			allowed["x"] = true
			allowed["y"] = true
			k.X, e = number("x", "px")
			if e == nil {
				k.Y, e = number("y", "px")
			}
		case "旋转":
			allowed["角度"] = true
			k.Value, e = number("角度", "")
		case "音量":
			allowed["增益"] = true
			allowed["起始增益"] = true
			if args["起始增益"] != "" {
				v, err := number("起始增益", "dB")
				if err != nil || v < -96 || v > 24 {
					return fx, "", fmt.Errorf("起始增益无效")
				}
				k.StartValue = &v
			}
			k.Value, e = number("增益", "dB")
			if k.Value < -96 || k.Value > 24 {
				e = fmt.Errorf("音量范围为-96到24dB")
			}
		}
		if e != nil {
			return fx, "", e
		}
		for name := range args {
			if !allowed[name] {
				return fx, "", fmt.Errorf("未知参数%s", name)
			}
		}
		id := fmt.Sprint(k.Kind, at)
		if seen[id] {
			return fx, "", fmt.Errorf("同一参数时刻重复")
		}
		seen[id] = true
		keys = append(keys, k)
	}
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
			return fx, "", fmt.Errorf("参数动画范围重叠")
		}
		last[k.Kind] = k.Frame + k.Duration
	}
	fx.ParameterKeyframes = nil
	// Gain uses the existing envelope so direct manipulation and rendering agree.
	fx.GainKeyframes = nil
	for _, k := range keys {
		if k.Kind != "音量" {
			fx.ParameterKeyframes = append(fx.ParameterKeyframes, k)
			continue
		}
		start := float64(k.Frame) / float64(c.Duration)
		end := float64(k.Frame+k.Duration) / float64(c.Duration)
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
	var body strings.Builder
	textCursor := 0
	for _, span := range spans {
		body.WriteString(text[textCursor:span.start])
		textCursor = span.end
	}
	body.WriteString(text[textCursor:])
	return fx, strings.TrimSpace(body.String()), nil
}
