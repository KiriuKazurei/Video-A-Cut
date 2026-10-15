package autoreview

import (
	"fmt"
	"math"
	"sort"
)

// ScorerVersion identifies the rules, weights and default thresholds. Any
// change to them, including a default threshold, must bump it.
const ScorerVersion = "v1"

// Component weights. They are fixed by ScorerVersion rather than configurable:
// a per-run weight would let a request tune its way past the floors.
const (
	WeightRecognition = 0.35
	WeightTTS         = 0.20
	WeightAV          = 0.30
	WeightPacing      = 0.15
)

// Names of the four scored components, in the order they are reported.
const (
	ItemRecognition = "recognition"
	ItemTTSComplete = "tts_complete"
	ItemAVAlignment = "av_alignment"
	ItemPacing      = "pacing"
)

// Threshold keys accepted in an authorization's scope.thresholds.
const (
	KeyHighScore          = "high_score_min"
	KeyMediumScore        = "medium_score_min"
	KeyRecognitionFloor   = "recognition_floor"
	KeyTTSFloor           = "tts_floor"
	KeyAVFloor            = "av_floor"
	KeyPacingFloor        = "pacing_floor"
	KeySceneConfidenceMin = "scene_confidence_min"
	KeyBriefCoverageMin   = "brief_coverage_min"
	KeyPaceMin            = "pace_min"
	KeyPaceMax            = "pace_max"
	KeyDurationTolerance  = "duration_tolerance_seconds"
	KeyWindowTolerance    = "window_tolerance_seconds"
)

// Thresholds are the cut-offs of one scoring. They are stored with every
// review, so a stored band can always be explained by the values it was
// computed with.
type Thresholds struct {
	HighScore                float64 `json:"high_score_min"`
	MediumScore              float64 `json:"medium_score_min"`
	RecognitionFloor         float64 `json:"recognition_floor"`
	TTSFloor                 float64 `json:"tts_floor"`
	AVFloor                  float64 `json:"av_floor"`
	PacingFloor              float64 `json:"pacing_floor"`
	SceneConfidenceMin       float64 `json:"scene_confidence_min"`
	BriefCoverageMin         float64 `json:"brief_coverage_min"`
	PaceMin                  float64 `json:"pace_min"`
	PaceMax                  float64 `json:"pace_max"`
	DurationToleranceSeconds float64 `json:"duration_tolerance_seconds"`
	WindowToleranceSeconds   float64 `json:"window_tolerance_seconds"`
}

// DefaultThresholds returns the defaults of ScorerVersion.
func DefaultThresholds() Thresholds {
	return Thresholds{
		HighScore: 0.85, MediumScore: 0.60,
		RecognitionFloor: 0.70, TTSFloor: 1.0, AVFloor: 0.90, PacingFloor: 0.70,
		SceneConfidenceMin: 0.60, BriefCoverageMin: 0.60,
		PaceMin: 2.0, PaceMax: 6.0,
		DurationToleranceSeconds: 0.025, WindowToleranceSeconds: 0.05,
	}
}

type thresholdField struct {
	key      string
	ptr      func(*Thresholds) *float64
	min, max float64
}

var thresholdFields = []thresholdField{
	{KeyHighScore, func(t *Thresholds) *float64 { return &t.HighScore }, 0, 1},
	{KeyMediumScore, func(t *Thresholds) *float64 { return &t.MediumScore }, 0, 1},
	{KeyRecognitionFloor, func(t *Thresholds) *float64 { return &t.RecognitionFloor }, 0, 1},
	{KeyTTSFloor, func(t *Thresholds) *float64 { return &t.TTSFloor }, 0, 1},
	{KeyAVFloor, func(t *Thresholds) *float64 { return &t.AVFloor }, 0, 1},
	{KeyPacingFloor, func(t *Thresholds) *float64 { return &t.PacingFloor }, 0, 1},
	{KeySceneConfidenceMin, func(t *Thresholds) *float64 { return &t.SceneConfidenceMin }, 0, 1},
	{KeyBriefCoverageMin, func(t *Thresholds) *float64 { return &t.BriefCoverageMin }, 0, 1},
	{KeyPaceMin, func(t *Thresholds) *float64 { return &t.PaceMin }, 0.1, 30},
	{KeyPaceMax, func(t *Thresholds) *float64 { return &t.PaceMax }, 0.1, 30},
	{KeyDurationTolerance, func(t *Thresholds) *float64 { return &t.DurationToleranceSeconds }, 0, 1},
	{KeyWindowTolerance, func(t *Thresholds) *float64 { return &t.WindowToleranceSeconds }, 0, 1},
}

// ThresholdKeys lists the accepted override keys in a stable order.
func ThresholdKeys() []string {
	keys := make([]string, 0, len(thresholdFields))
	for _, f := range thresholdFields {
		keys = append(keys, f.key)
	}
	sort.Strings(keys)
	return keys
}

// ResolveThresholds applies overrides (an authorization's scope.thresholds) to
// the defaults. An unknown key is an error, not ignored: a mistyped key would
// otherwise leave the default in force while the person believes otherwise.
func ResolveThresholds(overrides map[string]float64) (Thresholds, error) {
	t := DefaultThresholds()
	known := map[string]thresholdField{}
	for _, f := range thresholdFields {
		known[f.key] = f
	}
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f, ok := known[k]
		if !ok {
			return Thresholds{}, fmt.Errorf("autoreview: unknown threshold %q", k)
		}
		v := overrides[k]
		if math.IsNaN(v) || math.IsInf(v, 0) || v < f.min || v > f.max {
			return Thresholds{}, fmt.Errorf("autoreview: threshold %q must be between %g and %g", k, f.min, f.max)
		}
		*f.ptr(&t) = v
	}
	if t.MediumScore >= t.HighScore {
		return Thresholds{}, fmt.Errorf("autoreview: %s must be below %s", KeyMediumScore, KeyHighScore)
	}
	if t.PaceMin >= t.PaceMax {
		return Thresholds{}, fmt.Errorf("autoreview: %s must be below %s", KeyPaceMin, KeyPaceMax)
	}
	return t, nil
}
