// Package scriptsyntax parses the bounded script language. Braces describe
// editing data, never executable Java/C#/JavaScript expressions.
package scriptsyntax

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const Version = 1

type Subtitle struct {
	Text   string  `json:"text"`
	SizePX float64 `json:"size_px,omitempty"`
	Font   string  `json:"font,omitempty"`
}

type Paragraph struct {
	Index       int       `json:"index"`
	Source      string    `json:"source"`
	Instruction string    `json:"instruction"`
	Video       string    `json:"video,omitempty"`
	Subtitle    *Subtitle `json:"subtitle,omitempty"`
	Effect      string    `json:"effect,omitempty"`
}

type Diagnostic struct {
	Paragraph int    `json:"paragraph"`
	Column    int    `json:"column"`
	Message   string `json:"message"`
}

type Document struct {
	Version     int          `json:"version"`
	Paragraphs  []Paragraph  `json:"paragraphs"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

var blankLine = regexp.MustCompile(`\n[\t ]*\n+`)
var sizePattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)px$`)

// Parse retains diagnostics and original paragraphs so the editor can display
// invalid input without silently treating unknown instructions as ordinary text.
func Parse(source string) Document {
	doc := Document{Version: Version, Paragraphs: []Paragraph{}, Diagnostics: []Diagnostic{}}
	if !utf8.ValidString(source) || len(source) > 65536 {
		doc.Diagnostics = append(doc.Diagnostics, Diagnostic{Message: "脚本需为有效文字，最多 64 KiB"})
		return doc
	}
	source = strings.TrimSpace(strings.ReplaceAll(source, "\r\n", "\n"))
	if source == "" {
		return doc
	}
	chunks := blankLine.Split(source, -1)
	if len(chunks) > 100 {
		doc.Diagnostics = append(doc.Diagnostics, Diagnostic{Message: "最多 100 个段落"})
		return doc
	}
	for index, chunk := range chunks {
		p := Paragraph{Index: index, Source: strings.TrimSpace(chunk)}
		var text strings.Builder
		addError := func(offset int, message string) {
			doc.Diagnostics = append(doc.Diagnostics, Diagnostic{Paragraph: index, Column: utf8.RuneCountInString(p.Source[:offset]) + 1, Message: message})
		}
		for i := 0; i < len(p.Source); {
			if p.Source[i] == '\\' && i+1 < len(p.Source) && strings.ContainsRune("{}\\", rune(p.Source[i+1])) {
				text.WriteByte(p.Source[i+1])
				i += 2
				continue
			}
			if p.Source[i] == '}' {
				addError(i, "多余的右花括号；普通花括号请写成 \\}")
				i++
				continue
			}
			if p.Source[i] != '{' {
				text.WriteByte(p.Source[i])
				i++
				continue
			}
			start := i
			end, err := directiveEnd(p.Source, start)
			if err != nil {
				addError(start, err.Error())
				text.WriteString(p.Source[start:])
				break
			}
			body := strings.TrimSpace(p.Source[start+1 : end])
			switch {
			case strings.HasPrefix(body, "@"):
				name := strings.TrimSpace(strings.TrimPrefix(body, "@"))
				if name == "" || len(name) > 512 || strings.ContainsAny(name, "\r\n") {
					addError(start, "视频名称不能为空或跨行")
				} else if p.Video != "" {
					addError(start, "每段只能强制指定一个视频")
				} else {
					p.Video = name
				}
			case strings.HasPrefix(body, `"`):
				sub, err := parseSubtitle(body)
				if err != nil {
					addError(start, err.Error())
				} else if p.Subtitle != nil {
					addError(start, "每段只能指定一份强制字幕")
				} else {
					p.Subtitle = &sub
				}
			case body == "黑屏" || body == "白屏" || body == "交叉溶解":
				if p.Effect != "" {
					addError(start, "每段只能指定一个画面效果")
				} else {
					p.Effect = body
				}
			default:
				addError(start, "未知语法：首版支持 @视频名称、引号字幕、黑屏、白屏、交叉溶解")
			}
			i = end + 1
		}
		p.Instruction = strings.TrimSpace(text.String())
		doc.Paragraphs = append(doc.Paragraphs, p)
	}
	return doc
}

func directiveEnd(source string, start int) (int, error) {
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

func parseSubtitle(body string) (Subtitle, error) {
	var sub Subtitle
	parts := []string{}
	start, quoted, escaped := 0, false, false
	for i := 0; i < len(body); i++ {
		c := body[i]
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
		} else if c == '"' {
			quoted = true
		} else if c == ',' {
			parts = append(parts, strings.TrimSpace(body[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, strings.TrimSpace(body[start:]))
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 || json.Unmarshal([]byte(parts[0]), &sub.Text) != nil || strings.TrimSpace(sub.Text) == "" || utf8.RuneCountInString(sub.Text) > 2000 {
		return sub, fmt.Errorf("字幕正文需使用双引号，且为 1～2000 个字符")
	}
	seen := map[string]bool{}
	for _, part := range parts[1:] {
		key, value, ok := strings.Cut(part, ":")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || seen[key] {
			return sub, fmt.Errorf("字幕参数应为 名称:值，且不能重复")
		}
		seen[key] = true
		switch key {
		case "字号":
			m := sizePattern.FindStringSubmatch(value)
			if m == nil {
				return sub, fmt.Errorf("字号格式例如 24px")
			}
			sub.SizePX, _ = strconv.ParseFloat(m[1], 64)
			if math.IsNaN(sub.SizePX) || math.IsInf(sub.SizePX, 0) || sub.SizePX < 1 || sub.SizePX > 512 {
				return sub, fmt.Errorf("字号需为 1～512px")
			}
		case "字体":
			if json.Unmarshal([]byte(value), &sub.Font) != nil || strings.TrimSpace(sub.Font) == "" || utf8.RuneCountInString(sub.Font) > 100 || strings.ContainsAny(sub.Font, "\r\n\x00") {
				return sub, fmt.Errorf("字体需为双引号包围的系统字体名称")
			}
		default:
			return sub, fmt.Errorf("未知字幕参数；首版仅支持 字号、字体")
		}
	}
	return sub, nil
}
