package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// Policy is the media-ingest budget fixed on every run. It is versioned
// separately from the phase-6 content profile so neither fingerprint drifts.
type Policy struct {
	SchemaVersion         int    `json:"schema_version"`
	PolicyID              string `json:"policy_id"`
	MaxSourceBytes        int64  `json:"max_source_bytes"`
	MaxSourceDurationUs   int64  `json:"max_source_duration_us"`
	CopyChunkBytes        int64  `json:"copy_chunk_bytes"`
	MinFreeBytes          int64  `json:"min_free_bytes"`
	ChunkUs               int64  `json:"chunk_us"`
	OverlapUs             int64  `json:"overlap_us"`
	AnalysisMaxEdge       int    `json:"analysis_max_edge"`
	ChunkTimeoutSeconds   int    `json:"chunk_timeout_seconds"`
	ScanTimeoutSeconds    int    `json:"scan_timeout_seconds"`
	MaxCandidates         int    `json:"max_candidates"`
	MaxThumbnails         int    `json:"max_thumbnails"`
	MaxSelected           int    `json:"max_selected"`
	MaxOutputUs           int64  `json:"max_output_us"`
	MaxDetectionJSONBytes int64  `json:"max_detection_json_bytes"`
	PrepareTimeoutSeconds int    `json:"prepare_timeout_seconds"`
	FrameQuotaVersion     string `json:"frame_quota_version"`
	MaxFramesPerSegment   int    `json:"max_frames_per_segment"`
}

const (
	gib = int64(1) << 30
	mib = int64(1) << 20
	sec = int64(1_000_000)
)

// DefaultPolicy returns the first-version limits from the phase-7 document.
func DefaultPolicy() Policy {
	return Policy{
		SchemaVersion: 1, PolicyID: "media-ingest-default",
		MaxSourceBytes: 64 * gib, MaxSourceDurationUs: 6 * 3600 * sec,
		CopyChunkBytes: 64 * mib, MinFreeBytes: 1 * gib,
		ChunkUs: 60 * sec, OverlapUs: 1 * sec, AnalysisMaxEdge: 640,
		ChunkTimeoutSeconds: 120, ScanTimeoutSeconds: 7200,
		MaxCandidates: 2048, MaxThumbnails: 128, MaxSelected: 48,
		MaxOutputUs: 15 * 60 * sec, MaxDetectionJSONBytes: 16 * mib,
		PrepareTimeoutSeconds: 1800, FrameQuotaVersion: "quota-1", MaxFramesPerSegment: 5,
	}
}

// Validate keeps every field at or below the documented first-version
// ceiling. Operators may configure smaller budgets, never larger ones.
func (p Policy) Validate() error {
	d := DefaultPolicy()
	bad := func(field string) error {
		return fmt.Errorf("ingest policy %s is outside the allowed range: %w", field, model.ErrArgument)
	}
	switch {
	case p.SchemaVersion != 1:
		return bad("schema_version")
	case !rootID.MatchString(p.PolicyID):
		return bad("policy_id")
	case p.MaxSourceBytes < 1 || p.MaxSourceBytes > d.MaxSourceBytes:
		return bad("max_source_bytes")
	case p.MaxSourceDurationUs < sec || p.MaxSourceDurationUs > d.MaxSourceDurationUs:
		return bad("max_source_duration_us")
	case p.CopyChunkBytes < mib || p.CopyChunkBytes > 256*mib:
		return bad("copy_chunk_bytes")
	case p.MinFreeBytes < 0 || p.MinFreeBytes > 64*gib:
		return bad("min_free_bytes")
	case p.ChunkUs < 5*sec || p.ChunkUs > d.ChunkUs:
		return bad("chunk_us")
	case p.OverlapUs < 0 || p.OverlapUs > d.OverlapUs || p.OverlapUs >= p.ChunkUs:
		return bad("overlap_us")
	case p.AnalysisMaxEdge < 64 || p.AnalysisMaxEdge > d.AnalysisMaxEdge:
		return bad("analysis_max_edge")
	case p.ChunkTimeoutSeconds < 1 || p.ChunkTimeoutSeconds > d.ChunkTimeoutSeconds:
		return bad("chunk_timeout_seconds")
	case p.ScanTimeoutSeconds < 1 || p.ScanTimeoutSeconds > d.ScanTimeoutSeconds:
		return bad("scan_timeout_seconds")
	case p.MaxCandidates < 1 || p.MaxCandidates > d.MaxCandidates:
		return bad("max_candidates")
	case p.MaxThumbnails < 0 || p.MaxThumbnails > d.MaxThumbnails:
		return bad("max_thumbnails")
	case p.MaxSelected < 1 || p.MaxSelected > d.MaxSelected:
		return bad("max_selected")
	case p.MaxOutputUs < sec || p.MaxOutputUs > d.MaxOutputUs:
		return bad("max_output_us")
	case p.MaxDetectionJSONBytes < 64*1024 || p.MaxDetectionJSONBytes > d.MaxDetectionJSONBytes:
		return bad("max_detection_json_bytes")
	case p.PrepareTimeoutSeconds < 10 || p.PrepareTimeoutSeconds > 7200:
		return bad("prepare_timeout_seconds")
	case p.FrameQuotaVersion != d.FrameQuotaVersion:
		return bad("frame_quota_version")
	case p.MaxFramesPerSegment != d.MaxFramesPerSegment:
		return bad("max_frames_per_segment")
	}
	return nil
}

// Canonical returns the stored JSON bytes and their SHA-256.
func (p Policy) Canonical() (string, string, error) {
	if err := p.Validate(); err != nil {
		return "", "", err
	}
	return canonical(p)
}

// ParsePolicy decodes stored bytes and checks they still hash to sum.
func ParsePolicy(raw, sum string) (Policy, error) {
	var p Policy
	if err := strictDecode([]byte(raw), &p); err != nil {
		return Policy{}, fmt.Errorf("stored ingest policy is unreadable: %w", model.ErrInvalidState)
	}
	body, got, err := p.Canonical()
	if err != nil || got != sum || body != raw {
		return Policy{}, fmt.Errorf("stored ingest policy does not match its fingerprint: %w", model.ErrInvalidState)
	}
	return p, nil
}

func canonical(v any) (string, string, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(body)
	return string(body), hex.EncodeToString(sum[:]), nil
}

// Digest hashes any value's canonical JSON form.
func Digest(v any) string {
	_, sum, _ := canonical(v)
	return sum
}
