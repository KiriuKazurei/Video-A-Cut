package service_test

import (
	"encoding/json"
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCurrentProfileChecksRowIdentity(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()
	if _, e := svc.SaveProcessingProfile(ctx, "human:test", "first", 0, builtinProfile(1)); e != nil {
		t.Fatal(e)
	}
	p := builtinProfile(2)
	body, _ := json.Marshal(p)
	sum, _ := p.Fingerprint()
	_, e := storeOf(t, svc).DB().Exec(`UPDATE processing_profile_revisions SET canonical_json=?,sha256=? WHERE profile_id=?`, string(body), sum, p.ProfileID)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e := svc.GetProcessingProfile(ctx, p.ProfileID, 0); !errors.Is(e, model.ErrInvalidState) {
		t.Fatalf("current identity bypass: %v", e)
	}
}

func TestPreparedLaunchBindsAndRechecksGovernance(t *testing.T) {
	svc, root, run := workflowSetup(t)
	ctx := t.Context()
	if _, e := svc.CancelWorkflow(ctx, "human:test", run.RunID, "isolated fixture", run.Version); e != nil {
		t.Fatal(e)
	}
	p := builtinProfile(1)
	if _, e := svc.SaveProcessingProfile(ctx, "human:test", "profile", 0, p); e != nil {
		t.Fatal(e)
	}
	sha, _ := p.Fingerprint()
	readiness, e := svc.PreparedPreflight(ctx, "clip", p.ProfileID, 1)
	if e != nil {
		t.Fatal(e)
	}
	if readiness.CanStart {
		t.Fatal("missing capabilities allowed start")
	}
	for _, role := range []string{"recognizer", "narrator", "exporter"} {
		if e := svc.ReportWorkerCapability(ctx, role, role, preparation.Capability{ProfileSHA256: sha, ToolsReady: true, CredentialsReady: true, TTSVoices: []string{p.TTSVoice}}); e != nil {
			t.Fatal(e)
		}
	}
	readiness, e = svc.PreparedPreflight(ctx, "clip", p.ProfileID, 1)
	if e != nil {
		t.Fatal(e)
	}
	if !readiness.CanStart {
		t.Fatalf("fixture host tools are required: %+v", readiness)
	}
	input := preparation.PreparedStart{AssetID: "clip", ProfileID: p.ProfileID, ProfileRevision: 1, ExpectedAssetVersion: readiness.ExpectedAssetVersion, IdempotencyKey: "prepared"}
	db := storeOf(t, svc).DB()
	if _, e := db.Exec(`UPDATE assets SET locked=1 WHERE asset_id='clip'`); e != nil {
		t.Fatal(e)
	}
	if _, e := svc.StartPreparedWorkflow(ctx, "human:test", input); e == nil {
		t.Fatal("stale readiness allowed locked asset")
	}
	db.Exec(`UPDATE assets SET locked=0 WHERE asset_id='clip'`)
	readiness, _ = svc.PreparedPreflight(ctx, "clip", p.ProfileID, 1)
	input.ExpectedAssetVersion = readiness.ExpectedAssetVersion
	started, e := svc.StartPreparedWorkflow(ctx, "human:test", input)
	if e != nil {
		t.Fatal(e)
	}
	again, e := svc.StartPreparedWorkflow(ctx, "human:test", input)
	if e != nil || again.RunID != started.RunID {
		t.Fatalf("idempotency: %+v %v", again, e)
	}
	if _, e := svc.ClaimTask(ctx, "wrong-profile-worker", "recognizer", time.Now().Add(time.Minute)); !errors.Is(e, model.ErrNotFound) {
		t.Fatalf("unmatched worker stole task: %v", e)
	}
	task, e := svc.ClaimTask(ctx, "recognizer", "recognizer", time.Now().Add(time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	fixed, e := svc.GetTaskInput(ctx, "recognizer", "recognizer", task.TaskID)
	if e != nil || fixed.ProfileSHA256 != sha || fixed.ProcessingProfile.Revision != 1 {
		t.Fatalf("binding: %+v %v", fixed, e)
	}
	if _, e := svc.SaveProcessingProfile(ctx, "human:test", "second", 1, builtinProfile(2)); e != nil {
		t.Fatal(e)
	}
	fixed, e = svc.GetTaskInput(ctx, "recognizer", "recognizer", task.TaskID)
	if e != nil || fixed.ProcessingProfile.Revision != 1 {
		t.Fatalf("run drift: %+v %v", fixed, e)
	}
	writeWorkflowFixture(t, root, "bad-receipt")
	receipt := map[string]any{"task_id": task.TaskID, "revision_id": fixed.RevisionID, "content_mode": "builtin", "profile_sha256": "wrong"}
	raw, _ := json.Marshal(receipt)
	os.WriteFile(filepath.Join(root, "bad-receipt", "worker-receipt.json"), raw, 0644)
	if e := svc.SubmitDelivery(ctx, "recognizer", task.TaskID, "bad-receipt"); !errors.Is(e, model.ErrConflict) {
		t.Fatalf("wrong config result accepted: %v", e)
	}
}

func TestExpiredCapabilitiesAndExternalConsent(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()
	p := builtinProfile(1)
	p.ContentMode = "configured"
	p.Vision = preparation.Provider{Adapter: "vision", Endpoint: "https://example.com/vision", Model: "fixture", TokenEnv: "VISION_TEST"}
	p.Narration = preparation.Provider{Adapter: "narration", Endpoint: "https://example.com/narration", Model: "fixture", TokenEnv: "NARRATION_TEST"}
	if _, e := svc.SaveProcessingProfile(ctx, "human:test", "p", 0, p); e != nil {
		t.Fatal(e)
	}
	if e := svc.SetProcessingConsent(ctx, "human:test", p.ProfileID, 1, true); e != nil {
		t.Fatal(e)
	}
	if e := svc.SetProcessingConsent(ctx, "human:test", p.ProfileID, 1, false); e != nil {
		t.Fatal(e)
	}
	sha, _ := p.Fingerprint()
	svc.ReportWorkerCapability(ctx, "narrator", "narrator", preparation.Capability{ProfileSHA256: sha, ToolsReady: true, CredentialsReady: true, TTSVoices: []string{p.TTSVoice}})
	db := storeOf(t, svc).DB()
	db.Exec(`UPDATE worker_capabilities SET expires_at=?`, time.Now().UTC().Add(-time.Minute))
	caps, e := storeOf(t, svc).Capabilities(ctx, sha)
	if e != nil || len(caps) != 0 {
		t.Fatalf("stale capability: %+v %v", caps, e)
	}
	ok, e := storeOf(t, svc).ProfileConsent(ctx, p.ProfileID, 1, sha)
	if e != nil || ok {
		t.Fatal("revocation was not persisted")
	}
}
