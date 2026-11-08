package project

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/shotcraft"
)

// The same registry is statically imported by the workbench. Units, limits,
// completion and context validation share this source rather than new lists.
//
//go:embed script_directives.json
var scriptDirectiveRegistry []byte

var scriptDecimal = regexp.MustCompile(`^[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?$`)
var scriptDurationDecimal = regexp.MustCompile(`^\d+(?:\.\d+)?$`)

type ScriptParameterSpec struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Unit     string   `json:"unit,omitempty"`
	Required bool     `json:"required,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
}
type ScriptDirectiveSpec struct {
	Name       string                `json:"name"`
	Label      string                `json:"label"`
	Contexts   []string              `json:"contexts"`
	Mode       string                `json:"mode"`
	Parameters []ScriptParameterSpec `json:"parameters"`
}

var scriptSpecs = func() map[string]ScriptDirectiveSpec {
	var registry struct {
		Directives []ScriptDirectiveSpec `json:"directives"`
	}
	if err := json.Unmarshal(scriptDirectiveRegistry, &registry); err != nil {
		panic(err)
	}
	out := map[string]ScriptDirectiveSpec{}
	for _, spec := range registry.Directives {
		out[spec.Name] = spec
	}
	return out
}()

func ScriptDirective(name string) (ScriptDirectiveSpec, bool) {
	spec, ok := scriptSpecs[name]
	return spec, ok
}

type ScriptBlock struct {
	Start, End int
	Name       string
	Args       map[string]string
	Legacy     bool
}

func ScriptError(source string, offset int, message string) error {
	before := source[:min(max(offset, 0), len(source))]
	line := strings.Count(before, "\n") + 1
	column := utf8.RuneCountInString(before[strings.LastIndex(before, "\n")+1:]) + 1
	return fmt.Errorf("第%d行第%d列：%s", line, column, message)
}

// Split delimiters only outside JSON strings; commas/braces inside captions
// are text, including fullwidth punctuation and escaped quotation marks.
func scriptFields(body string) []string {
	parts := []string{}
	start, quoted, escaped := 0, false, false
	for i, r := range body {
		if quoted {
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == '"' {
				quoted = false
			}
			continue
		}
		if r == '"' {
			quoted = true
		} else if r == ',' || r == '，' {
			parts = append(parts, strings.TrimSpace(body[start:i]))
			start = i + utf8.RuneLen(r)
		}
	}
	parts = append(parts, strings.TrimSpace(body[start:]))
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func ParseScriptBlock(body, context string) (ScriptBlock, error) {
	block := ScriptBlock{Args: map[string]string{}}
	if reference := strings.TrimSpace(body); strings.HasPrefix(reference, "#") {
		id := strings.TrimSpace(reference[1:])
		if _, _, ok := shotcraft.Lookup(id); !ok {
			return block, fmt.Errorf("未知镜头卡或变体 %q", id)
		}
		block.Name = "#"
		block.Args["镜头卡"] = id
		return block, nil
	}
	if reference := strings.TrimSpace(body); strings.HasPrefix(reference, "@") {
		name := strings.TrimSpace(reference[1:])
		if name == "" || len(name) > 512 || strings.ContainsAny(name, "\r\n") {
			return block, fmt.Errorf("引用名称不能为空或跨行")
		}
		block.Name = "@"
		block.Args["引用"] = name
		return block, nil
	}
	fields := scriptFields(strings.TrimSpace(body))
	name := fields[0]
	if strings.HasPrefix(name, "\"") {
		block.Name = "字幕"
		block.Legacy = true
		block.Args["文本"] = name
	} else {
		block.Name = name
	}
	spec, ok := ScriptDirective(block.Name)
	if !ok {
		return block, fmt.Errorf("未知指令%s", block.Name)
	}
	if !slices.Contains(spec.Contexts, context) {
		return block, fmt.Errorf("%s仅适用于代码条工作台", block.Name)
	}
	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(field, ":")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return block, fmt.Errorf("%s参数需要名称:值", block.Name)
		}
		if _, exists := block.Args[key]; exists {
			return block, fmt.Errorf("%s参数%s重复", block.Name, key)
		}
		block.Args[key] = value
	}
	for key := range block.Args {
		if !slices.ContainsFunc(spec.Parameters, func(p ScriptParameterSpec) bool { return p.Name == key }) {
			return block, fmt.Errorf("%s不支持参数%s", block.Name, key)
		}
	}
	for _, p := range spec.Parameters {
		v, exists := block.Args[p.Name]
		if !exists {
			if p.Required {
				return block, fmt.Errorf("%s缺少参数%s", block.Name, p.Name)
			}
			continue
		}
		if p.Type == "number" {
			if _, err := ScriptNumber(block.Name, p.Name, v); err != nil {
				return block, err
			}
		}
		if p.Type == "string" {
			var text string
			if json.Unmarshal([]byte(v), &text) != nil || strings.TrimSpace(text) == "" {
				return block, fmt.Errorf("%s需要双引号包围的文字", p.Name)
			}
		}
	}
	return block, nil
}
func ScriptNumber(command, name, value string) (float64, error) {
	spec, _ := ScriptDirective(command)
	for _, p := range spec.Parameters {
		if p.Name == name {
			if p.Unit != "" && !strings.HasSuffix(value, p.Unit) {
				return 0, fmt.Errorf("%s.%s需要%s单位", command, name, p.Unit)
			}
			raw := strings.TrimSuffix(value, p.Unit)
			n, err := strconv.ParseFloat(raw, 64)
			if !scriptDecimal.MatchString(raw) || err != nil || !finite(n) || p.Min != nil && n < *p.Min || p.Max != nil && n > *p.Max {
				return 0, fmt.Errorf("%s.%s超出有效范围", command, name)
			}
			return n, nil
		}
	}
	return 0, fmt.Errorf("未知参数%s", name)
}
func ScriptBlocks(text, context string) ([]ScriptBlock, error) {
	out := []ScriptBlock{}
	for i := 0; i < len(text); {
		if text[i] == '\\' && i+1 < len(text) && strings.ContainsRune("{}\\", rune(text[i+1])) {
			i += 2
			continue
		}
		if text[i] == '}' {
			return nil, ScriptError(text, i, "多余右花括号，普通花括号请转义")
		}
		if text[i] != '{' {
			i++
			continue
		}
		end, err := ScriptDirectiveEnd(text, i)
		if err != nil {
			return nil, ScriptError(text, i, err.Error())
		}
		block, err := ParseScriptBlock(text[i+1:end], context)
		if err != nil {
			return nil, ScriptError(text, i, err.Error())
		}
		block.Start, block.End = i, end+1
		out = append(out, block)
		i = end + 1
	}
	return out, nil
}
