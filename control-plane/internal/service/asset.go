package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

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

// ListAllAssets returns every asset, with no visibility filtering.
//
// This is the counterpart to ListVisibleAssets and the two are not two
// implementations of one rule (docs/项目开发文档.md §11.2, §11.3). The agent
// view answers "what may this role see" and is the only decision point of
// that question — every agent-facing transport still routes through it and
// nothing here may be consulted by one. The human governance view answers a
// different question, "what exists": an operator who is about to hide, lock
// or release an asset must be able to see it, so a governance screen that
// filtered by agent visibility would hide precisely the rows it exists to
// change. Read access here is only ever reachable from the authenticated
// WebUI surface, which is already the human side of §11.3.
//
// The result is a non-nil slice so callers can range over an empty
// governance view without a nil check, and it is ordered by asset_id
// because the store orders it there.
func (s *Service) ListAllAssets(ctx context.Context) ([]model.Asset, error) {
	all, err := s.st.ListAssets(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: list all assets: %w", err)
	}
	if all == nil {
		return []model.Asset{}, nil
	}
	return all, nil
}

// UpdateAssetGovernance is the single write entry point for the human
// governance fields of an asset (docs §11.3): agent visibility, lock, human
// approval and the allowed-agents list. It is reachable from the WebUI over
// REST only; the agent-side MCP surface never exposes it, because these are
// exactly the switches that decide what an agent is allowed to see.
//
// The caller passes a whole model.Asset, which is what a REST handler already
// holds after decoding the request body. Only the four governance fields are
// honoured: the row is re-read and every other column keeps its stored value.
// That is what makes the entry point safe to hand a full object — a stale body
// carrying an old status, a bogus AssetID or a garbage artifacts map cannot
// move the pipeline backwards, cannot re-target the update at another row and
// cannot rewrite the ingest output, because none of those are governance
// state and this method does not write them.
//
// An empty actor is rejected with model.ErrArgument before any store access:
// every other field of this write is recoverable from the stored row, but an
// unattributable change is not, and an audit row that cannot say who did it
// is worthless. A missing asset is reported as model.ErrNotFound.
//
// An update that asks for the state already stored is a no-op. It returns
// that state without writing, publishing or auditing, so a double click or a
// retried request cannot push a duplicate asset_updated to every SSE
// subscriber or a second indistinguishable "who changed this" row into the
// log. The comparison includes the normalized allowed-agents list, so a
// caller that sends an unsorted list gets one audit row the first time and
// silence the second.
//
// AllowedAgents is stored deduplicated and sorted. The value is compared as
// text in the audit log and rendered by the governance UI, and two states
// that differ only by order or a repeated entry must not look like two
// different states to either.
func (s *Service) UpdateAssetGovernance(
	ctx context.Context, actor, assetID string, upd model.Asset,
) (model.Asset, error) {
	if actor == "" {
		return model.Asset{}, fmt.Errorf("service: update asset governance %s: actor is required: %w",
			assetID, model.ErrArgument)
	}

	a, err := s.st.GetAsset(ctx, assetID)
	if err != nil {
		return model.Asset{}, fmt.Errorf("service: update asset governance %s: %w", assetID, err)
	}

	agents := normalizeAllowedAgents(upd.AllowedAgents)
	if a.AgentVisible == upd.AgentVisible &&
		a.Locked == upd.Locked &&
		a.HumanApproved == upd.HumanApproved &&
		slices.Equal(a.AllowedAgents, agents) {
		// Nothing changes, so nothing is written, published or audited. The
		// normalized list is still handed back so the caller sees the same
		// canonical shape the write path would have stored.
		a.AllowedAgents = agents
		return a, nil
	}

	// Taken before the mutation: the audit detail is a diff against what the
	// row actually held, not against what it is about to hold.
	before := a.AllowedAgents
	a.AgentVisible = upd.AgentVisible
	a.Locked = upd.Locked
	a.HumanApproved = upd.HumanApproved
	a.AllowedAgents = agents

	if err := s.st.UpdateAsset(ctx, a); err != nil {
		return model.Asset{}, fmt.Errorf("service: update asset governance %s: %w", assetID, err)
	}

	s.publish("asset_updated", a)
	s.audit(ctx, actor, "asset.governance", assetID, governanceDetail(upd, before))
	return a, nil
}

// normalizeAllowedAgents deduplicates and sorts an allowed-agents list. A nil
// or empty input yields an empty non-nil slice so the stored and returned
// forms never differ by emptiness representation.
func normalizeAllowedAgents(agents []string) []string {
	out := make([]string, 0, len(agents))
	for _, ag := range agents {
		if ag != "" && !slices.Contains(out, ag) {
			out = append(out, ag)
		}
	}
	slices.Sort(out)
	return out
}

// governanceDetail names the fields the update changed, in a fixed order so
// two identical changes always read identically in the log. The booleans are
// written as explicit true/false rather than skipped when they match the
// previous value: an audit line that omits a field cannot be told apart from
// one where that field was never touched. The allowed-agents list is shown as
// the entries the update added, prefixed by "+", and the ones it removed,
// prefixed by "-"; an unchanged list contributes nothing.
func governanceDetail(upd model.Asset, previousAgents []string) string {
	parts := []string{
		fmt.Sprintf("agent_visible=%t", upd.AgentVisible),
		fmt.Sprintf("locked=%t", upd.Locked),
		fmt.Sprintf("human_approved=%t", upd.HumanApproved),
	}
	if d := agentListDiff(previousAgents, upd.AllowedAgents); d != "" {
		parts = append(parts, "allowed_agents="+d)
	}
	return strings.Join(parts, " ")
}

// agentListDiff reports what added and removed between the stored allowed
// agents and the incoming ones, as "+a" / "-c" tokens joined by commas. Both
// sides are normalized first, so the diff describes a real state change and
// never a reordering. An empty string means the list did not change.
func agentListDiff(stored, incoming []string) string {
	before := normalizeAllowedAgents(stored)
	after := normalizeAllowedAgents(incoming)

	var tokens []string
	for _, ag := range after {
		if !slices.Contains(before, ag) {
			tokens = append(tokens, "+"+ag)
		}
	}
	for _, ag := range before {
		if !slices.Contains(after, ag) {
			tokens = append(tokens, "-"+ag)
		}
	}
	return strings.Join(tokens, ",")
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
