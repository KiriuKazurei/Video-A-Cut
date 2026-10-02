package model

import "time"

// Execution statuses. Task queue status stays separate; these describe one
// ingest attempt and its resource handover.
const (
	ExecAllocated            = "allocated"
	ExecWaitingResource      = "waiting_resource"
	ExecRunning              = "running"
	ExecSuspended            = "suspended_connection"
	ExecStopRequested        = "stop_requested"
	ExecDraining             = "draining"
	ExecStopped              = "stopped"
	ExecSucceeded            = "succeeded"
	ExecFailed               = "failed"
	ExecCleanupBlocked       = "cleanup_blocked"
	ResourceHeld             = "held"
	ResourceDraining         = "draining"
	ResourceReleased         = "released"
	ResourceBlocked          = "blocked"
	ResourceRejectIfBusy     = "reject_if_busy"
	ResourceReplaceAfterStop = "replace_after_stop"
	DispositionWaitingDrain  = "queued_waiting_drain"
)

// IngestExecution is one claim of an ingest task. Generation increases on
// every new claim of that task and never reuses an older execution id.
type IngestExecution struct {
	ExecutionID       string
	TaskID            string
	RunID             string
	Generation        int
	AgentID           string
	Role              string
	RuntimeInstanceID string
	InputSHA256       string
	PolicySHA256      string
	Status            string
	LeaseUntil        *time.Time
	OwnedDir          string
	StopReason        string
	DrainedAt         *time.Time
	ResultRequestID   string
	SubmittedAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// WorkerInstance is one process of an agent. A second live instance must not
// silently take over the first.
type WorkerInstance struct {
	AgentID           string
	Role              string
	RuntimeInstanceID string
	Status            string
	ObservedAt        time.Time
}

// ExecutionControl is the persisted stop/ack record polled by the worker.
type ExecutionControl struct {
	ExecutionID    string
	ControlVersion int
	Command        string
	Reason         string
	AckedAt        *time.Time
	AckOutcome     string
	UpdatedAt      time.Time
}

// ExecutionResource is the server-side exclusive owner of one generated key.
type ExecutionResource struct {
	ResourceKey      string
	OwnerExecutionID string
	OwnerGeneration  int
	Strategy         string
	State            string
	Barrier          bool
	UpdatedAt        time.Time
}

// ExecutionRequest is the idempotency record for a claim or submit.
type ExecutionRequest struct {
	RuntimeInstanceID string
	RequestID         string
	Operation         string
	AgentID           string
	ExecutionID       string
	ContentSHA256     string
	ResultStatus      string
	CreatedAt         time.Time
}
