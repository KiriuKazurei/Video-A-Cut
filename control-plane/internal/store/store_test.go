package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// mustOpen opens a store on a fresh temp DB and fails the test on any error.
// t.TempDir() returns backslash-separated absolute paths on Windows; modernc
// SQLite handles those directly, so no path rewriting happens here.
func mustOpen(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/vac.db")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenAppliesMigrations(t *testing.T) {
	s := mustOpen(t)

	for _, table := range []string{"assets", "tasks", "agents", "audit_logs"} {
		var name string
		err := s.DB().QueryRow(`select name from sqlite_master where type='table' and name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing from sqlite_master: %v", table, err)
		}
		if name != table {
			t.Fatalf("table %s: got %q", table, name)
		}
	}
}

// TestOpenIsIdempotent reopens the same database file and verifies the
// migration ledger does not re-apply the already-recorded version.
func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	s1, err := store.Open(dir + "/vac.db")
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	// Read the ledger before closing the first handle: the pool is gone
	// afterwards, and the whole point of the assertion is what the first open
	// recorded.
	var before int
	if err := s1.DB().QueryRow(`select count(*) from schema_migrations`).Scan(&before); err != nil {
		t.Fatalf("count migrations before reopen: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	s2, err := store.Open(dir + "/vac.db")
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer func() { _ = s2.Close() }()

	// The count itself is not the point; that the second open did not grow it
	// is. Asserting a fixed total here would break every time a new migration
	// is added, which is the normal way this schema evolves. The migration
	// set is verified separately, against sqlite_master, in the tests that
	// own each version.
	var after int
	if err := s2.DB().QueryRow(`select count(*) from schema_migrations`).Scan(&after); err != nil {
		t.Fatalf("count migrations after reopen: %v", err)
	}
	if after != before {
		t.Fatalf("schema_migrations rows = %d after reopen, want %d (unchanged: no migration may re-apply)", after, before)
	}
}

// TestAgentAndAudit exercises UpsertAgent/GetAgent round-trips plus the
// audit log write/list path.
func TestAgentAndAudit(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.UpsertAgent(ctx, model.Agent{
		AgentID:       "narrator-01",
		Role:          "narrator",
		Health:        model.AgentHealthHealthy,
		CurrentTaskID: "t_001",
	}); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}

	got, err := s.GetAgent(ctx, "narrator-01")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.Role != "narrator" {
		t.Errorf("Role = %q, want %q", got.Role, "narrator")
	}
	if got.CurrentTaskID != "t_001" {
		t.Errorf("CurrentTaskID = %q, want %q", got.CurrentTaskID, "t_001")
	}

	if _, err := s.GetAgent(ctx, "ghost"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("GetAgent(ghost) err = %v, want model.ErrNotFound", err)
	}

	if err := s.WriteAudit(ctx, model.AuditLog{
		Actor:  "human:webui",
		Action: "asset.approve",
		Target: "clip_001",
		Detail: "{}",
	}); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}

	logs, err := s.ListAudit(ctx, 10)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("ListAudit returned %d entries, want 1", len(logs))
	}
	if logs[0].Target != "clip_001" {
		t.Errorf("Target = %q, want %q", logs[0].Target, "clip_001")
	}
	if logs[0].ID <= 0 {
		t.Errorf("ID = %d, want > 0", logs[0].ID)
	}
}

// TestUpsertAgentUpdatesExisting verifies the upsert is idempotent by
// primary key rather than inserting a second row.
func TestUpsertAgentUpdatesExisting(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	first := model.Agent{AgentID: "narrator-01", Role: "narrator", Health: model.AgentHealthHealthy, CurrentTaskID: "t_001"}
	if err := s.UpsertAgent(ctx, first); err != nil {
		t.Fatalf("first UpsertAgent: %v", err)
	}
	second := first
	second.CurrentTaskID = "t_002"
	if err := s.UpsertAgent(ctx, second); err != nil {
		t.Fatalf("second UpsertAgent: %v", err)
	}

	got, err := s.GetAgent(ctx, "narrator-01")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if got.CurrentTaskID != "t_002" {
		t.Errorf("CurrentTaskID = %q, want %q", got.CurrentTaskID, "t_002")
	}

	var count int
	if err := s.DB().QueryRow(`select count(*) from agents`).Scan(&count); err != nil {
		t.Fatalf("count agents: %v", err)
	}
	if count != 1 {
		t.Errorf("agents rows = %d, want 1", count)
	}
}

// TestWriteAuditRequiresFields checks the required-field guards.
func TestWriteAuditRequiresFields(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	cases := []struct {
		name string
		log  model.AuditLog
	}{
		{"empty actor", model.AuditLog{Actor: "", Action: "asset.approve", Target: "clip_001"}},
		{"empty action", model.AuditLog{Actor: "human:webui", Action: "", Target: "clip_001"}},
		{"empty target", model.AuditLog{Actor: "human:webui", Action: "asset.approve", Target: ""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.WriteAudit(ctx, tc.log)
			if !errors.Is(err, model.ErrArgument) {
				t.Errorf("err = %v, want model.ErrArgument", err)
			}
		})
	}
}

// TestListAuditLimitGuard verifies a non-positive limit falls back to a
// default instead of erroring or returning nothing.
func TestListAuditLimitGuard(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := s.WriteAudit(ctx, model.AuditLog{Actor: "human:webui", Action: "asset.approve", Target: "clip_001"}); err != nil {
			t.Fatalf("WriteAudit %d: %v", i, err)
		}
	}

	logs, err := s.ListAudit(ctx, 0)
	if err != nil {
		t.Fatalf("ListAudit(0): %v", err)
	}
	if len(logs) != 3 {
		t.Errorf("ListAudit(0) returned %d entries, want 3", len(logs))
	}

	logs, err = s.ListAudit(ctx, -5)
	if err != nil {
		t.Fatalf("ListAudit(-5): %v", err)
	}
	if len(logs) != 3 {
		t.Errorf("ListAudit(-5) returned %d entries, want 3", len(logs))
	}
}

// TestAssetCRUD drives the full create/get/update round trip through one
// store instance and asserts the persisted column values.
func TestAssetCRUD(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	created := model.Asset{
		AssetID:       "clip-001",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		HumanApproved: false,
		Locked:        false,
		AllowedAgents: []string{"asr", "editor"},
		Artifacts:     map[string]string{"edl": "edl.json"},
	}
	if err := s.CreateAsset(ctx, created); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	got, err := s.GetAsset(ctx, "clip-001")
	if err != nil {
		t.Fatalf("GetAsset after create: %v", err)
	}
	if got.Status != model.AssetStatusIngested {
		t.Errorf("Status = %q, want %q", got.Status, model.AssetStatusIngested)
	}
	if !got.AgentVisible {
		t.Error("AgentVisible = false, want true")
	}
	if len(got.AllowedAgents) != 2 {
		t.Errorf("len(AllowedAgents) = %d, want 2", len(got.AllowedAgents))
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("CreateAsset must populate CreatedAt/UpdatedAt")
	}
	if got.Artifacts["edl"] != "edl.json" {
		t.Errorf(`Artifacts["edl"] = %q, want %q`, got.Artifacts["edl"], "edl.json")
	}

	if err := s.UpdateAsset(ctx, model.Asset{
		AssetID:       "clip-001",
		Status:        model.AssetStatusExported,
		AgentVisible:  true,
		HumanApproved: true,
		Locked:        true,
		AllowedAgents: []string{"asr", "editor", "narrator"},
		Artifacts:     map[string]string{"edl": "edl.json", "video": "final.mp4"},
	}); err != nil {
		t.Fatalf("UpdateAsset: %v", err)
	}

	updated, err := s.GetAsset(ctx, "clip-001")
	if err != nil {
		t.Fatalf("GetAsset after update: %v", err)
	}
	if !updated.Locked {
		t.Error("Locked = false after update, want true")
	}
	if updated.Status != model.AssetStatusExported {
		t.Errorf("Status = %q, want %q", updated.Status, model.AssetStatusExported)
	}
	if !updated.HumanApproved {
		t.Error("HumanApproved = false after update, want true")
	}
	if len(updated.AllowedAgents) != 3 {
		t.Errorf("len(AllowedAgents) = %d, want 3", len(updated.AllowedAgents))
	}
	if updated.Artifacts["video"] != "final.mp4" {
		t.Errorf(`Artifacts["video"] = %q, want %q`, updated.Artifacts["video"], "final.mp4")
	}

	list, err := s.ListAssets(ctx)
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	if len(list) != 1 || list[0].AssetID != "clip-001" {
		t.Fatalf("ListAssets = %+v, want one clip-001 row", list)
	}
}

// TestCreateAssetRequiresID verifies the empty-id guard.
func TestCreateAssetRequiresID(t *testing.T) {
	s := mustOpen(t)

	err := s.CreateAsset(context.Background(), model.Asset{Status: model.AssetStatusIngested})
	if err == nil {
		t.Fatal("CreateAsset with empty AssetID must fail")
	}
	if !errors.Is(err, model.ErrArgument) {
		t.Fatalf("CreateAsset error = %v, want errors.Is ErrArgument", err)
	}
}

// TestUpdateAssetMissingReturnsNotFound verifies the update guard.
func TestUpdateAssetMissingReturnsNotFound(t *testing.T) {
	s := mustOpen(t)

	err := s.UpdateAsset(context.Background(), model.Asset{
		AssetID: "does-not-exist",
		Status:  model.AssetStatusExported,
	})
	if err == nil {
		t.Fatal("UpdateAsset on unknown asset_id must fail")
	}
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("UpdateAsset error = %v, want errors.Is ErrNotFound", err)
	}
}

// TestGetAssetMissingReturnsNotFound verifies the read path maps
// sql.ErrNoRows onto the model sentinel.
func TestGetAssetMissingReturnsNotFound(t *testing.T) {
	s := mustOpen(t)

	_, err := s.GetAsset(context.Background(), "missing")
	if err == nil {
		t.Fatal("GetAsset on unknown id must fail")
	}
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("GetAsset error = %v, want errors.Is ErrNotFound", err)
	}
}

// TestUpdateAssetRequiresID verifies the update path rejects an empty id.
func TestUpdateAssetRequiresID(t *testing.T) {
	s := mustOpen(t)

	err := s.UpdateAsset(context.Background(), model.Asset{Status: model.AssetStatusExported})
	if !errors.Is(err, model.ErrArgument) {
		t.Fatalf("UpdateAsset error = %v, want errors.Is ErrArgument", err)
	}
}

// TestCreateAssetDuplicateID verifies the primary-key conflict surfaces as
// model.ErrConflict rather than a raw driver error.
func TestCreateAssetDuplicateID(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	a := model.Asset{
		AssetID:       "clip-dup",
		Status:        model.AssetStatusIngested,
		AllowedAgents: []string{"asr"},
	}
	if err := s.CreateAsset(ctx, a); err != nil {
		t.Fatalf("first CreateAsset: %v", err)
	}
	err := s.CreateAsset(ctx, a)
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("second CreateAsset error = %v, want errors.Is ErrConflict", err)
	}
}

// TestListAssetsOrderedByID verifies ListAssets sorts by asset_id.
func TestListAssetsOrderedByID(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	for _, id := range []string{"clip-c", "clip-a", "clip-b"} {
		if err := s.CreateAsset(ctx, model.Asset{
			AssetID:       id,
			Status:        model.AssetStatusIngested,
			AllowedAgents: []string{"asr"},
		}); err != nil {
			t.Fatalf("CreateAsset(%s): %v", id, err)
		}
	}

	list, err := s.ListAssets(ctx)
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	want := []string{"clip-a", "clip-b", "clip-c"}
	if len(list) != len(want) {
		t.Fatalf("ListAssets returned %d rows, want %d", len(list), len(want))
	}
	for i, id := range want {
		if list[i].AssetID != id {
			t.Errorf("ListAssets[%d].AssetID = %q, want %q", i, list[i].AssetID, id)
		}
	}
}

// TestCreateAssetSetsTimestamps verifies the store sets both timestamps to a
// recent UTC instant, overwriting whatever the caller supplied.
func TestCreateAssetSetsTimestamps(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	before := time.Now().UTC().Add(-time.Second)
	if err := s.CreateAsset(ctx, model.Asset{
		AssetID:       "clip-ts",
		Status:        model.AssetStatusIngested,
		AllowedAgents: []string{"asr"},
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	got, err := s.GetAsset(ctx, "clip-ts")
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if got.CreatedAt.Before(before) || got.CreatedAt.After(after) {
		t.Errorf("CreatedAt = %v, want between %v and %v", got.CreatedAt, before, after)
	}
	if !got.UpdatedAt.Equal(got.CreatedAt) {
		t.Errorf("UpdatedAt = %v, want equal to CreatedAt %v", got.UpdatedAt, got.CreatedAt)
	}
	if got.CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt location = %v, want UTC", got.CreatedAt.Location())
	}
}

// TestCreateAssetDefaultsOptionalCollections verifies nil slices and nil maps
// round-trip as empty collections rather than NULL.
func TestCreateAssetDefaultsOptionalCollections(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.CreateAsset(ctx, model.Asset{
		AssetID: "clip-bare",
		Status:  model.AssetStatusIngested,
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	got, err := s.GetAsset(ctx, "clip-bare")
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if len(got.AllowedAgents) != 0 {
		t.Errorf("AllowedAgents = %v, want empty", got.AllowedAgents)
	}
	if len(got.Artifacts) != 0 {
		t.Errorf("Artifacts = %v, want empty", got.Artifacts)
	}
}

// TestUpdateAssetRefreshesUpdatedAt verifies the mutable-column update moves
// UpdatedAt while leaving CreatedAt alone.
func TestUpdateAssetRefreshesUpdatedAt(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.CreateAsset(ctx, model.Asset{
		AssetID:       "clip-upd",
		Status:        model.AssetStatusIngested,
		AllowedAgents: []string{"asr"},
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	original, err := s.GetAsset(ctx, "clip-upd")
	if err != nil {
		t.Fatalf("GetAsset before update: %v", err)
	}

	before := time.Now().UTC().Add(-time.Second)
	if err := s.UpdateAsset(ctx, model.Asset{
		AssetID:       "clip-upd",
		Status:        model.AssetStatusRecognized,
		AllowedAgents: []string{"asr"},
	}); err != nil {
		t.Fatalf("UpdateAsset: %v", err)
	}

	after, err := s.GetAsset(ctx, "clip-upd")
	if err != nil {
		t.Fatalf("GetAsset after update: %v", err)
	}
	if !after.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt = %v, want unchanged %v", after.CreatedAt, original.CreatedAt)
	}
	if after.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt = %v, want >= %v", after.UpdatedAt, before)
	}
	if after.UpdatedAt.Equal(original.UpdatedAt) {
		t.Error("UpdatedAt did not move on update")
	}
	if after.Status != model.AssetStatusRecognized {
		t.Errorf("Status = %q, want %q", after.Status, model.AssetStatusRecognized)
	}
}

// TestTaskClaimCandidates verifies the claim candidate query yields only the
// queued tasks of the requested role: a claimed task of the same role and a
// queued task of another role must both be filtered out.
func TestTaskClaimCandidates(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.CreateAsset(ctx, model.Asset{
		AssetID:       "clip_001",
		Status:        model.AssetStatusIngested,
		AllowedAgents: []string{"narrator"},
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	seed := func(id, role, status string) {
		t.Helper()
		if err := s.CreateTask(ctx, model.Task{
			TaskID:    id,
			AssetID:   "clip_001",
			Type:      model.TaskTypeTTS,
			AgentRole: role,
			Status:    status,
		}); err != nil {
			t.Fatalf("CreateTask(%s): %v", id, err)
		}
	}
	seed("t_001", "narrator", model.TaskStatusQueued)
	seed("t_002", "narrator", model.TaskStatusClaimed)
	seed("t_003", "recognizer", model.TaskStatusQueued)

	got, err := s.ClaimCandidates(ctx, "narrator")
	if err != nil {
		t.Fatalf("ClaimCandidates: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ClaimCandidates returned %d tasks, want 1: %+v", len(got), got)
	}
	if got[0].TaskID != "t_001" {
		t.Errorf("TaskID = %q, want %q", got[0].TaskID, "t_001")
	}
	if got[0].Status != model.TaskStatusQueued {
		t.Errorf("Status = %q, want %q", got[0].Status, model.TaskStatusQueued)
	}
	// Seeded without a lease or a claim: the nullable columns must decode to
	// nil pointers rather than the zero time.
	if got[0].LeaseUntil != nil {
		t.Errorf("LeaseUntil = %v, want nil", got[0].LeaseUntil)
	}
	if got[0].ClaimedAt != nil {
		t.Errorf("ClaimedAt = %v, want nil", got[0].ClaimedAt)
	}
	if got[0].AssetID != "clip_001" {
		t.Errorf("AssetID = %q, want %q", got[0].AssetID, "clip_001")
	}
}

// TestClaimCandidatesOrderedByTaskID verifies the oldest-task-first ordering
// the claim loop relies on, and that an unknown role yields an empty slice.
func TestClaimCandidatesOrderedByTaskID(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.CreateAsset(ctx, model.Asset{AssetID: "clip_001", Status: model.AssetStatusIngested, AllowedAgents: []string{"narrator"}}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	for _, id := range []string{"t_003", "t_001", "t_002"} {
		if err := s.CreateTask(ctx, model.Task{
			TaskID:    id,
			AssetID:   "clip_001",
			Type:      model.TaskTypeTTS,
			AgentRole: "narrator",
			Status:    model.TaskStatusQueued,
		}); err != nil {
			t.Fatalf("CreateTask(%s): %v", id, err)
		}
	}

	got, err := s.ClaimCandidates(ctx, "narrator")
	if err != nil {
		t.Fatalf("ClaimCandidates: %v", err)
	}
	want := []string{"t_001", "t_002", "t_003"}
	if len(got) != len(want) {
		t.Fatalf("ClaimCandidates returned %d tasks, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].TaskID != id {
			t.Errorf("ClaimCandidates[%d].TaskID = %q, want %q", i, got[i].TaskID, id)
		}
	}

	none, err := s.ClaimCandidates(ctx, "recognizer")
	if err != nil {
		t.Fatalf("ClaimCandidates(recognizer): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("ClaimCandidates(recognizer) = %+v, want empty", none)
	}
}

// TestTaskRoundTrip drives every stored task column through CreateTask and
// GetTask, including the nullable lease/claim columns and the store-stamped
// updated_at.
func TestTaskRoundTrip(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.CreateAsset(ctx, model.Asset{
		AssetID:       "clip_001",
		Status:        model.AssetStatusIngested,
		AllowedAgents: []string{"narrator"},
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	lease := time.Now().UTC().Add(30 * time.Minute)
	claimed := time.Now().UTC()
	in := model.Task{
		TaskID:     "t_001",
		AssetID:    "clip_001",
		Type:       model.TaskTypeTTS,
		AgentRole:  "narrator",
		AgentID:    "narrator-01",
		Status:     model.TaskStatusRunning,
		Progress:   0.42,
		Message:    "TTS 合成中 3/7",
		LeaseUntil: &lease,
		ClaimedAt:  &claimed,
		Artifacts:  map[string]string{"voice": "voice.wav"},
	}

	before := time.Now().UTC()
	if err := s.CreateTask(ctx, in); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if got.TaskID != "t_001" {
		t.Errorf("TaskID = %q, want %q", got.TaskID, "t_001")
	}
	if got.AssetID != "clip_001" {
		t.Errorf("AssetID = %q, want %q", got.AssetID, "clip_001")
	}
	if got.Type != model.TaskTypeTTS {
		t.Errorf("Type = %q, want %q", got.Type, model.TaskTypeTTS)
	}
	if got.AgentRole != "narrator" {
		t.Errorf("AgentRole = %q, want %q", got.AgentRole, "narrator")
	}
	if got.AgentID != "narrator-01" {
		t.Errorf("AgentID = %q, want %q", got.AgentID, "narrator-01")
	}
	if got.Status != model.TaskStatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, model.TaskStatusRunning)
	}
	if got.Progress != 0.42 {
		t.Errorf("Progress = %v, want 0.42", got.Progress)
	}
	if got.Message != "TTS 合成中 3/7" {
		t.Errorf("Message = %q, want %q", got.Message, "TTS 合成中 3/7")
	}
	if got.Artifacts["voice"] != "voice.wav" {
		t.Errorf(`Artifacts["voice"] = %q, want %q`, got.Artifacts["voice"], "voice.wav")
	}

	if got.LeaseUntil == nil {
		t.Fatal("LeaseUntil = nil, want non-nil")
	}
	if !got.LeaseUntil.Equal(lease) {
		t.Errorf("LeaseUntil = %v, want %v", got.LeaseUntil, lease)
	}
	if got.LeaseUntil.Location() != time.UTC {
		t.Errorf("LeaseUntil location = %v, want UTC", got.LeaseUntil.Location())
	}
	if got.ClaimedAt == nil {
		t.Fatal("ClaimedAt = nil, want non-nil")
	}
	if !got.ClaimedAt.Equal(claimed) {
		t.Errorf("ClaimedAt = %v, want %v", got.ClaimedAt, claimed)
	}

	// updated_at is stamped by the store, not by the caller.
	if got.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt = %v, want at or after %v", got.UpdatedAt, before)
	}
	if got.UpdatedAt.Location() != time.UTC {
		t.Errorf("UpdatedAt location = %v, want UTC", got.UpdatedAt.Location())
	}
}

// TestUpdateTaskMissingReturnsNotFound verifies the mutable-column update
// refuses silently-missing rows by wrapping model.ErrNotFound.
func TestUpdateTaskMissingReturnsNotFound(t *testing.T) {
	s := mustOpen(t)

	err := s.UpdateTask(context.Background(), model.Task{
		TaskID:    "does-not-exist",
		AssetID:   "clip_001",
		Type:      model.TaskTypeTTS,
		AgentRole: "narrator",
		Status:    model.TaskStatusSucceeded,
	})
	if err == nil {
		t.Fatal("UpdateTask on unknown task_id must fail")
	}
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("UpdateTask error = %v, want errors.Is ErrNotFound", err)
	}
}

// TestCreateTaskRequiresIDs verifies the identifier guards.
func TestCreateTaskRequiresIDs(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	cases := []struct {
		name string
		task model.Task
	}{
		{
			"empty task_id",
			model.Task{AssetID: "clip_001", Type: model.TaskTypeTTS, AgentRole: "narrator", Status: model.TaskStatusQueued},
		},
		{
			"empty asset_id",
			model.Task{TaskID: "t_001", Type: model.TaskTypeTTS, AgentRole: "narrator", Status: model.TaskStatusQueued},
		},
		{
			"both empty",
			model.Task{Type: model.TaskTypeTTS, AgentRole: "narrator", Status: model.TaskStatusQueued},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.CreateTask(ctx, tc.task)
			if !errors.Is(err, model.ErrArgument) {
				t.Errorf("CreateTask error = %v, want errors.Is ErrArgument", err)
			}
		})
	}
}

// TestGetTaskMissingReturnsNotFound verifies the single-row read maps
// sql.ErrNoRows onto the model sentinel.
func TestGetTaskMissingReturnsNotFound(t *testing.T) {
	s := mustOpen(t)

	_, err := s.GetTask(context.Background(), "missing")
	if err == nil {
		t.Fatal("GetTask on unknown id must fail")
	}
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("GetTask error = %v, want errors.Is ErrNotFound", err)
	}
}

// TestListActiveTasksExcludesTerminal seeds one task in each of the six
// statuses on a single asset and verifies the lease-recovery sweep input
// holds exactly the three non-terminal ones.
func TestListActiveTasksExcludesTerminal(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.CreateAsset(ctx, model.Asset{
		AssetID:       "clip_001",
		Status:        model.AssetStatusIngested,
		AllowedAgents: []string{"narrator"},
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	seed := func(id, status string) {
		t.Helper()
		if err := s.CreateTask(ctx, model.Task{
			TaskID:    id,
			AssetID:   "clip_001",
			Type:      model.TaskTypeTTS,
			AgentRole: "narrator",
			Status:    status,
		}); err != nil {
			t.Fatalf("CreateTask(%s): %v", id, err)
		}
	}
	seed("t_001", model.TaskStatusQueued)
	seed("t_002", model.TaskStatusClaimed)
	seed("t_003", model.TaskStatusRunning)
	seed("t_004", model.TaskStatusSucceeded)
	seed("t_005", model.TaskStatusFailed)
	seed("t_006", model.TaskStatusCancelled)

	got, err := s.ListActiveTasks(ctx)
	if err != nil {
		t.Fatalf("ListActiveTasks: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListActiveTasks returned %d tasks, want 3: %+v", len(got), got)
	}

	wantStatus := map[string]bool{
		model.TaskStatusQueued:    true,
		model.TaskStatusClaimed:   true,
		model.TaskStatusRunning:   true,
		model.TaskStatusSucceeded: false,
		model.TaskStatusFailed:    false,
		model.TaskStatusCancelled: false,
	}
	for _, tk := range got {
		if !wantStatus[tk.Status] {
			t.Errorf("ListActiveTasks returned status %q, want only queued/claimed/running", tk.Status)
		}
	}
	if got[0].AssetID != "clip_001" {
		t.Errorf("ListActiveTasks[0].AssetID = %q, want %q", got[0].AssetID, "clip_001")
	}
}

// TestListActiveTasksEmptyWhenOnlyTerminal verifies the sweep has nothing to
// look at once every task is terminal, and that it is a non-nil empty slice.
func TestListActiveTasksEmptyWhenOnlyTerminal(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()

	if err := s.CreateAsset(ctx, model.Asset{
		AssetID: "clip_001",
		Status:  model.AssetStatusIngested,
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	for _, id := range []string{"t_001", "t_002"} {
		if err := s.CreateTask(ctx, model.Task{
			TaskID:    id,
			AssetID:   "clip_001",
			Type:      model.TaskTypeTTS,
			AgentRole: "narrator",
			Status:    model.TaskStatusSucceeded,
		}); err != nil {
			t.Fatalf("CreateTask(%s): %v", id, err)
		}
	}

	got, err := s.ListActiveTasks(ctx)
	if err != nil {
		t.Fatalf("ListActiveTasks: %v", err)
	}
	if got == nil {
		t.Fatal("ListActiveTasks returned a nil slice, want an empty non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("ListActiveTasks returned %d tasks, want 0", len(got))
	}
}
