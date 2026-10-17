package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// VisibleSetView is the current「Agent 可见」set and whether a person has confirmed it.
type VisibleSetView struct {
	AssetIDs       []string             `json:"asset_ids"`
	SHA256         string               `json:"sha256"`
	Confirmed      bool                 `json:"confirmed"`
	ConfirmedSHA   string               `json:"confirmed_sha,omitempty"`
	ConfirmedBy    string               `json:"confirmed_by,omitempty"`
	ConfirmedAt    *time.Time           `json:"confirmed_at,omitempty"`
	ScriptFinalize bool                 `json:"script_finalize"`
	Stale          bool                 `json:"stale"`
	Assets         []model.VisibleAsset `json:"assets"`
}

// AuthorizationGrant is one line the auto-mode authorization page must show.
type AuthorizationGrant struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type loadedSet struct {
	IDs    []string
	SHA    string
	Assets []model.VisibleAsset
}

func fingerprintVisibleSet(ids []string) string {
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:])
}

func loadVisibleSet(ctx context.Context, st *store.Store) (loadedSet, error) {
	all, err := st.ListLibraryAssets(ctx)
	if err != nil {
		return loadedSet{}, err
	}
	ids := []string{}
	visible := []model.VisibleAsset{}
	for _, a := range all {
		if !a.AgentVisible {
			continue
		}
		ids = append(ids, a.AssetID)
		visible = append(visible, a)
	}
	if ids == nil {
		ids = []string{}
	}
	return loadedSet{IDs: ids, SHA: fingerprintVisibleSet(ids), Assets: visible}, nil
}

func (s *Service) VisibleSet(ctx context.Context) (VisibleSetView, error) {
	return s.visibleSetAt(ctx, s.st)
}

func (s *Service) visibleSetAt(ctx context.Context, st *store.Store) (VisibleSetView, error) {
	set, err := loadVisibleSet(ctx, st)
	if err != nil {
		return VisibleSetView{}, err
	}
	view := VisibleSetView{AssetIDs: set.IDs, SHA256: set.SHA, Assets: set.Assets}
	if view.Assets == nil {
		view.Assets = []model.VisibleAsset{}
	}
	conf, err := st.LatestVisibleConfirmation(ctx)
	if err != nil {
		if isNotFound(err) {
			view.Stale = true
			return view, nil
		}
		return VisibleSetView{}, err
	}
	at := conf.CreatedAt.UTC()
	view.ConfirmedSHA = conf.SetSHA
	view.ConfirmedBy = conf.Actor
	view.ConfirmedAt = &at
	view.ScriptFinalize = conf.ScriptFinalize
	view.Confirmed = conf.SetSHA == set.SHA
	view.Stale = !view.Confirmed
	return view, nil
}

func requireHumanConfirm(actor, action string) error {
	if actor == "" {
		return fmt.Errorf("service: %s: actor is required: %w", action, model.ErrArgument)
	}
	if !isHumanActor(actor) {
		return fmt.Errorf("service: %s requires a human actor: %w", action, model.ErrForbidden)
	}
	return nil
}

// ConfirmVisibleSet records a human confirmation of the current visible set.
// system:auto-review and agent actors are refused. scriptFinalize is set only
// by an auto-mode authorization, which is itself a human grant of both layers.
func (s *Service) ConfirmVisibleSet(ctx context.Context, actor, wantSHA string, scriptFinalize bool) (VisibleSetView, error) {
	if err := requireHumanConfirm(actor, "confirm visible set"); err != nil {
		return VisibleSetView{}, err
	}
	if wantSHA == "" {
		return VisibleSetView{}, fmt.Errorf("service: visible_set_sha is required: %w", model.ErrArgument)
	}
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		return s.writeVisibleConfirmation(ctx, tx, actor, wantSHA, scriptFinalize)
	})
	if err != nil {
		return VisibleSetView{}, err
	}
	return s.VisibleSet(ctx)
}

func (s *Service) writeVisibleConfirmation(ctx context.Context, tx *store.Store, actor, wantSHA string, scriptFinalize bool) error {
	set, err := loadVisibleSet(ctx, tx)
	if err != nil {
		return err
	}
	if wantSHA != set.SHA {
		return fmt.Errorf("service: visible set changed: %w", model.ErrConflict)
	}
	now := time.Now().UTC()
	if err := tx.InsertVisibleConfirmation(ctx, model.VisibleConfirmation{
		SetSHA: set.SHA, AssetIDs: set.IDs, Actor: actor, ScriptFinalize: scriptFinalize, CreatedAt: now,
	}); err != nil {
		return err
	}
	return auditTx(ctx, tx, actor, "visible_set.confirm", set.SHA, fmt.Sprintf("script_finalize=%t assets=%s", scriptFinalize, strings.Join(set.IDs, ",")))
}

// requireVisibleConfirmation is the gate for a new manual run and for entering
// the script stage. An in-progress run never calls it: start returns the
// existing run or a conflict before this check.
func (s *Service) requireVisibleConfirmation(ctx context.Context, tx *store.Store, assetID string) error {
	set, err := loadVisibleSet(ctx, tx)
	if err != nil {
		return err
	}
	if !slices.Contains(set.IDs, assetID) {
		return fmt.Errorf("service: asset %s is not in the agent-visible set: %w", assetID, model.ErrForbidden)
	}
	conf, err := tx.LatestVisibleConfirmation(ctx)
	if err != nil {
		if isNotFound(err) {
			return fmt.Errorf("service: visible set is not confirmed: %w", model.ErrForbidden)
		}
		return err
	}
	if conf.SetSHA != set.SHA {
		return fmt.Errorf("service: visible set confirmation is stale: %w", model.ErrForbidden)
	}
	return nil
}

// EnterScript opens a script for one asset. It is refused until a person has
// confirmed the current visible set, and it is refused to auto-review and agents.
func (s *Service) EnterScript(ctx context.Context, actor, assetID string) (model.Script, error) {
	if err := requireHumanConfirm(actor, "enter script"); err != nil {
		return model.Script{}, err
	}
	if !validTaskID(assetID) {
		return model.Script{}, fmt.Errorf("service: enter script: asset id is required: %w", model.ErrArgument)
	}
	var saved model.Script
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		if _, err := tx.GetAsset(ctx, assetID); err != nil {
			return err
		}
		if err := s.requireVisibleConfirmation(ctx, tx, assetID); err != nil {
			return err
		}
		set, err := loadVisibleSet(ctx, tx)
		if err != nil {
			return err
		}
		existing, err := tx.ListScripts(ctx, assetID)
		if err != nil {
			return err
		}
		for i := len(existing) - 1; i >= 0; i-- {
			sc := existing[i]
			if sc.VisibleSetSHA == set.SHA && (sc.Status == model.ScriptDraft || sc.Status == model.ScriptFinalized) {
				saved = sc
				return nil
			}
		}
		now := time.Now().UTC()
		saved = model.Script{
			ScriptID: newID("scr"), AssetID: assetID, Status: model.ScriptDraft,
			VisibleSetSHA: set.SHA, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.InsertScript(ctx, saved); err != nil {
			return err
		}
		return auditTx(ctx, tx, actor, "script.enter", saved.ScriptID, "asset="+assetID+" visible_set="+set.SHA)
	})
	if err != nil {
		return model.Script{}, err
	}
	return saved, nil
}

// FinalizeScript is a human confirmation. Auto-review and agents cannot call it.
func (s *Service) FinalizeScript(ctx context.Context, actor, scriptID string) (model.Script, error) {
	if err := requireHumanConfirm(actor, "finalize script"); err != nil {
		return model.Script{}, err
	}
	if scriptID == "" {
		return model.Script{}, fmt.Errorf("service: finalize script: id is required: %w", model.ErrArgument)
	}
	var saved model.Script
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		sc, err := tx.GetScript(ctx, scriptID)
		if err != nil {
			return err
		}
		if sc.Status != model.ScriptDraft {
			return fmt.Errorf("service: script %s is %s, not a draft: %w", scriptID, sc.Status, model.ErrInvalidState)
		}
		if err := s.requireVisibleConfirmation(ctx, tx, sc.AssetID); err != nil {
			return err
		}
		now := time.Now().UTC()
		sc.Status = model.ScriptFinalized
		sc.FinalizedBy = actor
		sc.FinalizedAt = &now
		sc.UpdatedAt = now
		if err := tx.UpdateScript(ctx, sc); err != nil {
			return err
		}
		saved = sc
		return auditTx(ctx, tx, actor, "script.finalize", scriptID, "asset="+sc.AssetID)
	})
	if err != nil {
		return model.Script{}, err
	}
	return saved, nil
}

func (s *Service) ListScripts(ctx context.Context, assetID string) ([]model.Script, error) {
	out, err := s.st.ListScripts(ctx, assetID)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return []model.Script{}, nil
	}
	return out, nil
}

// BatchSetAgentVisible toggles the Agent-visible layer for many ingested assets.
// A change expires downstream scripts and blocks existing deliveries.
func (s *Service) BatchSetAgentVisible(ctx context.Context, actor string, ids []string, visible bool) ([]model.Asset, error) {
	if err := requireHumanConfirm(actor, "batch visibility"); err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) > 200 {
		return nil, fmt.Errorf("service: batch visibility needs 1 to 200 asset ids: %w", model.ErrArgument)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !validTaskID(id) || seen[id] {
			return nil, fmt.Errorf("service: batch visibility has an invalid asset id: %w", model.ErrArgument)
		}
		seen[id] = true
	}
	var saved []model.Asset
	changed := false
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		for _, id := range ids {
			a, err := tx.GetAsset(ctx, id)
			if err != nil {
				return err
			}
			if a.AgentVisible == visible {
				saved = append(saved, a)
				continue
			}
			a.AgentVisible = visible
			if err := tx.UpdateAsset(ctx, a); err != nil {
				return err
			}
			got, err := tx.GetAsset(ctx, id)
			if err != nil {
				return err
			}
			saved = append(saved, got)
			changed = true
			if err := auditTx(ctx, tx, actor, "asset.governance", id, fmt.Sprintf("agent_visible=%t", visible)); err != nil {
				return err
			}
		}
		if changed {
			return s.expireVisibleDownstream(ctx, tx)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if saved == nil {
		saved = []model.Asset{}
	}
	for _, a := range saved {
		s.publish("asset_updated", cloneAsset(a))
	}
	return saved, nil
}

// expireVisibleDownstream marks scripts stale and blocks downloads of existing
// renders and delivery packages. Scripts are not deleted.
func (s *Service) expireVisibleDownstream(ctx context.Context, tx *store.Store) error {
	now := time.Now().UTC()
	if _, err := tx.ExpireOpenScripts(ctx, now, model.ScriptStaleVisibleSet); err != nil {
		return err
	}
	ids, err := tx.DownstreamAssetIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := tx.UpsertDeliveryInvalidation(ctx, id, model.ScriptStaleVisibleSet, now); err != nil {
			return err
		}
		if err := tx.InvalidatePreviewJobsForAssetExcept(ctx, id, ""); err != nil {
			return err
		}
		if err := tx.InvalidateDeliveryVariantJobsForAssetExcept(ctx, id, ""); err != nil {
			return err
		}
	}
	return auditTx(ctx, tx, "system", "visible_set.expire", "library", fmt.Sprintf("downstream=%d", len(ids)))
}

func (s *Service) deliveryBlocked(ctx context.Context, st *store.Store, assetID string) error {
	reason, blocked, err := st.DeliveryInvalidation(ctx, assetID)
	if err != nil {
		return err
	}
	if !blocked {
		return nil
	}
	return fmt.Errorf("service: delivery for %s is invalidated (%s); render a new version: %w", assetID, reason, model.ErrInvalidState)
}
