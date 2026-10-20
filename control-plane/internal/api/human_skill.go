package api

// The local UI transport is the human control surface. Window automation has
// the same application permissions as mouse and keyboard input. MCP and Worker
// requests retain their separate transports and identities.
// Media links are ordinary project-scoped URLs without human-session tickets.
func issueHumanMediaTicket(path string) (string, error) { return path, nil }
