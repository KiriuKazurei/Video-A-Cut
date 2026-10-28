package service

import "encoding/json"

func validBasicQuality(raw json.RawMessage, sha string) bool {
	var q struct {
		SHA256 string `json:"sha256"`
		Status string `json:"status"`
		Audio  string `json:"audio_status"`
		Video  string `json:"video_status"`
		Issues []struct {
			Kind  string  `json:"kind"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"issues"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &q) != nil || len(sha) != 64 || q.SHA256 != sha || q.Issues == nil || (q.Status != "passed" && q.Status != "failed" && q.Status != "not_checked") {
		return false
	}
	validItem := func(v string) bool { return v == "passed" || v == "failed" || v == "skipped" || v == "not_checked" }
	if !validItem(q.Audio) || !validItem(q.Video) {
		return false
	}
	if q.Status == "passed" && (q.Audio == "failed" || q.Audio == "not_checked" || q.Video == "failed" || q.Video == "not_checked" || (q.Audio == "skipped" && q.Video == "skipped")) {
		return false
	}
	if q.Status == "failed" && q.Audio != "failed" && q.Video != "failed" {
		return false
	}
	if (q.Status == "passed" && len(q.Issues) != 0) || (q.Status == "failed" && len(q.Issues) == 0) {
		return false
	}
	for _, i := range q.Issues {
		if (i.Kind != "silence" && i.Kind != "black") || i.Start < 0 || i.End <= i.Start {
			return false
		}
	}
	return true
}
func uncheckedBasicQuality(sha string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"sha256": sha, "status": "not_checked", "audio_status": "not_checked", "video_status": "not_checked", "issues": []any{}})
	return b
}
