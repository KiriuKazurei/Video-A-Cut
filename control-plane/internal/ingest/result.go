package ingest

import (
	"fmt"
	"math"
)

// Receipt binds a published package to one execution of one task input.
type Receipt struct {
	SchemaVersion int    `json:"schema_version"`
	TaskID        string `json:"task_id"`
	ExecutionID   string `json:"execution_id"`
	Stage         string `json:"stage"`
	InputSHA256   string `json:"input_sha256"`
	PolicySHA256  string `json:"policy_sha256"`
	WorkerVersion string `json:"worker_version"`
}

// ParseReceipt strictly decodes worker-receipt.json.
func ParseReceipt(raw []byte) (Receipt, error) {
	var r Receipt
	if len(raw) > 64*1024 || strictDecode(raw, &r) != nil || r.SchemaVersion != 1 || !shortText(r.WorkerVersion, 64) {
		return Receipt{}, argErr("worker-receipt.json is invalid")
	}
	return r, nil
}

// SnapshotDoc is the media_probe account of the copied source.
type SnapshotDoc struct {
	SchemaVersion int    `json:"schema_version"`
	SourceID      string `json:"source_id"`
	SourceVersion string `json:"source_version"`
	SizeBytes     int64  `json:"size_bytes"`
	MTimeNs       int64  `json:"mtime_ns"`
	SHA256        string `json:"sha256"`
	Snapshot      string `json:"snapshot"`
	ChunkBytes    int64  `json:"chunk_bytes"`
	Chunks        int    `json:"chunks"`
	Reused        bool   `json:"reused"`
}

// ParseSnapshot strictly decodes snapshot.json.
func ParseSnapshot(raw []byte) (SnapshotDoc, error) {
	var s SnapshotDoc
	if len(raw) > 64*1024 || strictDecode(raw, &s) != nil || s.SchemaVersion != 1 {
		return SnapshotDoc{}, argErr("snapshot.json is invalid")
	}
	return s, nil
}

// MappedSegment traces one prepared clip back to the recording.
type MappedSegment struct {
	SegmentID            string `json:"segment_id"`
	Index                int    `json:"index"`
	Media                string `json:"media"`
	MediaSHA256          string `json:"media_sha256"`
	SourceStartUs        int64  `json:"source_start_us"`
	SourceEndUs          int64  `json:"source_end_us"`
	SourceStartPTS       int64  `json:"source_start_pts"`
	SourceEndPTS         int64  `json:"source_end_pts"`
	TimelineInFrames     int64  `json:"timeline_in_frames"`
	OutputFrames         int64  `json:"output_frames"`
	OutputSamples        int64  `json:"output_samples"`
	MeasuredFrames       int64  `json:"measured_frames"`
	MeasuredSamples      int64  `json:"measured_samples"`
	StartErrorUs         int64  `json:"start_error_us"`
	EndErrorUs           int64  `json:"end_error_us"`
	AudioSampleError     int64  `json:"audio_sample_error"`
	SourceFramesInRange  int64  `json:"source_frames_in_range"`
	DuplicatedFrames     int64  `json:"duplicated_frames"`
	DroppedFrames        int64  `json:"dropped_frames"`
	AudioPadStartSamples int64  `json:"audio_pad_start_samples"`
	FrameQuota           int    `json:"frame_quota"`
}

// SourceMap is source-map.json.
type SourceMap struct {
	SchemaVersion        int             `json:"schema_version"`
	SourceID             string          `json:"source_id"`
	SourceSHA256         string          `json:"source_sha256"`
	SelectionSHA256      string          `json:"selection_sha256"`
	VideoStreamIndex     int             `json:"video_stream_index"`
	GameAudioStreamIndex *int            `json:"game_audio_stream_index"`
	SourceVideoTimeBase  string          `json:"source_video_time_base"`
	SourceOriginPTS      int64           `json:"source_origin_pts"`
	SourceOriginUs       int64           `json:"source_origin_us"`
	AudioOffsetUs        int64           `json:"audio_offset_us"`
	Output               Output          `json:"output"`
	FrameQuotaVersion    string          `json:"frame_quota_version"`
	Segments             []MappedSegment `json:"segments"`
}

// Provenance is ingest-provenance.json; it never names a filesystem path.
type Provenance struct {
	SchemaVersion     int    `json:"schema_version"`
	SourceID          string `json:"source_id"`
	SourceSHA256      string `json:"source_sha256"`
	SourceVersion     string `json:"source_version"`
	RunID             string `json:"run_id"`
	AnalysisRevision  int    `json:"analysis_revision"`
	AnalysisSHA256    string `json:"analysis_sha256"`
	SelectionRevision int    `json:"selection_revision"`
	SelectionSHA256   string `json:"selection_sha256"`
	PolicySHA256      string `json:"policy_sha256"`
	ProbeSHA256       string `json:"probe_sha256"`
	MethodVersion     string `json:"method_version"`
	ProfileID         string `json:"profile_id"`
	ProfileRevision   int    `json:"profile_revision"`
	ProfileSHA256     string `json:"profile_sha256"`
	FrameQuotaVersion string `json:"frame_quota_version"`
	WorkerVersion     string `json:"worker_version"`
}

// ParseProvenance strictly decodes and compares against the expected values.
func ParseProvenance(raw []byte, want Provenance) error {
	var got Provenance
	if len(raw) > 64*1024 || strictDecode(raw, &got) != nil {
		return argErr("ingest-provenance.json is invalid")
	}
	want.WorkerVersion = got.WorkerVersion
	want.MethodVersion = got.MethodVersion
	if got != want || !shortText(got.WorkerVersion, 64) || !shortText(got.MethodVersion, 64) {
		return argErr("ingest-provenance.json does not match the fixed run inputs")
	}
	return nil
}

// ParseSourceMap validates mapping, measured lengths and tolerances. A
// segment beyond one target frame of error, or audio beyond one frame of
// samples, blocks the package.
func ParseSourceMap(raw []byte, sel Selection, selSHA string, quotas []int, hasAudio bool) (SourceMap, error) {
	var m SourceMap
	if len(raw) > 4<<20 || strictDecode(raw, &m) != nil || m.SchemaVersion != 1 {
		return SourceMap{}, argErr("source-map.json is invalid")
	}
	if m.SelectionSHA256 != selSHA || m.SourceSHA256 != sel.SourceSHA256 || m.Output != sel.Output || m.FrameQuotaVersion != DefaultPolicy().FrameQuotaVersion {
		return SourceMap{}, argErr("source-map.json does not match the selection")
	}
	if (m.GameAudioStreamIndex != nil) != hasAudio {
		return SourceMap{}, argErr("source-map.json audio selection differs from the plan")
	}
	if len(m.Segments) != len(sel.SelectedSegments) {
		return SourceMap{}, argErr("source-map.json must list exactly the selected segments")
	}
	fps := sel.Output.FPS
	frameUs := int64(math.Ceil(1e6 / float64(fps)))
	samplesPerFrame := int64(sel.Output.SampleRate / fps)
	var timeline int64
	for i, seg := range m.Segments {
		want := sel.SelectedSegments[i]
		if seg.SegmentID != want.SegmentID || seg.Index != i+1 || seg.SourceStartUs != want.StartUs || seg.SourceEndUs != want.EndUs {
			return SourceMap{}, argErr("source-map.json segment %d does not match the selection", i+1)
		}
		if seg.Media != fmt.Sprintf("media/segment_%03d.mp4", i+1) || len(seg.MediaSHA256) != 64 {
			return SourceMap{}, argErr("source-map.json media reference is not controlled")
		}
		frames := OutputFrames(want.StartUs, want.EndUs, fps)
		if seg.OutputFrames != frames || seg.TimelineInFrames != timeline || seg.FrameQuota != quotas[i] {
			return SourceMap{}, argErr("source-map.json frame plan differs for %s", seg.SegmentID)
		}
		timeline += frames
		if abs64(seg.MeasuredFrames-frames) > 1 || abs64(seg.StartErrorUs) > frameUs || abs64(seg.EndErrorUs) > frameUs {
			return SourceMap{}, argErr("segment %s exceeds the one-frame boundary tolerance", seg.SegmentID)
		}
		samples := OutputSamples(frames, fps, sel.Output.SampleRate)
		if hasAudio {
			if seg.OutputSamples != samples || seg.MeasuredSamples <= 0 || abs64(seg.MeasuredSamples-samples) > samplesPerFrame || abs64(seg.AudioSampleError) > samplesPerFrame {
				return SourceMap{}, argErr("segment %s audio length exceeds tolerance", seg.SegmentID)
			}
		} else if seg.OutputSamples != 0 || seg.MeasuredSamples != 0 {
			return SourceMap{}, argErr("segment %s has audio although none was selected", seg.SegmentID)
		}
	}
	return m, nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// CheckEDL verifies the initial EDL lists exactly the prepared clips in
// source order with integer-frame lengths and the five required tracks.
func CheckEDL(edl map[string]any, m SourceMap) error {
	tl, _ := edl["timeline"].(map[string]any)
	if tl == nil || num(tl["fps"]) != float64(m.Output.FPS) || num(tl["sample_rate"]) != float64(m.Output.SampleRate) {
		return argErr("edl timeline must match the prepared output")
	}
	for _, track := range []string{"video", "game_audio", "voice", "subtitle", "music"} {
		if _, ok := edl[track].([]any); !ok {
			return argErr("edl must contain the %s array", track)
		}
	}
	for _, track := range []string{"voice", "subtitle", "music"} {
		if len(edl[track].([]any)) != 0 {
			return argErr("initial edl %s track must be empty", track)
		}
	}
	video := edl["video"].([]any)
	audio := edl["game_audio"].([]any)
	if len(video) != len(m.Segments) || (m.GameAudioStreamIndex != nil && len(audio) != len(m.Segments)) || (m.GameAudioStreamIndex == nil && len(audio) != 0) {
		return argErr("edl tracks must list exactly the prepared segments")
	}
	fps := float64(m.Output.FPS)
	for i, seg := range m.Segments {
		dur := float64(seg.OutputFrames) / fps
		in := float64(seg.TimelineInFrames) / fps
		for _, list := range [][]any{video, audio} {
			if len(list) == 0 {
				continue
			}
			item, _ := list[i].(map[string]any)
			if item == nil || item["src"] != seg.Media || num(item["in"]) != 0 || math.Abs(num(item["out"])-dur) > 1e-6 || math.Abs(num(item["timeline_in"])-in) > 1e-6 {
				return argErr("edl item %d does not match source-map", i+1)
			}
		}
		meta, _ := video[i].(map[string]any)["ingest"].(map[string]any)
		if meta == nil || meta["segment_id"] != seg.SegmentID || num(meta["frame_quota"]) != float64(seg.FrameQuota) {
			return argErr("edl video item %d lacks ingest metadata", i+1)
		}
	}
	return nil
}

func num(v any) float64 {
	f, ok := v.(float64)
	if !ok {
		return math.NaN()
	}
	return f
}
