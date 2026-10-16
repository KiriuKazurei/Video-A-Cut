package autoreview

// Band is the outcome of a scoring.
const (
	BandLow    = "low"
	BandMedium = "medium"
	BandHigh   = "high"
)

// Severity of a reason. A warn blocks HIGH; a fail forces LOW.
const (
	SeverityWarn = "warn"
	SeverityFail = "fail"
)

// Reason codes are a closed set. A reason never carries free text: what the
// person or a retry sees is rendered from a fixed template keyed by Code, and
// anything a model or a worker wrote (labels, narration, error strings) can
// not reach it.
const (
	CodeLowConfidence    = "low_confidence"
	CodeTTSIncomplete    = "tts_incomplete"
	CodeAVMisaligned     = "av_misaligned"
	CodePacingOutOfRange = "pacing_out_of_range"
	CodeItemBelowFloor   = "item_below_floor"
	CodeBriefLowCoverage = "brief_low_coverage"
	CodeInputMissing     = "input_missing"
	CodeNoScenes         = "no_scenes"
	CodePreviewFailed    = "preview_failed"
	CodePreviewCheck     = "preview_check_failed"
	CodeDurationMismatch = "duration_mismatch"
	CodeScoreBelowLow    = "score_below_low"
	CodeBriefUnparsable  = "brief_unparsable"
)

// ReasonCodes lists every code the scorer can emit.
var ReasonCodes = []string{
	CodeLowConfidence, CodeTTSIncomplete, CodeAVMisaligned, CodePacingOutOfRange, CodeItemBelowFloor,
	CodeBriefLowCoverage, CodeInputMissing, CodeNoScenes, CodePreviewFailed, CodePreviewCheck,
	CodeDurationMismatch, CodeScoreBelowLow, CodeBriefUnparsable,
}

// Reason explains part of a band. Item names the component for
// item_below_floor and input_missing. Value and Threshold are numbers only.
type Reason struct {
	Code       string   `json:"code"`
	Severity   string   `json:"severity"`
	Item       string   `json:"item,omitempty"`
	Value      *float64 `json:"value,omitempty"`
	Threshold  *float64 `json:"threshold,omitempty"`
	Improvable bool     `json:"improvable"`
}

func num(v float64) *float64 {
	v = round6(v)
	return &v
}
