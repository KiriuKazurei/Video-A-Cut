package service

import (
	"context"
	"fmt"
	"path"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// ReopenAsset is a human governance action: it takes an exported asset back
// to ingested so the pipeline can run another round on it.
//
// The current package is re-validated with the import rules and becomes the
// new source. This only works because the timeline CLI stores a
// self-contained edl.json whose sources live inside the package; the
// validation below would otherwise pass while the next build failed on
// missing media, so the edl is also required to be present in the package.
//
// Governance fields (visibility, lock, approval, allowed roles) are kept:
// reopening is not a reason to widen or narrow who may work on the asset.
// Active tasks on the asset block the reopen, since a running stage would
// otherwise finish against a package the human just replaced.
//
// A person reopening an asset whose auto run is still live takes that run over
// (it halts as human_takeover) and the reopen goes on. The same reopen coming
// from startWorkflow (fromStart) is refused instead, so that starting a run
// can never flip the asset under a live auto run.
func (s *Service) ReopenAsset(ctx context.Context, actor, assetID string) (model.Asset, error) {
	return s.reopenAsset(ctx, actor, assetID, false)
}

func (s *Service) reopenAsset(ctx context.Context, actor, assetID string, fromStart bool) (model.Asset, error) {
	if actor == "" {
		return model.Asset{}, fmt.Errorf("service: reopen asset %s: actor is required: %w", assetID, model.ErrArgument)
	}
	current, err := s.st.GetAsset(ctx, assetID)
	if err != nil {
		return model.Asset{}, fmt.Errorf("service: reopen asset %s: %w", assetID, err)
	}
	manifest := current.Artifacts["manifest"]
	if current.Status != model.AssetStatusExported || manifest == "" {
		return model.Asset{}, fmt.Errorf("service: reopen asset %s: only an exported package can be reopened: %w",
			assetID, model.ErrInvalidState)
	}
	artifacts, err := s.validatePackage(path.Dir(manifest))
	if err != nil {
		return model.Asset{}, fmt.Errorf("service: reopen asset %s: %w", assetID, err)
	}
	if artifacts["edl"] == "" {
		return model.Asset{}, fmt.Errorf("service: reopen asset %s: package has no edl: %w", assetID, model.ErrInvalidState)
	}
	var saved model.Asset
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		a, err := tx.GetAsset(ctx, assetID)
		if err != nil {
			return err
		}
		if a.Status != model.AssetStatusExported || a.Artifacts["manifest"] != manifest {
			return fmt.Errorf("asset changed while reopening: %w", model.ErrConflict)
		}
		if liveRun, live, err := tx.ActiveAutoForAsset(ctx, assetID); err != nil {
			return err
		} else if live {
			if fromStart {
				return fmt.Errorf("asset has auto run %s in progress: %w", liveRun, model.ErrConflict)
			}
			if err := autoTakeover(ctx, tx, liveRun, actor, model.AutoHaltHumanTakeover, "reopen"); err != nil {
				return err
			}
		}
		active, err := tx.ListActiveTasks(ctx)
		if err != nil {
			return err
		}
		for _, tk := range active {
			if tk.AssetID == assetID && tk.Status != model.TaskStatusQueued {
				return fmt.Errorf("task %s is %s on this asset: %w", tk.TaskID, tk.Status, model.ErrInvalidState)
			}
		}
		a.Artifacts = artifacts
		a.Status = model.AssetStatusIngested
		if err := tx.UpdateAsset(ctx, a); err != nil {
			return err
		}
		if err := tx.InvalidatePreviewJobsForAssetExcept(ctx, assetID, ""); err != nil {
			return err
		}
		if err := tx.InvalidateDeliveryVariantJobsForAssetExcept(ctx, assetID, ""); err != nil {
			return err
		}
		for _, task := range active {
			if task.AssetID == assetID && task.Type == model.TaskTypeDeliveryVariant {
				task.Status = model.TaskStatusCancelled
				task.LeaseUntil = nil
				task.Message = "资产重新打开，字幕交付任务已取消"
				if err := tx.UpdateTask(ctx, task); err != nil {
					return err
				}
			}
		}
		saved, err = tx.GetAsset(ctx, assetID)
		return err
	})
	if err != nil {
		return model.Asset{}, fmt.Errorf("service: reopen asset %s: %w", assetID, err)
	}
	s.publish("asset_updated", cloneAsset(saved))
	s.audit(ctx, actor, "asset.reopen", assetID, path.Dir(manifest))
	return cloneAsset(saved), nil
}
