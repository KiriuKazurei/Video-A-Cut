// Package skill defines the transport-independent project and Agent contracts.
package skill

import (
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
	"time"
)

type Project struct {
	ProfileID               string            `json:"profile_id,omitempty"`
	ProfileRevision         int               `json:"profile_revision,omitempty"`
	TranscriptionAuthorized bool              `json:"transcription_authorized"`
	ID                      string            `json:"id"`
	Name                    string            `json:"name"`
	Flow                    string            `json:"flow"`
	Version                 int               `json:"version"`
	Assets                  []string          `json:"assets"`
	Visible                 []string          `json:"visible"`
	ConfirmedSHA            string            `json:"confirmed_sha"`
	ConfirmedBy             string            `json:"confirmed_by,omitempty"`
	Defaults                map[string]string `json:"default_skills"`
	Disabled                []string          `json:"disabled_skills"`
	ExternalAuthorized      bool              `json:"external_authorized"`
	CreatedAt               time.Time         `json:"created_at"`
	UpdatedAt               time.Time         `json:"updated_at"`
}
type Package struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Source      string            `json:"source"`
	Builtin     bool              `json:"builtin"`
	Enabled     bool              `json:"enabled"`
	Runtime     string            `json:"runtime"`
	Flows       []string          `json:"compatible_flows"`
	Modes       []string          `json:"execution_modes"`
	Tools       []string          `json:"tools"`
	Permissions []string          `json:"permissions"`
	Inputs      []string          `json:"inputs"`
	Outputs     []string          `json:"outputs"`
	Content     string            `json:"content"`
	Resources   map[string]string `json:"resources"`
	SHA         string            `json:"content_sha256"`
	Upstream    string            `json:"upstream,omitempty"`
}
type Media struct {
	Kind    string  `json:"kind,omitempty"`
	ID      string  `json:"id"`
	AssetID string  `json:"asset_id"`
	Name    string  `json:"name"`
	In      float64 `json:"in"`
	Out     float64 `json:"out"`
	SHA     string  `json:"sha256"`
	Ref     string  `json:"-"`
}
type VisibleSet struct {
	ProjectID    string  `json:"project_id"`
	ScopeVersion int     `json:"scope_version"`
	SHA          string  `json:"sha256"`
	Confirmed    bool    `json:"confirmed"`
	Media        []Media `json:"media"`
}
type Segment struct {
	TTSEnabled       bool          `json:"tts_enabled"`
	SubtitlesEnabled *bool         `json:"subtitles_enabled,omitempty"`
	Voice            *tts.Settings `json:"voice,omitempty"`
	VoiceOffset      float64       `json:"voice_offset"`
	VoiceConsent     bool          `json:"voice_consent"`
	MediaID          string        `json:"media_id"`
	Start            float64       `json:"start"`
	End              float64       `json:"end"`
	Narration        string        `json:"narration"`
}
type Plan struct {
	References  []project.Reference `json:"references,omitempty"`
	Body        string              `json:"body"`
	Segments    []Segment           `json:"segments"`
	Subtitles   bool                `json:"subtitles"`
	Version     int                 `json:"version"`
	FinalizedBy string              `json:"finalized_by,omitempty"`
	FinalizedAt *time.Time          `json:"finalized_at,omitempty"`
}
type Budget struct {
	MaxSteps   int `json:"max_steps,omitempty"`
	MaxSeconds int `json:"max_seconds,omitempty"`
	MaxFrames  int `json:"max_frames,omitempty"`
}
type Run struct {
	StructuredEdit          bool            `json:"structured_edit,omitempty"`
	SequenceContentVersion  string          `json:"sequence_content_version,omitempty"`
	SequenceID              string          `json:"sequence_id,omitempty"`
	EditRequest             *EditRequest    `json:"edit_request,omitempty"`
	LeaseUntil              *time.Time      `json:"lease_until,omitempty"`
	AgentRole               string          `json:"agent_role,omitempty"`
	TranscriptionAuthorized bool            `json:"transcription_authorized"`
	ID                      string          `json:"id"`
	ProjectID               string          `json:"project_id"`
	ProjectVersion          int             `json:"project_version"`
	SkillID                 string          `json:"skill_id"`
	SkillSHA                string          `json:"skill_sha256"`
	SkillVersion            string          `json:"skill_version"`
	Mode                    string          `json:"execution_mode"`
	Flow                    string          `json:"flow"`
	VisibleSHA              string          `json:"visible_set_sha"`
	State                   string          `json:"state"`
	Reason                  string          `json:"reason,omitempty"`
	Owner                   string          `json:"owner"`
	Version                 int             `json:"version"`
	Steps                   int             `json:"steps"`
	BudgetStepBase          int             `json:"budget_step_base"`
	BudgetElapsedBase       int64           `json:"budget_elapsed_base"`
	ElapsedMilliseconds     int64           `json:"elapsed_milliseconds"`
	Budget                  Budget          `json:"budget"`
	Goal                    string          `json:"goal"`
	ProfileID               string          `json:"profile_id,omitempty"`
	ProfileRevision         int             `json:"profile_revision,omitempty"`
	ProfileSHA              string          `json:"profile_sha256,omitempty"`
	ExternalAuthorized      bool            `json:"external_authorized"`
	Plan                    *Plan           `json:"plan,omitempty"`
	Inspection              *Inspection     `json:"inspection,omitempty"`
	Summary                 json.RawMessage `json:"summary,omitempty"`
	Frames                  []Frame         `json:"frames"`
	FramesSpent             int             `json:"frames_spent"`
	Artifacts               []Artifact      `json:"artifacts"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
}
type EditWindow struct {
	SegmentIndex  int     `json:"segment_index"`
	MediaID       string  `json:"media_id"`
	TimelineStart float64 `json:"timeline_start"`
	TimelineEnd   float64 `json:"timeline_end"`
	SourceStart   float64 `json:"source_start"`
	SourceEnd     float64 `json:"source_end"`
}
type EditRequest struct {
	Prompt            string       `json:"prompt"`
	BasePlanVersion   int          `json:"base_plan_version"`
	BasePlan          Plan         `json:"base_plan"`
	Windows           []EditWindow `json:"windows"`
	RequestedStart    *float64     `json:"requested_start,omitempty"`
	RequestedEnd      *float64     `json:"requested_end,omitempty"`
	SourceArtifactID  string       `json:"source_artifact_id"`
	SourceArtifactSHA string       `json:"source_artifact_sha256"`
}
type Frame struct {
	ID       string  `json:"id"`
	MediaID  string  `json:"media_id"`
	PTS      float64 `json:"pts"`
	Score    float64 `json:"score"`
	Strategy string  `json:"strategy"`
	SHA      string  `json:"sha256"`
}
type Inspection struct {
	PlanVersion int      `json:"plan_version"`
	FrameIDs    []string `json:"frame_ids"`
	Verdict     string   `json:"verdict"`
	Issues      []string `json:"issues"`
}
type Artifact struct {
	BasicQuality json.RawMessage `json:"basic_quality,omitempty"`
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	Size         int64           `json:"size_bytes"`
	SHA          string          `json:"sha256"`
	Ref          string          `json:"-"`
	PlanVersion  int             `json:"plan_version,omitempty"`
}
type RunRequest struct {
	SequenceID          string `json:"sequence_id,omitempty"`
	Flow                string `json:"flow,omitempty"`
	AgentRole           string `json:"-"`
	AcceptTranscription bool   `json:"accept_transcription,omitempty"`
	ProjectID           string `json:"project_id"`
	SkillID             string `json:"skill_id"`
	Mode                string `json:"execution_mode"`
	VisibleSHA          string `json:"visible_set_sha"`
	Goal                string `json:"goal"`
	ProfileID           string `json:"profile_id,omitempty"`
	ProfileRevision     int    `json:"profile_revision,omitempty"`
	AcceptExternal      bool   `json:"accept_external,omitempty"`
	IdempotencyKey      string `json:"idempotency_key"`
	Budget              Budget `json:"budget,omitempty"`
}
type ToolRequest struct {
	Name            string          `json:"name"`
	Arguments       json.RawMessage `json:"arguments"`
	ExpectedVersion int             `json:"expected_version"`
	IdempotencyKey  string          `json:"idempotency_key"`
}
type Step struct {
	ID         string          `json:"id"`
	RunID      string          `json:"run_id"`
	Name       string          `json:"name"`
	State      string          `json:"state"`
	RequestSHA string          `json:"request_sha256"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}
