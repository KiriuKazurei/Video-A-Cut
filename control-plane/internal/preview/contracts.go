// Package preview defines MP4 preview protocol v1, not an active service.
// Shared wire declarations and presets live in contracts/mp4-preview/v1.
package preview

import (
	"context"
	"encoding/json"
	"errors"
)

const (
	ProtocolVersion  = 1
	PresetID         = "mp4-720p-v1"
	MediaFilename    = "final.mp4"
	ManifestFilename = "preview-manifest.json"
	ReportFilename   = "render-report.json"
	ReceiptFilename  = "preview-receipt.json"
)

var (
	ErrNotImplemented = errors.New("MP4 preview application is not implemented")
	ErrInvalidInput   = errors.New("invalid MP4 preview fingerprint input")
)

type StartRequest struct {
	SubtitleVariantID string `json:"subtitle_variant_id,omitempty"`
	RevisionID        string `json:"revision_id"`
	ExpectedVersion   int    `json:"expected_version"`
	PresetID          string `json:"preset_id"`
	RequestID         string `json:"request_id"`
}

type InputFile struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Input is fixed by Go at task creation. Hashes refer to actual source files.
// Preset is the full snapshot matching the shared preset file SHA-256.
type Input struct {
	SubtitleVariant *SubtitleVariantInput `json:"subtitle_variant,omitempty"`
	SchemaVersion   int                   `json:"schema_version"`
	PreviewID       string                `json:"preview_id"`
	TaskID          string                `json:"task_id"`
	AssetID         string                `json:"asset_id"`
	RunID           string                `json:"run_id"`
	RevisionID      string                `json:"revision_id"`
	PackageRef      string                `json:"package_ref"`
	EDLSHA256       string                `json:"edl_sha256"`
	ManifestSHA256  string                `json:"manifest_sha256"`
	PresetID        string                `json:"preset_id"`
	PresetSHA256    string                `json:"preset_sha256"`
	Preset          json.RawMessage       `json:"preset"`
	Files           []InputFile           `json:"files"`
	InputSHA256     string                `json:"input_sha256"`
}

type Check struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type Media struct {
	AudioInterval   *AudioInterval `json:"audio_interval,omitempty"`
	Width           int            `json:"width"`
	Height          int            `json:"height"`
	FPS             float64        `json:"fps"`
	FrameCount      int64          `json:"frame_count"`
	DurationSeconds float64        `json:"duration_seconds"`
	VideoCodec      string         `json:"video_codec"`
	AudioCodec      string         `json:"audio_codec"`
	SampleRate      int            `json:"sample_rate"`
	Channels        int            `json:"channels"`
}

type AudioInterval struct {
	StartPTS        int64   `json:"start_pts"`
	DurationTS      int64   `json:"duration_ts"`
	TimeBase        string  `json:"time_base"`
	StartSeconds    float64 `json:"start_seconds"`
	DurationSeconds float64 `json:"duration_seconds"`
}

type Artifact struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	MIME   string `json:"mime"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// View projects status/progress/message from Task. Initialize empty slices to serialize [].
type View struct {
	SubtitleVariantID string     `json:"subtitle_variant_id,omitempty"`
	StyleSHA256       string     `json:"style_sha256,omitempty"`
	PreviewID         string     `json:"preview_id"`
	TaskID            string     `json:"task_id"`
	AssetID           string     `json:"asset_id"`
	RunID             string     `json:"run_id"`
	RevisionID        string     `json:"revision_id"`
	PresetID          string     `json:"preset_id"`
	InputSHA256       string     `json:"input_sha256"`
	Status            string     `json:"status"`
	Progress          float64    `json:"progress"`
	Message           string     `json:"message,omitempty"`
	Invalidated       bool       `json:"invalidated"`
	IsCurrent         bool       `json:"is_current"`
	Artifacts         []Artifact `json:"artifacts"`
	Media             *Media     `json:"media,omitempty"`
}

type List struct {
	RunID           string  `json:"run_id"`
	RevisionID      string  `json:"revision_id"`
	ExpectedVersion int     `json:"expected_version"`
	CanStart        bool    `json:"can_start"`
	Checks          []Check `json:"checks"`
	Previews        []View  `json:"previews"`
}

type PresetRef struct {
	PresetID string `json:"preset_id"`
	SHA256   string `json:"sha256"`
}
type Capability struct {
	ExecutionProtocol int         `json:"execution_protocol"`
	SubtitleFormats   []string    `json:"subtitle_formats,omitempty"`
	ProtocolVersion   int         `json:"protocol_version"`
	Ready             bool        `json:"ready"`
	Presets           []PresetRef `json:"presets"`
	Checks            []Check     `json:"checks"`
}
type Font struct {
	Family string `json:"family"`
	SHA256 string `json:"sha256"`
}
type Receipt struct {
	Fonts           []Font `json:"fonts,omitempty"`
	SchemaVersion   int    `json:"schema_version"`
	PreviewID       string `json:"preview_id"`
	TaskID          string `json:"task_id"`
	InputSHA256     string `json:"input_sha256"`
	PresetSHA256    string `json:"preset_sha256"`
	RendererVersion string `json:"renderer_version"`
	FFmpegVersion   string `json:"ffmpeg_version"`
	Font            *Font  `json:"font,omitempty"`
}
type Report struct {
	SchemaVersion int     `json:"schema_version"`
	InputSHA256   string  `json:"input_sha256"`
	TargetFrames  int64   `json:"target_frames"`
	TargetSamples int64   `json:"target_samples"`
	Media         Media   `json:"media"`
	Checks        []Check `json:"checks"`
}
type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	PreviewID     string     `json:"preview_id"`
	TaskID        string     `json:"task_id"`
	InputSHA256   string     `json:"input_sha256"`
	PresetSHA256  string     `json:"preset_sha256"`
	Artifacts     []Artifact `json:"artifacts"`
}

// ResolvedFile is internal only; absolute paths never appear in public DTOs.
type ResolvedFile struct {
	AbsolutePath, Name, MIME, SHA256 string
	Size                             int64
}

// Application is the integration port owned by Agent B (Go state/service).
// API and MCP only project this port; implementation must use existing lease/governance guards.
type Application interface {
	Start(ctx context.Context, actor, runID string, req StartRequest) (View, error)
	List(ctx context.Context, runID string) (List, error)
	Get(ctx context.Context, previewID string) (View, error)
	Cancel(ctx context.Context, actor, previewID string) (View, error)
	TaskInput(ctx context.Context, agentID, taskID string) (Input, error)
	Submit(ctx context.Context, agentID, taskID, previewDir string) error
	ResolveFile(ctx context.Context, previewID, key string) (ResolvedFile, error)
}

type SubtitleVariantInput struct {
	VariantID    string    `json:"variant_id"`
	PackageRef   string    `json:"package_ref"`
	ASS          InputFile `json:"ass"`
	StyleSHA256  string    `json:"style_sha256"`
	FontFamilies []string  `json:"font_families"`
}
