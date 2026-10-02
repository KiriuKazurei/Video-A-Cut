// Package preparation defines phase-6 processing profiles and preflight ports.
// Profile validation is pure; explicitly requested provider diagnostics use bounded HTTP.
package preparation

import "context"

type Provider struct {
	Adapter  string `json:"adapter"`
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	TokenEnv string `json:"token_env,omitempty"`
	// Empty preserves the original custom protocol and historical fingerprints.
	APIFormat string `json:"api_format,omitempty"`
}

type Sampling struct {
	MaxFrames      int   `json:"max_frames"`
	MaxBytes       int64 `json:"max_bytes"`
	TimeoutSeconds int   `json:"timeout_seconds"`
}

// Profile contains references to credentials, never credentials themselves.
// Revision and fingerprint bind a future run to the selected configuration.
type Profile struct {
	SchemaVersion int      `json:"schema_version"`
	ProfileID     string   `json:"profile_id"`
	Revision      int      `json:"revision"`
	Name          string   `json:"name"`
	ContentMode   string   `json:"content_mode"`
	Vision        Provider `json:"vision"`
	Narration     Provider `json:"narration"`
	Sampling      Sampling `json:"sampling"`
	TTSVoice      string   `json:"tts_voice"`
	ExportTarget  string   `json:"export_target"`
}

type Check struct {
	Code    string `json:"code"`
	Status  string `json:"status"` // passed, blocked or not_checked
	Message string `json:"message"`
}

type Report struct {
	ExpectedAssetVersion string  `json:"expected_asset_version,omitempty"`
	SchemaVersion        int     `json:"schema_version"`
	ProfileID            string  `json:"profile_id"`
	ProfileRevision      int     `json:"profile_revision"`
	ProfileSHA256        string  `json:"profile_sha256"`
	Status               string  `json:"status"`
	CanStart             bool    `json:"can_start"`
	Checks               []Check `json:"checks"`
}

// A capability is reported by an authenticated Worker and expires server-side.
type Capability struct {
	ProfileSHA256    string   `json:"profile_sha256"`
	ToolsReady       bool     `json:"tools_ready"`
	CredentialsReady bool     `json:"credentials_ready"`
	TTSVoices        []string `json:"tts_voices,omitempty"`
}

// RuntimeProbe is implemented later by a bounded, local-only host/Worker probe.
// Calling a model or synthesizing speech is not part of this interface.
type RuntimeProbe interface {
	Probe(context.Context, Profile) ([]Check, error)
}

// ProfileRepository is a port, not a persistence implementation. Save must
// atomically check expected revision, append an immutable revision and audit it.
type ProfileRepository interface {
	Get(context.Context, string, int) (Profile, error)
	List(context.Context, int, int) ([]Profile, error)
	Save(context.Context, string, Profile, int) (Profile, error)
}

type PreparedStart struct {
	AssetID              string
	RevisionID           string
	ProfileID            string
	ProfileRevision      int
	ProfileSHA256        string
	ExpectedAssetVersion string
	IdempotencyKey       string
}

// WorkflowLauncher must recheck governance/configuration in the launch
// transaction. A successful preflight is neither a lease nor authorization.
type WorkflowLauncher interface {
	StartPrepared(context.Context, string, PreparedStart) (string, error)
}
