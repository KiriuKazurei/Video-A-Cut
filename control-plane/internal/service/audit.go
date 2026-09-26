package service

import (
	"context"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// ListAudit returns the newest audit entries, newest first, bounded by limit.
// A non-positive limit is normalised by the store to its default page size.
//
// This is a read path only and applies no rule of its own: the audit log is
// the record of what the write paths did, so filtering or hiding rows here
// would rewrite history for one reader and leave it intact for the next.
// Access control therefore belongs to the transport that calls this — the
// WebUI and the debug surfaces are operator-only, and the agent-side MCP
// surface never exposes it.
//
// The Phase 2 governance REST endpoints that render the approval and requeue
// history route through this same method. Keeping one reader means a new
// transport cannot invent a second, subtly different view of the log, and a
// pagination or ordering change is made once, in the store, for every reader.
func (s *Service) ListAudit(ctx context.Context, limit int) ([]model.AuditLog, error) {
	return s.st.ListAudit(ctx, limit)
}
