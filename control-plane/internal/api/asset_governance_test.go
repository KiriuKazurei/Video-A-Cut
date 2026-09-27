package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// patch sends one PATCH with a raw body through the router of an
// already-seeded server and returns the recorded response.
//
// The body is a string rather than a struct so the malformed and the
// unknown-field cases can be spelled exactly as a client would send them: a
// struct literal would refuse to compile the very payload the handler is
// required to reject.
func patch(t *testing.T, srv *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body)))
	return rec
}

// decodeAsset decodes a 200 response body as one asset.
//
// The body is the bare asset the governance screen renders, so a JSON null
// must be reported rather than decoded into a zero Asset: every assertion
// built on a zero value would silently "pass" against a body that carried
// nothing.
func decodeAsset(t *testing.T, rec *httptest.ResponseRecorder) model.Asset {
	t.Helper()
	if body := strings.TrimSpace(rec.Body.String()); !strings.HasPrefix(body, "{") {
		t.Fatalf("body is not a JSON object: %q", rec.Body.String())
	}
	var out model.Asset
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not a JSON asset: %v (%q)", err, rec.Body.String())
	}
	return out
}

// governanceAsset is a stored row that carries every field a partial PATCH
// must leave alone: engine state (status, artifacts) plus a governance
// allow-list and flags the request body never mentions.
//
// Seeding a bare zero asset would let a handler that wipes every unspecified
// field pass, because wiping a zero value changes nothing observable.
func governanceAsset() model.Asset {
	return model.Asset{
		AssetID:       "a_1",
		Status:        model.AssetStatusIngested,
		AgentVisible:  false,
		HumanApproved: false,
		Locked:        false,
		AllowedAgents: []string{"agent_a"},
		Artifacts:     map[string]string{"edl": "clips/a_1/edl.json"},
	}
}

// TestPatchAssetFlipsVisibility proves PATCH /api/assets/{id} turns agent
// visibility on and returns the updated row.
//
// The response must be the whole asset, not an echo of the request: the WebUI
// renders the governance screen from it, and an answer that carried only the
// fields the client sent would make the UI re-GET (and could not render the
// row it just changed without one).
func TestPatchAssetFlipsVisibility(t *testing.T) {
	srv := newSeededServer(t, governanceAsset())

	rec := patch(t, srv, "/api/assets/a_1", `{"agent_visible":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	got := decodeAsset(t, rec)
	if !got.AgentVisible {
		t.Fatalf("agent_visible = false, want true — the PATCH was not applied: %+v", got)
	}
	if got.Locked || got.HumanApproved {
		t.Fatalf("flags the body never mentioned were altered: %+v", got)
	}
	if got.Status != model.AssetStatusIngested {
		t.Fatalf("status = %q, want %q left untouched — pipeline state is not governance state",
			got.Status, model.AssetStatusIngested)
	}
	if got.Artifacts["edl"] != "clips/a_1/edl.json" {
		t.Fatalf("artifacts = %v, want the stored map left untouched", got.Artifacts)
	}
	if got.AssetID != "a_1" {
		t.Fatalf("asset_id = %q, want a_1 — a PATCH body must not be able to re-target the row", got.AssetID)
	}
	if joined := strings.Join(got.AllowedAgents, ","); joined != "agent_a" {
		t.Fatalf("allowed_agents = %v, want [agent_a] left untouched — a partial body must not wipe the allow-list",
			got.AllowedAgents)
	}

	// The change is persisted, not just echoed: the row is read back through
	// the same GET the WebUI issues on reload.
	stored := decodeAsset(t, get(t, srv, "/api/assets/a_1"))
	if !stored.AgentVisible {
		t.Fatalf("stored agent_visible = false, want true — the update did not reach the store")
	}
}

// TestPatchAssetLocksAsset proves the lock switch works through the same
// endpoint.
//
// Locking is what hides an asset from agents even while agent_visible is true
// (service.ListVisibleAssets), so the flag has to be settable from the human
// side on its own.
func TestPatchAssetLocksAsset(t *testing.T) {
	srv := newSeededServer(t, governanceAsset())

	rec := patch(t, srv, "/api/assets/a_1", `{"locked":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	got := decodeAsset(t, rec)
	if !got.Locked {
		t.Fatalf("locked = false, want true: %+v", got)
	}
	if got.AgentVisible {
		t.Fatalf("agent_visible = true, want false left untouched: %+v", got)
	}
	if joined := strings.Join(got.AllowedAgents, ","); joined != "agent_a" {
		t.Fatalf("allowed_agents = %v, want [agent_a] left untouched", got.AllowedAgents)
	}
}

// TestPatchAssetApprovesAsset proves human approval is settable through the
// governance endpoint (docs §11.3).
//
// Approval goes through UpdateAssetGovernance rather than ApproveAsset: both
// write, and calling both from one handler would write two audit rows for one
// click.
func TestPatchAssetApprovesAsset(t *testing.T) {
	srv := newSeededServer(t, governanceAsset())

	rec := patch(t, srv, "/api/assets/a_1", `{"human_approved":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	got := decodeAsset(t, rec)
	if !got.HumanApproved {
		t.Fatalf("human_approved = false, want true: %+v", got)
	}
	if joined := strings.Join(got.AllowedAgents, ","); joined != "agent_a" {
		t.Fatalf("allowed_agents = %v, want [agent_a] left untouched", got.AllowedAgents)
	}
}

// TestPatchAssetSetsAllowedAgents proves the allow-list is accepted and comes
// back in the canonical shape the store keeps.
//
// The request carries duplicates and a non-sorted order on purpose: the
// service normalizes before writing, and the response is the stored row, so
// the client learns the canonical form from the same answer that applied it.
func TestPatchAssetSetsAllowedAgents(t *testing.T) {
	srv := newSeededServer(t, governanceAsset())

	rec := patch(t, srv, "/api/assets/a_1", `{"allowed_agents":["b","a","b"]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	got := decodeAsset(t, rec)
	if joined := strings.Join(got.AllowedAgents, ","); joined != "a,b" {
		t.Fatalf("allowed_agents = %v, want the deduplicated sorted [a b]", got.AllowedAgents)
	}
}

// TestPatchAssetUnknownReturns404 proves a well-formed but unknown id is
// answered with the machine-readable not_found code, not a 500.
func TestPatchAssetUnknownReturns404(t *testing.T) {
	srv := newServer(t)

	rec := patch(t, srv, "/api/assets/nope", `{"agent_visible":true}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := decodeErr(t, rec).Code; got != codeNotFound {
		t.Fatalf("error code = %q, want %q", got, codeNotFound)
	}
}

// TestPatchAssetEmptyBodyIsNoOp proves an empty object changes nothing and
// leaves the audit log alone.
//
// A body of {} names no governance field, so every field keeps its stored
// value and the service's idempotence check must see a request that asks for
// the state already stored. This is the case a WebUI sends when it patches
// one field and serializes the whole row minus the fields it does not manage:
// an empty answer that flipped flags to false would clear a lock on refresh.
func TestPatchAssetEmptyBodyIsNoOp(t *testing.T) {
	stored := governanceAsset()
	stored.Locked = true
	stored.HumanApproved = true
	stored.AgentVisible = true
	srv := newSeededServer(t, stored)

	before, err := srv.svc.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAudit before: %v", err)
	}

	rec := patch(t, srv, "/api/assets/a_1", `{}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	got := decodeAsset(t, rec)
	if !got.AgentVisible || !got.Locked || !got.HumanApproved {
		t.Fatalf("an empty body cleared governance flags: %+v", got)
	}
	if joined := strings.Join(got.AllowedAgents, ","); joined != "agent_a" {
		t.Fatalf("allowed_agents = %v, want [agent_a] left untouched", got.AllowedAgents)
	}

	after, err := srv.svc.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAudit after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("audit rows = %d, want %d — a no-op must not write a row", len(after), len(before))
	}
}

// TestPatchAssetRejectsUnknownFields proves the request type is closed, which
// is the security point of this endpoint.
//
// The shared decoder refuses unknown fields, so a body that carries a field
// the governance request does not declare is a 400 rather than a silently
// ignored key. status is the case that matters: a handler decoding into
// model.Asset would accept it, and the pipeline state would become settable
// over REST by anyone who can reach the governance surface.
func TestPatchAssetRejectsUnknownFields(t *testing.T) {
	srv := newSeededServer(t, governanceAsset())

	rec := patch(t, srv, "/api/assets/a_1", `{"agent_visible":true,"status":"hacked"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d — a non-governance field must not be accepted", rec.Code, http.StatusBadRequest)
	}
	if got := decodeErr(t, rec).Code; got != codeArgument {
		t.Fatalf("error code = %q, want %q", got, codeArgument)
	}

	// The rejection is total: nothing was written.
	stored := decodeAsset(t, get(t, srv, "/api/assets/a_1"))
	if stored.Status != model.AssetStatusIngested || stored.AgentVisible {
		t.Fatalf("a rejected body still changed the row: %+v", stored)
	}
}

// TestPatchAssetRejectsBadBody proves a malformed body is a client error with
// the invalid_argument code, not a 500 and not a silent no-op.
func TestPatchAssetRejectsBadBody(t *testing.T) {
	srv := newSeededServer(t, governanceAsset())

	rec := patch(t, srv, "/api/assets/a_1", `not json`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := decodeErr(t, rec).Code; got != codeArgument {
		t.Fatalf("error code = %q, want %q", got, codeArgument)
	}
}

// TestPatchAssetRejectsPathSeparator proves an id carrying an escaped
// separator is refused as invalid_argument instead of reaching the store.
//
// ServeMux matches the unescaped segment, so a%2Fb arrives here as "a/b": the
// guard is the handler's, not the router's. The ids used here exist in no
// store, so a 404 would mean the value was forwarded.
func TestPatchAssetRejectsPathSeparator(t *testing.T) {
	srv := newServer(t)

	for _, path := range []string{
		"/api/assets/a%2Fb",
		"/api/assets/a%5Cb",
		"/api/assets/..%2F..%2Fetc%2Fpasswd",
	} {
		t.Run(path, func(t *testing.T) {
			rec := patch(t, srv, path, `{"locked":true}`)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d — a separator in the id must not reach the store",
					rec.Code, http.StatusBadRequest)
			}
			if got := decodeErr(t, rec).Code; got != codeArgument {
				t.Fatalf("error code = %q, want %q", got, codeArgument)
			}
		})
	}
}

// TestPatchAssetWritesAudit proves a governance change is attributable.
//
// The actor must be a human one: the retention sweep treats an "agent:"-prefixed
// actor as agent traffic and archives it on a different schedule, so a
// governance action recorded under an agent identity would be filed as machine
// chatter. This is the assertion that fails if someone replaces the fixed
// actor with a placeholder that does not carry the human: prefix.
func TestPatchAssetWritesAudit(t *testing.T) {
	srv := newSeededServer(t, governanceAsset())

	rec := patch(t, srv, "/api/assets/a_1", `{"locked":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	rows, err := srv.svc.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}

	var found *model.AuditLog
	for i := range rows {
		if rows[i].Action == "asset.governance" {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no asset.governance audit row after a successful PATCH: %+v", rows)
	}
	if !strings.HasPrefix(found.Actor, "human:") {
		t.Fatalf("actor = %q, want a human: prefix — an agent: actor is archived as machine traffic", found.Actor)
	}
	if found.Target != "a_1" {
		t.Fatalf("target = %q, want a_1", found.Target)
	}
}
