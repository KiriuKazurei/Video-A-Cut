package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// decodeAudit decodes the bare audit array GET /api/audit returns.
//
// The shape check on the raw text is not redundant with the unmarshal: a JSON
// null and an object both decode into a nil or empty slice without error, so
// an enveloped {"audit":[...]} body would pass a length assertion alone and
// leave a client ranging over nothing.
func decodeAudit(t *testing.T, rec *httptest.ResponseRecorder) []model.AuditLog {
	t.Helper()
	body := strings.TrimSpace(rec.Body.String())
	if !strings.HasPrefix(body, "[") {
		t.Fatalf("body does not start a JSON array: %q", rec.Body.String())
	}
	var out []model.AuditLog
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body is not a JSON array of audit rows: %v (%q)", err, rec.Body.String())
	}
	if out == nil {
		t.Fatalf("array decoded to nil, want at least an empty slice: %q", body)
	}
	return out
}

// auditActions collects the action names of a decoded audit array, in the
// order the endpoint returned them.
//
// The order matters: the store answers newest first, and a view that showed
// the oldest row at the top would send an operator looking at the wrong
// history when they open the governance screen.
func auditActions(rows []model.AuditLog) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Action)
	}
	return out
}

// seedAuditServer returns a Server whose log holds every class of writer the
// plane has: an ingest ("system"), a human governance change ("human:webui")
// and an agent-side claim ("agent:<id>").
//
// Every row is produced through the real service methods rather than written
// into the store, because the endpoint's contract is about what those write
// paths recorded — a test that inserted rows directly would still pass while
// the handler read a path no production write ever goes through.
func seedAuditServer(t *testing.T) *Server {
	t.Helper()
	srv := newSeededServer(t, governanceAsset())

	if rec := patch(t, srv, "/api/assets/a_1", `{"locked":true}`); rec.Code != http.StatusOK {
		t.Fatalf("governance PATCH status = %d, want %d (%s)",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	tk := model.Task{
		TaskID:    "t_1",
		AssetID:   "a_1",
		Type:      model.TaskTypeRecognize,
		AgentRole: "recognizer",
	}
	if err := srv.svc.CreateTask(context.Background(), tk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	// The claim is the one audited path an agent owns, and it is what puts
	// an "agent:"-prefixed actor into the log.
	if _, err := srv.svc.ClaimTask(context.Background(), "agent_1",
		"recognizer", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("claim task: %v", err)
	}
	return srv
}

// TestListAuditReturnsRows proves the endpoint answers 200 with the audit rows
// the write paths actually recorded.
//
// The actions asserted here are the full set the seeding produced — ingest,
// governance, task creation, task claim — so a handler that returned a
// filtered or partially decoded view would show up as a missing entry rather
// than as a wrong count.
func TestListAuditReturnsRows(t *testing.T) {
	srv := seedAuditServer(t)

	rec := get(t, srv, "/api/audit")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	rows := decodeAudit(t, rec)
	if len(rows) == 0 {
		t.Fatalf("no audit rows returned, want the four the write paths recorded: %+v", rows)
	}
	want := []string{"task.claim", "task.create", "asset.governance", "asset.create"}
	got := auditActions(rows)
	if len(got) != len(want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("actions = %v, want %v — the log must be newest first", got, want)
		}
	}

	// The rows carry their target, not just their name: an audit line that
	// does not say what it acted on is not attributable.
	for _, r := range rows {
		if r.Target == "" {
			t.Fatalf("row has an empty target: %+v", r)
		}
		if r.CreatedAt.IsZero() {
			t.Fatalf("row has no created_at: %+v", r)
		}
	}
}

// TestListAuditEmptyIsJSONArray pins the empty result as [] rather than null.
//
// A null body forces every client to nil-check before it can range over the
// response, and JSON null is exactly what a nil Go slice encodes to. The store
// builds its result with a non-nil empty slice; this test is the transport's
// half of that promise, so a later refactor that lets a nil through is caught
// here instead of in a browser console.
func TestListAuditEmptyIsJSONArray(t *testing.T) {
	srv := newServer(t)

	rec := get(t, srv, "/api/audit")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := decodeAudit(t, rec); len(got) != 0 {
		t.Fatalf("len = %d, want 0 — a fresh store has no history", len(got))
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("body = %q, want the JSON array literal []", got)
	}
}

// TestListAuditDefaultLimit proves an omitted limit is bounded rather than
// unbounded.
//
// More rows are written than the default page size, so the assertion is on
// the cap itself: the endpoint must answer exactly defaultAuditLimit rows, not
// everything the store holds. The alternative — passing a non-positive limit
// down so the store picks its own page size of 100 — would make the response
// size a detail of the storage layer instead of a decision of the surface that
// has to render and defend it.
func TestListAuditDefaultLimit(t *testing.T) {
	srv := newServer(t)

	total := defaultAuditLimit + 10
	ctx := context.Background()
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("a_%04d", i)
		if err := srv.svc.CreateAsset(ctx, model.Asset{
			AssetID: id,
			Status:  model.AssetStatusIngested,
		}); err != nil {
			t.Fatalf("create asset %s: %v", id, err)
		}
	}

	rec := get(t, srv, "/api/audit")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	rows := decodeAudit(t, rec)
	if len(rows) != defaultAuditLimit {
		t.Fatalf("len = %d, want %d — the default page size must bound the response",
			len(rows), defaultAuditLimit)
	}
	// Newest first, so the most recent row is the last asset created and
	// the oldest visible one is defaultAuditLimit rows back.
	newest := fmt.Sprintf("a_%04d", total-1)
	if rows[0].Target != newest {
		t.Fatalf("newest row target = %q, want %q — the log must be newest first",
			rows[0].Target, newest)
	}
	oldest := fmt.Sprintf("a_%04d", total-defaultAuditLimit)
	if rows[defaultAuditLimit-1].Target != oldest {
		t.Fatalf("oldest returned row target = %q, want %q",
			rows[defaultAuditLimit-1].Target, oldest)
	}
}

// TestListAuditRespectsLimitParam proves ?limit=3 returns exactly three rows
// and that they are the newest three.
//
// The store orders by id descending, so with six assets created in order the
// expected targets are a_6, a_5, a_4. The ordering is asserted alongside the
// count because a paging view is useless if it hands back the oldest page:
// an operator reading ?limit=3 wants what just happened, and an ascending
// list would never change as new rows arrive.
func TestListAuditRespectsLimitParam(t *testing.T) {
	srv := newServer(t)
	ctx := context.Background()
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("a_%d", i)
		if err := srv.svc.CreateAsset(ctx, model.Asset{
			AssetID: id,
			Status:  model.AssetStatusIngested,
		}); err != nil {
			t.Fatalf("create asset %s: %v", id, err)
		}
	}

	rec := get(t, srv, "/api/audit?limit=3")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	rows := decodeAudit(t, rec)
	if len(rows) != 3 {
		t.Fatalf("len = %d, want 3", len(rows))
	}
	want := []string{"a_6", "a_5", "a_4"}
	for i, w := range want {
		if rows[i].Target != w {
			t.Fatalf("targets = %v, want %v — the newest rows must come first",
				rowTargets(rows), want)
		}
	}
}

// rowTargets collects the target of every row, for a readable failure message.
func rowTargets(rows []model.AuditLog) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Target)
	}
	return out
}

// TestListAuditRejectsBadLimit proves a limit that is not a positive integer
// is a client error with the machine-readable invalid_argument code.
//
// A non-positive value is refused here rather than left to the store's own
// fallback (which silently normalises it to 100): the store's fallback exists
// for internal callers that pass a computed zero, and a client that typed
// ?limit=0 or ?limit=-5 has a bug that silence would hide. An empty value is
// rejected on the same grounds — the parameter was named and then left blank,
// which is a client bug, not an absent one.
func TestListAuditRejectsBadLimit(t *testing.T) {
	srv := seedAuditServer(t)

	cases := []struct {
		name  string
		query string
	}{
		{"not a number", "?limit=abc"},
		{"zero", "?limit=0"},
		{"negative", "?limit=-5"},
		{"present but empty", "?limit="},
		{"fractional", "?limit=1.5"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := get(t, srv, "/api/audit"+c.query)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (%s)",
					rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if got := decodeErr(t, rec).Code; got != codeArgument {
				t.Fatalf("error code = %q, want %q", got, codeArgument)
			}
			if !json.Valid(rec.Body.Bytes()) {
				t.Fatalf("body is not valid JSON: %q", rec.Body.String())
			}
		})
	}
}

// TestListAuditKeepsDetailIntact proves the detail column is returned whole.
//
// The allowed-agents list is what makes a long audit line: replacing a
// 60-entry list produces a diff that names every entry, and the assertions
// target the tail of it — the last added token and the removed one, which are
// the very last things the service wrote. A truncating encoder would drop
// exactly the part of the sentence that says what an agent was granted and
// taken away, which is the part a governance reviewer opened the log to read.
//
// Only the allow-list is changed, so the flag part of the detail reads as the
// values as stored: the PATCH left agent_visible and locked alone, and they
// are reported as false. An audit detail that named only the flags it was
// asked about would still be telling the truth; one that wrote what was stored
// is the one that keeps its value after the fact.
func TestListAuditKeepsDetailIntact(t *testing.T) {
	srv := newSeededServer(t, governanceAsset())

	agents := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		agents = append(agents, fmt.Sprintf("agent_long_name_%02d", i))
	}
	body, err := json.Marshal(patchAssetRequest{AllowedAgents: &agents})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if rec := patch(t, srv, "/api/assets/a_1", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec := get(t, srv, "/api/audit")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	rows := decodeAudit(t, rec)

	var found *model.AuditLog
	for i := range rows {
		if rows[i].Action == "asset.governance" {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no asset.governance row: %+v", rows)
	}
	if len(found.Detail) < 1000 {
		t.Fatalf("detail is %d bytes, want the full diff of a 60-entry allow-list", len(found.Detail))
	}
	// The tail of the detail: any truncation would drop these.
	if !strings.Contains(found.Detail, "+agent_long_name_59") {
		t.Fatalf("detail lost the last added agent: %q", found.Detail)
	}
	if !strings.Contains(found.Detail, "-agent_a") {
		t.Fatalf("detail lost the removed agent, which is written last: %q", found.Detail)
	}
	if !strings.HasSuffix(found.Detail, "-agent_a") {
		t.Fatalf("detail is truncated: it does not end with the last diff token: %q", found.Detail)
	}
}

// TestListAuditResponseIsJSON checks the endpoint is served as application/json
// with the nosniff header.
//
// Without nosniff an old browser will content-sniff a JSON body into something
// script-bearing, which is a real hazard on the origin that also serves the
// WebUI. The body must also be valid JSON, not just a 200: a handler that
// wrote the array through a plain-text writer would still answer 200.
func TestListAuditResponseIsJSON(t *testing.T) {
	srv := seedAuditServer(t)

	rec := get(t, srv, "/api/audit")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("body is not valid JSON: %q", rec.Body.String())
	}
	// A bare array, not an envelope: §9.1 documents no wrapper key, and a
	// wrapped body would put the list behind a key every client must know.
	if body := strings.TrimSpace(rec.Body.String()); !strings.HasPrefix(body, "[") {
		t.Fatalf("body = %q, want a bare JSON array", body)
	}
}

// TestListAuditActorPrefixVisible proves the governance view reports agent-side
// rows next to human ones.
//
// The actor prefix is load-bearing: the retention sweep files an "agent:"-prefix
// actor as agent traffic and a "human:" one as a governance record, so the two
// classes exist and must stay distinguishable. A view that hid, folded or
// rewrote either class would let an operator believe no agent had touched an
// asset (or that a machine action was a person's), which is the exact question
// this endpoint is opened to answer.
func TestListAuditActorPrefixVisible(t *testing.T) {
	srv := seedAuditServer(t)

	rec := get(t, srv, "/api/audit")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	rows := decodeAudit(t, rec)

	actors := make(map[string]bool, len(rows))
	for _, r := range rows {
		actors[r.Actor] = true
		if r.Actor == "" {
			t.Fatalf("row has an empty actor: %+v", r)
		}
	}
	// The fixed attribution the governance write path uses, in case the
	// constant is renamed: the assertion is on the prefix the sweep reads.
	if !actors[humanActor] {
		t.Fatalf("no %q row, want the human governance write recorded: %v", humanActor, rows)
	}
	if !actors["agent:agent_1"] {
		t.Fatalf("no agent:agent_1 row, want the agent-side claim recorded: %v", rows)
	}
	if !actors["system"] {
		t.Fatalf("no system row, want the ingest and task creation recorded: %v", rows)
	}
}
