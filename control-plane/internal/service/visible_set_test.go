package service_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func seedClip(t *testing.T, svc *service.Service, id string, visible bool) {
	t.Helper()
	seedAsset(t, svc, model.Asset{
		AssetID: id, Status: model.AssetStatusIngested, AgentVisible: visible,
		AllowedAgents: []string{"recognizer", "narrator", "exporter"},
	})
}

// Removing the confirmation check in EnterScript or StartWorkflow must turn
// this test red: an unconfirmed visible set cannot open a script or a run.
func TestUnconfirmedVisibleSetBlocksScriptAndStart(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()
	seedClip(t, svc, "clip", true)

	if _, err := svc.EnterScript(ctx, "human:test", "clip"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("EnterScript without confirmation: %v, want forbidden", err)
	}
	scripts, err := svc.ListScripts(ctx, "clip")
	if err != nil || len(scripts) != 0 {
		t.Fatalf("scripts after refused entry = %+v, %v", scripts, err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	edl := []byte(`{"timeline":{"fps":30,"sample_rate":48000},"video":[{"src":"a.mp4","in":0,"out":1,"timeline_in":0}],"game_audio":[],"voice":[],"subtitle":[],"music":[]}`)
	if err := os.WriteFile(filepath.Join(root, "src", "edl.json"), edl, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAssetGovernance(ctx, "human:test", "clip", model.Asset{
		AgentVisible: true, AllowedAgents: []string{"recognizer", "narrator", "exporter"},
	}); err != nil {
		t.Fatal(err)
	}
	// The governance write above did not confirm the set. A start must still fail
	// and must not leave a run behind.
	if _, err := svc.StartWorkflow(ctx, "human:webui", "clip", "", "no-confirm", "builtin"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("StartWorkflow without confirmation: %v, want forbidden", err)
	}
	runs, err := svc.ListWorkflows(ctx, "clip")
	if err != nil || len(runs) != 0 {
		t.Fatalf("runs after refused start = %+v, %v", runs, err)
	}
	for _, actor := range []string{"system:auto-review", "agent:worker"} {
		if _, err := svc.ConfirmVisibleSet(ctx, actor, "abc", false); !errors.Is(err, model.ErrForbidden) {
			t.Fatalf("%s ConfirmVisibleSet: %v, want forbidden", actor, err)
		}
		if _, err := svc.EnterScript(ctx, actor, "clip"); !errors.Is(err, model.ErrForbidden) {
			t.Fatalf("%s EnterScript: %v, want forbidden", actor, err)
		}
	}
}

func TestVisibleSetChangeExpiresScriptsAndBlocksDownload(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()
	seedClip(t, svc, "keep", true)
	seedClip(t, svc, "drop", true)
	confirmVisible(t, svc)

	finalized, err := svc.EnterScript(ctx, "human:test", "keep")
	if err != nil {
		t.Fatal(err)
	}
	finalized, err = svc.FinalizeScript(ctx, "human:test", finalized.ScriptID)
	if err != nil || finalized.Status != model.ScriptFinalized {
		t.Fatalf("finalize = %+v %v", finalized, err)
	}
	draft, err := svc.EnterScript(ctx, "human:test", "drop")
	if err != nil || draft.Status != model.ScriptDraft {
		t.Fatalf("draft = %+v %v", draft, err)
	}
	files, err := svc.ListDeliveryFiles(ctx, "keep")
	if err != nil || len(files) != 0 {
		t.Fatalf("files before change = %+v %v", files, err)
	}

	locked := true
	if _, err := svc.PatchAssetGovernance(ctx, "human:test", "keep", service.GovernancePatch{Locked: &locked}); err != nil {
		t.Fatal(err)
	}
	still, err := svc.ListScripts(ctx, "keep")
	if err != nil || len(still) != 1 || still[0].Status != model.ScriptFinalized {
		t.Fatalf("lock changed the script: %+v %v", still, err)
	}

	if _, err := svc.BatchSetAgentVisible(ctx, "system:auto-review", []string{"drop"}, false); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("agent batch: %v", err)
	}
	if _, err := svc.BatchSetAgentVisible(ctx, "human:test", []string{"drop"}, false); err != nil {
		t.Fatal(err)
	}
	kept, err := svc.ListScripts(ctx, "keep")
	if err != nil || len(kept) != 1 || kept[0].Status != model.ScriptNeedsRefinalize || kept[0].ScriptID != finalized.ScriptID {
		t.Fatalf("finalized script = %+v %v, want the same row needs_refinalize", kept, err)
	}
	dropped, err := svc.ListScripts(ctx, "drop")
	if err != nil || len(dropped) != 1 || dropped[0].Status != model.ScriptExpired || dropped[0].ScriptID != draft.ScriptID {
		t.Fatalf("draft script = %+v %v, want the same row expired", dropped, err)
	}
	if _, err := svc.ListDeliveryFiles(ctx, "keep"); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("download after visible-set change: %v, want invalid state", err)
	}
	if _, err := svc.EnterScript(ctx, "human:test", "keep"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("enter on a stale confirmation: %v", err)
	}

	shown := true
	if _, err := svc.BatchSetAgentVisible(ctx, "human:test", []string{"drop"}, shown); err != nil {
		t.Fatal(err)
	}
	confirmVisible(t, svc)
	again, err := svc.EnterScript(ctx, "human:test", "keep")
	if err != nil || again.ScriptID == finalized.ScriptID || again.Status != model.ScriptDraft {
		t.Fatalf("re-entry = %+v %v, want a new draft", again, err)
	}
	all, err := svc.ListScripts(ctx, "keep")
	if err != nil || len(all) != 2 {
		t.Fatalf("scripts after re-entry = %+v %v, want both rows kept", all, err)
	}
}

func TestInProgressRunReplaysAfterTheVisibleSetChanges(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	edl := []byte(`{"timeline":{"fps":30,"sample_rate":48000},"video":[{"src":"a.mp4","in":0,"out":1,"timeline_in":0}],"game_audio":[],"voice":[],"subtitle":[],"music":[]}`)
	if err := os.WriteFile(filepath.Join(root, "src", "edl.json"), edl, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateAsset(ctx, model.Asset{
		AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true,
		AllowedAgents: []string{"recognizer", "narrator", "exporter"},
		Artifacts:     map[string]string{"edl": "src/edl.json"},
	}); err != nil {
		t.Fatal(err)
	}
	confirmVisible(t, svc)
	run, err := svc.StartWorkflow(ctx, "human:webui", "clip", "", "idem-keep", "builtin")
	if err != nil {
		t.Fatal(err)
	}
	seedClip(t, svc, "extra", true)
	again, err := svc.StartWorkflow(ctx, "human:webui", "clip", "", "idem-keep", "builtin")
	if err != nil || again.RunID != run.RunID {
		t.Fatalf("replay after the set changed = %+v %v", again, err)
	}
	if _, err := svc.CancelWorkflow(ctx, "human:test", run.RunID, "finished", run.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartWorkflow(ctx, "human:webui", "clip", "", "idem-new", "builtin"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("new run on a stale confirmation: %v", err)
	}
}

func TestAutoRunParksWhenTheVisibleSetChanges(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	missing := env.request(t, "no-sha", "", false)
	missing.VisibleSetSHA = ""
	if _, _, err := env.svc.StartAutoRun(ctx, "human:webui", missing); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("auto start without visible_set_sha: %v", err)
	}
	run, view := env.start(t, "")
	if view.Scope.VisibleSetSHA == "" || !view.Scope.ScriptFinalize {
		t.Fatalf("authorization scope = %+v", view.Scope)
	}
	seedClip(t, env.svc, "extra", true)
	if err := env.svc.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	parked, err := env.svc.GetAuto(ctx, run.RunID)
	if err != nil || parked.State != model.AutoStateAwaitingDecision || parked.HaltReason != model.AutoWaitVisibleSet {
		t.Fatalf("after the set changed: %+v %v", parked, err)
	}
	if _, _, err := env.svc.DecideAuto(ctx, "system:auto-review", run.RunID, "confirm_visible_set", "", parked.AutoVersion, "agent-confirm"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("auto-review confirm: %v", err)
	}
	if _, _, err := env.svc.DecideAuto(ctx, "agent:worker", run.RunID, "confirm_visible_set", "", parked.AutoVersion, "worker-confirm"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("agent confirm: %v", err)
	}
	resumed, resumedView, err := env.svc.DecideAuto(ctx, "human:webui", run.RunID, "confirm_visible_set", "", parked.AutoVersion, "human-confirm")
	if err != nil || resumedView.State != model.AutoStateActive || resumed.RunID != run.RunID {
		t.Fatalf("human re-authorize = %+v %+v %v", resumed, resumedView, err)
	}

	legacy := newAutoEnv(t, false)
	legacyRun, _ := legacy.start(t, "")
	mustExec(t, storeOf(t, legacy.svc), `UPDATE workflow_auto SET scope_json = json_remove(scope_json, '$.visible_set_sha', '$.visible_asset_ids', '$.script_finalize') WHERE run_id = ?`, legacyRun.RunID)
	seedClip(t, legacy.svc, "later", true)
	if err := legacy.svc.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	still, err := legacy.svc.GetAuto(ctx, legacyRun.RunID)
	if err != nil || still.State != model.AutoStateActive {
		t.Fatalf("pre-F1 run was parked: %+v %v", still, err)
	}
}
