package model

import "time"

// Script statuses. Expired and needs_refinalize keep the row: a visible-set
// change marks downstream scripts stale and does not delete them.
const (
	ScriptDraft           = "draft"
	ScriptFinalized       = "finalized"
	ScriptExpired         = "expired"
	ScriptNeedsRefinalize = "needs_refinalize"
	ScriptStaleVisibleSet = "visible_set_changed"
)

// VisibleAsset is one library row as the authorization page lists it.
// Ingested is the「入库」layer; AgentVisible is the「Agent 可见」layer.
type VisibleAsset struct {
	AssetID      string `json:"asset_id"`
	Status       string `json:"status"`
	AgentVisible bool   `json:"agent_visible"`
	Ingested     bool   `json:"ingested"`
}

// VisibleConfirmation is the latest human grant of a visible set.
type VisibleConfirmation struct {
	ID             int64
	SetSHA         string
	AssetIDs       []string
	Actor          string
	ScriptFinalize bool
	CreatedAt      time.Time
}

// Script is one downstream script bound to the visible set it was written against.
type Script struct {
	ScriptID      string     `json:"script_id"`
	AssetID       string     `json:"asset_id"`
	Status        string     `json:"status"`
	VisibleSetSHA string     `json:"visible_set_sha"`
	Title         string     `json:"title,omitempty"`
	Body          string     `json:"body,omitempty"`
	FinalizedBy   string     `json:"finalized_by,omitempty"`
	FinalizedAt   *time.Time `json:"finalized_at,omitempty"`
	StaleReason   string     `json:"stale_reason,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
