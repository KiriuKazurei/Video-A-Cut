package api

import (
	"net/http"
	"strings"
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
// The pointers are what make the request a partial update. Decoding into
// model.Asset instead would leave every unspecified field at its zero value,
// which the service would read as "set agent_visible to false and clear the
// allow-list" — so a client that touches one switch would quietly clear the
// rest. With a nil pointer meaning "leave alone", the handler passes the
// stored value through for every field the client did not name, and the
// service's absolute-value semantics stop being a hazard.
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

	// A partial body is completed against the stored row before it is
	// handed to the service, because UpdateAssetGovernance treats the four
	// governance fields of its argument as absolute values: it overwrites
	// every stored governance field with whatever the caller's Asset holds,
	// and re-reads only the non-governance ones. So a zero-valued field in
	// the argument means "clear this", not "leave this alone".
	//
	// Reading the row first is what turns that into the partial update the
	// request body describes. It is not a second decision point for any
	// rule — every governance decision still belongs to the service; this
	// is the transport's own bookkeeping for which fields the client named,
	// which is information the service's signature has no way to carry.
	//
	// It also collapses the not-found case into one read: an id that does not
	// exist fails here with the same model.ErrNotFound the update would have
	// raised, so the answer is identical either way.
	//
	// There is no TOCTOU concern in reading first: the service re-reads the
	// row itself before writing, so the copy read here is only ever used to
	// fill fields the client left unnamed, never to decide what the client
	// asked for.
	stored, err := s.svc.GetAsset(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	// Only the fields the client named override the stored values; the rest
	// keep them. The service still re-reads the row and still overwrites
	// everything outside the four governance fields, so this Asset's other
	// members are never written.
	upd := stored
	if req.AgentVisible != nil {
		upd.AgentVisible = *req.AgentVisible
	}
	if req.Locked != nil {
		upd.Locked = *req.Locked
	}
	if req.HumanApproved != nil {
		upd.HumanApproved = *req.HumanApproved
	}
	if req.AllowedAgents != nil {
		upd.AllowedAgents = *req.AllowedAgents
	}

	asset, err := s.svc.UpdateAssetGovernance(r.Context(), humanActor, id, upd)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}
