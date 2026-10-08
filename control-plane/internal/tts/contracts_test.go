package tts

import (
	"math"
	"strings"
	"testing"
)

func TestSettingsBoundsAndEndpoint(t *testing.T) {
	base := Settings{Engine: "stepfun", Endpoint: "https://api.stepfun.com/step_plan/v1", Model: "stepaudio-2.5-tts", TokenEnv: "VAC_TTS_TOKEN", Voice: "vibrant-youth", Language: "zh", Speed: 1, Volume: 1}
	if e := base.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, edit := range []func(*Settings){
		func(s *Settings) { s.Speed = .49 }, func(s *Settings) { s.Volume = 2.01 }, func(s *Settings) { s.PitchSemitones = 13 }, func(s *Settings) { s.Speed = math.NaN() },
		func(s *Settings) { s.Instruction = strings.Repeat("声", 201) }, func(s *Settings) { s.Endpoint = "https://user:secret@api.stepfun.com/v1" },
		func(s *Settings) { s.Endpoint = "http://remote.example/v1" }, func(s *Settings) { s.Endpoint += "?key=secret" }, func(s *Settings) { s.TokenEnv = "bad-name" },
	} {
		s := base
		edit(&s)
		if s.Validate() == nil {
			t.Fatalf("invalid settings accepted: %+v", s)
		}
	}
	base.Endpoint = "http://127.0.0.1:1234/step_plan/v1"
	if base.Validate() != nil || base.External() {
		t.Fatal("loopback adapter rejected")
	}
	if ValidText(strings.Repeat("文", 1001), 1000, true) || ValidText("  ", 1000, true) || ValidText("a\x00b", 1000, true) {
		t.Fatal("invalid text accepted")
	}
	if !ValidText(strings.Repeat("🎵", 1000), 1000, true) {
		t.Fatal("Unicode character limit changed")
	}
}
