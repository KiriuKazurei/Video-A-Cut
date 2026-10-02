package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
)

func TestPreparationRESTIsReadOnlyAndBlocked(t *testing.T) {
	srv := newSeededServer(t, model.Asset{AssetID: "clip", Status: model.AssetStatusExported, Locked: true})
	body := `{"schema_version":1,"profile_id":"regression","revision":1,"name":"回归","content_mode":"builtin","vision":{"adapter":"builtin"},"narration":{"adapter":"builtin"},"sampling":{"max_frames":12,"max_bytes":12582912,"timeout_seconds":60},"tts_voice":"Microsoft Huihui Desktop","export_target":"premiere"}`
	rec := postJSON(t, srv, "/api/assets/clip/workflow-preflight", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d: %s", rec.Code, rec.Body.String())
	}
	var report preparation.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.CanStart || report.Status != "blocked" {
		t.Fatal("skeleton must not claim readiness")
	}
	asset, err := srv.svc.GetAsset(t.Context(), "clip")
	if err != nil {
		t.Fatal(err)
	}
	if !asset.Locked || asset.Status != model.AssetStatusExported {
		t.Fatal("preflight changed governance")
	}
	runs, err := srv.svc.ListWorkflows(t.Context(), "clip")
	if err != nil || len(runs) != 0 {
		t.Fatal("preflight created workflow")
	}
	bad := postJSON(t, srv, "/api/assets/clip/workflow-preflight", `{"token":"secret-marker"}`)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("raw token accepted: %d", bad.Code)
	}
}
