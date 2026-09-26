package model

import "time"

// Asset is a single processing unit: one clip folder with its edl.json.
type Asset struct {
	AssetID       string            `json:"asset_id"`
	Status        string            `json:"status"`
	AgentVisible  bool              `json:"agent_visible"`
	HumanApproved bool              `json:"human_approved"`
	Locked        bool              `json:"locked"`
	AllowedAgents []string          `json:"allowed_agents"`
	Artifacts     map[string]string `json:"artifacts"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// Asset status values from docs/项目开发文档.md §7.1.
const (
	AssetStatusIngested   = "ingested"
	AssetStatusRecognized = "recognized"
	AssetStatusNarrated   = "narrated"
	AssetStatusExported   = "exported"
)
