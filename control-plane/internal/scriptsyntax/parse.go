// Package scriptsyntax parses the bounded script language. Braces describe
// editing data, never executable Java/C#/JavaScript expressions.
package scriptsyntax

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"regexp"
	"strings"
	"unicode/utf8"
)

const Version = 3

type Subtitle struct {
	Text   string  `json:"text"`
	SizePX float64 `json:"size_px,omitempty"`
	Font   string  `json:"font,omitempty"`
}

type Paragraph struct {
	SubtitleCues       []project.SubtitleCue        `json:"subtitle_cues,omitempty"`
	VideoID            string                       `json:"video_id,omitempty"`
	ResolvedReferences []project.ReferenceCandidate `json:"resolved_references,omitempty"`
	References         []string                     `json:"references,omitempty"`
	Index              int                          `json:"index"`
	Source             string                       `json:"source"`
	Instruction        string                       `json:"instruction"`
	Video              string                       `json:"video,omitempty"`
	Subtitle           *Subtitle                    `json:"subtitle,omitempty"`
	Effect             string                       `json:"effect,omitempty"`
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
			block, syntaxErr := project.ParseScriptBlock(body, "paragraph")
			if syntaxErr != nil {
				addError(start, syntaxErr.Error())
				i = end + 1
				continue
			}
			switch {
			case block.Name == "字幕" && (!block.Legacy || block.Args["时间"] != "" || block.Args["持续"] != ""):
				cue, err := project.ParseSubtitleCue(block, project.Clip{Duration: 30 * 60 * 60 * 24}, project.Rate{Num: 30, Den: 1})
				if err != nil {
					addError(start, err.Error())
				} else {
					if block.Args["持续"] == "" {
						cue.Duration = 0
					}
					if value := block.Args["时间"]; value != "" && !strings.HasPrefix(value, "条内") {
						cue.TimeBase = "sequence"
					}
					p.SubtitleCues = append(p.SubtitleCues, cue)
				}
			case strings.HasPrefix(body, "@"):
				name := strings.TrimSpace(strings.TrimPrefix(body, "@"))
				if name == "" || len(name) > 512 || strings.ContainsAny(name, "\r\n") {
					addError(start, "视频名称不能为空或跨行")
				} else {
					p.References = append(p.References, name)
					if p.Video == "" {
						p.Video = name
					}
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
	return project.ScriptDirectiveEnd(source, start)
}

func parseSubtitle(body string) (Subtitle, error) {
	block, err := project.ParseScriptBlock(body, "paragraph")
	if err != nil {
		return Subtitle{}, err
	}
	cue, err := project.ParseSubtitleCue(block, project.Clip{Duration: 1}, project.Rate{Num: 30, Den: 1})
	return Subtitle{Text: cue.Text, SizePX: cue.SizePX, Font: cue.Font}, err
}
