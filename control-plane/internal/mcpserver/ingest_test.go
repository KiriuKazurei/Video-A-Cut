package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// The ingest task input travels through the same output schema as workflow
// inputs; it must validate over the real MCP transport.
func TestIngestTaskInputPassesToolOutputSchema(t *testing.T) {
	f := newLoopFixture(t, time.Minute, Agent{ID: "ing1", Role: ingest.RoleIngester})
	ctx := context.Background()
	base := t.TempDir()
	delivery, rec := filepath.Join(base, "delivery"), filepath.Join(base, "rec")
	for _, d := range []string{delivery, rec} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(rec, "a.mkv"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Above 2^53 and not a multiple of 256: a float64 round trip changes it.
	mtime := time.Unix(1790863408, 541840500)
	if err := os.Chtimes(filepath.Join(rec, "a.mkv"), mtime, mtime); err != nil {
		t.Fatal(err)
	}
	roots := []ingest.Root{{ID: "rec", Name: "录屏", Path: rec}}
	if err := f.svc.ConfigureDeliveryRoot(delivery); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ConfigureIngest(roots, nil); err != nil {
		t.Fatal(err)
	}
	reg, err := f.svc.RegisterRecording(ctx, "human:test", "raw1", "rec", "a.mkv", "k1")
	if err != nil {
		t.Fatal(err)
	}
	visible, locked := true, false
	agents := []string{ingest.RoleIngester}
	if _, err := f.svc.PatchAssetGovernance(ctx, "human:test", "raw1", service.GovernancePatch{AgentVisible: &visible, Locked: &locked, AllowedAgents: &agents}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StartIngestRun(ctx, "human:test", "raw1", reg.Source.SourceID, reg.SourceVersion, "k2"); err != nil {
		t.Fatal(err)
	}
	capability := map[string]any{"ingest_capability": service.IngestCapability{RootsSHA256: ingest.RootsFingerprint(roots), FFmpegReady: true, FFprobeReady: true,
		Operations: []string{"media_probe", "segment", "media_prepare"}, WorkerVersion: "test/1", ExecutionProtocol: service.ExecutionProtocolVersion}}
	if _, isErr, raw := f.call("ing1", "heartbeat", capability); isErr {
		t.Fatalf("capability heartbeat: %s", raw)
	}
	out, isErr, raw := f.call("ing1", "claim_task", map[string]any{"runtime_instance_id": "inst-1", "request_id": "req-1"})
	if isErr || out["claimed"] != true {
		t.Fatalf("claim: %s", raw)
	}
	taskID := out["task"].(map[string]any)["task_id"].(string)
	scope := out["scope"].(map[string]any)
	in, isErr, raw := f.call("ing1", "get_task_input", map[string]any{
		"task_id": taskID, "runtime_instance_id": scope["runtime_instance_id"], "execution_id": scope["execution_id"], "generation": scope["generation"],
	})
	if isErr || in["input_kind"] != "media_ingest" || in["ingest"] == nil {
		t.Fatalf("get_task_input: %s", raw)
	}
	if src := in["ingest"].(map[string]any)["source"].(map[string]any); src["relative_path"] != "a.mkv" {
		t.Fatalf("ingest source: %v", src)
	}
	if !strings.Contains(raw, `"mtime_ns":"1790863408541840500"`) {
		t.Fatalf("mtime_ns lost precision on the wire: %s", raw)
	}
}
