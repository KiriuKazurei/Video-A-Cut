package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestPreviewStoreCRUDAndIdempotency(t *testing.T) {
	ctx := context.Background()
	s := mustOpen(t)

	// Create prerequisite asset, workflow run, revision, and task
	asset := model.Asset{
		AssetID:       "asset-prev-1",
		Status:        model.AssetStatusExported,
		AgentVisible:  true,
		AllowedAgents: []string{"exporter"},
	}
	if err := s.CreateAsset(ctx, asset); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	rev := model.Revision{
		RevisionID:    "rev-prev-1",
		AssetID:       asset.AssetID,
		SchemaVersion: 1,
		PackageRef:    "exports/p1",
		EDLSHA256:     "abc",
		CreatedAt:     time.Now().UTC(),
		CreatedBy:     "agent:exporter",
		Reason:        "export",
	}
	if err := s.InsertRevision(ctx, rev); err != nil {
		t.Fatalf("InsertRevision: %v", err)
	}

	run := model.WorkflowRun{
		RunID:             "run-prev-1",
		AssetID:           asset.AssetID,
		BaseRevisionID:    rev.RevisionID,
		CurrentRevisionID: rev.RevisionID,
		Status:            model.WorkflowReadyAcceptance,
		Stage:             "acceptance",
		Version:           1,
		ContentMode:       "configured",
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := s.InsertWorkflow(ctx, run); err != nil {
		t.Fatalf("InsertWorkflow: %v", err)
	}

	task := model.Task{
		TaskID:    "task-prev-1",
		AssetID:   asset.AssetID,
		Type:      model.TaskTypePreview,
		AgentRole: "exporter",
		Status:    model.TaskStatusQueued,
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	job := model.PreviewJob{
		PreviewID:       "prev-1",
		TaskID:          task.TaskID,
		AssetID:         asset.AssetID,
		RunID:           run.RunID,
		RevisionID:      rev.RevisionID,
		RequestID:       "req-1",
		ExpectedVersion: 1,
		InputJSON:       `{"schema_version":1}`,
		InputSHA256:     "input-sha-1",
		PresetSHA256:    "preset-sha-1",
		CreatedBy:       "human:tester",
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := s.InsertPreviewJob(ctx, job); err != nil {
		t.Fatalf("InsertPreviewJob: %v", err)
	}

	// Read by ID
	got, err := s.GetPreviewJob(ctx, "prev-1")
	if err != nil {
		t.Fatalf("GetPreviewJob: %v", err)
	}
	if got.PreviewID != "prev-1" || got.TaskID != task.TaskID || got.Invalidated {
		t.Fatalf("GetPreviewJob unexpected: %+v", got)
	}

	// Read by TaskID
	byTask, err := s.GetPreviewJobByTaskID(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetPreviewJobByTaskID: %v", err)
	}
	if byTask.PreviewID != "prev-1" {
		t.Fatalf("GetPreviewJobByTaskID unexpected: %+v", byTask)
	}

	// Read by (run_id, request_id)
	byReq, err := s.GetPreviewJobByRequest(ctx, run.RunID, "req-1")
	if err != nil {
		t.Fatalf("GetPreviewJobByRequest: %v", err)
	}
	if byReq.PreviewID != "prev-1" {
		t.Fatalf("GetPreviewJobByRequest unexpected: %+v", byReq)
	}

	// Reusable preview query
	reusable, err := s.FindReusablePreviewJob(ctx, run.RunID, rev.RevisionID, "input-sha-1")
	if err != nil {
		t.Fatalf("FindReusablePreviewJob: %v", err)
	}
	if reusable.PreviewID != "prev-1" {
		t.Fatalf("FindReusablePreviewJob returned %s, want prev-1", reusable.PreviewID)
	}

	// Update completion
	if err := s.UpdatePreviewJobSuccess(ctx, "prev-1", "previews/out", "manifest-sha", `{"width":1280}`); err != nil {
		t.Fatalf("UpdatePreviewJobSuccess: %v", err)
	}
	updated, err := s.GetPreviewJob(ctx, "prev-1")
	if err != nil {
		t.Fatalf("GetPreviewJob after update: %v", err)
	}
	if updated.OutputDir != "previews/out" || updated.ManifestSHA256 != "manifest-sha" || updated.MediaJSON != `{"width":1280}` {
		t.Fatalf("Updated fields mismatch: %+v", updated)
	}

	// Artifacts
	artifacts := []model.PreviewArtifactRecord{
		{PreviewID: "prev-1", Key: "mp4", Kind: "preview", Path: "final.mp4", MIME: "video/mp4", Size: 1024, SHA256: "mp4sha"},
		{PreviewID: "prev-1", Key: "report", Kind: "render_report", Path: "render-report.json", MIME: "application/json", Size: 256, SHA256: "repsha"},
	}
	if err := s.InsertPreviewArtifacts(ctx, artifacts); err != nil {
		t.Fatalf("InsertPreviewArtifacts: %v", err)
	}

	artList, err := s.ListPreviewArtifacts(ctx, "prev-1")
	if err != nil {
		t.Fatalf("ListPreviewArtifacts: %v", err)
	}
	if len(artList) != 2 {
		t.Fatalf("ListPreviewArtifacts count = %d, want 2", len(artList))
	}

	mp4Art, err := s.GetPreviewArtifact(ctx, "prev-1", "mp4")
	if err != nil {
		t.Fatalf("GetPreviewArtifact(mp4): %v", err)
	}
	if mp4Art.Path != "final.mp4" {
		t.Fatalf("mp4Art path = %s, want final.mp4", mp4Art.Path)
	}

	// Invalidation
	if err := s.InvalidatePreviewJob(ctx, "prev-1"); err != nil {
		t.Fatalf("InvalidatePreviewJob: %v", err)
	}
	invGot, err := s.GetPreviewJob(ctx, "prev-1")
	if err != nil {
		t.Fatalf("GetPreviewJob after invalidation: %v", err)
	}
	if !invGot.Invalidated {
		t.Fatalf("expected job to be invalidated")
	}

	// Once invalidated, should not be reusable
	if _, err := s.FindReusablePreviewJob(ctx, run.RunID, rev.RevisionID, "input-sha-1"); err == nil {
		t.Fatalf("expected no reusable job after invalidation")
	}

	// Worker capability
	now := time.Now().UTC()
	if err := s.PutPreviewCapability(ctx, "agent-exp-1", 1, true, `{"ready":true}`, now.Add(time.Minute)); err != nil {
		t.Fatalf("PutPreviewCapability: %v", err)
	}
	capGot, err := s.GetActivePreviewCapability(ctx, "agent-exp-1")
	if err != nil {
		t.Fatalf("GetActivePreviewCapability: %v", err)
	}
	if capGot.AgentID != "agent-exp-1" || !capGot.Ready || capGot.ProtocolVersion != 1 {
		t.Fatalf("Unexpected capability: %+v", capGot)
	}

	activeCaps, err := s.ListActivePreviewCapabilities(ctx)
	if err != nil {
		t.Fatalf("ListActivePreviewCapabilities: %v", err)
	}
	if len(activeCaps) != 1 {
		t.Fatalf("ListActivePreviewCapabilities count = %d, want 1", len(activeCaps))
	}
}
