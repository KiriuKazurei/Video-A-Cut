package autoreview

import (
	"math"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestSceneConfidenceMinBoundary(t *testing.T) {
	score := func(c float64) Result {
		in := perfectInput()
		in.Scenes[0].Confidence = fp(c)
		return Score(in, DefaultThresholds())
	}
	if r := score(0.60); hasCode(r, CodeLowConfidence) || r.Band != BandHigh {
		t.Fatalf("exactly scene_confidence_min must pass: %v %s", codes(r), r.Band)
	}
	if r := score(0.599999); !hasCode(r, CodeLowConfidence) || r.Band == BandHigh {
		t.Fatalf("just below scene_confidence_min must warn: %v %s", codes(r), r.Band)
	}
	if r := score(0.61); hasCode(r, CodeLowConfidence) {
		t.Fatal("above the minimum warned")
	}
}

func TestWindowToleranceEdges(t *testing.T) {
	th := DefaultThresholds()
	th.WindowToleranceSeconds = 0.5 // binary-exact values: the edge is hit exactly
	check := func(name string, edit func(*Line), wantMisaligned bool) {
		t.Helper()
		in := perfectInput()
		in.Lines[0].Start, in.Lines[0].End = 1, 2
		in.Lines[0].Audio, in.Lines[0].Subtitle = &Interval{1, 2}, &Interval{1, 2}
		edit(&in.Lines[0])
		r := Score(in, th)
		if got := hasCode(r, CodeAVMisaligned); got != wantMisaligned {
			t.Fatalf("%s: av_misaligned = %v, want %v (%v)", name, got, wantMisaligned, codes(r))
		}
	}
	check("audio at both edges", func(l *Line) { l.Audio = &Interval{0.5, 2.5} }, false)
	check("audio start past tolerance", func(l *Line) { l.Audio = &Interval{0.4999, 2.0} }, true)
	check("audio end past tolerance", func(l *Line) { l.Audio = &Interval{1.0, 2.5001} }, true)
	check("subtitle at both edges", func(l *Line) { l.Subtitle = &Interval{0.5, 2.5} }, false)
	check("subtitle start past tolerance", func(l *Line) { l.Subtitle = &Interval{0.4999, 2.0} }, true)
	check("subtitle end past tolerance", func(l *Line) { l.Subtitle = &Interval{1.0, 2.5001} }, true)
	check("inverted interval", func(l *Line) { l.Subtitle = &Interval{2.0, 1.0} }, true)
}

func TestDurationToleranceEdge(t *testing.T) {
	th := DefaultThresholds()
	mismatch := func(preview float64) bool {
		in := perfectInput()
		in.Preview.Media.DurationSeconds = preview
		return hasCode(Score(in, th), CodeDurationMismatch)
	}
	if mismatch(30.025) || mismatch(29.975) {
		t.Fatal("a deviation of exactly the tolerance must pass")
	}
	if !mismatch(30.026) || !mismatch(29.97) {
		t.Fatal("a deviation past the tolerance must fail")
	}
}

func TestZeroLengthAndInvertedAudioIsNotAudio(t *testing.T) {
	for name, iv := range map[string]*Interval{"zero": {1, 1}, "inverted": {2, 1}, "nan": {math.NaN(), 1}, "inf": {0, math.Inf(1)}} {
		in := perfectInput()
		in.Lines[0].Audio = iv
		r := Score(in, DefaultThresholds())
		if !hasCode(r, CodeTTSIncomplete) || component(r, ItemTTSComplete).Score != 0.9 || r.Facts.LinesWithAudio != 9 || r.Band != BandLow {
			t.Fatalf("%s: %v tts=%v lines=%d band=%s", name, codes(r), component(r, ItemTTSComplete).Score, r.Facts.LinesWithAudio, r.Band)
		}
	}
}

func TestOutOfRangeConfidenceIsMissingNotScored(t *testing.T) {
	for name, c := range map[string]float64{"negative": -0.1, "above one": 1.1, "nan": math.NaN(), "inf": math.Inf(1)} {
		in := perfectInput()
		in.Scenes[0].Confidence = fp(c)
		r := Score(in, DefaultThresholds())
		rec := component(r, ItemRecognition)
		if rec.Available || rec.Score != 0 || !hasCode(r, CodeInputMissing) || r.Band == BandHigh {
			t.Fatalf("%s: recognition %+v band %s reasons %v", name, rec, r.Band, codes(r))
		}
	}
	// The edges themselves are valid.
	for _, c := range []float64{0, 1} {
		in := perfectInput()
		in.Scenes[0].Confidence = fp(c)
		if !component(Score(in, DefaultThresholds()), ItemRecognition).Available {
			t.Fatalf("confidence %v rejected", c)
		}
	}
}

func TestPacingGuardsAgainstNonPositiveWindows(t *testing.T) {
	for name, end := range map[string]float64{"zero": 0.2, "negative": 0.1} {
		in := perfectInput()
		in.Lines[0].Start, in.Lines[0].End = 0.2, end
		r := Score(in, DefaultThresholds())
		if !hasCode(r, CodePacingOutOfRange) || r.Facts.MaxPace != nil || component(r, ItemPacing).Score != 0.9 {
			t.Fatalf("%s: %v maxpace=%v pacing=%v", name, codes(r), r.Facts.MaxPace, component(r, ItemPacing).Score)
		}
		for _, x := range r.Reasons {
			if x.Code == CodePacingOutOfRange && x.Value != nil {
				t.Fatalf("%s: an infinite pace was reported as a number: %v", name, *x.Value)
			}
		}
	}
}

func TestPaceRangeEdges(t *testing.T) {
	th := DefaultThresholds()
	pace := func(text string, window float64) float64 {
		in := perfectInput()
		in.Lines[0].Text, in.Lines[0].Start, in.Lines[0].End = text, 0.2, 0.2+window
		return component(Score(in, th), ItemPacing).Score
	}
	if got := pace("一二三四五六", 1); got != 1 { // 6 chars/s = pace_max
		t.Fatalf("pace_max edge scored %v", got)
	}
	if got := pace("一二三四五六七", 1); got != 0.9 {
		t.Fatalf("above pace_max scored %v", got)
	}
	if got := pace("一二", 1); got != 1 { // 2 chars/s = pace_min
		t.Fatalf("pace_min edge scored %v", got)
	}
	if got := pace("一", 1); got != 0.9 {
		t.Fatalf("below pace_min scored %v", got)
	}
}

func TestFeedbackItemsDedupeKeepOrderAndDropNonHints(t *testing.T) {
	v := fp(0.5)
	items := FeedbackItems([]Reason{
		{Code: CodeLowConfidence, Value: v},
		{Code: CodeInputMissing, Item: ItemAVAlignment},
		{Code: CodeTTSIncomplete},
		{Code: CodeLowConfidence, Value: fp(0.1)},
		{Code: CodePreviewFailed},
		{Code: CodeBriefUnparsable},
		{Code: CodeTTSIncomplete},
	})
	if len(items) != 2 || items[0].Code != CodeLowConfidence || items[1].Code != CodeTTSIncomplete {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Value == nil || *items[0].Value != 0.5 {
		t.Fatalf("the first occurrence must win: %+v", items[0])
	}
	if got := RenderFeedbackAll(items); len(got) != 2 {
		t.Fatalf("texts = %v", got)
	}
	if _, ok := RenderFeedback(model.AutoFeedbackItem{Code: "made_up"}); ok {
		t.Fatal("an unknown code rendered")
	}
}
