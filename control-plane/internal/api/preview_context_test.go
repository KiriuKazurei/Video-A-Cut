package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// Exercise the real REST revision/evidence contract used by the monitor.
func TestPreviewEvidenceRejectsOldRevisionAfterEdit(t *testing.T) {
	root, srv := deliveryFixture(t)
	dir := filepath.Join(root, "preview-source")
	if err := os.MkdirAll(filepath.Join(dir, "samples"), 0700); err != nil {
		t.Fatal(err)
	}
	var frame bytes.Buffer
	if err := png.Encode(&frame, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(frame.Bytes())
	digest := hex.EncodeToString(hash[:])
	if err := os.WriteFile(filepath.Join(dir, "samples", "frame.png"), frame.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	files := map[string]any{
		"edl.json": map[string]any{
			"timeline":  map[string]any{"fps": 30, "sample_rate": 48000},
			"video":     []any{map[string]any{"src": "a.mp4", "in": 0, "out": 1, "timeline_in": 0}},
			"scenes":    []any{map[string]any{"scene_id": "scene", "label": "old scene", "index": 0, "evidence_frames": []string{digest}}},
			"narration": []any{}, "game_audio": []any{}, "voice": []any{}, "subtitle": []any{}, "music": []any{},
		},
		"samples/evidence-manifest.json": map[string]any{"frames": []any{map[string]any{"path": "samples/frame.png", "sha256": digest, "clip_index": 0, "timestamp": 0.5}}},
		"delivery-manifest.json": map[string]any{"schema_version": 1, "artifacts": []any{
			map[string]string{"kind": "edl", "path": "edl.json"}, map[string]string{"kind": "sample", "path": "samples/frame.png"}, map[string]string{"kind": "evidence_manifest", "path": "samples/evidence-manifest.json"},
		}},
	}
	for name, value := range files {
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.svc.CreateAsset(t.Context(), model.Asset{AssetID: "preview-asset", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"recognizer", "narrator", "exporter"}, Artifacts: map[string]string{"edl": "preview-source/edl.json"}}); err != nil {
		t.Fatal(err)
	}
	visible, err := srv.svc.VisibleSet(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.svc.ConfirmVisibleSet(t.Context(), "human:test", visible.SHA256, false); err != nil {
		t.Fatal(err)
	}
	run, err := srv.svc.StartWorkflow(t.Context(), "human:webui", "preview-asset", "", "preview-test", "configured")
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/workflows/" + run.RunID
	oldURL := fmt.Sprintf("%s/evidence/%s?revision_id=%s", base, digest, run.CurrentRevisionID)
	if rec := deliveryRequest(t, srv, http.MethodGet, oldURL, ""); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), frame.Bytes()) {
		t.Fatalf("initial evidence = %d %s", rec.Code, rec.Body.String())
	}
	body, err := json.Marshal(map[string]any{"expected_version": run.Version, "revision_id": run.CurrentRevisionID, "scene_id": "scene", "label": "new scene"})
	if err != nil {
		t.Fatal(err)
	}
	edited := deliveryRequest(t, srv, http.MethodPost, base+"/revisions", string(body))
	if edited.Code != http.StatusCreated && edited.Code != http.StatusOK {
		t.Fatalf("edit = %d %s", edited.Code, edited.Body.String())
	}
	var updated model.WorkflowRun
	if err := json.Unmarshal(edited.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.CurrentRevisionID == run.CurrentRevisionID || updated.Version <= run.Version {
		t.Fatal("edit did not advance revision/version")
	}
	if rec := deliveryRequest(t, srv, http.MethodGet, oldURL, ""); rec.Code != http.StatusConflict {
		t.Fatalf("old evidence = %d %s, want 409", rec.Code, rec.Body.String())
	}
	newURL := fmt.Sprintf("%s/evidence/%s?revision_id=%s", base, digest, updated.CurrentRevisionID)
	if rec := deliveryRequest(t, srv, http.MethodGet, newURL, ""); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), frame.Bytes()) {
		t.Fatalf("new evidence = %d %s", rec.Code, rec.Body.String())
	}
	rec := deliveryRequest(t, srv, http.MethodGet, base+"/review", "")
	var view service.ReviewView
	if rec.Code != http.StatusOK {
		t.Fatalf("review = %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Revision.RevisionID != updated.CurrentRevisionID || view.Scenes[0]["label"] != "new scene" {
		t.Fatal("review and evidence contexts disagree")
	}
}
