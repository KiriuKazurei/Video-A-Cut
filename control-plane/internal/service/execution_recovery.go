package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"time"
)

type runtimeProcessIdentity struct {
	PID         int    `json:"pid"`
	Created     string `json:"created"`
	Path        string `json:"path"`
	JobEnforced bool   `json:"job_enforced"`
}

// The current owner never borrows a former identity. Only local OS death
// evidence can reconcile a revoked resource after a worker crash.
func (s *Service) ReconcileExecutionDrain(ctx context.Context, scope ExecutionScope, formerID string) error {
	if !idemKeyPattern.MatchString(formerID) {
		return model.ErrArgument
	}
	return s.WithExecutionTx(ctx, scope, opRecovery, func(ctx context.Context, tx *store.Store, auth executionAuth) error {
		former, err := tx.GetIngestExecution(ctx, formerID)
		if err != nil {
			return err
		}
		if former.AgentID != scope.AgentID || former.Role != scope.Role || former.ExecutionID == scope.ExecutionID {
			return model.ErrForbidden
		}
		oldRun, err := tx.GetIngestRun(ctx, former.RunID)
		if err != nil {
			return err
		}
		if oldRun.SourceID != auth.Run.SourceID {
			return model.ErrForbidden
		}
		if former.Status == model.ExecStopped {
			return nil
		}
		if !executionRevoked(former.Status) {
			return model.ErrResourceBusy
		}
		resource, err := tx.GetExecutionResource(ctx, sourceSnapshotKey(auth.Run.SourceID))
		if err != nil {
			return err
		}
		if resource.OwnerExecutionID != formerID {
			return model.ErrConflict
		}
		if !idemKeyPattern.MatchString(former.RuntimeInstanceID) {
			return model.ErrConflict
		}
		raw, _, err := s.readSmall(".vac-runtime/instances/"+former.RuntimeInstanceID+".json", 1<<20)
		if err != nil {
			return fmt.Errorf("former owner has no verifiable local runtime record: %w", model.ErrResourceBusy)
		}
		var record struct {
			Schema   int                      `json:"schema_version"`
			Instance string                   `json:"runtime_instance_id"`
			Owner    runtimeProcessIdentity   `json:"owner"`
			Entries  []runtimeProcessIdentity `json:"entries"`
		}
		if err := json.Unmarshal(raw, &record); err != nil || record.Schema != 2 || record.Instance != former.RuntimeInstanceID || record.Owner.PID <= 0 || record.Owner.Created == "" || record.Owner.Path == "" || len(record.Entries) > 1024 {
			return model.ErrResourceBusy
		}
		live, err := executionProcessLive(record.Owner)
		if err != nil || live {
			return fmt.Errorf("former runtime is still active or cannot be verified: %w", model.ErrResourceBusy)
		}
		for _, child := range record.Entries {
			if !child.JobEnforced || child.PID <= 0 || child.Created == "" || child.Path == "" {
				return model.ErrResourceBusy
			}
			live, err := executionProcessLive(child)
			if err != nil || live {
				return model.ErrResourceBusy
			}
		}
		now := time.Now().UTC()
		former.Status, former.DrainedAt = model.ExecStopped, &now
		if err := tx.UpdateIngestExecution(ctx, former); err != nil {
			return err
		}
		resource.OwnerExecutionID, resource.OwnerGeneration = "", 0
		resource.State, resource.Barrier, resource.UpdatedAt = model.ResourceReleased, false, now
		if err := tx.UpsertExecutionResource(ctx, resource); err != nil {
			return err
		}
		if err := tx.AckExecutionControl(ctx, formerID, "stopped", now); err != nil {
			return err
		}
		return auditTx(ctx, tx, "agent:"+scope.AgentID, "execution.recovered_drain", formerID, "verified former runtime and kernel-owned media tree exited")
	})
}
