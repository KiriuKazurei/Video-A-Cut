package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// Orchestration rules shared by CreateTask, ClaimTask, FailTask and the lease
// recovery sweep. They decide *when* a queued task may be handed out; the
// state machine itself stays in task.go.

// DefaultMaxAttempts is how many lease recoveries a task survives before the
// sweep fails it instead of requeueing it. A task whose input crashes every
// worker would otherwise cycle through the queue forever and look "running"
// to the WebUI indefinitely.
const DefaultMaxAttempts = 3

// maxDependencies bounds the JSON column and the per-claim lookup cost.
const maxDependencies = 16

// SetMaxAttempts configures the recovery cap. Non-positive restores the
// default. Call before serving.
func (s *Service) SetMaxAttempts(n int) {
	if n <= 0 {
		n = DefaultMaxAttempts
	}
	s.maxAttempts = n
}

func (s *Service) attemptCap() int {
	if s.maxAttempts <= 0 {
		return DefaultMaxAttempts
	}
	return s.maxAttempts
}

// normalizeDependsOn validates and de-duplicates a dependency list while
// keeping its order. Self-reference is refused here; cycles through other
// tasks are impossible because dependencies must already exist.
func normalizeDependsOn(taskID string, deps []string) ([]string, error) {
	if len(deps) > maxDependencies {
		return nil, fmt.Errorf("service: create task %s: at most %d dependencies: %w", taskID, maxDependencies, model.ErrArgument)
	}
	seen := make(map[string]bool, len(deps))
	out := make([]string, 0, len(deps))
	for _, d := range deps {
		if !validTaskID(d) {
			return nil, fmt.Errorf("service: create task %s: invalid dependency id: %w", taskID, model.ErrArgument)
		}
		if d == taskID {
			return nil, fmt.Errorf("service: create task %s: task cannot depend on itself: %w", taskID, model.ErrArgument)
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out, nil
}

// dependenciesReady reports whether every dependency has succeeded. A
// missing dependency row is treated as not ready rather than an error: rows
// are never deleted today, and a claim must not fail the whole queue scan
// because of one inconsistent task.
func dependenciesReady(ctx context.Context, tx *store.Store, tk model.Task) (bool, error) {
	for _, dep := range tk.DependsOn {
		d, err := tx.GetTask(ctx, dep)
		if errors.Is(err, model.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if d.Status != model.TaskStatusSucceeded {
			return false, nil
		}
	}
	return true, nil
}

// typeReady applies per-type asset preconditions. Export rebuilds from the
// asset's edl artifact; an exported asset's edl is the package copy whose
// source paths no longer resolve, so it is held back until a human re-imports
// the source package. Keeping the task queued (instead of letting a worker
// claim and fail it) keeps the reason visible as a waiting state.
//
// Other stages are not gated here: a worker may produce an edl from nothing
// (recognition on raw footage), and SubmitDelivery already refuses a package
// without one. Exported assets are frozen for every stage, though — the edl
// now belongs to the handover package and a human re-import starts the next
// round.
func typeReady(tk model.Task, asset model.Asset) bool {
	if asset.Status == model.AssetStatusExported {
		return false
	}
	if tk.Type == model.TaskTypeExport {
		return asset.Artifacts["edl"] != ""
	}
	return true
}

// cascadeFailure fails every queued task that (transitively) depends on
// rootID. Each dependent is closed in its own transaction and audited with
// the upstream cause, so the WebUI and audit log explain why a task that was
// never claimed ended as failed. Running tasks are left alone: they were
// claimed while their dependencies were satisfied.
func (s *Service) cascadeFailure(ctx context.Context, rootID string) {
	frontier := []string{rootID}
	visited := map[string]bool{rootID: true}
	for len(frontier) > 0 {
		cause := frontier[0]
		frontier = frontier[1:]
		queued, err := s.st.ListActiveTasks(ctx)
		if err != nil {
			s.audit(ctx, "queue", "task.cascade_error", cause, err.Error())
			return
		}
		for _, cand := range queued {
			if visited[cand.TaskID] || cand.Status != model.TaskStatusQueued || !containsString(cand.DependsOn, cause) {
				continue
			}
			visited[cand.TaskID] = true
			var saved model.Task
			changed := false
			err := s.st.Transaction(ctx, func(tx *store.Store) error {
				tk, err := tx.GetTask(ctx, cand.TaskID)
				if err != nil {
					return err
				}
				if tk.Status != model.TaskStatusQueued {
					return nil
				}
				tk.Status = model.TaskStatusFailed
				tk.Message = "dependency " + cause + " did not succeed"
				if err := tx.UpdateTask(ctx, tk); err != nil {
					return err
				}
				saved, err = tx.GetTask(ctx, tk.TaskID)
				changed = err == nil
				return err
			})
			if err != nil {
				s.audit(ctx, "queue", "task.cascade_error", cand.TaskID, err.Error())
				continue
			}
			if changed {
				s.publish("task_updated", cloneTask(saved))
				s.audit(ctx, "queue", "task.fail", saved.TaskID, "dependency "+cause+" did not succeed")
				frontier = append(frontier, saved.TaskID)
			}
		}
	}
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
