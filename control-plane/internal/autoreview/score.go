package autoreview

import (
	"math"
	"strings"
	"unicode"
)

// Interval is a span on the timeline in seconds.
type Interval struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Scene is what the recognizer produced for one clip. A nil Confidence means
// the recognizer reported none (builtin metadata labels do not).
type Scene struct {
	Label      string   `json:"label"`
	Method     string   `json:"method,omitempty"`
	Confidence *float64 `json:"confidence"`
}

// Line is one narration line joined with its audio and subtitle. Audio is nil
// when no usable audio file exists for the line; Subtitle is nil when there is
// no cue.
type Line struct {
	ID       string    `json:"id"`
	Text     string    `json:"text"`
	Start    float64   `json:"start"`
	End      float64   `json:"end"`
	Audio    *Interval `json:"audio"`
	Subtitle *Interval `json:"subtitle"`
}

// Check is one render-report check.
type Check struct {
	Code   string `json:"code"`
	Status string `json:"status"`
}

// Preview is the finished MP4 preview of the exported revision. Status is the
// preview task's status ("succeeded" for a usable one). Media is nil when the
// preview produced none.
type Preview struct {
	Status string  `json:"status"`
	Checks []Check `json:"checks"`
	Media  *Media  `json:"media"`
}

// Media holds what the scorer needs from the probed preview file.
type Media struct {
	DurationSeconds float64 `json:"duration_seconds"`
}

// Input is everything the scorer looks at. Zero values mean "unknown", never
// "good".
type Input struct {
	Scenes             []Scene  `json:"scenes"`
	Lines              []Line   `json:"lines"`
	EDLDurationSeconds float64  `json:"edl_duration_seconds"`
	Brief              string   `json:"brief"`
	Preview            *Preview `json:"preview"`
}

// Component is one scored item. When Available is false the item could not be
// computed; it contributes zero to the score and a reason says why.
type Component struct {
	Name      string  `json:"name"`
	Score     float64 `json:"score"`
	Weight    float64 `json:"weight"`
	Available bool    `json:"available"`
}

// Facts are the counts and extremes the result was computed from, kept for the
// stored review (inputs_json). They hold numbers only, never user or model
// text.
type Facts struct {
	Scenes             int      `json:"scenes"`
	Lines              int      `json:"lines"`
	LinesWithAudio     int      `json:"lines_with_audio"`
	ConfidenceMean     *float64 `json:"confidence_mean,omitempty"`
	ConfidenceMin      *float64 `json:"confidence_min,omitempty"`
	MaxPace            *float64 `json:"max_pace,omitempty"`
	SubtitleFit        *float64 `json:"subtitle_fit,omitempty"`
	AudioFit           *float64 `json:"audio_fit,omitempty"`
	EDLDurationSeconds float64  `json:"edl_duration_seconds"`
	PreviewDuration    *float64 `json:"preview_duration_seconds,omitempty"`
	DurationDeviation  *float64 `json:"duration_deviation_seconds,omitempty"`
	BriefKeywords      int      `json:"brief_keywords"`
	BriefProvided      bool     `json:"brief_provided"`
}

// Result is the outcome of Score. Improvable is only meaningful for a LOW
// band: it is false when a retry would repeat the same environmental or
// missing-input failure, so the driver stops instead of spending more.
type Result struct {
	ScorerVersion string      `json:"scorer_version"`
	Band          string      `json:"band"`
	Score         float64     `json:"score"`
	Components    []Component `json:"components"`
	BriefCoverage *float64    `json:"brief_coverage"`
	Reasons       []Reason    `json:"reasons"`
	Improvable    bool        `json:"improvable"`
	Facts         Facts       `json:"facts"`
	Thresholds    Thresholds  `json:"thresholds"`
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

func validNumber(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func nonSpaceRunes(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

func within(inner, outer Interval, eps float64) bool {
	return validNumber(inner.Start) && validNumber(inner.End) && inner.End >= inner.Start &&
		inner.Start >= outer.Start-eps && inner.End <= outer.End+eps
}

// Score computes the band of one export. It is deterministic and never fails:
// whatever cannot be computed degrades the result instead of raising an error.
func Score(in Input, th Thresholds) Result {
	res := Result{ScorerVersion: ScorerVersion, Thresholds: th, Reasons: []Reason{}, Improvable: true}
	res.Facts.Scenes, res.Facts.Lines = len(in.Scenes), len(in.Lines)
	if validNumber(in.EDLDurationSeconds) {
		res.Facts.EDLDurationSeconds = round6(in.EDLDurationSeconds)
	}
	res.Facts.BriefProvided = strings.TrimSpace(in.Brief) != ""

	var reasons []Reason
	missing := func(item string) {
		reasons = append(reasons, Reason{Code: CodeInputMissing, Severity: SeverityWarn, Item: item, Improvable: false})
	}
	fail := func(code string, improvable bool, value, threshold *float64) {
		reasons = append(reasons, Reason{Code: code, Severity: SeverityFail, Value: value, Threshold: threshold, Improvable: improvable})
	}
	warn := func(code string, improvable bool, value, threshold *float64) {
		reasons = append(reasons, Reason{Code: code, Severity: SeverityWarn, Value: value, Threshold: threshold, Improvable: improvable})
	}

	// recognition
	recog := Component{Name: ItemRecognition, Weight: WeightRecognition}
	switch {
	case len(in.Scenes) == 0:
		fail(CodeNoScenes, true, nil, nil)
	default:
		sum, min, ok := 0.0, math.Inf(1), true
		for _, sc := range in.Scenes {
			if sc.Confidence == nil || !validNumber(*sc.Confidence) || *sc.Confidence < 0 || *sc.Confidence > 1 {
				ok = false
				break
			}
			sum += *sc.Confidence
			min = math.Min(min, *sc.Confidence)
		}
		if !ok {
			missing(ItemRecognition)
			break
		}
		recog.Score, recog.Available = sum/float64(len(in.Scenes)), true
		mean := round6(recog.Score)
		res.Facts.ConfidenceMean, res.Facts.ConfidenceMin = &mean, num(min)
		if round6(min) < th.SceneConfidenceMin {
			warn(CodeLowConfidence, true, num(min), num(th.SceneConfidenceMin))
		}
	}

	// tts_complete
	tts := Component{Name: ItemTTSComplete, Weight: WeightTTS}
	if len(in.Lines) == 0 {
		missing(ItemTTSComplete)
	} else {
		ok := 0
		for _, l := range in.Lines {
			if l.Audio != nil && validNumber(l.Audio.Start) && validNumber(l.Audio.End) && l.Audio.End > l.Audio.Start {
				ok++
			}
		}
		res.Facts.LinesWithAudio = ok
		tts.Score, tts.Available = float64(ok)/float64(len(in.Lines)), true
		if ok < len(in.Lines) {
			fail(CodeTTSIncomplete, true, num(tts.Score), num(1))
		}
	}

	// pacing
	pace := Component{Name: ItemPacing, Weight: WeightPacing}
	if len(in.Lines) == 0 {
		missing(ItemPacing)
	} else {
		inRange, maxPace := 0, 0.0
		for _, l := range in.Lines {
			w := l.End - l.Start
			if !validNumber(w) || w <= 0 {
				maxPace = math.Inf(1)
				continue
			}
			p := float64(nonSpaceRunes(l.Text)) / w
			maxPace = math.Max(maxPace, p)
			if p >= th.PaceMin && p <= th.PaceMax {
				inRange++
			}
		}
		pace.Score, pace.Available = float64(inRange)/float64(len(in.Lines)), true
		if math.IsInf(maxPace, 1) {
			warn(CodePacingOutOfRange, true, nil, num(th.PaceMax))
		} else {
			res.Facts.MaxPace = num(maxPace)
			if round6(maxPace) > th.PaceMax {
				warn(CodePacingOutOfRange, true, num(maxPace), num(th.PaceMax))
			}
		}
	}

	// av_alignment: needs a usable preview, the EDL duration and the lines.
	av := Component{Name: ItemAVAlignment, Weight: WeightAV}
	switch {
	case in.Preview == nil:
		missing(ItemAVAlignment)
	case in.Preview.Status != "succeeded":
		fail(CodePreviewFailed, false, nil, nil)
	default:
		for _, c := range in.Preview.Checks {
			if c.Status != "passed" {
				fail(CodePreviewCheck, false, nil, nil)
				break
			}
		}
		switch {
		case len(in.Preview.Checks) == 0:
			// The server forces five passed checks into every accepted report;
			// none at all means the report was not read. A missing item scores
			// 0, whatever the media says: the media was not vouched for. One
			// reason is enough, whether or not the media is also unusable.
			missing(ItemAVAlignment)
		case in.Preview.Media == nil || !validNumber(in.Preview.Media.DurationSeconds) || in.Preview.Media.DurationSeconds <= 0 ||
			!validNumber(in.EDLDurationSeconds) || in.EDLDurationSeconds <= 0 || len(in.Lines) == 0:
			missing(ItemAVAlignment)
		default:
			dev := math.Abs(in.Preview.Media.DurationSeconds - in.EDLDurationSeconds)
			res.Facts.PreviewDuration, res.Facts.DurationDeviation = num(in.Preview.Media.DurationSeconds), num(dev)
			durScore := 1.0
			if round6(dev) > th.DurationToleranceSeconds {
				durScore = 0
				fail(CodeDurationMismatch, false, num(dev), num(th.DurationToleranceSeconds))
			}
			subOK, audOK, overflow := 0, 0, false
			for _, l := range in.Lines {
				win := Interval{Start: l.Start, End: l.End}
				if l.Subtitle != nil {
					if within(*l.Subtitle, win, th.WindowToleranceSeconds) {
						subOK++
					} else {
						overflow = true
					}
				}
				if l.Audio != nil {
					if within(*l.Audio, win, th.WindowToleranceSeconds) {
						audOK++
					} else {
						overflow = true
					}
				}
			}
			n := float64(len(in.Lines))
			subFit, audFit := float64(subOK)/n, float64(audOK)/n
			res.Facts.SubtitleFit, res.Facts.AudioFit = num(subFit), num(audFit)
			av.Score, av.Available = (durScore+subFit+audFit)/3, true
			if overflow {
				warn(CodeAVMisaligned, true, num(math.Min(subFit, audFit)), num(1))
			}
		}
	}

	comps := []*Component{&recog, &tts, &av, &pace}
	score := 0.0
	for _, c := range comps {
		c.Score = round6(c.Score)
		score += c.Weight * c.Score
	}
	res.Score = round6(score)
	for _, c := range comps {
		res.Components = append(res.Components, *c)
	}

	// Floors: a weighted average must not hide one bad item.
	floors := map[string]float64{ItemRecognition: th.RecognitionFloor, ItemTTSComplete: th.TTSFloor, ItemAVAlignment: th.AVFloor, ItemPacing: th.PacingFloor}
	for _, c := range comps {
		if c.Available && c.Score < floors[c.Name] {
			reasons = append(reasons, Reason{Code: CodeItemBelowFloor, Severity: SeverityWarn, Item: c.Name,
				Value: num(c.Score), Threshold: num(floors[c.Name]), Improvable: true})
		}
	}

	// brief_coverage only ever downgrades HIGH. It is not in the score and
	// can not make a result LOW.
	if res.Facts.BriefProvided {
		labels := make([]string, 0, len(in.Scenes))
		for _, sc := range in.Scenes {
			labels = append(labels, sc.Label)
		}
		cov, kw := BriefCoverage(in.Brief, labels)
		res.Facts.BriefKeywords = kw
		if kw > 0 {
			res.BriefCoverage = num(cov)
			if round6(cov) < th.BriefCoverageMin {
				warn(CodeBriefLowCoverage, true, num(cov), num(th.BriefCoverageMin))
			}
		} else {
			// A brief was given but yields no keyword (one Han character, only
			// punctuation): the coverage gate cannot be evaluated. A gate that
			// cannot run does not pass, so HIGH is closed. A rerun cannot fix
			// the brief, hence not improvable; the result is MEDIUM and the
			// person decides.
			warn(CodeBriefUnparsable, false, nil, nil)
		}
	}

	hasFail, allImprovable, hasWarn := false, true, false
	for _, r := range reasons {
		if r.Severity == SeverityFail {
			hasFail = true
			allImprovable = allImprovable && r.Improvable
		} else {
			hasWarn = true
		}
	}
	switch {
	case hasFail:
		res.Band, res.Improvable = BandLow, allImprovable
	case res.Score < th.MediumScore:
		res.Band = BandLow
		res.Improvable = true
		for _, r := range reasons {
			if r.Code == CodeInputMissing {
				res.Improvable = false
			}
		}
		reasons = append(reasons, Reason{Code: CodeScoreBelowLow, Severity: SeverityFail, Value: num(res.Score),
			Threshold: num(th.MediumScore), Improvable: res.Improvable})
	case res.Score >= th.HighScore && !hasWarn:
		res.Band = BandHigh
	default:
		res.Band = BandMedium
	}
	if res.Band != BandLow {
		res.Improvable = false
	}
	res.Reasons = append(res.Reasons, reasons...)
	return res
}

// BriefCoverage returns the share of the brief's keywords that occur in the
// scene labels, and the keyword count. Only labels are searched, never the
// narration: the narration is written by a model that has read the brief and
// would repeat it. Even a full match is no credit; see Score. A count of zero
// means the brief produced no keyword, so there is nothing to measure.
func BriefCoverage(brief string, labels []string) (float64, int) {
	keywords := briefKeywords(brief)
	if len(keywords) == 0 {
		return 0, 0
	}
	hay := strings.ToLower(strings.Join(labels, "\n"))
	hits := 0
	for _, k := range keywords {
		if strings.Contains(hay, k) {
			hits++
		}
	}
	return float64(hits) / float64(len(keywords)), len(keywords)
}

const maxBriefKeywords = 32

// briefKeywords splits a brief into at most maxBriefKeywords lowercase
// keywords in order of first appearance: runs of letters or digits of at least
// two characters, and overlapping character pairs inside runs of Han
// characters (which have no spaces to split on). The order is fixed, so the
// result is deterministic.
func briefKeywords(brief string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(k string) {
		if len(out) < maxBriefKeywords && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	var word, han []rune
	flushWord := func() {
		if len(word) >= 2 {
			add(strings.ToLower(string(word)))
		}
		word = word[:0]
	}
	flushHan := func() {
		for i := 0; i+1 < len(han); i++ {
			add(string(han[i : i+2]))
		}
		han = han[:0]
	}
	for _, r := range brief {
		switch {
		case unicode.Is(unicode.Han, r):
			flushWord()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushHan()
			word = append(word, r)
		default:
			flushWord()
			flushHan()
		}
	}
	flushWord()
	flushHan()
	return out
}
