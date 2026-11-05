package project

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

func scriptFrameLabel(frame int64, rate Rate) string {
	nominal := int64(math.Round(rate.Value()))
	if nominal < 1 {
		nominal = 30
	}
	return fmt.Sprintf("%02d:%02d:%02d:%02d", frame/nominal/3600, frame/nominal/60%60, frame/nominal%60, frame%nominal)
}
func scriptFloat(value float64) string {
	return strconv.FormatFloat(math.Round(value*1e9)/1e9, 'f', -1, 64)
}
func codeKindBlocks(c Clip, rate Rate, kind string) []string {
	out := []string{}
	e := c.Effects
	time := func(frame int64) string { return scriptFrameLabel(c.At+frame, rate) }
	duration := func(frames int64) string { return fmt.Sprintf("%df", frames) }
	if kind == "字幕" {
		for _, cue := range e.Subtitles {
			text, _ := json.Marshal(cue.Text)
			line := fmt.Sprintf("{字幕, 文本:%s, 时间:%s, 持续:%s", text, time(cue.Frame), duration(cue.Duration))
			if cue.SizePX != 0 {
				line += ", 字号:" + scriptFloat(cue.SizePX) + "px"
			}
			if cue.Font != "" {
				font, _ := json.Marshal(cue.Font)
				line += ", 字体:" + string(font)
			}
			out = append(out, line+"}")
		}
		return out
	}
	if kind == "音量" {
		if len(e.GainKeyframes) < 2 {
			value := e.GainDB
			if len(e.GainKeyframes) == 1 {
				value = e.GainKeyframes[0].DB
			}
			return []string{fmt.Sprintf("{音量, 时间:%s, 增益:%sdB}", time(0), scriptFloat(value))}
		}
		for i := 1; i < len(e.GainKeyframes); i++ {
			a, b := e.GainKeyframes[i-1], e.GainKeyframes[i]
			start, end := int64(math.Round(a.T*float64(c.Duration))), int64(math.Round(b.T*float64(c.Duration)))
			if end > start {
				out = append(out, fmt.Sprintf("{音量, 时间:%s, 起始增益:%sdB, 增益:%sdB, 持续:%s}", time(start), scriptFloat(a.DB), scriptFloat(b.DB), duration(end-start)))
			}
		}
		return out
	}
	keys := []ParameterKeyframe{}
	for _, key := range e.ParameterKeyframes {
		if key.Kind == kind {
			keys = append(keys, key)
		}
	}
	if !slices.ContainsFunc(keys, func(k ParameterKeyframe) bool { return k.Frame == 0 }) {
		var initial string
		switch kind {
		case "移动":
			if e.X != 0 || e.Y != 0 {
				initial = "x:" + scriptFloat(e.X) + "px, y:" + scriptFloat(e.Y) + "px"
			}
		case "旋转":
			if e.Rotation != 0 {
				initial = "角度:" + scriptFloat(e.Rotation)
			}
		case "缩放":
			if e.Scale != 1 {
				initial = "比例:" + scriptFloat(e.Scale*100) + "%"
			}
		case "透明度":
			if e.Opacity != nil {
				initial = "到:" + scriptFloat(*e.Opacity*100) + "%"
			}
		}
		if initial != "" {
			out = append(out, fmt.Sprintf("{%s, 时间:%s, %s}", kind, time(0), initial))
		}
	}
	for _, key := range keys {
		value := ""
		switch kind {
		case "移动":
			value = "x:" + scriptFloat(key.X) + "px, y:" + scriptFloat(key.Y) + "px"
		case "旋转":
			value = "角度:" + scriptFloat(key.Value)
		case "缩放":
			value = "比例:" + scriptFloat(key.Value*100) + "%"
		case "透明度":
			if key.StartValue != nil {
				value = "从:" + scriptFloat(*key.StartValue*100) + "%, "
			}
			value += "到:" + scriptFloat(key.Value*100) + "%"
		}
		line := fmt.Sprintf("{%s, 时间:%s, %s", kind, time(key.Frame), value)
		if key.Duration > 0 {
			line += ", 持续:" + duration(key.Duration)
		}
		out = append(out, line+"}")
	}
	return out
}
func scriptKindVersion(e Effects, kind string) string {
	keys := []ParameterKeyframe{}
	for _, key := range e.ParameterKeyframes {
		if key.Kind == kind {
			keys = append(keys, key)
		}
	}
	var value any
	switch kind {
	case "移动":
		value = []any{e.X, e.Y, keys}
	case "旋转":
		value = []any{e.Rotation, keys}
	case "缩放":
		value = []any{e.Scale, keys}
	case "透明度":
		value = []any{e.Opacity, keys}
	case "音量":
		value = []any{e.GainDB, e.GainKeyframes}
	case "字幕":
		value = e.Subtitles
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

// Only replace a time argument outside quoted text. A caption may itself
// contain text that looks exactly like a parameter field.
func rebaseScriptBlock(raw string, delta int64, rate Rate) string {
	if delta == 0 {
		return raw
	}
	body := raw[1 : len(raw)-1]
	start, quoted, escaped := 0, false, false
	for i := 0; i <= len(body); i++ {
		if i < len(body) {
			c := body[i]
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
				continue
			}
			if c != ',' && !(c == 0xef && strings.HasPrefix(body[i:], "，")) {
				continue
			}
		}
		field := body[start:i]
		key, value, ok := strings.Cut(field, ":")
		if ok && strings.TrimSpace(key) == "时间" {
			v := strings.TrimSpace(value)
			if strings.HasPrefix(v, "条内") {
				return raw
			}
			frame, err := ParseScriptTime(v, Clip{}, rate)
			if err != nil {
				return raw
			}
			prefix := ""
			if strings.HasPrefix(v, "序列") {
				prefix = "序列"
			}
			at := 1 + start + strings.Index(field, ":") + 1 + len(value) - len(strings.TrimLeftFunc(value, unicode.IsSpace))
			return raw[:at] + prefix + scriptFrameLabel(frame+delta, rate) + raw[at+len(v):]
		}
		start = i + 1
		if i < len(body) && strings.HasPrefix(body[i:], "，") {
			i += 2
			start = i + 1
		}
	}
	return raw
}

// Text is an authored projection of the effects model. Gesture edits patch only
// their directive family; prose, blank lines and other authored blocks survive.
func syncCodeScript(before Clip, after *Clip, rate Rate) {
	if before.Code == nil || before.Code.Script == nil || after.Code == nil {
		return
	}
	source := *before.Code.Script
	blocks, err := ScriptBlocks(source, "code")
	if err != nil {
		return
	}
	changed := map[string]bool{}
	for _, kind := range []string{"音量", "移动", "旋转", "缩放", "透明度", "字幕"} {
		changed[kind] = scriptKindVersion(before.Effects, kind) != scriptKindVersion(after.Effects, kind)
	}
	written := map[string]bool{}
	var out strings.Builder
	cursor := 0
	for _, block := range blocks {
		out.WriteString(source[cursor:block.Start])
		cursor = block.End
		if changed[block.Name] {
			if !written[block.Name] {
				out.WriteString(strings.Join(codeKindBlocks(*after, rate, block.Name), "\n"))
				written[block.Name] = true
			}
		} else {
			out.WriteString(rebaseScriptBlock(source[block.Start:block.End], after.At-before.Code.ScriptAt, rate))
		}
	}
	out.WriteString(source[cursor:])
	for _, kind := range []string{"音量", "移动", "旋转", "缩放", "透明度", "字幕"} {
		if changed[kind] && !written[kind] {
			text := strings.Join(codeKindBlocks(*after, rate, kind), "\n")
			if text != "" {
				if out.Len() > 0 {
					out.WriteByte('\n')
				}
				out.WriteString(text)
			}
		}
	}
	text := out.String()
	after.Code.Script = &text
	after.Code.ScriptAt = after.At
}
