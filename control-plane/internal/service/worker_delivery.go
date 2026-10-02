package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// packageStatus maps each pipeline task type to the asset status its package
// establishes. Every stage hands over a whole package (edl.json, manifest and
// the media the edl references), so the asset always points at one
// self-contained, listable, downloadable directory.
var packageStatus = map[string]string{
	model.TaskTypeRecognize: model.AssetStatusRecognized,
	model.TaskTypeSort:      model.AssetStatusRecognized,
	model.TaskTypeNarrate:   model.AssetStatusNarrated,
	model.TaskTypeTTS:       model.AssetStatusNarrated,
	model.TaskTypeSubtitle:  model.AssetStatusNarrated,
	model.TaskTypeMix:       model.AssetStatusNarrated,
	model.TaskTypeExport:    model.AssetStatusExported,
}

// SubmitDelivery closes a pipeline task by registering a package the worker
// wrote below the delivery root.
//
// The worker never names filesystem paths the control plane trusts: it names
// a root-relative package directory, and the same validation used by human
// import (manifest schema, per-file containment, symlink resolution) decides
// what gets recorded. Task closure and the asset's artifact map change in one
// transaction, so the WebUI never sees a succeeded export without files or
// files attached to a task that is still running.
//
// Package validation runs before the transaction because it only reads the
// filesystem; the ownership, lease and state checks run inside it. A retried
// submission of a task that already succeeded is a no-op, matching
// SubmitResult.
func (s *Service) SubmitDelivery(ctx context.Context, agentID, taskID, packageDir string) error {
	artifacts, err := s.validatePackage(filepath.FromSlash(packageDir))
	if err != nil {
		return fmt.Errorf("service: submit delivery on task %s: %w", taskID, err)
	}
	var savedTask model.Task
	var savedAsset model.Asset
	changed := false
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		tk, err := guardTaskMutation(ctx, tx, agentID, taskID)
		if err != nil {
			return err
		}
		if tk.Status == model.TaskStatusSucceeded {
			return nil
		}
		if err := s.WorkflowBlocks(ctx, tx, tk.TaskID); err != nil {
			return err
		}
		nextStatus, ok := packageStatus[tk.Type]
		if !ok {
			return fmt.Errorf("service: submit delivery on task %s: type %q does not produce a package: %w",
				taskID, tk.Type, model.ErrArgument)
		}
		if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
			return fmt.Errorf("service: submit delivery on task %s: status %q is not active: %w",
				taskID, tk.Status, model.ErrInvalidState)
		}
		if leaseExpired(tk) {
			return fmt.Errorf("service: submit delivery on task %s: %w", taskID, model.ErrLeaseExpired)
		}
		asset, err := tx.GetAsset(ctx, tk.AssetID)
		if err != nil {
			return fmt.Errorf("service: submit delivery on task %s asset: %w", taskID, err)
		}
		// Governance may have changed since the claim; a locked or hidden
		// asset must not receive new files from an agent.
		if !assetVisibleForRole(asset, tk.AgentRole) {
			return fmt.Errorf("service: submit delivery on task %s: asset not visible: %w", taskID, model.ErrForbidden)
		}
		// The package replaces the asset's artifact map rather than merging
		// into it. Delivery listing resolves every key relative to the one
		// registered manifest; keys left over from the source package would
		// point outside the new package and fail the whole listing. The CLI
		// copies source media into the package, so nothing is lost.
		if artifacts["edl"] == "" {
			return fmt.Errorf("service: submit delivery on task %s: package declares no edl: %w", taskID, model.ErrArgument)
		}
		asset.Artifacts = cloneStringMap(artifacts)
		asset.Status = nextStatus
		if err := tx.UpdateAsset(ctx, asset); err != nil {
			return fmt.Errorf("service: submit delivery on task %s asset: %w", taskID, err)
		}

		tk.Status = model.TaskStatusSucceeded
		tk.Progress = 1
		tk.Artifacts = cloneStringMap(artifacts)
		tk.LeaseUntil = nil
		if err := tx.UpdateTask(ctx, tk); err != nil {
			return fmt.Errorf("service: submit delivery on task %s: %w", taskID, err)
		}
		if savedTask, err = tx.GetTask(ctx, taskID); err != nil {
			return err
		}
		if savedAsset, err = tx.GetAsset(ctx, tk.AssetID); err != nil {
			return err
		}
		if err := s.NoteWorkflowSuccess(ctx, tx, tk, packageDir); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		if s.WorkflowBlocks(ctx, s.st, taskID) != nil {
			s.audit(ctx, "agent:"+agentID, "stale_delivery.reject", taskID, err.Error())
		}
		return err
	}
	if changed {
		s.publishTaskWorkflow(ctx, taskID)
		s.publish("task_updated", cloneTask(savedTask))
		s.publish("asset_updated", cloneAsset(savedAsset))
		s.audit(ctx, "agent:"+agentID, "task.submit", taskID, "delivery:"+filepath.ToSlash(packageDir))
	}
	return nil
}
