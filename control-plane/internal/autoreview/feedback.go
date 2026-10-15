package autoreview

import (
	"fmt"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// FeedbackTemplateVersion is the version of the texts below. It moves with
// ScorerVersion.
const FeedbackTemplateVersion = ScorerVersion

// feedbackTemplates is the only source of retry feedback text. Placeholders
// {value} and {threshold} are replaced by numbers. Nothing a model produced
// (labels, narration) and nothing a worker said (errors, logs) is ever put in
// here, which keeps a retry from being a channel for prompt injection.
var feedbackTemplates = map[string]string{
	"low_confidence":        "上一轮有场景识别置信度偏低（最低 {value}，要求不低于 {threshold}），请更谨慎取舍，只保留画面证据明确的内容。",
	"tts_incomplete":        "上一轮有旁白没有生成出可用的语音（完成度 {value}），请缩短过长的旁白句子后重试。",
	"av_misaligned":         "上一轮有字幕或语音超出了旁白时间窗（对齐度 {value}），请让每条旁白在其时间窗内读完。",
	"pacing_out_of_range":   "上一轮有旁白语速超出范围（最快 {value} 字/秒，上限 {threshold} 字/秒），请把旁白写得更简短。",
	"item_below_floor":      "上一轮有评分项低于下限（{value}，下限 {threshold}），请整体更稳妥地取舍。",
	"brief_low_coverage":    "上一轮场景标签与用户需求的重合度偏低（{value}，要求不低于 {threshold}），请更贴近用户需求选择片段。",
	"scene_criteria_failed": "上一轮有场景不满足自动确认的条件，请只保留证据清晰、标签单行且互不重复的场景。",
}

// FeedbackItems turns the reasons of a LOW result into the structured hints
// the next round's task input carries: a code from the fixed set with optional
// numbers, once per code, in reason order. Reasons without a feedback code
// (environmental failures, missing inputs) produce nothing.
func FeedbackItems(reasons []Reason) []model.AutoFeedbackItem {
	var out []model.AutoFeedbackItem
	seen := map[string]bool{}
	for _, r := range reasons {
		if !model.AutoFeedbackCodes[r.Code] || seen[r.Code] {
			continue
		}
		seen[r.Code] = true
		out = append(out, model.AutoFeedbackItem{Code: r.Code, Value: r.Value, Threshold: r.Threshold})
	}
	return out
}

// RenderFeedback renders one item from its fixed template. It reports false
// for a code that has no template.
func RenderFeedback(item model.AutoFeedbackItem) (string, bool) {
	tpl, ok := feedbackTemplates[item.Code]
	if !ok || !model.AutoFeedbackCodes[item.Code] {
		return "", false
	}
	format := func(v *float64) string {
		if v == nil {
			return "—"
		}
		return fmt.Sprintf("%.2f", *v)
	}
	r := strings.NewReplacer("{value}", format(item.Value), "{threshold}", format(item.Threshold))
	return r.Replace(tpl), true
}

// RenderFeedbackAll renders every item that has a template, in order.
func RenderFeedbackAll(items []model.AutoFeedbackItem) []string {
	var out []string
	for _, it := range items {
		if text, ok := RenderFeedback(it); ok {
			out = append(out, text)
		}
	}
	return out
}
