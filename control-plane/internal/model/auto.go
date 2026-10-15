package model

import (
	"encoding/json"
	"time"
)

// AutoActor is the identity automatic approvals are recorded under. It exists
// only inside the service: the HTTP layer never accepts an actor from a
// request. It deliberately does not start with "agent:" — MCP workers are
// recorded as "agent:<agent_id>", and an auto-reviewer that shared that
// namespace could not be told apart from a worker configured with the same id.
const AutoActor = "system:auto-review"

// Auto-run states. They overlay a WorkflowRun and never replace its Status,
// so idx_workflow_one_active and the existing run JSON are unaffected.
const (
	AutoStateActive           = "active"
	AutoStateAwaitingDecision = "awaiting_user_decision"
	AutoStateCompleted        = "completed"
	AutoStateHalted           = "halted"
	AutoStateRevoked          = "revoked"
	AutoStateCancelled        = "cancelled"
)

// Auto decision kinds recorded in auto_decisions.
const (
	AutoDecisionAuthorize    = "authorize"
	AutoDecisionRevoke       = "revoke"
	AutoDecisionSceneConfirm = "scene_confirm"
	AutoDecisionSkipHuman    = "skip_human_decided"
	AutoDecisionUserExport   = "user_export"
	AutoDecisionUserDiscard  = "user_discard"
	AutoDecisionTakeover     = "human_takeover"
)

// Ledger action kinds recorded in auto_actions.
const (
	AutoActionSceneConfirm = "scene_confirm"
	AutoActionScore        = "score"
)

// Halt reasons recorded in workflow_auto.halt_reason.
const (
	AutoHaltUserDiscarded      = "user_discarded"
	AutoHaltHumanTakeover      = "human_takeover"
	AutoHaltHumanDecisionClash = "human_decision_conflict"
)

// AutoFeedbackCodes is the closed set of reason codes a retry may carry back
// to the model. Free text from a model or a worker is never fed back.
var AutoFeedbackCodes = map[string]bool{
	"low_confidence":        true,
	"tts_incomplete":        true,
	"av_misaligned":         true,
	"pacing_out_of_range":   true,
	"item_below_floor":      true,
	"brief_low_coverage":    true,
	"scene_criteria_failed": true,
}

// AutoFeedbackItem is one structured retry hint: a code from
// AutoFeedbackCodes and optional numbers, nothing else.
type AutoFeedbackItem struct {
	Code      string   `json:"code"`
	Value     *float64 `json:"value,omitempty"`
	Threshold *float64 `json:"threshold,omitempty"`
}

// AutoScope is what a human authorized. It can be narrowed after creation but
// never widened; widening needs a new authorization.
type AutoScope struct {
	ExternalEgress         bool               `json:"external_egress"`
	AutoSceneConfirm       bool               `json:"auto_scene_confirm"`
	AutoNarrationApprove   bool               `json:"auto_narration_approve"`
	AutoPreviewExport      bool               `json:"auto_preview_export"`
	MaxRounds              int                `json:"max_rounds"`
	MaxTotalRounds         int                `json:"max_total_rounds"`
	MaxStageRetries        int                `json:"max_stage_retries"`
	MaxActions             int                `json:"max_actions"`
	MaxTTSChars            int                `json:"max_tts_chars"`
	MaxWallSeconds         int                `json:"max_wall_seconds"`
	DecisionTimeoutSeconds int                `json:"decision_timeout_seconds"`
	Thresholds             map[string]float64 `json:"thresholds"`
}

// WorkflowAuto is the auto-run overlay of one workflow run.
type WorkflowAuto struct {
	RunID               string     `json:"run_id"`
	AutoVersion         int        `json:"auto_version"`
	State               string     `json:"state"`
	HaltReason          string     `json:"halt_reason,omitempty"`
	Brief               string     `json:"brief"`
	BriefSHA256         string     `json:"brief_sha256"`
	AuthorizedBy        string     `json:"authorized_by"`
	AuthorizedAt        time.Time  `json:"authorized_at"`
	RevokedBy           string     `json:"revoked_by,omitempty"`
	RevokedAt           *time.Time `json:"revoked_at,omitempty"`
	RevokeReason        string     `json:"revoke_reason,omitempty"`
	ConsentAtRevoke     string     `json:"consent_at_revoke,omitempty"`
	ConsentGrantedByRun bool       `json:"consent_granted_by_run"`
	NoticeSHA256        string     `json:"notice_sha256"`
	Scope               AutoScope  `json:"scope"`
	ProfileID           string     `json:"profile_id"`
	ProfileRevision     int        `json:"profile_revision"`
	ProfileSHA256       string     `json:"profile_sha256"`
	Round               int        `json:"round"`
	MaxRounds           int        `json:"max_rounds"`
	TotalRounds         int        `json:"total_rounds"`
	MaxTotalRounds      int        `json:"max_total_rounds"`
	ActionsUsed         int        `json:"actions_used"`
	TTSCharsUsed        int        `json:"tts_chars_used"`
	DeliveryHold        bool       `json:"delivery_hold"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	DeadlineAt          time.Time  `json:"deadline_at"`
	PausedAt            *time.Time `json:"paused_at,omitempty"`
	DecisionDeadlineAt  *time.Time `json:"decision_deadline_at,omitempty"`
}

// AutoRound is one automatic or user-requested rerun of the pipeline.
type AutoRound struct {
	RunID          string     `json:"run_id"`
	Round          int        `json:"round"`
	BaseRevisionID string     `json:"base_revision_id"`
	Feedback       string     `json:"feedback,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	Outcome        string     `json:"outcome,omitempty"`
}

// AutoReview is one stored self-assessment, written by the scorer in
// internal/autoreview. Reasons holds closed codes and numbers only.
type AutoReview struct {
	ReviewID      string          `json:"review_id"`
	RunID         string          `json:"run_id"`
	Round         int             `json:"round"`
	RevisionID    string          `json:"revision_id"`
	PreviewID     string          `json:"preview_id,omitempty"`
	ScorerVersion string          `json:"scorer_version"`
	Score         float64         `json:"score"`
	Band          string          `json:"band"`
	Reasons       json.RawMessage `json:"reasons"`
	Inputs        json.RawMessage `json:"inputs"`
	Thresholds    json.RawMessage `json:"thresholds"`
	CreatedAt     time.Time       `json:"created_at"`
}

// AutoDecision is the retained governance record of an automatic approval or
// a human decision on an auto run.
type AutoDecision struct {
	ID        int64           `json:"id"`
	RunID     string          `json:"run_id"`
	Round     int             `json:"round"`
	Kind      string          `json:"kind"`
	Target    string          `json:"target,omitempty"`
	Actor     string          `json:"actor"`
	Basis     json.RawMessage `json:"basis"`
	CreatedAt time.Time       `json:"created_at"`
}
