package model

import "time"

// PreviewJob records the binding between a preview task, run, revision, and its fixed input.
type PreviewJob struct {
	PreviewID       string    `json:"preview_id"`
	TaskID          string    `json:"task_id"`
	AssetID         string    `json:"asset_id"`
	RunID           string    `json:"run_id"`
	RevisionID      string    `json:"revision_id"`
	RequestID       string    `json:"request_id"`
	ExpectedVersion int       `json:"expected_version"`
	InputJSON       string    `json:"input_json"`
	InputSHA256     string    `json:"input_sha256"`
	PresetSHA256    string    `json:"preset_sha256"`
	Invalidated     bool      `json:"invalidated"`
	OutputDir       string    `json:"output_dir"`
	ManifestSHA256  string    `json:"manifest_sha256"`
	MediaJSON       string    `json:"media_json"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PreviewArtifactRecord is one persisted artifact in preview_artifacts.
type PreviewArtifactRecord struct {
	PreviewID string `json:"preview_id"`
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	MIME      string `json:"mime"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

// PreviewWorkerCapability holds worker preview capability reporting and expiry.
type PreviewWorkerCapability struct {
	AgentID         string    `json:"agent_id"`
	ProtocolVersion int       `json:"protocol_version"`
	Ready           bool      `json:"ready"`
	CapabilityJSON  string    `json:"capability_json"`
	ObservedAt      time.Time `json:"observed_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}
