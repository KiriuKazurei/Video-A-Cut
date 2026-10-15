package service_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// Offline end-to-end runs of the auto mode (design §9, B4). Each test drives
// real workers against loopback stubs with a dummy token; see the harness.

func serviceScope() service.AutoScopeRequest { return service.AutoScopeRequest{} }

func (e *e2eEnv) kinds(table, kind string) int {
	e.t.Helper()
	return count(e.t, e.svc, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE run_id = '%s' AND kind = '%s'`, table, e.runID, kind))
}

// reviewBands lists the bands of the stored reviews, oldest first.
func (e *e2eEnv) reviewBands() []string {
	e.t.Helper()
	reviews, err := e.svc.ListAutoReviews(e.t.Context(), e.runID)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, r := range reviews {
		out = append(out, r.Band)
	}
	return out
}

func (e *e2eEnv) decide(decision, feedback string) (int, service.AutoView) {
	e.t.Helper()
	body := map[string]any{"decision": decision, "feedback": feedback, "expected_auto_version": e.auto().AutoVersion, "idempotency_key": "e2e-" + decision + "-" + fmt.Sprint(e.auto().AutoVersion)}
	code, raw := e.post("/api/workflows/"+e.runID+"/auto/decision", body)
	var out struct {
		Auto service.AutoView `json:"auto"`
	}
	_ = json.Unmarshal(raw, &out)
	return code, out.Auto
}

// assertDelivered checks everything that "delivered" means (§6.0): the delivery
// package downloads and carries the package files, the 720p preview of the
// current export exists, and the run still waits for the person's acceptance.
func (e *e2eEnv) assertDelivered() {
	e.t.Helper()
	v := e.view()
	if !v.Delivery.Delivered || !v.Delivery.PackageReady || !v.Delivery.PreviewCurrent || v.Delivery.PreviewStatus != model.TaskStatusSucceeded ||
		v.Delivery.PreviewPreset != "mp4-720p-v1" || v.Delivery.Spec != "720p" {
		e.t.Fatalf("not delivered: %+v", v.Delivery)
	}
	code, raw := e.get("/api/workflows/" + e.runID + "/delivery.zip")
	if code != http.StatusOK {
		e.t.Fatalf("delivery.zip = %d", code)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		e.t.Fatal(err)
	}
	have := map[string]bool{}
	for _, f := range zr.File {
		have[f.Name] = true
	}
	for _, want := range []string{"edl.json", "subtitles.srt", "delivery-manifest.json"} {
		if !have[want] {
			e.t.Fatalf("delivery.zip lacks %s: %v", want, have)
		}
	}
	code, raw = e.get("/api/workflows/" + e.runID + "/previews")
	if code != http.StatusOK || !strings.Contains(string(raw), `"preset_id":"mp4-720p-v1"`) || !strings.Contains(string(raw), `"status":"succeeded"`) {
		e.t.Fatalf("previews = %d %s", code, raw)
	}
	run, _, err := e.svc.GetWorkflow(e.t.Context(), e.runID)
	if err != nil || run.Status != model.WorkflowReadyAcceptance {
		e.t.Fatalf("run = %+v %v", run, err)
	}
	if code, raw = e.get("/api/workflows/" + e.runID + "/acceptance"); code != http.StatusOK || strings.TrimSpace(string(raw)) != "[]" {
		e.t.Fatalf("auto mode touched the human acceptance: %d %s", code, raw)
	}
}

// assertNoLeak checks what the stubs saw: only the dummy token, and nothing
// from the brief or a reason in any place but the declared fields.
func (e *e2eEnv) assertStubSawOnlyTheDummyToken() {
	e.t.Helper()
	_, _, speech, bad := e.stub.seen()
	if bad != 0 || speech == 0 {
		e.t.Fatalf("stub saw %d bad tokens and %d speech calls", bad, speech)
	}
}

func TestAutoE2EHighCompletesAndDelivers(t *testing.T) {
	e := newE2EEnv(t)
	e.startAuto("团战", serviceScope())
	if !e.pump(e.settled) {
		t.Fatalf("not settled: %+v", e.auto())
	}
	a := e.auto()
	if a.State != model.AutoStateCompleted || a.DeliveryHold || a.Round != 0 || a.TotalRounds != 0 {
		t.Fatalf("state = %+v", a)
	}
	if got := e.reviewBands(); len(got) != 1 || got[0] != "high" {
		t.Fatalf("bands = %v", got)
	}
	e.assertDelivered()
	e.assertStubSawOnlyTheDummyToken()
	if v := e.view(); v.Decision != nil || v.DeliveryHoldEnforced {
		t.Fatalf("view = %+v", v)
	}
	// The brief reached the vision model as a data field of the request.
	vision, narration, _, _ := e.stub.seen()
	if len(vision) == 0 || len(narration) == 0 || vision[0]["user_brief"] != "团战" || narration[0]["user_brief"] != "团战" {
		t.Fatalf("brief not carried: %v %v", vision, narration)
	}
	if _, has := vision[0]["revise_feedback"]; has {
		t.Fatal("first pass carried feedback")
	}
	// Exactly the automatic actions of one clean pass, each once.
	for kind, want := range map[string]int{"preview": 1, "score": 1, "rerun": 0} {
		if got := e.kinds("auto_actions", kind); got != want {
			t.Fatalf("auto_actions %s = %d, want %d", kind, got, want)
		}
	}
	// A later tick finds nothing to do: no new decision, review or task.
	before := e.auto()
	if err := e.svc.AutoTick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if after := e.auto(); after.AutoVersion != before.AutoVersion || len(e.queued()) != 0 {
		t.Fatalf("a completed run moved: %+v", after)
	}
	if len(e.events.named("auto.reviewed", 1)) < 1 || len(e.events.named("auto.changed", 3)) < 3 {
		t.Fatal("auto.reviewed / auto.changed were not published")
	}
}

// MEDIUM: recognition 0.65 is above the scene gate (0.60) and below the HIGH
// floor (0.70), so the run is confirmed automatically and then waits.
func TestAutoE2EMediumWaitsThenExports(t *testing.T) {
	e := newE2EEnv(t)
	e.stub.set(0.65, "团战画面", "这是一段精彩的团战画面")
	e.startAuto("团战", serviceScope())
	e.pump(e.settled)
	a := e.auto()
	if a.State != model.AutoStateAwaitingDecision || a.HaltReason != model.AutoWaitMedium || !a.DeliveryHold || a.DecisionDeadlineAt == nil || a.PausedAt == nil {
		t.Fatalf("state = %+v", a)
	}
	if got := e.reviewBands(); len(got) != 1 || got[0] != "medium" {
		t.Fatalf("bands = %v", got)
	}
	v := e.view()
	if v.Decision == nil || !v.Decision.CanExport || !v.Decision.CanRevise || v.Decision.Reason != "medium" || !v.DeliveryHold || v.DeliveryHoldEnforced {
		t.Fatalf("prompt = %+v %+v", v.Decision, v)
	}
	// Documented v1 behaviour: the hold is a label, the package downloads anyway.
	if code, _ := e.get("/api/workflows/" + e.runID + "/delivery.zip"); code != http.StatusOK {
		t.Fatalf("delivery.zip while held = %d", code)
	}
	// Nothing runs on its own while the run waits, however long that is.
	e.clock.Advance(71 * 3600 * 1e9)
	if err := e.svc.AutoTick(t.Context()); err != nil || e.auto().State != model.AutoStateAwaitingDecision || len(e.queued()) != 0 {
		t.Fatalf("a waiting run moved: %v %+v", err, e.auto())
	}
	// The user exports.
	code, out := e.decide("export", "")
	if code != http.StatusOK || out.State != model.AutoStateCompleted || out.DeliveryHold || out.Decision != nil || !out.Delivery.Delivered {
		t.Fatalf("export = %d %+v", code, out)
	}
	if e.kinds("auto_decisions", "user_export") != 1 {
		t.Fatal("the export decision was not recorded once")
	}
	e.assertDelivered()
	e.assertStubSawOnlyTheDummyToken()
	if got := e.kinds("auto_actions", "rerun"); got != 0 {
		t.Fatalf("reruns = %d", got)
	}
}

func TestAutoE2EMediumRevisedThenCompletes(t *testing.T) {
	e := newE2EEnv(t)
	e.stub.set(0.65, "团战画面", "这是一段精彩的团战画面")
	e.startAuto("团战", serviceScope())
	e.pump(e.settled)
	if a := e.auto(); a.State != model.AutoStateAwaitingDecision {
		t.Fatalf("state = %+v", a)
	}
	before := e.auto()
	e.stub.set(0.95, "团战画面", "这是一段精彩的团战画面") // the revised model output is better
	e.clock.Advance(3600 * 1e9)             // an hour of thinking: the wall clock restarts at the revise
	code, out := e.decide("revise", "只保留最精彩的团战")
	if code != http.StatusOK || out.State != model.AutoStateActive || out.TotalRounds != 1 || out.Round != 0 || out.DeliveryHold ||
		out.ActionsUsed != before.ActionsUsed || !out.DeadlineAt.After(before.DeadlineAt) || out.PausedAt != nil || out.Decision != nil {
		t.Fatalf("revise = %d state=%s total=%d round=%d hold=%v actions=%d/%d deadline-moved=%v paused=%v decision=%v",
			code, out.State, out.TotalRounds, out.Round, out.DeliveryHold, out.ActionsUsed, before.ActionsUsed, out.DeadlineAt.After(before.DeadlineAt), out.PausedAt, out.Decision)
	}
	if v := e.view(); v.Delivery.Delivered {
		t.Fatalf("the invalidated export still counts as delivered: %+v", v.Delivery)
	}
	e.pump(e.settled)
	a := e.auto()
	if a.State != model.AutoStateCompleted || a.DeliveryHold || a.Round != 0 || a.TotalRounds != 1 {
		t.Fatalf("after the revision: %+v", a)
	}
	if got := e.reviewBands(); len(got) != 2 || got[0] != "medium" || got[1] != "high" {
		t.Fatalf("bands = %v", got)
	}
	e.assertDelivered()
	// The user's words reached the model as a data field, only after the revise.
	vision, _, _, _ := e.stub.seen()
	if _, has := vision[0]["revise_note"]; has || vision[len(vision)-1]["revise_note"] != "只保留最精彩的团战" {
		t.Fatalf("revise note carried wrongly: first=%v last=%v", vision[0], vision[len(vision)-1])
	}
	// A manual revise is not an automatic round.
	if e.kinds("auto_actions", "rerun") != 0 || e.kinds("auto_decisions", "user_revise") != 1 || e.kinds("auto_decisions", "auto_rerun") != 0 {
		t.Fatal("the revise was accounted as an automatic rerun")
	}
}

// LOW that mends: the first recognition is below the scene gate, the fixed
// feedback reaches the model, the second pass is good.
func TestAutoE2ELowRerunsAndMends(t *testing.T) {
	e := newE2EEnv(t)
	e.stub.set(0.5, "团战画面", "这是一段精彩的团战画面")
	e.stub.improveOnFeedback(0.95)
	e.startAuto("团战", serviceScope())
	e.pump(e.settled)
	a := e.auto()
	if a.State != model.AutoStateCompleted || a.Round != 1 || a.TotalRounds != 1 || a.DeliveryHold {
		t.Fatalf("state = %+v", a)
	}
	v := e.view()
	if len(v.Rounds) != 1 || !strings.Contains(v.Rounds[0].Feedback, `"low_confidence"`) || strings.Contains(v.Rounds[0].Feedback, "团战画面") {
		t.Fatalf("rounds = %+v", v.Rounds)
	}
	if e.kinds("auto_decisions", "auto_rerun") != 1 || e.kinds("auto_actions", "rerun") != 1 {
		t.Fatal("expected exactly one automatic rerun")
	}
	vision, _, _, _ := e.stub.seen()
	if len(vision) != 2 {
		t.Fatalf("vision calls = %d", len(vision))
	}
	fb, _ := vision[1]["revise_feedback"].([]any)
	if len(fb) != 1 || fb[0].(map[string]any)["code"] != "low_confidence" {
		t.Fatalf("the second pass must carry the fixed reason code only: %v", vision[1])
	}
	if got := e.reviewBands(); len(got) != 1 || got[0] != "high" {
		t.Fatalf("bands = %v", got)
	}
	e.assertDelivered()
}

// LOW that does not mend: the cap stops the loop, nothing more is queued, and
// there is no export to release.
func TestAutoE2ELowExhaustsThenWaitsAndCannotExport(t *testing.T) {
	e := newE2EEnv(t)
	e.stub.set(0.5, "团战画面", "这是一段精彩的团战画面")
	e.startAuto("团战", serviceScope())
	e.pump(e.settled)
	a := e.auto()
	if a.State != model.AutoStateAwaitingDecision || a.HaltReason != model.AutoWaitExhausted || a.Round != 1 || a.MaxRounds != 1 || !a.DeliveryHold {
		t.Fatalf("state = %+v", a)
	}
	if q := e.queued(); len(q) != 0 {
		t.Fatalf("tasks were queued after the cap: %+v", q)
	}
	if err := e.svc.AutoTick(t.Context()); err != nil || len(e.queued()) != 0 || e.kinds("auto_actions", "rerun") != 1 {
		t.Fatalf("a spent run kept going: %v", err)
	}
	vision, _, _, _ := e.stub.seen()
	if len(vision) != 2 {
		t.Fatalf("vision calls = %d, want 2 (first pass + one rerun)", len(vision))
	}
	v := e.view()
	if v.Decision == nil || v.Decision.Reason != "exhausted" || v.Decision.CanExport || !v.Decision.CanRevise || v.Delivery.Delivered {
		t.Fatalf("prompt = %+v delivery = %+v", v.Decision, v.Delivery)
	}
	// There was never an export, so "export" is refused and changes nothing.
	if code, _ := e.decide("export", ""); code != http.StatusConflict {
		t.Fatalf("export without a delivery = %d, want 409", code)
	}
	if after := e.auto(); after.State != model.AutoStateAwaitingDecision || !after.DeliveryHold {
		t.Fatalf("a refused export changed the run: %+v", after)
	}
	if code, out := e.decide("discard", ""); code != http.StatusOK || out.State != model.AutoStateHalted || out.HaltReason != model.AutoHaltUserDiscarded {
		t.Fatalf("discard = %d %+v", code, out)
	}
}

// A failed preview is LOW and not improvable (a rerun cannot fix the renderer):
// the run halts for a person instead of spending rounds.
func TestAutoE2EFailedPreviewHalts(t *testing.T) {
	e := newE2EEnv(t)
	e.failPreview = true
	e.startAuto("团战", serviceScope())
	e.pump(e.settled)
	a := e.auto()
	if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltNotImprovable || a.Round != 0 {
		t.Fatalf("state = %+v", a)
	}
	if got := e.reviewBands(); len(got) != 1 || got[0] != "low" {
		t.Fatalf("bands = %v", got)
	}
	reviews, _ := e.svc.ListAutoReviews(t.Context(), e.runID)
	if !strings.Contains(string(reviews[0].Reasons), "preview_failed") {
		t.Fatalf("reasons = %s", reviews[0].Reasons)
	}
	if v := e.view(); v.Delivery.Delivered || v.Decision != nil {
		t.Fatalf("view = %+v", v)
	}
	if len(e.queued()) != 0 {
		t.Fatal("tasks queued after the halt")
	}
}
