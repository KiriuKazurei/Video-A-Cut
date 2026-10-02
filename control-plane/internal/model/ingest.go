package model

import "time"

// RecordingSource is one registered local recording. The snapshot, hash and
// probe version are filled once media_probe publishes the copy, and never
// change afterwards.
type RecordingSource struct {
	SourceID      string     `json:"source_id"`
	AssetID       string     `json:"asset_id"`
	RootID        string     `json:"root_id"`
	RelativePath  string     `json:"relative_path"`
	SourceVersion string     `json:"source_version"`
	SizeBytes     int64      `json:"size_bytes"`
	MTimeNs       int64      `json:"mtime_ns"`
	SnapshotRef   string     `json:"-"`
	HasSnapshot   bool       `json:"has_snapshot"`
	SHA256        string     `json:"sha256,omitempty"`
	ProbeVersion  string     `json:"probe_version,omitempty"`
	CreatedBy     string     `json:"created_by"`
	CreatedAt     time.Time  `json:"created_at"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
}

// IngestRun tracks one import of a source into an EDL package.
type IngestRun struct {
	RunID             string            `json:"run_id"`
	AssetID           string            `json:"asset_id"`
	SourceID          string            `json:"source_id"`
	State             string            `json:"state"`
	Stage             string            `json:"stage"`
	Version           int               `json:"version"`
	PolicyJSON        string            `json:"-"`
	PolicySHA256      string            `json:"policy_sha256"`
	RootsSHA256       string            `json:"-"`
	AnalysisRevision  int               `json:"analysis_revision"`
	SelectionRevision int               `json:"selection_revision"`
	CurrentTaskID     string            `json:"current_task_id,omitempty"`
	ErrorCode         string            `json:"error_code,omitempty"`
	ErrorMessage      string            `json:"error_message,omitempty"`
	ProbeJSON         string            `json:"-"`
	ProbeSHA256       string            `json:"probe_sha256,omitempty"`
	SegmentsRef       string            `json:"-"`
	SegmentsSHA256    string            `json:"segments_sha256,omitempty"`
	Files             map[string]string `json:"-"`
	PackageRef        string            `json:"-"`
	ProfileID         string            `json:"profile_id,omitempty"`
	ProfileRevision   int               `json:"profile_revision,omitempty"`
	ProfileSHA256     string            `json:"profile_sha256,omitempty"`
	CreatedBy         string            `json:"created_by"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// IngestPlanRevision is an append-only analysis or selection plan.
type IngestPlanRevision struct {
	RunID         string    `json:"run_id"`
	Revision      int       `json:"revision"`
	Kind          string    `json:"kind"`
	SchemaVersion int       `json:"schema_version"`
	CanonicalJSON string    `json:"-"`
	SHA256        string    `json:"sha256"`
	Actor         string    `json:"actor"`
	CreatedAt     time.Time `json:"created_at"`
}

// IngestTaskBinding ties a queued task to fixed run inputs and records the
// execution currently allowed to write results for it.
type IngestTaskBinding struct {
	TaskID         string    `json:"task_id"`
	RunID          string    `json:"run_id"`
	Stage          string    `json:"stage"`
	PlanRevision   int       `json:"plan_revision"`
	InputSHA256    string    `json:"input_sha256"`
	RootsSHA256    string    `json:"-"`
	ExecutionID    string    `json:"-"`
	ExecutionSeq   int       `json:"execution_seq"`
	AttemptOf      string    `json:"attempt_of,omitempty"`
	Invalidated    bool      `json:"invalidated"`
	ResultRef      string    `json:"-"`
	ResultSHA256   string    `json:"result_sha256,omitempty"`
	HandoverState  string    `json:"handover_state,omitempty"`
	ExecutionState string    `json:"execution_state,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// IngestCheckpoint references one verified block or segment.
type IngestCheckpoint struct {
	TaskID            string    `json:"task_id"`
	Sequence          int       `json:"sequence"`
	ExecutionID       string    `json:"execution_id"`
	InputSHA256       string    `json:"input_sha256"`
	PolicySHA256      string    `json:"policy_sha256"`
	Kind              string    `json:"kind"`
	ItemIndex         int       `json:"item_index"`
	Ref               string    `json:"ref"`
	SHA256            string    `json:"sha256"`
	ManifestVersion   int       `json:"manifest_version,omitempty"`
	JournalVersion    int       `json:"journal_version,omitempty"`
	SourceExecutionID string    `json:"source_execution_id,omitempty"`
	ItemStatus        string    `json:"item_status,omitempty"`
	Summary           string    `json:"summary,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}
