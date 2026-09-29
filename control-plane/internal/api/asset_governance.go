package api

import (
	"net/http"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// humanActor is the attribution every human governance write carries while
// the plane has no authentication layer.
//
// Phase 3 introduces Token/OAuth, at which point this constant is replaced by
// the identity the token resolves to. Until then it is deliberately not
// configurable and not taken from a header: a caller-supplied actor would let
// anyone attribute a governance change to anyone else, and the audit log's
// value is exactly that the actor is trustworthy. That the surface listens on
// localhost is what bounds the risk today, not the constant's obscurity.
//
// The "human:" prefix is load-bearing rather than cosmetic: the retention
// sweep classifies rows by it, and an agent-prefixed actor would be archived
// as agent traffic instead of kept as a governance record.
const humanActor = "human:webui"

// patchAssetRequest is the body of PATCH /api/assets/{id}: the four governance
// fields and nothing else, each as a pointer so "absent" and "set to zero
// value" are distinguishable.
//
// The pointers are what make the request a partial update. The service merges
// them against the row inside its SQLite transaction, so absent fields keep
// their latest committed value even when concurrent PATCH requests update
// different governance fields.
//
// The type is closed on purpose. decodeJSON sets DisallowUnknownFields, so a
// body carrying status, asset_id, artifacts or updated_at is refused outright
// rather than silently ignored: a PATCH that cannot name a field cannot change
// it, and the pipeline state is not reachable through this URL at all. The
// service re-reads the stored row and overwrites everything outside these
// four fields anyway, so the type is a belt to that braces — but a client that
// sends a bogus field is a client with a bug, and silence would hide it.
type patchAssetRequest struct {
	AgentVisible  *bool     `json:"agent_visible"`
	Locked        *bool     `json:"locked"`
	HumanApproved *bool     `json:"human_approved"`
	AllowedAgents *[]string `json:"allowed_agents"`
}

// patchAsset answers PATCH /api/assets/{id} with the updated asset.
//
// It is the human governance write surface (docs §11.3): visibility, lock,
// approval and the allowed-agents list. It holds no rule of its own — the
// request type decides what is settable, the service decides whether the
// change is legal, stores it, publishes and audits it, and this handler
// decodes, delegates and encodes.
//
// The response is 200 with the whole stored asset rather than 204 without a
// body: the WebUI renders its governance row from this answer, and a 204
// would force a re-GET per click. It is also the caller's only way to learn
// the canonical form of what it wrote — the allowed-agents list is stored
// deduplicated and sorted, and that normalized shape is what the response
// carries.
//
// Approval is not routed through service.ApproveAsset: this endpoint calls
// UpdateAssetGovernance for all four fields, so one request writes one audit
// row. Calling both would double-write for a single click.
func (s *Server) patchAsset(w http.ResponseWriter, r *http.Request) {
	// The id guard is identical to getAsset's, including why it exists:
	// r.PathValue returns the URL-decoded segment, so an escaped separator
	// (a%2Fb) reaches this handler as "a/b" and must be refused here rather
	// than passed to the store as a lookup key.
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, codeArgument, "asset id is required")
		return
	}
	if strings.ContainsAny(id, "/\\") {
		writeError(w, http.StatusBadRequest, codeArgument,
			"asset id must not contain a path separator")
		return
	}

	var req patchAssetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		// decodeJSON has already classified the failure: a malformed or
		// empty body is model.ErrArgument, an oversize one errBodyTooLarge,
		// so the mapping to 400 and 413 is done in one place.
		writeServiceError(w, err)
		return
	}

	asset, err := s.svc.PatchAssetGovernance(r.Context(), humanActor, id, service.GovernancePatch{
		AgentVisible:  req.AgentVisible,
		Locked:        req.Locked,
		HumanApproved: req.HumanApproved,
		AllowedAgents: req.AllowedAgents,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}
