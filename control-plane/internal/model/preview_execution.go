package model

// PreviewExecution fences an attempt; output retains its immutable preview identity.
type PreviewExecution struct {
	ExecutionID       string
	TaskID            string
	AgentID           string
	RuntimeInstanceID string
	Generation        int
	InputSHA256       string
}
