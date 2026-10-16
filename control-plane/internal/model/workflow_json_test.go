package model_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// The auto-run overlay lives in side tables. A run that is not an auto run
// must serialize exactly as it did before: same names, same order, no new key.
func TestWorkflowRunJSONShapeUnchanged(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 30, 0, 0, time.UTC)
	full, err := json.Marshal(model.WorkflowRun{
		RunID: "run_1", AssetID: "clip", BaseRevisionID: "rev_1", CurrentRevisionID: "rev_2", Status: "awaiting_review",
		Stage: "scene_review", Version: 3, ContentMode: "configured", CreatedAt: at, UpdatedAt: at,
		ErrorCode: "e", BlockedReason: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	const wantFull = `{"run_id":"run_1","asset_id":"clip","base_revision_id":"rev_1","current_revision_id":"rev_2","status":"awaiting_review","stage":"scene_review","version":3,"content_mode":"configured","created_at":"2026-10-16T09:30:00Z","updated_at":"2026-10-16T09:30:00Z","error_code":"e","blocked_reason":"b"}`
	if string(full) != wantFull {
		t.Fatalf("WorkflowRun JSON changed:\n got %s\nwant %s", full, wantFull)
	}
	short, _ := json.Marshal(model.WorkflowRun{RunID: "run_1", AssetID: "clip", CreatedAt: at, UpdatedAt: at})
	const wantShort = `{"run_id":"run_1","asset_id":"clip","base_revision_id":"","current_revision_id":"","status":"","stage":"","version":0,"content_mode":"","created_at":"2026-10-16T09:30:00Z","updated_at":"2026-10-16T09:30:00Z"}`
	if string(short) != wantShort {
		t.Fatalf("WorkflowRun JSON (omitempty) changed:\n got %s\nwant %s", short, wantShort)
	}
}
