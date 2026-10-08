package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

const PreviewExecutionProtocol = 2

func previewExecutionOutcome(ex model.PreviewExecution, tk model.Task, obsolete bool) ClaimOutcome {
	out := ClaimOutcome{Claimed: !obsolete, Obsolete: obsolete, ExecutionProtocol: PreviewExecutionProtocol,
		Scope: ExecutionScope{AgentID: ex.AgentID, Role: "exporter", TaskID: ex.TaskID,
			RuntimeInstanceID: ex.RuntimeInstanceID, ExecutionID: ex.ExecutionID,
			Generation: ex.Generation, InputSHA256: ex.InputSHA256}}
	if !obsolete {
		out.Task = tk
		out.LeaseExpiresAt = tk.LeaseUntil
		if tk.LeaseUntil != nil {
			out.LeaseRemaining = time.Until(*tk.LeaseUntil)
		}
	}
	return out
}

func claimPreviewExecution(ctx context.Context, tx *store.Store, tk model.Task, opt ClaimOptions) (ClaimOutcome, error) {
	if !idemKeyPattern.MatchString(opt.RuntimeInstanceID) || !idemKeyPattern.MatchString(opt.RequestID) {
		return ClaimOutcome{}, fmt.Errorf("preview claims require runtime_instance_id and request_id: %w", model.ErrProtocolUpgrade)
	}
	ex, err := tx.GetPreviewExecution(ctx, tk.TaskID, tk.Attempts+1)
	if errors.Is(err, model.ErrNotFound) {
		job, e := tx.GetPreviewJobByTaskID(ctx, tk.TaskID)
		if e != nil {
			return ClaimOutcome{}, e
		}
		ex = model.PreviewExecution{ExecutionID: newID("pex"), TaskID: tk.TaskID, AgentID: tk.AgentID,
			RuntimeInstanceID: opt.RuntimeInstanceID, Generation: tk.Attempts + 1, InputSHA256: job.InputSHA256}
		if e = tx.InsertPreviewExecution(ctx, ex); e != nil {
			return ClaimOutcome{}, e
		}
	} else if err != nil {
		return ClaimOutcome{}, err
	}
	if ex.AgentID != tk.AgentID || ex.RuntimeInstanceID != opt.RuntimeInstanceID {
		return ClaimOutcome{}, model.ErrInstanceConflict
	}
	if err = tx.BindPreviewExecutionRequest(ctx, opt.RuntimeInstanceID, opt.RequestID, ex.ExecutionID); err != nil {
		return ClaimOutcome{}, err
	}
	return previewExecutionOutcome(ex, tk, false), nil
}

func authorizePreviewExecution(ctx context.Context, tx *store.Store, scope ExecutionScope, tk model.Task, needInput bool) error {
	if tk.AgentID != scope.AgentID || tk.AgentRole != scope.Role {
		return model.ErrForbidden
	}
	if err := scopeComplete(scope, needInput); err != nil {
		return staleExecution("preview execution scope is missing or invalid")
	}
	ex, err := tx.GetPreviewExecution(ctx, tk.TaskID, scope.Generation)
	if errors.Is(err, model.ErrNotFound) {
		return staleExecution("preview execution is unknown")
	}
	if err != nil {
		return err
	}
	if ex.AgentID != scope.AgentID ||
		ex.ExecutionID != scope.ExecutionID || ex.RuntimeInstanceID != scope.RuntimeInstanceID ||
		ex.Generation != tk.Attempts+1 || (scope.InputSHA256 != "" && scope.InputSHA256 != ex.InputSHA256) {
		return staleExecution("preview execution has been replaced")
	}
	return nil
}

// GetTaskStatusScoped lets a submitting Worker reconcile a lost response.
func (s *Service) GetTaskStatusScoped(ctx context.Context, scope ExecutionScope) (model.Task, error) {
	var task model.Task
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		auth, err := authorizeExecution(ctx, tx, scope, opResult)
		if err != nil {
			return err
		}
		task = auth.Task
		if task.AgentID != scope.AgentID || task.AgentRole != scope.Role {
			return model.ErrForbidden
		}
		return nil
	})
	return task, err
}
