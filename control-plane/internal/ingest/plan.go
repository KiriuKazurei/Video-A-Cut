package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// Task types and the role that executes them.
const (
	TaskMediaProbe   = "media_probe"
	TaskSegment      = "segment"
	TaskMediaPrepare = "media_prepare"
	RoleIngester     = "ingester"
)

// IsTaskType reports whether t is an ingest task type.
func IsTaskType(t string) bool {
	return t == TaskMediaProbe || t == TaskSegment || t == TaskMediaPrepare
}

// Run states and stages.
const (
	StateQueued         = "queued"
	StateProcessing     = "processing"
	StateAwaitingReview = "awaiting_review"
	StateReady          = "ready"
	StateFailed         = "failed"
	StateCancelled      = "cancelled"

	StageProbe         = "probe"
	StageSegment       = "segment"
	StageSegmentReview = "segment_review"
	StagePrepare       = "prepare"
	StageReady         = "ready"
)

var segmentID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func strictDecode(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data")
	}
	return nil
}

func argErr(format string, args ...any) error {
	return fmt.Errorf(format+": %w", append(args, model.ErrArgument)...)
}

func shortText(s string, max int) bool {
	if utf8.RuneCountInString(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// ProbeStream is one sanitized stream description. Unknown values stay
// empty or "unknown"; nothing here may carry a filesystem path.
type ProbeStream struct {
	Index          int            `json:"index"`
	Type           string         `json:"type"`
	Codec          string         `json:"codec"`
	Profile        string         `json:"profile"`
	Disposition    map[string]int `json:"disposition"`
	Language       string         `json:"language"`
	Title          string         `json:"title"`
	TimeBase       string         `json:"time_base"`
	StartPTS       *int64         `json:"start_pts"`
	StartUs        *int64         `json:"start_us"`
	DurationUs     *int64         `json:"duration_us"`
	Width          int            `json:"width"`
	Height         int            `json:"height"`
	Rotation       int            `json:"rotation"`
	PixFmt         string         `json:"pix_fmt"`
	ColorTransfer  string         `json:"color_transfer"`
	ColorPrimaries string         `json:"color_primaries"`
	AvgFrameRate   string         `json:"avg_frame_rate"`
	RFrameRate     string         `json:"r_frame_rate"`
	FrameRateMode  string         `json:"frame_rate_mode"`
	SampleRate     int            `json:"sample_rate"`
	Channels       int            `json:"channels"`
	ChannelLayout  string         `json:"channel_layout"`
}

// Probe is the media_probe result stored on the run.
type Probe struct {
	SchemaVersion int           `json:"schema_version"`
	ProbeVersion  string        `json:"probe_version"`
	Container     string        `json:"container"`
	SizeBytes     int64         `json:"size_bytes"`
	DurationUs    int64         `json:"duration_us"`
	Streams       []ProbeStream `json:"streams"`
	Limitations   []string      `json:"limitations"`
}

// ParseProbe strictly decodes and bounds a probe document.
func ParseProbe(raw []byte, p Policy, sourceSize int64) (Probe, error) {
	var pr Probe
	if len(raw) > 1<<20 || strictDecode(raw, &pr) != nil {
		return Probe{}, argErr("probe.json is not a valid probe document")
	}
	if pr.SchemaVersion != 1 || !shortText(pr.ProbeVersion, 64) || pr.ProbeVersion == "" || !shortText(pr.Container, 100) {
		return Probe{}, argErr("probe.json identity is invalid")
	}
	if pr.SizeBytes != sourceSize {
		return Probe{}, argErr("probe size does not match the registered source")
	}
	if pr.DurationUs <= 0 || pr.DurationUs > p.MaxSourceDurationUs {
		return Probe{}, argErr("recording duration is unknown or exceeds the policy limit")
	}
	if len(pr.Streams) == 0 || len(pr.Streams) > 64 || len(pr.Limitations) > 32 {
		return Probe{}, argErr("probe stream list is empty or too long")
	}
	seen := map[int]bool{}
	videos := 0
	for _, st := range pr.Streams {
		if st.Index < 0 || seen[st.Index] {
			return Probe{}, argErr("probe stream indexes must be unique")
		}
		seen[st.Index] = true
		for _, text := range []string{st.Type, st.Codec, st.Profile, st.Language, st.TimeBase, st.PixFmt, st.ColorTransfer, st.ColorPrimaries, st.AvgFrameRate, st.RFrameRate, st.FrameRateMode, st.ChannelLayout} {
			if !shortText(text, 64) {
				return Probe{}, argErr("probe stream field is too long")
			}
		}
		if !shortText(st.Title, 200) || len(st.Disposition) > 32 {
			return Probe{}, argErr("probe stream title or disposition is too long")
		}
		switch st.Type {
		case "video":
			videos++
			if st.Width <= 0 || st.Height <= 0 {
				return Probe{}, argErr("video stream %d has no dimensions", st.Index)
			}
		case "audio":
			if st.SampleRate <= 0 || st.Channels <= 0 {
				return Probe{}, argErr("audio stream %d has no sample rate or channels", st.Index)
			}
		case "subtitle", "data", "attachment", "unknown":
		default:
			return Probe{}, argErr("probe stream type is unknown")
		}
	}
	for _, l := range pr.Limitations {
		if !shortText(l, 64) || l == "" {
			return Probe{}, argErr("probe limitation is invalid")
		}
	}
	if videos == 0 {
		return Probe{}, argErr("recording has no video stream")
	}
	return pr, nil
}

// Stream returns the probed stream with index i.
func (p Probe) Stream(i int) (ProbeStream, bool) {
	for _, st := range p.Streams {
		if st.Index == i {
			return st, true
		}
	}
	return ProbeStream{}, false
}

// Segmentation holds the explicit detection settings of an analysis plan.
type Segmentation struct {
	Method       string  `json:"method"`
	Threshold    float64 `json:"threshold"`
	MinSegmentUs int64   `json:"min_segment_us"`
	MaxSegmentUs int64   `json:"max_segment_us"`
}

// AnalysisPlan is the immutable plan a segment task executes.
type AnalysisPlan struct {
	SchemaVersion        int          `json:"schema_version"`
	SourceID             string       `json:"source_id"`
	SourceSHA256         string       `json:"source_sha256"`
	ProbeSHA256          string       `json:"probe_sha256"`
	PolicySHA256         string       `json:"policy_sha256"`
	VideoStreamIndex     int          `json:"video_stream_index"`
	GameAudioStreamIndex *int         `json:"game_audio_stream_index"`
	SourceRangeUs        [2]int64     `json:"source_range_us"`
	Segmentation         Segmentation `json:"segmentation"`
}

// ValidateAnalysis checks stream choice, range and detection settings
// against the probed source.
func ValidateAnalysis(plan AnalysisPlan, pr Probe) error {
	if plan.SchemaVersion != 1 {
		return argErr("analysis plan schema_version must be 1")
	}
	v, ok := pr.Stream(plan.VideoStreamIndex)
	if !ok || v.Type != "video" {
		return argErr("video_stream_index must name a probed video stream")
	}
	if plan.GameAudioStreamIndex != nil {
		a, ok := pr.Stream(*plan.GameAudioStreamIndex)
		if !ok || a.Type != "audio" {
			return argErr("game_audio_stream_index must name a probed audio stream or be null")
		}
	}
	r := plan.SourceRangeUs
	if r[0] < 0 || r[1] <= r[0] || r[1] > pr.DurationUs {
		return argErr("source_range_us must be [start,end) inside the probed duration")
	}
	s := plan.Segmentation
	if s.Method != "scene_change" {
		return argErr("segmentation.method must be scene_change")
	}
	if math.IsNaN(s.Threshold) || s.Threshold < 0.05 || s.Threshold > 0.95 {
		return argErr("segmentation.threshold must be within 0.05..0.95")
	}
	if s.MinSegmentUs < 500_000 || s.MaxSegmentUs > 30*60*sec || s.MinSegmentUs*2 > s.MaxSegmentUs {
		return argErr("segmentation lengths must satisfy 0.5s <= min, 2*min <= max <= 30min")
	}
	return nil
}

// Output fixes the prepared media format.
type Output struct {
	FPS        int `json:"fps"`
	SampleRate int `json:"sample_rate"`
}

// SelectedSegment is one chosen source range [start_us, end_us).
type SelectedSegment struct {
	SegmentID string `json:"segment_id"`
	StartUs   int64  `json:"start_us"`
	EndUs     int64  `json:"end_us"`
}

// Selection is an immutable revision of the human segment choice.
type Selection struct {
	SchemaVersion    int               `json:"schema_version"`
	BasePlanRevision int               `json:"base_plan_revision"`
	SourceSHA256     string            `json:"source_sha256"`
	SelectedSegments []SelectedSegment `json:"selected_segments"`
	Output           Output            `json:"output"`
}

// OutputFrames is the integer frame length of a source range at fps.
func OutputFrames(startUs, endUs int64, fps int) int64 {
	return ((endUs-startUs)*int64(fps) + 500_000) / 1_000_000
}

// OutputSamples is the exact sample length of frames at fps.
func OutputSamples(frames int64, fps, rate int) int64 {
	return frames * int64(rate) / int64(fps)
}

// ValidateSelection checks order, overlap, bounds and policy budgets. The
// first version keeps source order; sort is a later content stage.
func ValidateSelection(sel Selection, pr Probe, p Policy) error {
	if sel.SchemaVersion != 1 || sel.BasePlanRevision < 1 {
		return argErr("selection schema_version or base_plan_revision is invalid")
	}
	if (sel.Output.FPS != 30 && sel.Output.FPS != 60) || sel.Output.SampleRate != 48000 {
		return argErr("output must be {fps:30|60, sample_rate:48000}")
	}
	n := len(sel.SelectedSegments)
	if n == 0 || n > p.MaxSelected {
		return argErr("select 1..%d segments", p.MaxSelected)
	}
	ids := map[string]bool{}
	var total, prevEnd int64
	for i, s := range sel.SelectedSegments {
		if !segmentID.MatchString(s.SegmentID) || ids[s.SegmentID] {
			return argErr("segment ids must be unique [A-Za-z0-9_-]{1,64}")
		}
		ids[s.SegmentID] = true
		if s.StartUs < 0 || s.EndUs > pr.DurationUs || s.EndUs <= s.StartUs {
			return argErr("segment %s must stay inside the probed source range", s.SegmentID)
		}
		if i > 0 && s.StartUs < prevEnd {
			return argErr("segments must be in source order without overlap")
		}
		if OutputFrames(s.StartUs, s.EndUs, sel.Output.FPS) < 1 {
			return argErr("segment %s is shorter than one output frame", s.SegmentID)
		}
		prevEnd = s.EndUs
		total += s.EndUs - s.StartUs
	}
	if total > p.MaxOutputUs {
		return argErr("selected output length exceeds the policy limit; shorten the selection")
	}
	return nil
}

// Cut is one detected scene-change time.
type Cut struct {
	TUs   int64   `json:"t_us"`
	Score float64 `json:"score"`
}

// Candidate is one reviewable segment proposal.
type Candidate struct {
	SegmentID string  `json:"segment_id"`
	StartUs   int64   `json:"start_us"`
	EndUs     int64   `json:"end_us"`
	Score     float64 `json:"score"`
	Reason    string  `json:"reason"`
	Thumbnail string  `json:"thumbnail"`
}

// DetectConfig records how candidates were computed.
type DetectConfig struct {
	Threshold    float64 `json:"threshold"`
	MinSegmentUs int64   `json:"min_segment_us"`
	MaxSegmentUs int64   `json:"max_segment_us"`
	ChunkUs      int64   `json:"chunk_us"`
	OverlapUs    int64   `json:"overlap_us"`
	MaxEdge      int     `json:"max_edge"`
	MinCutGapUs  int64   `json:"min_cut_gap_us"`
}

// SegmentsDoc is the segment task result.
type SegmentsDoc struct {
	SchemaVersion int          `json:"schema_version"`
	Method        string       `json:"method"`
	MethodVersion string       `json:"method_version"`
	PlanSHA256    string       `json:"plan_sha256"`
	RangeUs       [2]int64     `json:"range_us"`
	Config        DetectConfig `json:"config"`
	ChunksTotal   int          `json:"chunks_total"`
	ChunksDone    int          `json:"chunks_done"`
	Complete      bool         `json:"complete"`
	Cuts          []Cut        `json:"cuts"`
	Candidates    []Candidate  `json:"candidates"`
}

var candidateReasons = map[string]bool{"scene_change": true, "range_start": true, "duration_limit": true, "merged_short": true}

// ParseSegments validates a complete, gap-free candidate list.
func ParseSegments(raw []byte, plan AnalysisPlan, planSHA string, p Policy) (SegmentsDoc, error) {
	var d SegmentsDoc
	if int64(len(raw)) > p.MaxDetectionJSONBytes || strictDecode(raw, &d) != nil {
		return SegmentsDoc{}, argErr("segments.json is invalid or exceeds the size budget")
	}
	if d.SchemaVersion != 1 || d.Method != plan.Segmentation.Method || !shortText(d.MethodVersion, 64) || d.MethodVersion == "" || d.PlanSHA256 != planSHA {
		return SegmentsDoc{}, argErr("segments.json does not belong to the current analysis plan")
	}
	if d.RangeUs != plan.SourceRangeUs {
		return SegmentsDoc{}, argErr("segments.json range differs from the plan")
	}
	if !d.Complete || d.ChunksTotal < 1 || d.ChunksDone != d.ChunksTotal {
		return SegmentsDoc{}, argErr("partial scans cannot be registered as complete")
	}
	if d.Config.Threshold != plan.Segmentation.Threshold || d.Config.MinSegmentUs != plan.Segmentation.MinSegmentUs || d.Config.MaxSegmentUs != plan.Segmentation.MaxSegmentUs {
		return SegmentsDoc{}, argErr("segments.json detection settings differ from the plan")
	}
	if len(d.Candidates) == 0 || len(d.Candidates) > p.MaxCandidates || len(d.Cuts) > p.MaxCandidates {
		return SegmentsDoc{}, argErr("candidate count exceeds the policy budget")
	}
	for _, c := range d.Cuts {
		if c.TUs <= d.RangeUs[0] || c.TUs >= d.RangeUs[1] || math.IsNaN(c.Score) {
			return SegmentsDoc{}, argErr("cut lies outside the analysis range")
		}
	}
	if !sort.SliceIsSorted(d.Cuts, func(i, j int) bool { return d.Cuts[i].TUs < d.Cuts[j].TUs }) {
		return SegmentsDoc{}, argErr("cuts must be sorted")
	}
	ids := map[string]bool{}
	thumbs := 0
	cursor := d.RangeUs[0]
	for _, c := range d.Candidates {
		if !segmentID.MatchString(c.SegmentID) || ids[c.SegmentID] || !candidateReasons[c.Reason] || math.IsNaN(c.Score) {
			return SegmentsDoc{}, argErr("candidate identity or reason is invalid")
		}
		ids[c.SegmentID] = true
		if c.StartUs != cursor || c.EndUs <= c.StartUs {
			return SegmentsDoc{}, argErr("candidates must cover the range without gaps or overlap")
		}
		cursor = c.EndUs
		if c.Thumbnail != "" {
			thumbs++
			if c.Thumbnail != "thumbnails/"+c.SegmentID+".jpg" {
				return SegmentsDoc{}, argErr("candidate thumbnail path is not controlled")
			}
		}
	}
	if cursor != d.RangeUs[1] {
		return SegmentsDoc{}, argErr("candidates do not reach the end of the range")
	}
	if thumbs > p.MaxThumbnails {
		return SegmentsDoc{}, argErr("thumbnail count exceeds the policy budget")
	}
	return d, nil
}

// FrameQuota gives every selected segment one evidence frame, then spends
// the remaining budget on the longest segments first, at most perSegment
// each. It fails when the selection cannot get one frame per segment.
func FrameQuota(durations []int64, maxFrames, perSegment int) ([]int, error) {
	n := len(durations)
	if n == 0 || n > maxFrames {
		return nil, fmt.Errorf("selected segments (%d) exceed the profile frame budget (%d); select fewer segments or publish a larger profile: %w", n, maxFrames, model.ErrInvalidState)
	}
	quota := make([]int, n)
	order := make([]int, n)
	for i := range quota {
		quota[i] = 1
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return durations[order[a]] > durations[order[b]] })
	remaining := maxFrames - n
	for round := 2; round <= perSegment && remaining > 0; round++ {
		for _, i := range order {
			if remaining == 0 {
				break
			}
			quota[i]++
			remaining--
		}
	}
	return quota, nil
}
