package model

import "time"

// Workflow statuses. They belong to a run, not to a Task: awaiting_review is
// a human gate and must not be stored as a task status.
const (
	WorkflowCreated         = "created"
	WorkflowRunning         = "running"
	WorkflowAwaitingReview  = "awaiting_review"
	WorkflowFailed          = "failed"
	WorkflowCancelled       = "cancelled"
	WorkflowReadyAcceptance = "ready_for_acceptance"
)

// Acceptance results. A downloadable package is not a passed acceptance.
const (
	AcceptancePending = "pending"
	AcceptancePassed  = "passed"
	AcceptanceFailed  = "failed"
)

// Revision is one immutable package snapshot for an asset.
type Revision struct {
	RevisionID             string    `json:"revision_id"`
	AssetID                string    `json:"asset_id"`
	ParentRevisionID       string    `json:"parent_revision_id,omitempty"`
	SchemaVersion          int       `json:"schema_version"`
	PackageRef             string    `json:"package_ref"`
	EDLSHA256              string    `json:"edl_sha256"`
	EvidenceManifestSHA256 string    `json:"evidence_manifest_sha256"`
	CreatedAt              time.Time `json:"created_at"`
	CreatedBy              string    `json:"created_by"`
	Reason                 string    `json:"reason"`
}

// WorkflowRun is the single active procedure for one asset, plus history.
type WorkflowRun struct {
	RunID             string    `json:"run_id"`
	AssetID           string    `json:"asset_id"`
	BaseRevisionID    string    `json:"base_revision_id"`
	CurrentRevisionID string    `json:"current_revision_id"`
	Status            string    `json:"status"`
	Stage             string    `json:"stage"`
	Version           int       `json:"version"`
	ContentMode       string    `json:"content_mode"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	ErrorCode         string    `json:"error_code,omitempty"`
	BlockedReason     string    `json:"blocked_reason,omitempty"`
}

// WorkflowStage binds one task to the revision it must read.
type WorkflowStage struct {
	RunID            string `json:"run_id"`
	TaskID           string `json:"task_id"`
	Stage            string `json:"stage"`
	InputRevisionID  string `json:"input_revision_id"`
	OutputRevisionID string `json:"output_revision_id,omitempty"`
	AttemptOf        string `json:"attempt_of,omitempty"`
	Invalidated      bool   `json:"invalidated"`
}

// SceneReview is a human decision on one scene of one revision.
type SceneReview struct {
	RunID          string    `json:"run_id"`
	RevisionID     string    `json:"revision_id"`
	SceneID        string    `json:"scene_id"`
	Decision       string    `json:"decision"`
	Note           string    `json:"note,omitempty"`
	EvidenceSHA256 string    `json:"evidence_sha256,omitempty"`
	Actor          string    `json:"actor"`
	CreatedAt      time.Time `json:"created_at"`
}

// AcceptanceRecord is a human check. Workers cannot create one.
type AcceptanceRecord struct {
	ID               int64     `json:"id"`
	RunID            string    `json:"run_id"`
	ExportRevisionID string    `json:"export_revision_id"`
	ManifestSHA256   string    `json:"manifest_sha256"`
	CheckItem        string    `json:"check_item"`
	Result           string    `json:"result"`
	Actor            string    `json:"actor"`
	Note             string    `json:"note,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
