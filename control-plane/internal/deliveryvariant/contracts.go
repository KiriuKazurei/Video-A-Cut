package deliveryvariant

import (
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
)

const SpecID = "ass-basic-v1"
const ProtocolVersion = 1

type InputFile = preview.InputFile
type Artifact = preview.Artifact
type Check = preview.Check
type ResolvedFile = preview.ResolvedFile
type StartRequest struct {
	DraftVersion    int    `json:"draft_version,omitempty"`
	StyleSHA256     string `json:"style_sha256,omitempty"`
	RevisionID      string `json:"revision_id"`
	ExpectedVersion int    `json:"expected_version"`
	RequestID       string `json:"request_id"`
	SpecID          string `json:"spec_id"`
}
type Input struct {
	StyleDocument        *StyleDocument  `json:"style_document,omitempty"`
	StyleSHA256          string          `json:"style_sha256,omitempty"`
	SchemaVersion        int             `json:"schema_version"`
	VariantID            string          `json:"variant_id"`
	TaskID               string          `json:"task_id"`
	AssetID              string          `json:"asset_id"`
	RunID                string          `json:"run_id"`
	RevisionID           string          `json:"revision_id"`
	ParentPackageRef     string          `json:"parent_package_ref"`
	EDLSHA256            string          `json:"edl_sha256"`
	ParentManifestSHA256 string          `json:"parent_manifest_sha256"`
	Format               string          `json:"format"`
	SpecID               string          `json:"spec_id"`
	SpecSHA256           string          `json:"spec_sha256"`
	Spec                 json.RawMessage `json:"spec"`
	SourceFiles          []InputFile     `json:"source_files"`
	InputSHA256          string          `json:"input_sha256"`
}
type Binding struct {
	SchemaVersion int    `json:"schema_version"`
	VariantID     string `json:"variant_id"`
	TaskID        string `json:"task_id"`
	AssetID       string `json:"asset_id"`
	RunID         string `json:"run_id"`
	RevisionID    string `json:"revision_id"`
	InputSHA256   string `json:"input_sha256"`
	SpecSHA256    string `json:"spec_sha256"`
}
type Manifest struct {
	Binding
	Artifacts []Artifact `json:"artifacts"`
}
type Receipt struct {
	Binding
	ConverterVersion string `json:"converter_version"`
}
type Report struct {
	Binding
	SubtitleCount        int     `json:"subtitle_count"`
	DurationCentiseconds int64   `json:"duration_centiseconds"`
	Checks               []Check `json:"checks"`
}
type View struct {
	StyleSHA256 string     `json:"style_sha256,omitempty"`
	VariantID   string     `json:"variant_id"`
	TaskID      string     `json:"task_id"`
	AssetID     string     `json:"asset_id"`
	RunID       string     `json:"run_id"`
	RevisionID  string     `json:"revision_id"`
	SpecID      string     `json:"spec_id"`
	Format      string     `json:"format"`
	InputSHA256 string     `json:"input_sha256"`
	Status      string     `json:"status"`
	Progress    float64    `json:"progress"`
	Message     string     `json:"message,omitempty"`
	Invalidated bool       `json:"invalidated"`
	IsCurrent   bool       `json:"is_current"`
	Artifacts   []Artifact `json:"artifacts"`
}
type List struct {
	CanStartEditable bool      `json:"can_start_editable"`
	RunID            string    `json:"run_id"`
	RevisionID       string    `json:"revision_id"`
	ExpectedVersion  int       `json:"expected_version"`
	CanStart         bool      `json:"can_start"`
	Checks           []Check   `json:"checks"`
	Specs            []SpecRef `json:"specs"`
	Variants         []View    `json:"variants"`
}
type SpecRef struct {
	SpecID string `json:"spec_id"`
	SHA256 string `json:"sha256"`
}
type Capability struct {
	Fonts           []FontFamily `json:"fonts,omitempty"`
	ProtocolVersion int          `json:"protocol_version"`
	Ready           bool         `json:"ready"`
	Specs           []SpecRef    `json:"specs"`
}
