package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// TaskInput is the fixed package a worker must read. Versioned is false for
// tasks that are not part of a workflow run; those still return the asset EDL
// so both workers share one call.
type TaskInput struct {
	ProcessingProfile  *preparation.Profile `json:"processing_profile,omitempty"`
	ProfileSHA256      string               `json:"profile_sha256,omitempty"`
	Versioned          bool                 `json:"versioned"`
	Cancelled          bool                 `json:"cancelled"`
	TaskID             string               `json:"task_id"`
	AssetID            string               `json:"asset_id"`
	RevisionID         string               `json:"revision_id,omitempty"`
	PackageRef         string               `json:"package_ref,omitempty"`
	EDLPath            string               `json:"edl_path"`
	EDL                map[string]any       `json:"edl"`
	ContentMode        string               `json:"content_mode,omitempty"`
	NarrationApprovals []string             `json:"narration_approvals"`
	// InputKind is media_ingest for ingester tasks, which carry Ingest and
	// no EDL. Older tasks omit both fields.
	InputKind string       `json:"input_kind,omitempty"`
	Ingest    *IngestInput `json:"ingest,omitempty"`
}

var stageRoles = map[string]string{
	model.TaskTypeRecognize: "recognizer",
	model.TaskTypeSort:      "recognizer",
	model.TaskTypeNarrate:   "narrator",
	model.TaskTypeTTS:       "narrator",
	model.TaskTypeSubtitle:  "narrator",
	model.TaskTypeMix:       "narrator",
	model.TaskTypeExport:    "exporter",
}

var autoNext = map[string]string{
	model.TaskTypeSort:     model.TaskTypeNarrate,
	model.TaskTypeTTS:      model.TaskTypeSubtitle,
	model.TaskTypeSubtitle: model.TaskTypeMix,
	model.TaskTypeMix:      model.TaskTypeExport,
}

// StartWorkflow opens one run on an explicit revision, or on a new initial
// revision of the asset's current package. Builtin mode is recorded and is
// not described as model recognition.
func (s *Service) StartWorkflow(ctx context.Context, actor, assetID, revisionID, idemKey, contentMode string) (model.WorkflowRun, error) {
	return s.startWorkflow(ctx, actor, assetID, revisionID, idemKey, contentMode, nil)
}

func (s *Service) startWorkflow(ctx context.Context, actor, assetID, revisionID, idemKey, contentMode string, prepared *preparation.PreparedStart) (model.WorkflowRun, error) {
	if actor == "" || !validTaskID(assetID) {
		return model.WorkflowRun{}, fmt.Errorf("service: start workflow: actor and asset are required: %w", model.ErrArgument)
	}
	if contentMode != "builtin" && contentMode != "configured" {
		return model.WorkflowRun{}, fmt.Errorf("service: start workflow: content_mode must be builtin or configured; builtin is not model recognition: %w", model.ErrArgument)
	}
	if idemKey == "" || strings.ContainsAny(idemKey, "/\\") {
		return model.WorkflowRun{}, fmt.Errorf("service: start workflow: idempotency_key is required: %w", model.ErrArgument)
	}
	scope := "workflow.start:" + assetID
	requestHash := contentMode + "|" + revisionID
	if prepared != nil {
		scope = "prepared.start:" + assetID
		requestHash += "|" + prepared.ProfileSHA256 + "|" + prepared.ExpectedAssetVersion
	}
	if cached, err := s.idempotent(ctx, scope, idemKey, requestHash); err == nil {
		var run model.WorkflowRun
		if json.Unmarshal(cached, &run) == nil {
			return run, nil
		}
	} else if !errors.Is(err, model.ErrNotFound) {
		return model.WorkflowRun{}, err
	}
	asset, err := s.st.GetAsset(ctx, assetID)
	if err != nil {
		return model.WorkflowRun{}, fmt.Errorf("service: start workflow %s: %w", assetID, err)
	}
	if asset.Locked || !asset.AgentVisible {
		return model.WorkflowRun{}, fmt.Errorf("service: start workflow %s: asset is locked or hidden: %w", assetID, model.ErrForbidden)
	}
	if asset.InputKind == model.InputKindRawRecording {
		return model.WorkflowRun{}, fmt.Errorf("service: start workflow %s: raw recording has no prepared EDL yet: %w", assetID, model.ErrInvalidState)
	}
	if _, err := s.st.ActiveIngestRun(ctx, assetID); err == nil {
		return model.WorkflowRun{}, fmt.Errorf("service: start workflow %s: an ingest run is still active: %w", assetID, model.ErrConflict)
	} else if !errors.Is(err, model.ErrNotFound) {
		return model.WorkflowRun{}, err
	}
	for _, role := range []string{"recognizer", "narrator", "exporter"} {
		if !containsString(asset.AllowedAgents, role) {
			return model.WorkflowRun{}, fmt.Errorf("service: start workflow %s: allowed_agents missing %s: %w", assetID, role, model.ErrInvalidState)
		}
	}
	if asset.Status == model.AssetStatusExported && prepared == nil {
		if _, err := s.ReopenAsset(ctx, actor, assetID); err != nil {
			return model.WorkflowRun{}, err
		}
	}
	var run model.WorkflowRun
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		if got, body, e := tx.GetIdempotency(ctx, scope, idemKey); e == nil {
			if got != requestHash {
				return model.ErrConflict
			}
			return json.Unmarshal([]byte(body), &run)
		} else if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		if prepared != nil {
			row, e := tx.GetProfileRevision(ctx, prepared.ProfileID, prepared.ProfileRevision)
			if e != nil {
				return e
			}
			p, e := verifiedProfile(row.ProfileID, row.CanonicalJSON, row.SHA256)
			if e != nil {
				return e
			}
			if p.Revision != row.Revision || p.SchemaVersion != row.SchemaVersion || row.SHA256 != prepared.ProfileSHA256 {
				return model.ErrConflict
			}
			current, e := tx.GetAsset(ctx, assetID)
			if e != nil {
				return e
			}
			checks, version, e := s.preparedChecks(ctx, tx, current, p, row.SHA256)
			if e != nil {
				return e
			}
			if version != prepared.ExpectedAssetVersion {
				return model.ErrConflict
			}
			for _, c := range checks {
				if c.Status != "passed" {
					return model.ErrInvalidState
				}
			}
			if current.Status == model.AssetStatusExported {
				current.Status = model.AssetStatusIngested
				if e := tx.UpdateAsset(ctx, current); e != nil {
					return e
				}
			}
		}
		if _, err := tx.ActiveWorkflow(ctx, assetID); err == nil {
			return fmt.Errorf("service: start workflow %s: a run is already active: %w", assetID, model.ErrConflict)
		} else if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		rev, err := s.ensureRevision(ctx, tx, actor, assetID, revisionID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		run = model.WorkflowRun{
			RunID: newID("run"), AssetID: assetID, BaseRevisionID: rev.RevisionID, CurrentRevisionID: rev.RevisionID,
			Status: model.WorkflowRunning, Stage: model.TaskTypeRecognize, Version: 1, ContentMode: contentMode,
			CreatedAt: now, UpdatedAt: now,
		}
		if contentMode == "builtin" {
			run.BlockedReason = "content_mode=builtin is regression only, not model recognition"
		}
		if err := tx.InsertWorkflow(ctx, run); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return fmt.Errorf("service: start workflow %s: %w", assetID, model.ErrConflict)
			}
			return err
		}
		if prepared != nil {
			row, e := tx.GetProfileRevision(ctx, prepared.ProfileID, prepared.ProfileRevision)
			if e != nil {
				return e
			}
			if e := tx.BindProfile(ctx, store.ProfileBinding{RunID: run.RunID, ProfileID: row.ProfileID, Revision: row.Revision, SHA256: row.SHA256, JSON: row.CanonicalJSON}); e != nil {
				return e
			}
			if e := tx.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "workflow.profile.bind", Target: run.RunID, Detail: row.ProfileID + " sha256=" + row.SHA256}); e != nil {
				return e
			}
		}
		if err := s.queueStage(ctx, tx, run, model.TaskTypeRecognize, rev.RevisionID, ""); err != nil {
			return err
		}
		body, _ := json.Marshal(run)
		return tx.PutIdempotency(ctx, scope, idemKey, requestHash, string(body))
	})
	if err != nil {
		return model.WorkflowRun{}, err
	}

	s.publish("workflow.changed", run)
	s.audit(ctx, actor, "workflow.start", run.RunID, "content_mode="+contentMode+" revision="+run.BaseRevisionID)
	return run, nil
}

func (s *Service) ensureRevision(ctx context.Context, tx *store.Store, actor, assetID, revisionID string) (model.Revision, error) {
	if revisionID != "" {
		rev, err := tx.GetRevision(ctx, revisionID)
		if err != nil {
			return model.Revision{}, err
		}
		if rev.AssetID != assetID {
			return model.Revision{}, fmt.Errorf("revision belongs to another asset: %w", model.ErrArgument)
		}
		if _, err := s.readRevision(rev); err != nil {
			return model.Revision{}, err
		}
		return rev, nil
	}
	asset, err := tx.GetAsset(ctx, assetID)
	if err != nil {
		return model.Revision{}, err
	}
	edlRel := asset.Artifacts["edl"]
	if edlRel == "" {
		return model.Revision{}, fmt.Errorf("asset has no edl to revise: %w", model.ErrInvalidState)
	}
	pkg := strings.TrimSuffix(filepathSlashDir(edlRel), "/")
	edl, _, err := s.readEDLRel(edlRel)
	if err != nil {
		return model.Revision{}, err
	}
	snapshot := "revisions/" + assetID + "/" + newID("rev")
	if err := s.copyPackage(pkg, snapshot, edl); err != nil {
		return model.Revision{}, err
	}
	pkg = snapshot
	edlRel = pkg + "/edl.json"
	sum, err := s.hashUnderRoot(edlRel)
	if err != nil {
		return model.Revision{}, err
	}
	ev, err := evidenceHash(s.deliveryRoot, pkg)
	if err != nil {
		return model.Revision{}, err
	}
	rev := model.Revision{
		RevisionID: newID("rev"), AssetID: assetID, SchemaVersion: 1, PackageRef: pkg,
		EDLSHA256: sum, EvidenceManifestSHA256: ev, CreatedAt: time.Now().UTC(), CreatedBy: actor,
		Reason: "initial",
	}
	if err := tx.InsertRevision(ctx, rev); err != nil {
		return model.Revision{}, err
	}
	return rev, nil
}

func (s *Service) queueStage(ctx context.Context, tx *store.Store, run model.WorkflowRun, stage, inputRevision, attemptOf string) error {
	role := stageRoles[stage]
	taskID := newID("wf" + stage)
	tk := model.Task{
		TaskID: taskID, AssetID: run.AssetID, Type: stage, AgentRole: role, Status: model.TaskStatusQueued,
	}
	if err := tx.CreateTask(ctx, tk); err != nil {
		return err
	}
	return tx.InsertStage(ctx, model.WorkflowStage{
		RunID: run.RunID, TaskID: taskID, Stage: stage, InputRevisionID: inputRevision, AttemptOf: attemptOf,
	})
}

func (s *Service) GetWorkflow(ctx context.Context, runID string) (model.WorkflowRun, []model.WorkflowStage, error) {
	run, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return model.WorkflowRun{}, nil, err
	}
	stages, err := s.st.ListStages(ctx, runID)
	return run, stages, err
}

func (s *Service) ListWorkflows(ctx context.Context, assetID string) ([]model.WorkflowRun, error) {
	return s.st.ListWorkflows(ctx, assetID, 20)
}
func (s *Service) ListWorkflowsPage(ctx context.Context, assetID string, limit, offset int) ([]model.WorkflowRun, error) {
	if limit < 1 || limit > 50 || offset < 0 || offset > 100000 {
		return nil, model.ErrArgument
	}
	return s.st.ListWorkflowsPage(ctx, assetID, limit, offset)
}

// NoteWorkflowSuccess records the delivered package as the stage output and
// opens the next gate or task. A task that is not in a run is ignored.
func (s *Service) NoteWorkflowSuccess(ctx context.Context, tx *store.Store, tk model.Task, packageDir string) error {
	stg, err := tx.StageByTask(ctx, tk.TaskID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	run, err := tx.GetWorkflow(ctx, stg.RunID)
	if err != nil {
		return err
	}
	if stg.Invalidated || run.Status == model.WorkflowCancelled {
		return fmt.Errorf("service: stale delivery %s: %w", tk.TaskID, model.ErrConflict)
	}
	if _, _, err := s.boundProfile(ctx, tx, run.RunID); err != nil {
		return err
	}
	if stg.InputRevisionID != run.CurrentRevisionID {
		return fmt.Errorf("stage input is no longer current: %w", model.ErrConflict)
	}
	if path, _, e := s.deliveryPath(filepathSlash(packageDir)+"/worker-receipt.json", false); e == nil {
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		var receipt struct {
			ProfileSHA256 string `json:"profile_sha256"`
			TaskID        string `json:"task_id"`
			RevisionID    string `json:"revision_id"`
			ContentMode   string `json:"content_mode"`
		}
		if json.Unmarshal(raw, &receipt) != nil || receipt.TaskID != tk.TaskID || receipt.RevisionID != stg.InputRevisionID || receipt.ContentMode != run.ContentMode {
			return fmt.Errorf("delivery receipt does not match fixed input: %w", model.ErrConflict)
		}
		if _, sha, e := s.boundProfile(ctx, tx, run.RunID); e != nil {
			return e
		} else if sha != "" && sha != receipt.ProfileSHA256 {
			return model.ErrConflict
		}
	} else if !errors.Is(e, model.ErrNotFound) {
		return e
	} else if _, sha, e := s.boundProfile(ctx, tx, run.RunID); e != nil {
		return e
	} else if sha != "" {
		return fmt.Errorf("bound profile requires a delivery receipt: %w", model.ErrConflict)
	}
	edl, _, err := s.readPackageEDL(filepathSlash(packageDir))
	if err != nil {
		return err
	}
	if stg.Stage == model.TaskTypeRecognize || stg.Stage == model.TaskTypeSort || stg.Stage == model.TaskTypeNarrate {
		if _, err := s.evidenceFiles(filepathSlash(packageDir), edl); err != nil {
			return err
		}
	}
	if run.ContentMode == "configured" && stg.Stage == model.TaskTypeRecognize {
		for _, scene := range asMapSlice(edl["scenes"]) {
			if scene["method"] == "metadata_only" {
				return fmt.Errorf("configured workflow received builtin result: %w", model.ErrInvalidState)
			}
		}
	}
	sum, err := s.hashUnderRoot(filepathSlash(packageDir) + "/edl.json")
	if err != nil {
		return err
	}
	ev, err := evidenceHash(s.deliveryRoot, filepathSlash(packageDir))
	if err != nil {
		return err
	}
	rev := model.Revision{
		RevisionID: newID("rev"), AssetID: tk.AssetID, ParentRevisionID: stg.InputRevisionID,
		SchemaVersion: 1, PackageRef: filepathSlash(packageDir), EDLSHA256: sum, EvidenceManifestSHA256: ev,
		CreatedAt: time.Now().UTC(), CreatedBy: "agent:" + tk.AgentID, Reason: stg.Stage,
	}
	if err := tx.InsertRevision(ctx, rev); err != nil {
		return err
	}
	if err := tx.SetStageOutput(ctx, tk.TaskID, rev.RevisionID); err != nil {
		return err
	}
	run.CurrentRevisionID = rev.RevisionID
	run.ErrorCode = ""
	switch stg.Stage {
	case model.TaskTypeRecognize:
		run.Status = model.WorkflowAwaitingReview
		run.Stage = "scene_review"
		run.BlockedReason = "confirm every scene against its evidence"
	case model.TaskTypeNarrate:
		run.Status = model.WorkflowAwaitingReview
		run.Stage = "draft_review"
		run.BlockedReason = "approve every narration draft"
	case model.TaskTypeExport:
		run.Status = model.WorkflowReadyAcceptance
		run.Stage = "acceptance"
		run.BlockedReason = "acceptance is pending human review"
	default:
		next := autoNext[stg.Stage]
		if next == "" {
			return fmt.Errorf("service: no next stage after %s: %w", stg.Stage, model.ErrInvalidState)
		}
		run.Status = model.WorkflowRunning
		run.Stage = next
		run.BlockedReason = ""
		if err := s.queueStage(ctx, tx, run, next, rev.RevisionID, ""); err != nil {
			return err
		}
	}
	run.Version++
	run.UpdatedAt = time.Now().UTC()
	if err := tx.UpdateWorkflow(ctx, run); err != nil {
		return err
	}
	if run.Status == model.WorkflowReadyAcceptance {
		return auditTx(ctx, tx, "system", "workflow.ready", run.RunID, "revision="+rev.RevisionID)
	}
	return nil
}

// NoteWorkflowFailure marks the run failed when a bound task fails.
func (s *Service) NoteWorkflowFailure(ctx context.Context, tx *store.Store, tk model.Task, reason string) error {
	stg, err := tx.StageByTask(ctx, tk.TaskID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if stg.Invalidated {
		return nil
	}
	run, err := tx.GetWorkflow(ctx, stg.RunID)
	if err != nil || run.Status == model.WorkflowCancelled {
		return err
	}
	run.Status = model.WorkflowFailed
	run.Stage = stg.Stage
	run.ErrorCode = "task_failed"
	run.BlockedReason = reason
	run.Version++
	run.UpdatedAt = time.Now().UTC()
	return tx.UpdateWorkflow(ctx, run)
}

// WorkflowBlocks reports whether a held task was cancelled or invalidated.
func (s *Service) WorkflowBlocks(ctx context.Context, tx *store.Store, taskID string) error {
	if err := ingestBlocks(ctx, tx, taskID); err != nil {
		return err
	}
	stg, err := tx.StageByTask(ctx, taskID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	run, err := tx.GetWorkflow(ctx, stg.RunID)
	if err != nil {
		return err
	}
	if stg.Invalidated || run.Status == model.WorkflowCancelled {
		return fmt.Errorf("service: task %s was cancelled with its workflow: %w", taskID, model.ErrConflict)
	}
	return nil
}

// GetTaskInput returns the package the held task must read.
func (s *Service) GetTaskInput(ctx context.Context, agentID, role, taskID string) (TaskInput, error) {
	return s.GetTaskInputScoped(ctx, ExecutionScope{AgentID: agentID, Role: role, TaskID: taskID})
}

// GetTaskInputScoped returns the input of the presented execution. An old
// scope does not receive the replacement execution's input.
func (s *Service) GetTaskInputScoped(ctx context.Context, scope ExecutionScope) (TaskInput, error) {
	var auth executionAuth
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		var e error
		auth, e = authorizeExecution(ctx, tx, scope, opGetInput)
		return e
	})
	if err != nil {
		return TaskInput{}, err
	}
	if !auth.Historical {
		in, err := s.ingestTaskInput(ctx, auth.Task, auth.Binding)
		if err != nil {
			return TaskInput{}, err
		}
		if in.Ingest != nil && in.Ingest.ExecutionID != scope.ExecutionID {
			return TaskInput{}, staleExecution("refused to return a different execution")
		}
		return in, nil
	}
	return s.loadHistoricalTaskInput(ctx, scope.AgentID, scope.Role, scope.TaskID)
}

func (s *Service) loadHistoricalTaskInput(ctx context.Context, agentID, role, taskID string) (TaskInput, error) {
	tk, err := s.st.GetTask(ctx, taskID)
	if err != nil {
		return TaskInput{}, err
	}
	if tk.AgentID != agentID || tk.AgentRole != role {
		return TaskInput{}, fmt.Errorf("service: task input %s: %w", taskID, model.ErrForbidden)
	}
	if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
		return TaskInput{}, fmt.Errorf("task is not active: %w", model.ErrInvalidState)
	}
	if leaseExpired(tk) {
		return TaskInput{}, fmt.Errorf("service: task input %s: %w", taskID, model.ErrLeaseExpired)
	}
	asset, err := s.st.GetAsset(ctx, tk.AssetID)
	if err != nil {
		return TaskInput{}, err
	}
	if !assetVisibleForRole(asset, role) {
		return TaskInput{}, fmt.Errorf("service: task input %s: %w", taskID, model.ErrForbidden)
	}
	if b, err := s.st.GetIngestBinding(ctx, taskID); err == nil {
		return s.ingestTaskInput(ctx, tk, b)
	} else if !errors.Is(err, model.ErrNotFound) {
		return TaskInput{}, err
	}
	stg, err := s.st.StageByTask(ctx, taskID)
	if errors.Is(err, model.ErrNotFound) {
		edlRel := asset.Artifacts["edl"]
		edl, _, readErr := s.readEDLRel(edlRel)
		if readErr != nil {
			return TaskInput{}, readErr
		}
		return TaskInput{Versioned: false, TaskID: taskID, AssetID: tk.AssetID, EDLPath: edlRel, EDL: edl, NarrationApprovals: []string{}}, nil
	}
	if err != nil {
		return TaskInput{}, err
	}
	run, err := s.st.GetWorkflow(ctx, stg.RunID)
	if err != nil {
		return TaskInput{}, err
	}
	rev, err := s.st.GetRevision(ctx, stg.InputRevisionID)
	if err != nil {
		return TaskInput{}, err
	}
	edl, err := s.readRevision(rev)
	if err != nil {
		return TaskInput{}, err
	}
	approvals := []string{}
	if hashes, err := s.st.ListNarrationApprovalIDs(ctx, run.RunID, rev.RevisionID); err == nil {
		for _, hash := range hashes {
			approvals = append(approvals, hash)
		}
	}
	profile, profileSHA, err := s.boundProfile(ctx, s.st, run.RunID)
	if err != nil {
		return TaskInput{}, err
	}
	return TaskInput{
		ProcessingProfile: profile, ProfileSHA256: profileSHA,
		Versioned: true, Cancelled: stg.Invalidated || run.Status == model.WorkflowCancelled,
		TaskID: taskID, AssetID: tk.AssetID, RevisionID: rev.RevisionID, PackageRef: rev.PackageRef, ContentMode: run.ContentMode,
		EDLPath: rev.PackageRef + "/edl.json", EDL: edl, NarrationApprovals: approvals,
	}, nil
}

func (s *Service) readEDLRel(rel string) (map[string]any, string, error) {
	if rel == "" {
		return nil, "", fmt.Errorf("asset has no edl: %w", model.ErrNotFound)
	}
	sum, err := s.hashUnderRoot(rel)
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(joinRoot(s.deliveryRoot, rel))
	if err != nil {
		return nil, "", err
	}
	var edl map[string]any
	if json.Unmarshal(raw, &edl) != nil || edl == nil {
		return nil, "", fmt.Errorf("stored edl is not an object: %w", model.ErrInvalidState)
	}
	return edl, sum, nil
}

func (s *Service) idempotent(ctx context.Context, scope, key, hash string) ([]byte, error) {
	got, body, err := s.st.GetIdempotency(ctx, scope, key)
	if err != nil {
		return nil, err
	}
	if got != hash {
		return nil, fmt.Errorf("service: idempotency key reused with different content: %w", model.ErrConflict)
	}
	return []byte(body), nil
}

func newID(prefix string) string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return prefix + "_" + hex.EncodeToString(buf[:])
}

func filepathSlash(p string) string {
	return strings.Trim(strings.ReplaceAll(p, "\\", "/"), "/")
}

func filepathSlashDir(rel string) string {
	rel = filepathSlash(rel)
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return ""
}
