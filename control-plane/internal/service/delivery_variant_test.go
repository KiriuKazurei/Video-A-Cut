package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	dv "github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/deliveryvariant"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func addVariantSRT(t *testing.T, root string, rev model.Revision, text string) {
	t.Helper()
	pkg := filepath.Join(root, filepath.FromSlash(rev.PackageRef))
	b, err := os.ReadFile(filepath.Join(pkg, "delivery-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Schema    int                 `json:"schema_version"`
		Artifacts []map[string]string `json:"artifacts"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Artifacts = append(manifest.Artifacts, map[string]string{"kind": "subtitles", "path": "subtitles.srt"})
	b, _ = json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(pkg, "delivery-manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "subtitles.srt"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func registerVariantCapability(t *testing.T, svc *service.Service, agent string) {
	t.Helper()
	if err := svc.ReportDeliveryVariantCapability(t.Context(), agent, "exporter", dv.Capability{ProtocolVersion: 1, Ready: true, Specs: []dv.SpecRef{{SpecID: dv.SpecID, SHA256: dv.SpecSHA}}}); err != nil {
		t.Fatal(err)
	}
}
func variantRequest(run model.WorkflowRun, rev model.Revision, id string) dv.StartRequest {
	return dv.StartRequest{RevisionID: rev.RevisionID, ExpectedVersion: run.Version, RequestID: id, SpecID: dv.SpecID}
}
func TestVariantReadinessAndCapabilityIsolation(t *testing.T) {
	svc, root, asset, run, rev := setupPreviewExportFixture(t)
	ctx := t.Context()
	registerValidCapability(t, svc, "old-worker")
	req := variantRequest(run, rev, "first")
	if _, _, err := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, req); !errors.Is(err, model.ErrInvalidState) {
		t.Fatal(err)
	}
	registerVariantCapability(t, svc, "new-worker")
	list, err := svc.ListDeliveryVariants(ctx, run.RunID)
	if err != nil || list.CanStart {
		t.Fatal(list, err)
	}
	addVariantSRT(t, root, rev, "1\n00:00:00,005 --> 00:00:00,995\n中文字幕\n\n")
	list, err = svc.ListDeliveryVariants(ctx, run.RunID)
	if err != nil || !list.CanStart {
		t.Fatal(list, err)
	}
	view, created, err := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, req)
	if err != nil || !created {
		t.Fatal(view, created, err)
	}
	if _, err := svc.ClaimForExecution(ctx, "old-worker", "exporter", time.Now().Add(time.Minute), service.ClaimOptions{}); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("old worker claimed ASS", err)
	}
	outcome, err := svc.ClaimForExecution(ctx, "new-worker", "exporter", time.Now().Add(time.Minute), service.ClaimOptions{})
	if err != nil || outcome.Task.TaskID != view.TaskID {
		t.Fatal(outcome, err)
	}
	asset.Locked = true
	if err := getTestStore(svc).UpdateAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	list, err = svc.ListDeliveryVariants(ctx, run.RunID)
	if err != nil || list.CanStart {
		t.Fatal("locked ASS readiness", list, err)
	}
	if _, _, err := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, variantRequest(run, rev, "locked")); err == nil {
		t.Fatal("locked start allowed")
	}
}
func TestVariantDurableAliasesAndReopenCancellation(t *testing.T) {
	svc, root, asset, run, rev := setupPreviewExportFixture(t)
	ctx := t.Context()
	addVariantSRT(t, root, rev, "")
	registerVariantCapability(t, svc, "exporter")
	first, _, err := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, variantRequest(run, rev, "first"))
	if err != nil {
		t.Fatal(err)
	}
	alias, created, err := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, variantRequest(run, rev, "alias"))
	if err != nil || created || alias.VariantID != first.VariantID {
		t.Fatal(alias, created, err)
	}
	req := variantRequest(run, rev, "alias")
	req.ExpectedVersion++
	if _, _, err := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, req); !errors.Is(err, model.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := svc.ReopenAsset(ctx, "human:test", asset.AssetID); err != nil {
		t.Fatal(err)
	}
	old, created, err := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, variantRequest(run, rev, "alias"))
	if err != nil || created || old.Status != model.TaskStatusCancelled || !old.Invalidated || old.IsCurrent {
		t.Fatal(old, created, err)
	}
}
func renderVariantForTest(t *testing.T, svc *service.Service, root string, view dv.View) string {
	t.Helper()
	job, err := getTestStore(svc).GetDeliveryVariantJob(t.Context(), view.VariantID)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(root, "variant-input.json")
	if err := os.WriteFile(input, []byte(job.InputJSON), 0600); err != nil {
		t.Fatal(err)
	}
	output := "delivery-variants/" + view.AssetID + "/" + view.VariantID
	repo, _ := filepath.Abs("../../..")
	code := `import {readFile} from 'node:fs/promises';import {pathToFileURL} from 'node:url';import {join} from 'node:path';const {buildDeliveryVariant}=await import(pathToFileURL(process.argv[1]));const input=JSON.parse(await readFile(process.argv[2]));await buildDeliveryVariant({input,packageDir:join(process.argv[3],input.parent_package_ref),outputDir:join(process.argv[3],process.argv[4])});`
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-e", code, filepath.Join(repo, "workers/node/timeline-cli/src/delivery-variant.mjs"), input, root, output)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("Node ASS: %v %s", e, b)
	}
	return output
}
func TestVariantRejectsTamperedASSDespiteUpdatedManifestAndPassedReport(t *testing.T) {
	svc, root, _, run, rev := setupPreviewExportFixture(t)
	ctx := t.Context()
	addVariantSRT(t, root, rev, "1\n00:00:00,000 --> 00:00:00,990\n已批准字幕\n\n")
	registerVariantCapability(t, svc, "exporter")
	view, _, err := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, variantRequest(run, rev, "tamper"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimForExecution(ctx, "exporter", "exporter", time.Now().Add(time.Minute), service.ClaimOptions{}); err != nil {
		t.Fatal(err)
	}
	output := renderVariantForTest(t, svc, root, view)
	directory := filepath.Join(root, filepath.FromSlash(output))
	original, _ := os.ReadFile(filepath.Join(directory, "subtitles.ass"))
	changed := []byte(string(original) + "Dialogue: 0,0:00:00.00,0:00:00.99,Default,,0,0,0,,篡改文字\n")
	if err := os.WriteFile(filepath.Join(directory, "subtitles.ass"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(directory, "variant-manifest.json"))
	var manifest dv.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Artifacts {
		if manifest.Artifacts[i].Key == "ass" {
			manifest.Artifacts[i].Size = int64(len(changed))
			manifest.Artifacts[i].SHA256 = hashBytes(changed)
		}
	}
	raw, _ = json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(directory, "variant-manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := svc.SubmitDeliveryVariant(ctx, "exporter", view.TaskID, output); !errors.Is(err, model.ErrArgument) {
		t.Fatal("tampered ASS accepted", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "subtitles.ass"), original, 0600); err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Artifacts {
		if manifest.Artifacts[i].Key == "ass" {
			manifest.Artifacts[i].Size = int64(len(original))
			manifest.Artifacts[i].SHA256 = hashBytes(original)
		}
	}
	raw, _ = json.Marshal(manifest)
	_ = os.WriteFile(filepath.Join(directory, "variant-manifest.json"), raw, 0600)
	if err := svc.SubmitDeliveryVariant(ctx, "exporter", view.TaskID, output); err != nil {
		t.Fatal("valid ASS rejected", err)
	}
	if _, err := svc.ResolveDeliveryVariantFile(ctx, view.VariantID, "unknown"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal(err)
	}
	fresh, created, err := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, variantRequest(run, rev, "new-intent"))
	if err != nil || !created || fresh.VariantID == view.VariantID {
		t.Fatal(fresh, created, err)
	}
}
