package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// CreateAsset registers a new asset.
//
// An empty AssetID is rejected with model.ErrArgument and an id that already
// exists with model.ErrConflict; the conflict is caught by the pre-check, not
// by the database, so a duplicate costs one SELECT. The store stamps
// CreatedAt and UpdatedAt, so a caller-supplied timestamp is ignored. On
// success the asset is announced as asset_created and recorded in the audit
// log under the "system" actor, because an ingest is automatic rather than a
// human governance action.
func (s *Service) CreateAsset(ctx context.Context, a model.Asset) error {
	if a.AssetID == "" {
		return fmt.Errorf("service: create asset: asset_id is required: %w", model.ErrArgument)
	}

	if _, err := s.st.GetAsset(ctx, a.AssetID); err == nil {
		return fmt.Errorf("service: create asset %s: %w", a.AssetID, model.ErrConflict)
	} else if !isNotFound(err) {
		return fmt.Errorf("service: create asset %s: %w", a.AssetID, err)
	}

	if err := s.st.CreateAsset(ctx, a); err != nil {
		return err
	}

	s.publish("asset_created", a)
	s.audit(ctx, "system", "asset.create", a.AssetID, "")
	return nil
}

// ListVisibleAssets returns the assets one role may see.
//
// This is the sole visibility decision point of the control plane
// (docs/项目开发文档.md §11.2): the only implementation of "what may an agent
// see". REST, MCP and SSE must all route their asset listings through here
// instead of filtering themselves. A second filter, in any handler, would
// drift from this one the first time the rules change — and a second listing
// path that forgets the filter entirely publishes a locked or foreign asset to
// an agent. Nothing here depends on the file system: agents never read disk
// directly, every read is mediated by the control plane.
//
// An asset is visible to a role when it is flagged for agents, is not locked
// and the role is on its allowed-agents list. Locked assets stay hidden even
// when the flags allow them, so a lock always wins.
//
// An empty role is anonymous and therefore sees nothing: it returns an empty
// slice, not an error. Every result, including a no-match, is a non-nil slice
// so callers can range over it without a nil check.
func (s *Service) ListVisibleAssets(ctx context.Context, role string) ([]model.Asset, error) {
	if role == "" {
		return []model.Asset{}, nil
	}

	all, err := s.st.ListAssets(ctx)
	if err != nil {
		return nil, err
	}

	out := []model.Asset{}
	for _, a := range all {
		if !a.AgentVisible || a.Locked {
			continue
		}
		if !slices.Contains(a.AllowedAgents, role) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// ApproveAsset is a human governance action (docs §11.3): it flips the
// HumanApproved flag of one asset. It is reachable from the WebUI over REST
// only, never through the agent-side MCP surface.
//
// An empty actor is rejected with model.ErrArgument, because an approval must
// always be attributable. Repeating a call whose outcome is already in place
// is a no-op — it returns nil without re-publishing or writing a second audit
// entry, so a double click or a retried request cannot spam either channel.
func (s *Service) ApproveAsset(ctx context.Context, actor, assetID string, approved bool) error {
	if actor == "" {
		return fmt.Errorf("service: approve asset %s: actor is required: %w", assetID, model.ErrArgument)
	}

	a, err := s.st.GetAsset(ctx, assetID)
	if err != nil {
		return fmt.Errorf("service: approve asset %s: %w", assetID, err)
	}
	if a.HumanApproved == approved {
		return nil
	}

	a.HumanApproved = approved
	if err := s.st.UpdateAsset(ctx, a); err != nil {
		return fmt.Errorf("service: approve asset %s: %w", assetID, err)
	}

	s.publish("asset_updated", a)
	s.audit(ctx, actor, approveAction(approved), assetID, "")
	return nil
}

// approveAction names the audit entry for a governance outcome.
func approveAction(approved bool) string {
	if approved {
		return "asset.approve"
	}
	return "asset.unapprove"
}

// GetAsset returns one asset by id.
//
// It is a thin pass-through to the store: handlers need the raw asset to
// render governance views, where the caller has already been authenticated as
// a human operator. It deliberately does not apply the visibility rules —
// agents must call ListVisibleAssets instead.
func (s *Service) GetAsset(ctx context.Context, id string) (model.Asset, error) {
	return s.st.GetAsset(ctx, id)
}

// isNotFound reports whether err came from a missing row.
func isNotFound(err error) bool {
	return errors.Is(err, model.ErrNotFound)
}
