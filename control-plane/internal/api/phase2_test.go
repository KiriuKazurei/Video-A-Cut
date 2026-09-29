package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// TestPhase2RESTServiceStoreSSEAudit proves one governance change traverses
// the actual HTTP, service, SQLite, event stream, and audit paths as one
// connected operation.
func TestPhase2RESTServiceStoreSSEAudit(t *testing.T) {
	srv := newServer(t)
	bus := events.New()
	t.Cleanup(bus.Close)
	srv.svc.SetBus(bus)
	seed := model.Asset{
		AssetID:      "phase2_asset",
		Status:       model.AssetStatusIngested,
		AgentVisible: true,
		AllowedAgents: []string{
			"recognizer",
		},
	}
	if err := srv.svc.CreateAsset(context.Background(), seed); err != nil {
		t.Fatal(err)
	}

	stream, _ := connectSSE(t, srv)
	defer stream.stop()
	base := stream.ts.URL
	client := stream.ts.Client()

	getAsset := func() model.Asset {
		t.Helper()
		resp, err := client.Get(base + "/api/assets/phase2_asset")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("GET asset = %d: %s", resp.StatusCode, body)
		}
		var got model.Asset
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	before := getAsset()
	if before.AssetID != seed.AssetID || before.HumanApproved {
		t.Fatalf("initial REST snapshot = %+v", before)
	}

	patchReq, err := http.NewRequest(http.MethodPatch, base+"/api/assets/phase2_asset", strings.NewReader(`{"human_approved":true}`))
	if err != nil {
		t.Fatal(err)
	}
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq.Header.Set("Origin", base)
	patchResp, err := client.Do(patchReq)
	if err != nil {
		t.Fatal(err)
	}
	var patched model.Asset
	if patchResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(patchResp.Body)
		patchResp.Body.Close()
		t.Fatalf("PATCH asset = %d: %s", patchResp.StatusCode, body)
	}
	if err := json.NewDecoder(patchResp.Body).Decode(&patched); err != nil {
		patchResp.Body.Close()
		t.Fatal(err)
	}
	patchResp.Body.Close()
	if !patched.HumanApproved || patched.UpdatedAt.IsZero() {
		t.Fatalf("PATCH did not return the canonical saved asset: %+v", patched)
	}

	frame := stream.waitFrameName(t, "asset_updated")
	var eventAsset model.Asset
	if err := json.Unmarshal([]byte(frame.data), &eventAsset); err != nil {
		t.Fatalf("SSE payload is not an asset: %v (%q)", err, frame.data)
	}
	if !reflect.DeepEqual(eventAsset, patched) {
		t.Fatalf("SSE snapshot differs from PATCH response:\nSSE:   %+v\nPATCH: %+v", eventAsset, patched)
	}
	if persisted := getAsset(); !reflect.DeepEqual(persisted, patched) {
		t.Fatalf("GET after PATCH differs from stored response:\nGET:   %+v\nPATCH: %+v", persisted, patched)
	}

	auditResp, err := client.Get(base + "/api/audit")
	if err != nil {
		t.Fatal(err)
	}
	defer auditResp.Body.Close()
	if auditResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(auditResp.Body)
		t.Fatalf("GET audit = %d: %s", auditResp.StatusCode, body)
	}
	var rows []model.AuditLog
	if err := json.NewDecoder(auditResp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Action == "asset.governance" && row.Target == seed.AssetID {
			if row.Actor != humanActor || !strings.Contains(row.Detail, "human_approved=true") || row.CreatedAt.IsZero() {
				t.Fatalf("audit row does not describe the REST change: %+v", row)
			}
			return
		}
	}
	t.Fatalf("audit did not contain the persisted governance change: %+v", rows)
}
