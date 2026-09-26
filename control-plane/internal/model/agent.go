package model

import "time"

// Agent is a registered worker identity. Agent-level state is stored
// separately from Task-level state (docs §7.3).
type Agent struct {
	AgentID       string    `json:"agent_id"`
	Role          string    `json:"role"`
	LastSeen      time.Time `json:"last_seen"`
	CurrentTaskID string    `json:"current_task_id,omitempty"`
	Health        string    `json:"health"`
}

// Agent health values.
const (
	AgentHealthHealthy = "healthy"
	AgentHealthStale   = "stale"
	AgentHealthUnknown = "unknown"
)
