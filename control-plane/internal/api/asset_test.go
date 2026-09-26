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

// newSeededServer returns a Server whose backing store already holds the
// given assets.
//
// The service is the real one newServer assembles, so seeding goes through
// service.CreateAsset exactly as an ingest would. A test that instead reached
// into the store would compile and pass while the handler called a different
// path than production does.
func newSeededServer(t *testing.T, assets ...model.Asset) *Server {
	t.Helper()
	srv := newServer(t)
	for _, a := range assets {
		if err := srv.svc.CreateAsset(context.Background(), a); err != nil {
			t.Fatalf("create asset %s: %v", a.AssetID, err)
		}
	}
	return srv
}

// get sends one GET through the router of an already-seeded server and returns
// the recorded response.
func get(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// decodeAssets decodes a response body as the bare JSON array of assets the
// list endpoint returns.
//
// A JSON null or an object decodes without error into the slice, so the body
// shape is checked as text too: "null" and {"assets":[...]} would both decode
// into a zero or empty slice and slip past a length assertion alone.
func decodeAssets(t *testing.T, rec *httptest.ResponseRecorder) []model.Asset {
	t.Helper()
	body := strings.TrimSpace(rec.Body.String())
	if !strings.HasPrefix(body, "[") {
		t.Fatalf("body does not start a JSON array: %q", rec.Body.String())
	}
	var out []model.Asset
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not a JSON array of assets: %v (%q)", err, rec.Body.String())
	}
	if out == nil {
		t.Fatalf("array decoded to nil, want at least an empty slice: %q", body)
	}
	return out
}

// TestListAssetsReturnsHiddenAssets proves the human governance view reports
// every row, not the agent-visible subset.
//
// The two views are deliberately different questions (§11.2, §11.3): the agent
// view answers "what may this role see" and the human view answers "what
// exists". A governance screen that filtered by agent visibility would hide
// precisely the rows it exists to hide, lock and release, so the row with
// agent_visible=false and the locked row must both come back.
func TestListAssetsReturnsHiddenAssets(t *testing.T) {
	srv := newSeededServer(t,
		model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"agent"}},
		model.Asset{AssetID: "a_2", Status: model.AssetStatusIngested},
		model.Asset{AssetID: "a_3", Status: model.AssetStatusIngested, AgentVisible: true, Locked: true},
	)

	rec := get(t, srv, "/api/assets")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	got := decodeAssets(t, rec)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 — the governance view must not filter by agent visibility", len(got))
	}
	ids := make([]string, 0, len(got))
	for _, a := range got {
		ids = append(ids, a.AssetID)
	}
	if strings.Join(ids, ",") != "a_1,a_2,a_3" {
		t.Fatalf("ids = %v, want [a_1 a_2 a_3] in asset_id order", ids)
	}
}

// TestListAssetsEmptyIsJSONArray pins the empty result as [] rather than null.
//
// A null body forces every client to nil-check before it can range over the
// response, and a JSON null is what a nil Go slice encodes to. The service
// already promises a non-nil slice; this test is the transport's half of that
// promise, so a later refactor that lets the nil through is caught here rather
// than in a browser console.
func TestListAssetsEmptyIsJSONArray(t *testing.T) {
	srv := newServer(t)

	rec := get(t, srv, "/api/assets")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := decodeAssets(t, rec); len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("body = %q, want the JSON array literal []", got)
	}
}

// TestAssetEndpointsResponseIsJSON checks both asset endpoints are served as
// application/json with the nosniff header.
//
// Without nosniff an old browser will content-sniff a JSON body into something
// script-bearing, which is a real hazard on the origin that also serves the
// WebUI. The Content-Type must be exactly application/json, not the
// text/plain default a writer that forgot writeJSON would leave behind.
func TestAssetEndpointsResponseIsJSON(t *testing.T) {
	srv := newSeededServer(t, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

	for _, path := range []string{"/api/assets", "/api/assets/a_1"} {
		t.Run(path, func(t *testing.T) {
			rec := get(t, srv, path)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
			}
		})
	}
}

// TestListAssetsBodyIsNotEnveloped pins the shape: a bare array, not
// {"assets":[...]}.
//
// The envelope exists for errors, where the "error" key leaves room for a
// future sibling. Wrapping a success body in one would put the list behind a
// key the WebUI has to know about, and §9.1 documents none.
func TestListAssetsBodyIsNotEnveloped(t *testing.T) {
	srv := newSeededServer(t, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})

	rec := get(t, srv, "/api/assets")

	body := strings.TrimSpace(rec.Body.String())
	if !strings.HasPrefix(body, "[") || !strings.HasSuffix(body, "]") {
		t.Fatalf("body = %q, want a bare JSON array", body)
	}
	var probe struct {
		Assets []model.Asset `json:"assets"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &probe)
	if probe.Assets != nil {
		t.Fatalf("body decoded an \"assets\" key, want a bare array: %q", body)
	}
}

// TestGetAssetReturnsOne proves GET /api/assets/{id} returns the stored asset
// with its governance fields intact.
//
// The row is a hidden, human-approved, locked asset with an artifacts map and
// an allowed-agents list: every field that a visibility-filtered view or a
// partial encoder would quietly drop is asserted here, because the WebUI
// governance screen renders the whole row.
func TestGetAssetReturnsOne(t *testing.T) {
	stored := model.Asset{
		AssetID:       "a_1",
		Status:        model.AssetStatusIngested,
		AgentVisible:  false,
		HumanApproved: true,
		Locked:        true,
		AllowedAgents: []string{"agent_b", "agent_a"},
		Artifacts:     map[string]string{"edl": "clips/a_1/edl.json"},
	}
	srv := newSeededServer(t, stored)

	rec := get(t, srv, "/api/assets/a_1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not a JSON object: %v (%q)", err, rec.Body.String())
	}
	if raw["asset_id"] != "a_1" {
		t.Fatalf("asset_id = %v, want a_1", raw["asset_id"])
	}
	if raw["agent_visible"] != false {
		t.Fatalf("agent_visible = %v, want false returned as-is", raw["agent_visible"])
	}
	if raw["locked"] != true {
		t.Fatalf("locked = %v, want true", raw["locked"])
	}
	if raw["human_approved"] != true {
		t.Fatalf("human_approved = %v, want true", raw["human_approved"])
	}
	if raw["status"] != model.AssetStatusIngested {
		t.Fatalf("status = %v, want %q", raw["status"], model.AssetStatusIngested)
	}
	agents, ok := raw["allowed_agents"].([]any)
	if !ok || len(agents) != 2 {
		t.Fatalf("allowed_agents = %v, want a 2-entry array", raw["allowed_agents"])
	}
	arts, ok := raw["artifacts"].(map[string]any)
	if !ok || arts["edl"] != "clips/a_1/edl.json" {
		t.Fatalf("artifacts = %v, want the stored edl key", raw["artifacts"])
	}
	if raw["created_at"] == nil || raw["created_at"] == "" {
		t.Fatalf("created_at = %v, want a timestamp", raw["created_at"])
	}
}

// TestGetAssetUnknownReturns404 proves a well-formed but unknown id is a 404
// with the machine-readable not_found code, not a 500 and not a bare 404.
func TestGetAssetUnknownReturns404(t *testing.T) {
	srv := newServer(t)

	rec := get(t, srv, "/api/assets/nope")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if got := decodeErr(t, rec).Code; got != codeNotFound {
		t.Fatalf("error code = %q, want %q", got, codeNotFound)
	}
}

// TestGetAssetIgnoresVisibility proves the detail endpoint does not apply the
// agent-visibility rules, matching the list endpoint's governance view.
//
// The only visibility decision point is service.ListVisibleAssets; if this
// handler filtered anything it would be the second copy of that rule (§11.2).
func TestGetAssetIgnoresVisibility(t *testing.T) {
	srv := newSeededServer(t, model.Asset{
		AssetID:      "hidden_1",
		Status:       model.AssetStatusIngested,
		AgentVisible: false,
		Locked:       true,
	})

	rec := get(t, srv, "/api/assets/hidden_1")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — a hidden locked asset is visible to the human governance view",
			rec.Code, http.StatusOK)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not a JSON object: %v (%q)", err, rec.Body.String())
	}
	if raw["agent_visible"] != false || raw["locked"] != true {
		t.Fatalf("governance flags were altered: %v", raw)
	}
}

// TestGetAssetRejectsPathSeparator proves an id carrying a path separator is
// rejected as invalid_argument instead of being looked up.
//
// ServeMux matches the unescaped segment, so a%2Fb reaches the handler as
// "a/b" — the value must be refused there rather than passed to the store.
// The ids used here exist in no store, so a 404 answer would mean the value
// was forwarded; 400 is the only acceptable outcome.
func TestGetAssetRejectsPathSeparator(t *testing.T) {
	srv := newServer(t)

	for _, path := range []string{
		"/api/assets/a%2Fb",
		"/api/assets/a%5Cb",
		"/api/assets/..%2F..%2Fetc%2Fpasswd",
	} {
		t.Run(path, func(t *testing.T) {
			rec := get(t, srv, path)

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

// TestGetAssetEmptyIDRejected pins the empty-id guard from the handler's own
// side.
//
// The router itself never dispatches an empty segment: /api/assets/ matches
// no pattern and answers 404 with the not_found code, which is the correct
// answer for a malformed URL. The handler's guard covers the other route in:
// a request produced by something other than ServeMux's pattern matching,
// which this test supplies directly so the guard is actually exercised rather
// than assumed.
func TestGetAssetEmptyIDRejected(t *testing.T) {
	srv := newServer(t)

	// The routing half: an empty segment is a 404 through the envelope.
	rec := get(t, srv, "/api/assets/")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d for /api/assets/", rec.Code, http.StatusNotFound)
	}
	if got := decodeErr(t, rec).Code; got != codeNotFound {
		t.Fatalf("error code = %q, want %q", got, codeNotFound)
	}

	// The handler half: an empty id that does reach getAsset is a 400, not
	// a store round-trip.
	req := httptest.NewRequest(http.MethodGet, "/api/assets/", nil)
	req.SetPathValue("id", "")
	rec2 := httptest.NewRecorder()
	srv.getAsset(rec2, req)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d for an empty id", rec2.Code, http.StatusBadRequest)
	}
	if got := decodeErr(t, rec2).Code; got != codeArgument {
		t.Fatalf("error code = %q, want %q", got, codeArgument)
	}
}
