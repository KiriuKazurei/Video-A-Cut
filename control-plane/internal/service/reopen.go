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
func (s *Service) ReopenAsset(ctx context.Context, actor, assetID string) (model.Asset, error) {
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
