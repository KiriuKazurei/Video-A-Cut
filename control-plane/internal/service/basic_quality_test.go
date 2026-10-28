package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBasicQualityRejectsStaleAndContradictoryResults(t *testing.T) {
	sha := strings.Repeat("a", 64)
	cases := []struct {
		name, raw string
		valid     bool
	}{
		{"matching", `{"sha256":"` + sha + `","status":"passed","audio_status":"skipped","video_status":"passed","issues":[]}`, true},
		{"other film", `{"sha256":"` + strings.Repeat("b", 64) + `","status":"passed","issues":[]}`, false},
		{"passed with issue", `{"sha256":"` + sha + `","status":"passed","issues":[{"kind":"black","start":1,"end":2}]}`, false},
		{"failure with no issue", `{"sha256":"` + sha + `","status":"failed","audio_status":"skipped","video_status":"failed","issues":[]}`, false},
		{"invalid interval", `{"sha256":"` + sha + `","status":"failed","audio_status":"failed","video_status":"passed","issues":[{"kind":"silence","start":2,"end":1}]}`, false},
		{"missing issues", `{"sha256":"` + sha + `","status":"passed"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validBasicQuality(json.RawMessage(c.raw), sha); got != c.valid {
				t.Fatalf("valid = %v, want %v", got, c.valid)
			}
		})
	}
}
