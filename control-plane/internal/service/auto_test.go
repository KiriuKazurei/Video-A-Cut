package service_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

type autoEnv struct {
	svc      *service.Service
	root     string
	profile  preparation.Profile
	sha      string
	deliver  int
	external bool
}

func autoProfile(external bool) preparation.Profile {
	p := builtinProfile(1)
	p.ProfileID = "auto_profile"
	p.ContentMode = "configured"
	scheme, host := "http", "127.0.0.1:18080"
	if external {
		scheme, host = "https", "example.com"
	}
	p.Vision = preparation.Provider{Adapter: "vision", Endpoint: scheme + "://" + host + "/vision", Model: "fixture", TokenEnv: "VISION_TEST"}
	p.Narration = preparation.Provider{Adapter: "narration", Endpoint: scheme + "://" + host + "/narration", Model: "fixture", TokenEnv: "NARRATION_TEST"}
	return p
}

// newAutoEnv builds a service with one ingested asset, a configured profile
// and fresh worker capabilities for all three roles.
func newAutoEnv(t *testing.T, external bool) *autoEnv {
	t.Helper()
	svc := newService(t)
	root := t.TempDir()
	writeWorkflowFixture(t, root, "src")
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := svc.CreateAsset(ctx, model.Asset{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true,
		AllowedAgents: []string{"recognizer", "narrator", "exporter"}, Artifacts: map[string]string{"edl": "src/edl.json"}}); err != nil {
		t.Fatal(err)
	}
	p := autoProfile(external)
	if _, err := svc.SaveProcessingProfile(ctx, "human:test", "profile", 0, p); err != nil {
		t.Fatal(err)
	}
	sha, _ := p.Fingerprint()
	for _, role := range []string{"recognizer", "narrator", "exporter"} {
		if err := svc.ReportWorkerCapability(ctx, role, role, preparation.Capability{ProfileSHA256: sha, ToolsReady: true, CredentialsReady: true, TTSVoices: []string{p.TTSVoice}}); err != nil {
			t.Fatal(err)
		}
	}
	return &autoEnv{svc: svc, root: root, profile: p, sha: sha, external: external}
}

func (e *autoEnv) request(t *testing.T, key, brief string, accept bool) service.AutoRunRequest {
	t.Helper()
	report, err := e.svc.PreparedPreflight(t.Context(), "clip", e.profile.ProfileID, 1)
	if err != nil {
		t.Fatal(err)
	}
	notice, err := e.svc.AutoNoticeFor(t.Context(), e.profile.ProfileID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return service.AutoRunRequest{AssetID: "clip", ProfileID: e.profile.ProfileID, ProfileRevision: 1,
		ExpectedAssetVersion: report.ExpectedAssetVersion, Brief: brief, AcceptExternalEgress: accept,
		NoticeSHA256: notice.NoticeSHA256, IdempotencyKey: key}
}

func (e *autoEnv) start(t *testing.T, brief string) (model.WorkflowRun, service.AutoView) {
	t.Helper()
	run, view, err := e.svc.StartAutoRun(t.Context(), "human:webui", e.request(t, "auto-"+brief, brief, e.external))
	if err != nil {
		t.Fatal(err)
	}
	return run, view
}

// recognized delivers the recognize stage with two scenes. conf and label are
// per scene; the receipt carries the given brief hash.
func (e *autoEnv) recognized(t *testing.T, run model.WorkflowRun, conf [2]float64, receiptBrief string) error {
	t.Helper()
	ctx := t.Context()
	tk, err := e.svc.ClaimTask(ctx, "recognizer", "recognizer", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := e.svc.GetTaskInput(ctx, "recognizer", "recognizer", tk.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	e.deliver++
	pkg := fmt.Sprintf("recognized%d", e.deliver)
	writeWorkflowFixture(t, e.root, pkg)
	edlPath := filepath.Join(e.root, pkg, "edl.json")
	raw, _ := os.ReadFile(edlPath)
	var edl map[string]any
	_ = json.Unmarshal(raw, &edl)
	digest := edl["scenes"].([]any)[0].(map[string]any)["evidence_frames"].([]any)[0]
	edl["video"] = []any{
		map[string]any{"src": "a.mp4", "in": 0, "out": 1, "timeline_in": 0},
		map[string]any{"src": "a.mp4", "in": 1, "out": 2, "timeline_in": 1},
	}
	edl["scenes"] = []any{
		map[string]any{"scene_id": "scn_1", "index": 0, "label": "团战A", "method": "vision", "confidence": conf[0], "sequence_rank": 0, "evidence_frames": []any{digest}},
		map[string]any{"scene_id": "scn_2", "index": 1, "label": "团战B", "method": "vision", "confidence": conf[1], "sequence_rank": 1, "evidence_frames": []any{digest}},
	}
	body, _ := json.Marshal(edl)
	if err := os.WriteFile(edlPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	receipt := map[string]any{"task_id": tk.TaskID, "revision_id": fixed.RevisionID, "content_mode": "configured", "profile_sha256": e.sha}
	if receiptBrief != "" {
		receipt["brief_sha256"] = receiptBrief
	}
	body, _ = json.Marshal(receipt)
	if err := os.WriteFile(filepath.Join(e.root, pkg, "worker-receipt.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	return e.svc.SubmitDelivery(ctx, "recognizer", tk.TaskID, pkg)
}

func (e *autoEnv) atSceneReview(t *testing.T, brief string, conf [2]float64) (model.WorkflowRun, service.AutoView) {
	t.Helper()
	run, view := e.start(t, brief)
	if err := e.recognized(t, run, conf, view.BriefSHA256); err != nil {
		t.Fatal(err)
	}
	now, _, err := e.svc.GetWorkflow(t.Context(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if now.Stage != "scene_review" || now.Status != model.WorkflowAwaitingReview {
		t.Fatalf("run is at %s/%s, want scene_review", now.Stage, now.Status)
	}
	view, err = e.svc.GetAuto(t.Context(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return now, view
}

func auditActions(t *testing.T, svc *service.Service) map[string][]model.AuditLog {
	t.Helper()
	rows, err := svc.ListAudit(t.Context(), 500)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]model.AuditLog{}
	for _, r := range rows {
		out[r.Action] = append(out[r.Action], r)
	}
	return out
}

func count(t *testing.T, svc *service.Service, query string, args ...any) int {
	t.Helper()
	var n int
	if err := storeOf(t, svc).DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestStartAutoRunBindsOverlayAndIsIdempotent(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	req := env.request(t, "k1", "只保留团战", false)
	if _, _, err := env.svc.StartAutoRun(ctx, "agent:worker", req); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("an agent must not authorize: %v", err)
	}
	if _, _, err := env.svc.StartAutoRun(ctx, "system:auto-review", req); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("the auto-reviewer must not authorize: %v", err)
	}
	run, view, err := env.svc.StartAutoRun(ctx, "human:webui", req)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != model.AutoStateActive || view.AutoVersion != 1 || view.Round != 0 || view.MaxRounds != 1 || view.MaxTotalRounds != 5 ||
		view.AuthorizedBy != "human:webui" || view.ProfileID != env.profile.ProfileID || view.ProfileSHA256 != env.sha ||
		view.Brief != "只保留团战" || len(view.BriefSHA256) != 64 || view.DeliveryHold || view.ConsentGrantedByRun ||
		view.Scope.ExternalEgress || !view.Scope.AutoSceneConfirm || view.Scope.MaxActions != 200 || view.Rounds == nil || view.Decisions == nil {
		t.Fatalf("view = %+v", view)
	}
	if len(view.Decisions) != 1 || view.Decisions[0].Kind != model.AutoDecisionAuthorize || view.Decisions[0].Actor != "human:webui" {
		t.Fatalf("decisions = %+v", view.Decisions)
	}
	if run.Stage != model.TaskTypeRecognize || run.Status != model.WorkflowRunning {
		t.Fatalf("run = %+v", run)
	}
	again, view2, err := env.svc.StartAutoRun(ctx, "human:webui", req)
	if err != nil || again.RunID != run.RunID || view2.AutoVersion != 1 {
		t.Fatalf("replay: %+v %+v %v", again, view2, err)
	}
	if n := count(t, env.svc, `SELECT COUNT(*) FROM workflow_auto`); n != 1 {
		t.Fatalf("replay created %d overlays", n)
	}
	changed := req
	changed.Brief = "别的需求"
	if _, _, err := env.svc.StartAutoRun(ctx, "human:webui", changed); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("same key, different brief: %v", err)
	}
	if n := count(t, env.svc, `SELECT COUNT(*) FROM workflow_profile_bindings WHERE run_id=?`, run.RunID); n != 1 {
		t.Fatalf("profile binding rows = %d", n)
	}
	if got := auditActions(t, env.svc); len(got["auto.authorize"]) != 1 || got["auto.authorize"][0].Actor != "human:webui" || len(got["profile.consent"]) != 0 {
		t.Fatalf("audit = %+v", got)
	}
	tk, err := env.svc.ClaimTask(ctx, "recognizer", "recognizer", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	in, err := env.svc.GetTaskInput(ctx, "recognizer", "recognizer", tk.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if in.Auto == nil || in.Auto.Brief != "只保留团战" || in.Auto.BriefSHA256 != view.BriefSHA256 || in.Auto.Round != 0 || len(in.Auto.Feedback) != 0 {
		t.Fatalf("task input auto = %+v", in.Auto)
	}
}

func TestStartAutoRunValidatesBriefScopeAndNotice(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	good := env.request(t, "k", "x", false)
	cases := map[string]func(r *service.AutoRunRequest){
		"missing notice":    func(r *service.AutoRunRequest) { r.NoticeSHA256 = "" },
		"wrong notice":      func(r *service.AutoRunRequest) { r.NoticeSHA256 = strings.Repeat("0", 64) },
		"long brief":        func(r *service.AutoRunRequest) { r.Brief = strings.Repeat("字", 2001) },
		"control char":      func(r *service.AutoRunRequest) { r.Brief = "a\x00b" },
		"invalid utf8":      func(r *service.AutoRunRequest) { r.Brief = "a\xffb" },
		"three rounds":      func(r *service.AutoRunRequest) { n := 3; r.Scope.MaxRounds = &n },
		"zero actions":      func(r *service.AutoRunRequest) { n := 0; r.Scope.MaxActions = &n },
		"bad threshold key": func(r *service.AutoRunRequest) { r.Scope.Thresholds = map[string]float64{"Bad Key": 1} },
		"unknown threshold": func(r *service.AutoRunRequest) { r.Scope.Thresholds = map[string]float64{"nope": 0.5} },
		"inverted bands":    func(r *service.AutoRunRequest) { r.Scope.Thresholds = map[string]float64{"high_score_min": 0.5} },
		"floor above one":   func(r *service.AutoRunRequest) { r.Scope.Thresholds = map[string]float64{"av_floor": 1.5} },
		"total below rounds": func(r *service.AutoRunRequest) {
			a, b := 2, 1
			r.Scope.MaxRounds, r.Scope.MaxTotalRounds = &a, &b
		},
	}
	for name, edit := range cases {
		r := good
		edit(&r)
		r.IdempotencyKey = "k-" + strings.ReplaceAll(name, " ", "-")
		if _, _, err := env.svc.StartAutoRun(ctx, "human:webui", r); !errors.Is(err, model.ErrArgument) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if n := count(t, env.svc, `SELECT COUNT(*) FROM workflow_runs`); n != 0 {
		t.Fatalf("a rejected authorization left %d runs", n)
	}
	builtin := builtinProfile(1)
	builtin.ProfileID = "builtin_profile"
	if _, err := env.svc.SaveProcessingProfile(ctx, "human:test", "b", 0, builtin); err != nil {
		t.Fatal(err)
	}
	r := good
	r.ProfileID, r.IdempotencyKey = builtin.ProfileID, "builtin"
	if _, _, err := env.svc.StartAutoRun(ctx, "human:webui", r); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("builtin profile must be refused: %v", err)
	}
}

func TestStartAutoRunConsentOnlyForExternalProfiles(t *testing.T) {
	ext := newAutoEnv(t, true)
	ctx := t.Context()
	consent := func() bool {
		ok, err := storeOf(t, ext.svc).ProfileConsent(ctx, ext.profile.ProfileID, 1, ext.sha)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	req := ext.request(t, "k", "", false)
	if _, _, err := ext.svc.StartAutoRun(ctx, "human:webui", req); !errors.Is(err, model.ErrArgument) || consent() {
		t.Fatalf("external egress without acceptance: err=%v consent=%v", err, consent())
	}
	req.AcceptExternalEgress = true
	stale := req
	stale.ExpectedAssetVersion = "stale"
	if _, _, err := ext.svc.StartAutoRun(ctx, "human:webui", stale); !errors.Is(err, model.ErrConflict) || consent() {
		t.Fatalf("a failed start must roll the consent back: err=%v consent=%v", err, consent())
	}
	req = ext.request(t, "k2", "", true)
	run, view, err := ext.svc.StartAutoRun(ctx, "human:webui", req)
	if err != nil {
		t.Fatal(err)
	}
	if !consent() || !view.ConsentGrantedByRun || !view.Scope.ExternalEgress || run.RunID == "" {
		t.Fatalf("consent=%v view=%+v", consent(), view)
	}
	rows := auditActions(t, ext.svc)["profile.consent"]
	if len(rows) != 1 || rows[0].Actor != "human:webui" || !strings.Contains(rows[0].Detail, "via=auto.authorize") {
		t.Fatalf("consent audit = %+v", rows)
	}
	notice, _ := ext.svc.AutoNoticeFor(ctx, ext.profile.ProfileID, 1)
	if !notice.External || !strings.Contains(notice.Notice, "example.com") || !strings.Contains(notice.Notice, "VISION_TEST") {
		t.Fatalf("external notice = %+v", notice)
	}

	loop := newAutoEnv(t, false)
	if _, _, err := loop.svc.StartAutoRun(ctx, "human:webui", loop.request(t, "k", "", false)); err != nil {
		t.Fatalf("all-loopback profile needs no egress acceptance: %v", err)
	}
	if n := len(auditActions(t, loop.svc)["profile.consent"]); n != 0 {
		t.Fatal("consent step ran for a loopback profile")
	}
	ln, _ := loop.svc.AutoNoticeFor(ctx, loop.profile.ProfileID, 1)
	if ln.External || !strings.Contains(ln.Notice, "不向外部发送数据") || ln.NoticeSHA256 == notice.NoticeSHA256 {
		t.Fatalf("loopback notice = %+v", ln)
	}
}

func TestAutoRevokeStopsActions(t *testing.T) {
	env := newAutoEnv(t, true)
	ctx := t.Context()
	run, view := env.atSceneReview(t, "只保留团战", [2]float64{0.9, 0.8})
	if _, err := env.svc.RevokeAuto(ctx, "agent:x", run.RunID, "no", view.AutoVersion, false); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("agent revoke: %v", err)
	}
	if _, err := env.svc.RevokeAuto(ctx, "human:webui", run.RunID, "  ", view.AutoVersion, false); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("empty reason: %v", err)
	}
	if _, err := env.svc.RevokeAuto(ctx, "human:webui", run.RunID, "stale", view.AutoVersion+1, false); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale version: %v", err)
	}
	actionsBefore := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions`)
	tasksBefore := count(t, env.svc, `SELECT COUNT(*) FROM tasks`)
	revoked, err := env.svc.RevokeAuto(ctx, "human:webui", run.RunID, "不想自动了", view.AutoVersion, false)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.State != model.AutoStateRevoked || revoked.RevokedBy != "human:webui" || revoked.RevokeReason != "不想自动了" || revoked.AutoVersion != view.AutoVersion+1 {
		t.Fatalf("revoked = %+v", revoked)
	}
	var snap struct {
		Granted           bool `json:"granted"`
		GrantedByThisRun  bool `json:"granted_by_this_run"`
		ProfileRevisionNo int  `json:"revision"`
	}
	if err := json.Unmarshal([]byte(revoked.ConsentAtRevoke), &snap); err != nil || !snap.Granted || !snap.GrantedByThisRun || snap.ProfileRevisionNo != 1 {
		t.Fatalf("consent_at_revoke = %q (%v)", revoked.ConsentAtRevoke, err)
	}
	if ok, _ := storeOf(t, env.svc).ProfileConsent(ctx, env.profile.ProfileID, 1, env.sha); !ok {
		t.Fatal("profile consent is shared with manual runs and must survive a plain revoke")
	}
	_, err = env.svc.AutoConfirmScene(ctx, run.RunID, run.Version, revoked.AutoVersion, run.CurrentRevisionID)
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("auto action after revoke: %v", err)
	}
	if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions`); n != actionsBefore {
		t.Fatalf("auto_actions %d -> %d", actionsBefore, n)
	}
	if n := count(t, env.svc, `SELECT COUNT(*) FROM tasks`); n != tasksBefore {
		t.Fatalf("tasks %d -> %d", tasksBefore, n)
	}
	if n := count(t, env.svc, `SELECT COUNT(*) FROM scene_reviews WHERE run_id=?`, run.RunID); n != 0 {
		t.Fatalf("scene_reviews written after revoke: %d", n)
	}
	rows := auditActions(t, env.svc)["auto.revoke"]
	if len(rows) != 1 || rows[0].Actor != "human:webui" {
		t.Fatalf("revoke audit = %+v", rows)
	}
	cur, _, _ := env.svc.GetWorkflow(ctx, run.RunID)
	if cur.Status != model.WorkflowAwaitingReview {
		t.Fatalf("revoking must leave the review gate open for a human: %+v", cur)
	}
	if _, err := env.svc.RevokeAuto(ctx, "human:webui", run.RunID, "again", revoked.AutoVersion, false); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("second revoke: %v", err)
	}
	if _, err := env.svc.ConfirmScene(ctx, "human:webui", run.RunID, cur.Version, cur.CurrentRevisionID, "scn_1", "confirmed", ""); err != nil {
		t.Fatalf("a human can still review after revoke: %v", err)
	}
	// alsoRevokeConsent withdraws the profile consent and records the old state.
	env2 := newAutoEnv(t, true)
	run2, view2 := env2.start(t, "")
	out, err := env2.svc.RevokeAuto(ctx, "human:webui", run2.RunID, "x", view2.AutoVersion, true)
	if err != nil || !strings.Contains(out.ConsentAtRevoke, `"granted":true`) {
		t.Fatalf("consent revoke: %+v %v", out, err)
	}
	if ok, _ := storeOf(t, env2.svc).ProfileConsent(ctx, env2.profile.ProfileID, 1, env2.sha); ok {
		t.Fatal("also_revoke_consent did not withdraw the consent")
	}
	// A run that is not an auto run has nothing to revoke.
	if _, err := env2.svc.RevokeAuto(ctx, "human:webui", "run_missing", "x", 1, false); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
}

func TestAutoConfirmSceneConfirmsAllInOneTransaction(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
	if _, err := env.svc.AutoConfirmScene(ctx, run.RunID, run.Version, view.AutoVersion+1, run.CurrentRevisionID); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale auto version: %v", err)
	}
	if _, err := env.svc.AutoConfirmScene(ctx, run.RunID, run.Version+1, view.AutoVersion, run.CurrentRevisionID); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale run version: %v", err)
	}
	after, err := env.svc.AutoConfirmScene(ctx, run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != run.Version+1 || after.Stage == "scene_review" {
		t.Fatalf("run did not advance in one step: %+v", after)
	}
	reviews, _ := storeOf(t, env.svc).ListSceneReviews(ctx, run.RunID, run.CurrentRevisionID)
	if len(reviews) != 2 {
		t.Fatalf("reviews = %+v", reviews)
	}
	for _, r := range reviews {
		if r.Actor != model.AutoActor || r.Decision != "confirmed" {
			t.Fatalf("review = %+v", r)
		}
	}
	v2, _ := env.svc.GetAuto(ctx, run.RunID)
	if v2.ActionsUsed != 1 || v2.AutoVersion != view.AutoVersion+1 {
		t.Fatalf("budget accounting: %+v", v2)
	}
	if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='scene_confirm'`, run.RunID); n != 1 {
		t.Fatalf("ledger rows = %d", n)
	}
	kinds := map[string]int{}
	for _, d := range v2.Decisions {
		kinds[d.Kind]++
		if d.Kind == model.AutoDecisionSceneConfirm && d.Actor != model.AutoActor {
			t.Fatalf("decision actor = %q", d.Actor)
		}
	}
	if kinds[model.AutoDecisionSceneConfirm] != 2 || kinds[model.AutoDecisionAuthorize] != 1 {
		t.Fatalf("decisions = %+v", kinds)
	}
	if len(auditActions(t, env.svc)["auto.scene_confirm"]) != 1 {
		t.Fatal("scene confirmation not audited")
	}
}

func TestAutoConfirmSceneSkipsHumanDecidedRows(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
	// A human confirms one scene first. The scene stays human-owned and the
	// batch still completes without counting it as an automatic action.
	afterHuman, err := env.svc.ConfirmScene(ctx, "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "scn_1", "confirmed", "ok")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := env.svc.GetAuto(ctx, run.RunID); got.State != model.AutoStateActive {
		t.Fatalf("a human confirmation must not halt the run: %+v", got)
	}
	after, err := env.svc.AutoConfirmScene(ctx, run.RunID, afterHuman.Version, view.AutoVersion, run.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Stage == "scene_review" {
		t.Fatalf("batch did not complete: %+v", after)
	}
	reviews, _ := storeOf(t, env.svc).ListSceneReviews(ctx, run.RunID, run.CurrentRevisionID)
	actors := map[string]string{}
	for _, r := range reviews {
		actors[r.SceneID] = r.Actor
	}
	if actors["scn_1"] != "human:webui" || actors["scn_2"] != model.AutoActor {
		t.Fatalf("actors = %+v", actors)
	}
	v2, _ := env.svc.GetAuto(ctx, run.RunID)
	if v2.ActionsUsed != 1 || v2.State != model.AutoStateActive {
		t.Fatalf("view = %+v", v2)
	}
	skipped := 0
	for _, d := range v2.Decisions {
		if d.Kind == model.AutoDecisionSkipHuman && d.Target == "scn_1" && d.Actor == "human:webui" {
			skipped++
		}
	}
	if skipped != 1 {
		t.Fatalf("skip decisions = %d (%+v)", skipped, v2.Decisions)
	}
}

func TestAutoConfirmSceneRefusesWhatItCannotJustify(t *testing.T) {
	ctx := t.Context()
	// Low confidence: nothing is written and the error names the reason.
	env := newAutoEnv(t, false)
	run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.3})
	_, err := env.svc.AutoConfirmScene(ctx, run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID)
	var policy *service.AutoPolicyError
	if !errors.As(err, &policy) || policy.Code != "low_confidence" || !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("low confidence: %v", err)
	}
	if n := count(t, env.svc, `SELECT COUNT(*) FROM scene_reviews`) + count(t, env.svc, `SELECT COUNT(*) FROM auto_actions`); n != 0 {
		t.Fatalf("a refused batch wrote %d rows", n)
	}
	// A human rejection is never confirmed over.
	afterHuman, err := env.svc.ConfirmScene(ctx, "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "scn_1", "rejected", "bad")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.AutoConfirmScene(ctx, run.RunID, afterHuman.Version, view.AutoVersion, run.CurrentRevisionID); err == nil {
		t.Fatal("auto confirmed around a human rejection")
	}
	if reviews, _ := storeOf(t, env.svc).ListSceneReviews(ctx, run.RunID, run.CurrentRevisionID); len(reviews) != 1 || reviews[0].Actor != "human:webui" {
		t.Fatalf("reviews = %+v", reviews)
	}
	// Not an auto run.
	plain := newAutoEnv(t, false)
	pr, err := plain.svc.StartWorkflow(ctx, "human:webui", "clip", "", "plain", "configured")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.svc.AutoConfirmScene(ctx, pr.RunID, pr.Version, 1, pr.CurrentRevisionID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("non-auto run: %v", err)
	}
	// Wrong stage.
	early := newAutoEnv(t, false)
	er, ev := early.start(t, "")
	if _, err := early.svc.AutoConfirmScene(ctx, er.RunID, er.Version, ev.AutoVersion, er.CurrentRevisionID); err == nil {
		t.Fatal("confirmed before scene review")
	}
	// Spent budget and an expired deadline.
	spent := newAutoEnv(t, false)
	sr, sv := spent.atSceneReview(t, "", [2]float64{0.9, 0.8})
	db := storeOf(t, spent.svc).DB()
	if _, err := db.Exec(`UPDATE workflow_auto SET actions_used = 200 WHERE run_id=?`, sr.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := spent.svc.AutoConfirmScene(ctx, sr.RunID, sr.Version, sv.AutoVersion, sr.CurrentRevisionID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("budget: %v", err)
	}
	if _, err := db.Exec(`UPDATE workflow_auto SET actions_used = 0, deadline_at = ? WHERE run_id=?`, time.Now().UTC().Add(-time.Minute), sr.RunID); err != nil {
		t.Fatal(err)
	}
	if _, err := spent.svc.AutoConfirmScene(ctx, sr.RunID, sr.Version, sv.AutoVersion, sr.CurrentRevisionID); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("deadline: %v", err)
	}
}

func TestAutoReceiptMustCarryTheRunsBrief(t *testing.T) {
	env := newAutoEnv(t, false)
	run, view := env.start(t, "只保留团战")
	if err := env.recognized(t, run, [2]float64{0.9, 0.8}, ""); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("receipt without the brief hash: %v", err)
	}
	if got, _, _ := env.svc.GetWorkflow(t.Context(), run.RunID); got.Stage != model.TaskTypeRecognize {
		t.Fatalf("a refused delivery advanced the run: %+v", got)
	}
	if view.BriefSHA256 == "" {
		t.Fatal("brief hash missing")
	}
	// A run without a brief must not accept a receipt that names one.
	plain := newAutoEnv(t, false)
	prun, pview := plain.start(t, "")
	if err := plain.recognized(t, prun, [2]float64{0.9, 0.8}, strings.Repeat("a", 64)); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("receipt with a stray brief hash: %v", err)
	}
	if pview.BriefSHA256 != "" {
		t.Fatalf("empty brief must have no hash: %q", pview.BriefSHA256)
	}
}

func TestRuleAAutoRunExcludesNewRunsAndReopen(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	run, view := env.start(t, "")
	if _, err := env.svc.StartWorkflow(ctx, "human:webui", "clip", "", "manual", "configured"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("StartWorkflow beside an auto run: %v", err)
	}
	if _, _, err := env.svc.StartAutoRun(ctx, "human:webui", env.request(t, "second", "", false)); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("second auto run: %v", err)
	}
	// Force the asset to look exported, as it does after an auto run's export.
	st := storeOf(t, env.svc)
	a, _ := st.GetAsset(ctx, "clip")
	a.Status = model.AssetStatusExported
	a.Artifacts = map[string]string{"edl": "src/edl.json", "manifest": "src/delivery-manifest.json"}
	if err := st.UpdateAsset(ctx, a); err != nil {
		t.Fatal(err)
	}
	// The rule fires before the reopen side effects, not after.
	if _, err := env.svc.StartWorkflow(ctx, "human:webui", "clip", "", "manual2", "configured"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("StartWorkflow on an exported asset: %v", err)
	}
	if got, _ := st.GetAsset(ctx, "clip"); got.Status != model.AssetStatusExported {
		t.Fatalf("a refused start flipped the asset to %s", got.Status)
	}
	if _, err := env.svc.ReopenAssetFromStart(ctx, "human:webui", "clip"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("ReopenAsset(fromStart): %v", err)
	}
	if got, _ := st.GetAsset(ctx, "clip"); got.Status != model.AssetStatusExported {
		t.Fatalf("the guard flipped the asset to %s", got.Status)
	}
	if got, _ := env.svc.GetAuto(ctx, run.RunID); got.State != model.AutoStateActive || got.AutoVersion != view.AutoVersion {
		t.Fatalf("a refused call changed the auto run: %+v", got)
	}
	// A person reopening takes the run over, then the reopen proceeds.
	reopened, err := env.svc.ReopenAsset(ctx, "human:webui", "clip")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != model.AssetStatusIngested {
		t.Fatalf("reopened = %+v", reopened)
	}
	got, _ := env.svc.GetAuto(ctx, run.RunID)
	if got.State != model.AutoStateHalted || got.HaltReason != model.AutoHaltHumanTakeover || got.AutoVersion != view.AutoVersion+1 {
		t.Fatalf("takeover = %+v", got)
	}
	takeovers := 0
	for _, d := range got.Decisions {
		if d.Kind == model.AutoDecisionTakeover && d.Actor == "human:webui" {
			takeovers++
		}
	}
	if takeovers != 1 || len(auditActions(t, env.svc)["auto.takeover"]) != 1 {
		t.Fatalf("takeover record: %+v", got.Decisions)
	}
}

func TestDecideAuto(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	run, view := env.start(t, "")
	if _, _, err := env.svc.DecideAuto(ctx, "human:webui", run.RunID, "export", "", view.AutoVersion, "d1"); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("decision while active: %v", err)
	}
	db := storeOf(t, env.svc).DB()
	if _, err := db.Exec(`UPDATE workflow_auto SET state='awaiting_user_decision', delivery_hold=1 WHERE run_id=?`, run.RunID); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"agent": func() error {
			_, _, err := env.svc.DecideAuto(ctx, "agent:x", run.RunID, "export", "", view.AutoVersion, "d")
			return err
		},
		"unknown decision": func() error {
			_, _, err := env.svc.DecideAuto(ctx, "human:webui", run.RunID, "approve", "", view.AutoVersion, "d")
			return err
		},
		"revise without feedback": func() error {
			_, _, err := env.svc.DecideAuto(ctx, "human:webui", run.RunID, "revise", "", view.AutoVersion, "d")
			return err
		},
		"stale version": func() error {
			_, _, err := env.svc.DecideAuto(ctx, "human:webui", run.RunID, "export", "", view.AutoVersion+5, "d")
			return err
		},
		"revise (driver not available yet)": func() error {
			_, _, err := env.svc.DecideAuto(ctx, "human:webui", run.RunID, "revise", "节奏再快一点", view.AutoVersion, "d")
			return err
		},
	} {
		if err := call(); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if got, _ := env.svc.GetAuto(ctx, run.RunID); got.State != model.AutoStateAwaitingDecision || !got.DeliveryHold {
		t.Fatalf("a refused decision changed the run: %+v", got)
	}
	_, out, err := env.svc.DecideAuto(ctx, "human:webui", run.RunID, "export", "", view.AutoVersion, "d-export")
	if err != nil {
		t.Fatal(err)
	}
	if out.State != model.AutoStateCompleted || out.DeliveryHold || out.AutoVersion != view.AutoVersion+1 {
		t.Fatalf("export = %+v", out)
	}
	_, replay, err := env.svc.DecideAuto(ctx, "human:webui", run.RunID, "export", "", view.AutoVersion, "d-export")
	if err != nil || replay.AutoVersion != out.AutoVersion {
		t.Fatalf("replay = %+v %v", replay, err)
	}
	exports := 0
	for _, d := range replay.Decisions {
		if d.Kind == model.AutoDecisionUserExport && d.Actor == "human:webui" {
			exports++
		}
	}
	if exports != 1 {
		t.Fatalf("replay wrote %d export decisions", exports)
	}
	disc := newAutoEnv(t, false)
	dr, dv := disc.start(t, "")
	if _, err := storeOf(t, disc.svc).DB().Exec(`UPDATE workflow_auto SET state='awaiting_user_decision' WHERE run_id=?`, dr.RunID); err != nil {
		t.Fatal(err)
	}
	_, out, err = disc.svc.DecideAuto(ctx, "human:webui", dr.RunID, "discard", "", dv.AutoVersion, "d-discard")
	if err != nil || out.State != model.AutoStateHalted || out.HaltReason != model.AutoHaltUserDiscarded {
		t.Fatalf("discard = %+v %v", out, err)
	}
}

func TestRecordAcceptanceIsHumanOnly(t *testing.T) {
	svc, _, run := workflowSetup(t)
	ctx := t.Context()
	for _, actor := range []string{"system:auto-review", "agent:x", "Human:x", "HUMAN:x", "", "human", "xhuman:x", " human:x"} {
		if _, err := svc.RecordAcceptance(ctx, actor, run.RunID, "premiere", "passed", "", run.Version); !errors.Is(err, model.ErrForbidden) {
			t.Fatalf("actor %q: %v", actor, err)
		}
	}
	// A human passes the allow-list and reaches the ordinary state check.
	if _, err := svc.RecordAcceptance(ctx, "human:webui", run.RunID, "premiere", "passed", "", run.Version); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("human: %v", err)
	}
}

func TestNonAutoRunTaskInputHasNoAutoBlock(t *testing.T) {
	svc, _, run := workflowSetup(t)
	tk, err := svc.ClaimTask(t.Context(), "recognizer", "recognizer", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	in, err := svc.GetTaskInput(t.Context(), "recognizer", "recognizer", tk.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(in)
	if in.Auto != nil || strings.Contains(string(raw), `"auto"`) {
		t.Fatalf("a non-auto run grew an auto block: %s", raw)
	}
	if _, err := svc.GetAuto(t.Context(), run.RunID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("GetAuto on a non-auto run: %v", err)
	}
}
