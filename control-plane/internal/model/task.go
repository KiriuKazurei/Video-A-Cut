package model

import "time"

// Task is one unit of Agent work, always bound to an Asset.
type Task struct {
	TaskID     string            `json:"task_id"`
	AssetID    string            `json:"asset_id"`
	Type       string            `json:"type"`
	AgentRole  string            `json:"agent_role"`
	AgentID    string            `json:"agent_id,omitempty"`
	Status     string            `json:"status"`
	Progress   float64           `json:"progress"`
	Message    string            `json:"message,omitempty"`
	LeaseUntil *time.Time        `json:"lease_expires_at,omitempty"`
	ClaimedAt  *time.Time        `json:"claimed_at,omitempty"`
	UpdatedAt  time.Time         `json:"updated_at"`
	Artifacts  map[string]string `json:"artifacts,omitempty"`
}

// Task state machine: queued -> claimed -> running -> succeeded.
// running may fail or be cancelled.
const (
	TaskStatusQueued    = "queued"
	TaskStatusClaimed   = "claimed"
	TaskStatusRunning   = "running"
	TaskStatusSucceeded = "succeeded"
	TaskStatusFailed    = "failed"
	TaskStatusCancelled = "cancelled"
)

// Task types produced by the pipeline.
const (
	TaskTypeRecognize = "recognize"
	TaskTypeSort      = "sort"
	TaskTypeNarrate   = "narrate"
	TaskTypeTTS       = "tts"
	TaskTypeSubtitle  = "subtitle"
	TaskTypeMix       = "mix"
	TaskTypeExport    = "export"
	TaskTypePreview   = "preview"
)
