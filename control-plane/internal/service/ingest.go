package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

type ingestConfig struct {
	roots    []ingest.Root
	policy   ingest.Policy
	rootsSHA string
}

// ConfigureIngest fixes the import roots and media policy at startup. No
// roots leaves the raw-recording entry disabled while old packages work.
func (s *Service) ConfigureIngest(roots []ingest.Root, policy *ingest.Policy) error {
	if err := ingest.ValidateRoots(roots); err != nil {
		return err
	}
	for _, r := range roots {
		info, err := os.Stat(r.Path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("ingest root %s is not an existing directory: %w", r.ID, model.ErrArgument)
		}
	}
	p := ingest.DefaultPolicy()
	if policy != nil {
		p = *policy
	}
	if err := p.Validate(); err != nil {
		return err
	}
	s.ingest = ingestConfig{roots: append([]ingest.Root(nil), roots...), policy: p, rootsSHA: ingest.RootsFingerprint(roots)}
	return nil
}

// IngestRoots is the browser view of the configured roots.
type IngestRoots struct {
	Configured bool                `json:"configured"`
	Roots      []ingest.PublicRoot `json:"roots"`
	Policy     ingest.Policy       `json:"policy"`
	Ingesters  []json.RawMessage   `json:"ingesters"`
}

func (s *Service) ListIngestRoots(ctx context.Context) (IngestRoots, error) {
	out := IngestRoots{Configured: len(s.ingest.roots) > 0, Roots: []ingest.PublicRoot{}, Policy: s.ingest.policy, Ingesters: []json.RawMessage{}}
	for _, r := range s.ingest.roots {
		out.Roots = append(out.Roots, ingest.PublicRoot{ID: r.ID, Name: r.Name})
	}
	caps, err := s.st.FreshIngestCapabilities(ctx)
	if err != nil {
		return out, err
	}
	for _, c := range caps {
		out.Ingesters = append(out.Ingesters, json.RawMessage(c))
	}
	return out, nil
}

var idemKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// ingestTx runs fn once per (scope,key). A replay returns the stored
// response; the same key with different content is a conflict.
func (s *Service) ingestTx(ctx context.Context, scope, key, requestHash string, out any, fn func(tx *store.Store) (any, error)) (bool, error) {
	if !idemKeyPattern.MatchString(key) {
		return false, fmt.Errorf("idempotency_key must be 1..128 safe characters: %w", model.ErrArgument)
	}
	replayed := false
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		if got, body, e := tx.GetIdempotency(ctx, scope, key); e == nil {
			if got != requestHash {
				return fmt.Errorf("idempotency key reused with different content: %w", model.ErrConflict)
			}
			replayed = true
			return json.Unmarshal([]byte(body), out)
		} else if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		v, e := fn(tx)
		if e != nil {
			return e
		}
		body, e := json.Marshal(v)
		if e != nil {
			return e
		}
		if e := json.Unmarshal(body, out); e != nil {
			return e
		}
		return tx.PutIdempotency(ctx, scope, key, requestHash, string(body))
	})
	return replayed, err
}

func (s *Service) requireIngest() error {
	if len(s.ingest.roots) == 0 || s.deliveryRoot == "" {
		return fmt.Errorf("raw recording import is not configured (ingest_roots): %w", model.ErrInvalidState)
	}
	return nil
}

// RecordingRegistration is the response of RegisterRecording.
type RecordingRegistration struct {
	Source        model.RecordingSource `json:"source"`
	SourceVersion string                `json:"source_version"`
	Asset         model.Asset           `json:"asset"`
}

// RegisterRecording records a confined local file as the raw input of an
// asset. It never copies or hashes the video; media_probe does that later.
// A new asset stays hidden until a human allows the ingester.
func (s *Service) RegisterRecording(ctx context.Context, actor, assetID, rootID, rel, key string) (RecordingRegistration, error) {
	var out RecordingRegistration
	if err := s.requireIngest(); err != nil {
		return out, err
	}
	if actor == "" || !validTaskID(assetID) || len(assetID) > 128 {
		return out, fmt.Errorf("asset_id must be a safe identifier: %w", model.ErrArgument)
	}
	root, err := ingest.FindRoot(s.ingest.roots, rootID)
	if err != nil {
		return out, err
	}
	clean, err := ingest.CleanRelative(rel)
	if err != nil {
		return out, err
	}
	file, err := ingest.ResolveSource(root, clean)
	if err != nil {
		return out, err
	}
	if file.Size <= 0 || file.Size > s.ingest.policy.MaxSourceBytes {
		return out, fmt.Errorf("recording size must be 1..%d bytes: %w", s.ingest.policy.MaxSourceBytes, model.ErrArgument)
	}
	version := ingest.SourceVersion(root.ID, clean, file.Size, file.MTimeNs)
	created := false
	replayed, err := s.ingestTx(ctx, "ingest.recording:"+assetID, key, root.ID+"\n"+clean, &out, func(tx *store.Store) (any, error) {
		asset, e := tx.GetAsset(ctx, assetID)
		switch {
		case errors.Is(e, model.ErrNotFound):
			asset = model.Asset{AssetID: assetID, Status: model.AssetStatusIngested, AllowedAgents: []string{}, Artifacts: map[string]string{}, InputKind: model.InputKindRawRecording}
			if e := tx.CreateAsset(ctx, asset); e != nil {
				return nil, e
			}
			created = true
		case e != nil:
			return nil, e
		default:
			if asset.InputKind != model.InputKindRawRecording && asset.IngestRunID == "" {
				return nil, fmt.Errorf("asset already holds an EDL package; use a new asset id for a raw recording: %w", model.ErrConflict)
			}
			if _, e := tx.ActiveWorkflow(ctx, assetID); e == nil {
				return nil, fmt.Errorf("asset has an active content workflow: %w", model.ErrConflict)
			} else if !errors.Is(e, model.ErrNotFound) {
				return nil, e
			}
		}
		if _, e := tx.ActiveIngestRun(ctx, assetID); e == nil {
			return nil, fmt.Errorf("asset has an active ingest run: %w", model.ErrConflict)
		} else if !errors.Is(e, model.ErrNotFound) {
			return nil, e
		}
		src := model.RecordingSource{SourceID: newID("src"), AssetID: assetID, RootID: root.ID, RelativePath: clean, SourceVersion: version,
			SizeBytes: file.Size, MTimeNs: file.MTimeNs, CreatedBy: actor, CreatedAt: time.Now().UTC()}
		if e := tx.InsertSource(ctx, src); e != nil {
			return nil, e
		}
		if e := auditTx(ctx, tx, actor, "ingest.recording.register", assetID, "source="+src.SourceID+" root="+root.ID); e != nil {
			return nil, e
		}
		saved, e := tx.GetAsset(ctx, assetID)
		if e != nil {
			return nil, e
		}
		src, e = tx.GetSource(ctx, src.SourceID)
		return RecordingRegistration{Source: src, SourceVersion: version, Asset: saved}, e
	})
	if err == nil && !replayed {
		if created {
			s.publish("asset_created", cloneAsset(out.Asset))
		}
		s.publish("ingest.changed", map[string]any{"asset_id": assetID, "source_id": out.Source.SourceID})
	}
	return out, err
}

// ListRecordingSources returns the sources registered for an asset.
func (s *Service) ListRecordingSources(ctx context.Context, assetID string) ([]model.RecordingSource, error) {
	return s.st.ListSources(ctx, assetID)
}

// IngestStart is the response of StartIngestRun.
type IngestStart struct {
	Run         model.IngestRun `json:"run"`
	Task        model.Task      `json:"task"`
	Disposition string          `json:"disposition,omitempty"`
}

// StartIngestRun queues media_probe for a registered source once a human
// has allowed the ingester on the asset.
func (s *Service) StartIngestRun(ctx context.Context, actor, assetID, sourceID, expectedVersion, key string) (IngestStart, error) {
	return s.StartIngestRunWithPolicy(ctx, actor, assetID, sourceID, expectedVersion, key, "")
}

// StartIngestRunWithPolicy queues media_probe. reject_if_busy leaves an
// occupied recording untouched. replace_after_stop revokes the old execution
// in the same transaction and queues the new run behind the drain barrier.
func (s *Service) StartIngestRunWithPolicy(ctx context.Context, actor, assetID, sourceID, expectedVersion, key, policy string) (IngestStart, error) {
	var out IngestStart
	if err := s.requireIngest(); err != nil {
		return out, err
	}
	src, err := s.st.GetSource(ctx, sourceID)
	if err != nil {
		return out, err
	}
	if src.AssetID != assetID {
		return out, fmt.Errorf("source belongs to another asset: %w", model.ErrArgument)
	}
	if expectedVersion == "" || expectedVersion != src.SourceVersion {
		return out, fmt.Errorf("expected_source_version does not match the registration: %w", model.ErrConflict)
	}
	if err := s.checkSourceUnchanged(src); err != nil {
		return out, err
	}
	policyJSON, policySHA, err := s.ingest.policy.Canonical()
	if err != nil {
		return out, err
	}
	var wake []string
	replayed, err := s.ingestTx(ctx, "ingest.start:"+assetID, key, sourceID+"\n"+expectedVersion+"\n"+policy, &out, func(tx *store.Store) (any, error) {
		waiting, e := s.prepareStartSlot(ctx, tx, assetID, actor, policy, &wake)
		if e != nil {
			return nil, e
		}
		asset, e := tx.GetAsset(ctx, assetID)
		if e != nil {
			return nil, e
		}
		if !assetVisibleForRole(asset, ingest.RoleIngester) {
			return nil, fmt.Errorf("allow local ingester processing on this asset first (visible, unlocked, allowed_agents contains ingester): %w", model.ErrForbidden)
		}
		if _, e := tx.ActiveWorkflow(ctx, assetID); e == nil {
			return nil, fmt.Errorf("asset has an active content workflow: %w", model.ErrConflict)
		} else if !errors.Is(e, model.ErrNotFound) {
			return nil, e
		}
		now := time.Now().UTC()
		run := model.IngestRun{RunID: newID("ing"), AssetID: assetID, SourceID: sourceID, State: ingest.StateQueued, Stage: ingest.StageProbe,
			Version: 1, PolicyJSON: policyJSON, PolicySHA256: policySHA, RootsSHA256: s.ingest.rootsSHA, Files: map[string]string{},
			CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
		if e := tx.InsertIngestRun(ctx, run); e != nil {
			if errors.Is(e, model.ErrConflict) {
				return nil, fmt.Errorf("asset already has an active ingest run: %w", model.ErrConflict)
			}
			return nil, e
		}
		input := ingest.Digest(map[string]any{"stage": ingest.TaskMediaProbe, "source_id": sourceID, "source_version": src.SourceVersion, "policy_sha256": policySHA, "roots_sha256": s.ingest.rootsSHA})
		tk, e := s.queueIngestTask(ctx, tx, &run, ingest.TaskMediaProbe, 0, input, "")
		if e != nil {
			return nil, e
		}
		if e := tx.UpdateIngestRun(ctx, run, run.Version); e != nil {
			return nil, e
		}
		if e := auditTx(ctx, tx, actor, "ingest.run.start", run.RunID, "source="+sourceID+" policy="+policySHA); e != nil {
			return nil, e
		}
		started := IngestStart{Run: run, Task: tk}
		if waiting {
			started.Disposition = model.DispositionWaitingDrain
		}
		return started, nil
	})
	if err == nil {
		s.finishControl(wake)
	}
	if err == nil && !replayed {
		s.publishIngest(out.Run)
		s.publish("task_created", cloneTask(out.Task))
	}
	return out, err
}

// checkSourceUnchanged re-resolves the registered file and compares its
// change marker with the registration.
func (s *Service) checkSourceUnchanged(src model.RecordingSource) error {
	root, err := ingest.FindRoot(s.ingest.roots, src.RootID)
	if err != nil {
		return err
	}
	file, err := ingest.ResolveSource(root, src.RelativePath)
	if err != nil {
		return err
	}
	if ingest.SourceVersion(root.ID, src.RelativePath, file.Size, file.MTimeNs) != src.SourceVersion {
		return fmt.Errorf("the recording changed after registration; register it again: %w", model.ErrConflict)
	}
	return nil
}

// queueIngestTask creates the next ingester task and its binding, and points
// the run at it. The caller persists the run.
func (s *Service) queueIngestTask(ctx context.Context, tx *store.Store, run *model.IngestRun, kind string, planRevision int, input, attemptOf string) (model.Task, error) {
	tk := model.Task{TaskID: newID("in" + strings.ReplaceAll(kind, "_", "")), AssetID: run.AssetID, Type: kind, AgentRole: ingest.RoleIngester,
		Status: model.TaskStatusQueued, Artifacts: map[string]string{}}
	if err := tx.CreateTask(ctx, tk); err != nil {
		return model.Task{}, err
	}
	if err := tx.InsertIngestBinding(ctx, model.IngestTaskBinding{TaskID: tk.TaskID, RunID: run.RunID, Stage: kind, PlanRevision: planRevision,
		InputSHA256: input, RootsSHA256: run.RootsSHA256, AttemptOf: attemptOf, CreatedAt: time.Now().UTC()}); err != nil {
		return model.Task{}, err
	}
	run.CurrentTaskID = tk.TaskID
	return tx.GetTask(ctx, tk.TaskID)
}

func (s *Service) publishIngest(run model.IngestRun) {
	s.publish("ingest.changed", map[string]any{"asset_id": run.AssetID, "run_id": run.RunID, "state": run.State, "stage": run.Stage, "version": run.Version})
}

// loadRunForUpdate reads a run inside tx and enforces the optimistic
// version and allowed states.
func loadRunForUpdate(ctx context.Context, tx *store.Store, runID string, expected int, states ...string) (model.IngestRun, error) {
	run, err := tx.GetIngestRun(ctx, runID)
	if err != nil {
		return run, err
	}
	if expected != run.Version {
		return run, fmt.Errorf("ingest run changed (version %d); refresh and retry: %w", run.Version, model.ErrConflict)
	}
	for _, st := range states {
		if run.State+"/"+run.Stage == st || run.State == st {
			return run, nil
		}
	}
	return run, fmt.Errorf("ingest run is %s/%s: %w", run.State, run.Stage, model.ErrInvalidState)
}

func (s *Service) runProbe(run model.IngestRun) (ingest.Probe, error) {
	if run.ProbeJSON == "" || ingest.Digest(json.RawMessage(run.ProbeJSON)) != run.ProbeSHA256 {
		return ingest.Probe{}, fmt.Errorf("ingest run has no verified probe: %w", model.ErrInvalidState)
	}
	var pr ingest.Probe
	if err := json.Unmarshal([]byte(run.ProbeJSON), &pr); err != nil {
		return pr, fmt.Errorf("stored probe is unreadable: %w", model.ErrInvalidState)
	}
	return pr, nil
}

func runPolicy(run model.IngestRun) (ingest.Policy, error) {
	return ingest.ParsePolicy(run.PolicyJSON, run.PolicySHA256)
}

// AnalysisRequest is the human analysis-plan input.
type AnalysisRequest struct {
	ExpectedVersion      int                 `json:"expected_version"`
	VideoStreamIndex     int                 `json:"video_stream_index"`
	GameAudioStreamIndex *int                `json:"game_audio_stream_index"`
	SourceRangeUs        *[2]int64           `json:"source_range_us"`
	Segmentation         ingest.Segmentation `json:"segmentation"`
	IdempotencyKey       string              `json:"idempotency_key"`
	ResourcePolicy       string              `json:"resource_policy,omitempty"`
}

// SubmitAnalysisPlan fixes a new analysis plan revision and queues segment.
func (s *Service) SubmitAnalysisPlan(ctx context.Context, actor, runID string, req AnalysisRequest) (IngestStart, error) {
	var out IngestStart
	if req.Segmentation.Method == "" {
		req.Segmentation.Method = "scene_change"
	}
	reqHash := ingest.Digest(req)
	var wake []string
	replayed, err := s.ingestTx(ctx, "ingest.analysis:"+runID, req.IdempotencyKey, reqHash, &out, func(tx *store.Store) (any, error) {
		run, e := tx.GetIngestRun(ctx, runID)
		if e != nil {
			return nil, e
		}
		if req.ExpectedVersion != run.Version {
			return nil, fmt.Errorf("ingest run changed (version %d); refresh and retry: %w", run.Version, model.ErrConflict)
		}
		waiting := false
		if ex, live, e := liveExecutionOf(ctx, tx, run); e != nil {
			return nil, e
		} else if live {
			policy, e := normalizeResourcePolicy(req.ResourcePolicy)
			if e != nil {
				return nil, e
			}
			if e := rejectAgentPreempt(actor, policy); e != nil {
				return nil, e
			}
			if policy == model.ResourceRejectIfBusy {
				return nil, fmt.Errorf("an execution holds this recording: %w", model.ErrResourceBusy)
			}
			if run.ProbeSHA256 == "" {
				return nil, fmt.Errorf("probe is not registered: %w", model.ErrInvalidState)
			}
			if e := s.revokeExecution(ctx, tx, ex, "replace_after_stop", &wake); e != nil {
				return nil, e
			}
			if b, e := tx.GetIngestBinding(ctx, run.CurrentTaskID); e == nil {
				b.Invalidated = true
				b.HandoverState = "draining"
				if e := tx.UpdateIngestBinding(ctx, b); e != nil {
					return nil, e
				}
			} else if e != nil {
				return nil, e
			}
			tk, e := tx.GetTask(ctx, run.CurrentTaskID)
			if e != nil {
				return nil, e
			}
			if tk.Status == model.TaskStatusQueued || tk.Status == model.TaskStatusClaimed || tk.Status == model.TaskStatusRunning {
				tk.Status, tk.LeaseUntil, tk.Message = model.TaskStatusCancelled, nil, "replaced by a new analysis plan"
				if e := tx.UpdateTask(ctx, tk); e != nil {
					return nil, e
				}
			}
			waiting = true
		} else if run.State+"/"+run.Stage != "awaiting_review/probe" && run.State+"/"+run.Stage != "awaiting_review/segment_review" {
			return nil, fmt.Errorf("ingest run is %s/%s: %w", run.State, run.Stage, model.ErrInvalidState)
		}
		if e := s.ingestGovernance(ctx, tx, run.AssetID); e != nil {
			return nil, e
		}
		pr, e := s.runProbe(run)
		if e != nil {
			return nil, e
		}
		src, e := tx.GetSource(ctx, run.SourceID)
		if e != nil {
			return nil, e
		}
		if src.SHA256 == "" {
			return nil, fmt.Errorf("source snapshot is not published: %w", model.ErrInvalidState)
		}
		rng := [2]int64{0, pr.DurationUs}
		if req.SourceRangeUs != nil {
			rng = *req.SourceRangeUs
		}
		plan := ingest.AnalysisPlan{SchemaVersion: 1, SourceID: src.SourceID, SourceSHA256: src.SHA256, ProbeSHA256: run.ProbeSHA256,
			PolicySHA256: run.PolicySHA256, VideoStreamIndex: req.VideoStreamIndex, GameAudioStreamIndex: req.GameAudioStreamIndex,
			SourceRangeUs: rng, Segmentation: req.Segmentation}
		if e := ingest.ValidateAnalysis(plan, pr); e != nil {
			return nil, e
		}
		body, sum := mustCanonical(plan)
		rev, e := tx.NextPlanRevision(ctx, runID)
		if e != nil {
			return nil, e
		}
		if e := tx.InsertPlanRevision(ctx, model.IngestPlanRevision{RunID: runID, Revision: rev, Kind: "analysis", SchemaVersion: 1, CanonicalJSON: body, SHA256: sum, Actor: actor, CreatedAt: time.Now().UTC()}); e != nil {
			return nil, e
		}
		prev := run.Version
		run.AnalysisRevision, run.SelectionRevision = rev, 0
		run.SegmentsRef, run.SegmentsSHA256 = "", ""
		dropFiles(run.Files, "thumb_")
		input := ingest.Digest(map[string]any{"stage": ingest.TaskSegment, "source_sha256": src.SHA256, "probe_sha256": run.ProbeSHA256, "analysis_sha256": sum, "policy_sha256": run.PolicySHA256})
		tk, e := s.queueIngestTask(ctx, tx, &run, ingest.TaskSegment, rev, input, "")
		if e != nil {
			return nil, e
		}
		run.State, run.Stage, run.ErrorCode, run.ErrorMessage = ingest.StateQueued, ingest.StageSegment, "", ""
		run.Version++
		if e := tx.UpdateIngestRun(ctx, run, prev); e != nil {
			return nil, e
		}
		if e := auditTx(ctx, tx, actor, "ingest.analysis.plan", runID, fmt.Sprintf("revision=%d sha256=%s", rev, sum)); e != nil {
			return nil, e
		}
		started := IngestStart{Run: run, Task: tk}
		if waiting {
			started.Disposition = model.DispositionWaitingDrain
		}
		return started, nil
	})
	if err == nil {
		s.finishControl(wake)
	}
	if err == nil && !replayed {
		s.publishIngest(out.Run)
		s.publish("task_created", cloneTask(out.Task))
	}
	return out, err
}

// SelectionRequest is the human selection input.
type SelectionRequest struct {
	ExpectedVersion  int                      `json:"expected_version"`
	BasePlanRevision int                      `json:"base_plan_revision"`
	SelectedSegments []ingest.SelectedSegment `json:"selected_segments"`
	Output           ingest.Output            `json:"output"`
	IdempotencyKey   string                   `json:"idempotency_key"`
}

// SubmitSelection saves an immutable selection revision. It does not
// transcode; prepare does.
func (s *Service) SubmitSelection(ctx context.Context, actor, runID string, req SelectionRequest) (model.IngestRun, error) {
	var out model.IngestRun
	reqHash := ingest.Digest(req)
	replayed, err := s.ingestTx(ctx, "ingest.selection:"+runID, req.IdempotencyKey, reqHash, &out, func(tx *store.Store) (any, error) {
		run, e := loadRunForUpdate(ctx, tx, runID, req.ExpectedVersion, "awaiting_review/segment_review")
		if e != nil {
			return nil, e
		}
		if req.BasePlanRevision != run.AnalysisRevision {
			return nil, fmt.Errorf("base_plan_revision is not the current analysis plan (%d): %w", run.AnalysisRevision, model.ErrConflict)
		}
		pr, e := s.runProbe(run)
		if e != nil {
			return nil, e
		}
		policy, e := runPolicy(run)
		if e != nil {
			return nil, e
		}
		src, e := tx.GetSource(ctx, run.SourceID)
		if e != nil {
			return nil, e
		}
		sel := ingest.Selection{SchemaVersion: 1, BasePlanRevision: req.BasePlanRevision, SourceSHA256: src.SHA256, SelectedSegments: req.SelectedSegments, Output: req.Output}
		if e := ingest.ValidateSelection(sel, pr, policy); e != nil {
			return nil, e
		}
		body, sum := mustCanonical(sel)
		rev, e := tx.NextPlanRevision(ctx, runID)
		if e != nil {
			return nil, e
		}
		if e := tx.InsertPlanRevision(ctx, model.IngestPlanRevision{RunID: runID, Revision: rev, Kind: "selection", SchemaVersion: 1, CanonicalJSON: body, SHA256: sum, Actor: actor, CreatedAt: time.Now().UTC()}); e != nil {
			return nil, e
		}
		prev := run.Version
		run.SelectionRevision = rev
		run.Version++
		if e := tx.UpdateIngestRun(ctx, run, prev); e != nil {
			return nil, e
		}
		if e := auditTx(ctx, tx, actor, "ingest.selection", runID, fmt.Sprintf("revision=%d segments=%d sha256=%s", rev, len(sel.SelectedSegments), sum)); e != nil {
			return nil, e
		}
		return run, nil
	})
	if err == nil && !replayed {
		s.publishIngest(out)
	}
	return out, err
}

// PrepareRequest binds a selection revision and a content profile.
type PrepareRequest struct {
	ExpectedVersion int    `json:"expected_version"`
	PlanRevision    int    `json:"plan_revision"`
	ProfileID       string `json:"profile_id"`
	ProfileRevision int    `json:"profile_revision"`
	IdempotencyKey  string `json:"idempotency_key"`
	ResourcePolicy  string `json:"resource_policy,omitempty"`
}

// PrepareIngest checks the selection against the profile frame budget,
// binds that profile revision and queues media_prepare. No model is called.
func (s *Service) PrepareIngest(ctx context.Context, actor, runID string, req PrepareRequest) (IngestStart, error) {
	var out IngestStart
	if req.ProfileRevision < 1 {
		return out, fmt.Errorf("profile_revision is required: %w", model.ErrArgument)
	}
	profile, profileSHA, err := s.GetProcessingProfile(ctx, req.ProfileID, req.ProfileRevision)
	if err != nil {
		return out, err
	}
	reqHash := ingest.Digest(req) + profileSHA
	var wake []string
	replayed, err := s.ingestTx(ctx, "ingest.prepare:"+runID, req.IdempotencyKey, reqHash, &out, func(tx *store.Store) (any, error) {
		run, e := tx.GetIngestRun(ctx, runID)
		if e != nil {
			return nil, e
		}
		if req.ExpectedVersion != run.Version {
			return nil, fmt.Errorf("ingest run changed (version %d); refresh and retry: %w", run.Version, model.ErrConflict)
		}
		waiting := false
		if ex, live, e := liveExecutionOf(ctx, tx, run); e != nil {
			return nil, e
		} else if live {
			policy, e := normalizeResourcePolicy(req.ResourcePolicy)
			if e != nil {
				return nil, e
			}
			if e := rejectAgentPreempt(actor, policy); e != nil {
				return nil, e
			}
			if policy == model.ResourceRejectIfBusy {
				return nil, fmt.Errorf("an execution holds this recording: %w", model.ErrResourceBusy)
			}
			if e := s.revokeExecution(ctx, tx, ex, "replace_after_stop", &wake); e != nil {
				return nil, e
			}
			if b, e := tx.GetIngestBinding(ctx, run.CurrentTaskID); e == nil {
				b.Invalidated = true
				b.HandoverState = "draining"
				if e := tx.UpdateIngestBinding(ctx, b); e != nil {
					return nil, e
				}
			} else if e != nil {
				return nil, e
			}
			tk, e := tx.GetTask(ctx, run.CurrentTaskID)
			if e != nil {
				return nil, e
			}
			if tk.Status == model.TaskStatusQueued || tk.Status == model.TaskStatusClaimed || tk.Status == model.TaskStatusRunning {
				tk.Status, tk.LeaseUntil, tk.Message = model.TaskStatusCancelled, nil, "replaced by media prepare"
				if e := tx.UpdateTask(ctx, tk); e != nil {
					return nil, e
				}
			}
			waiting = true
		} else if run.State+"/"+run.Stage != "awaiting_review/segment_review" {
			return nil, fmt.Errorf("ingest run is %s/%s: %w", run.State, run.Stage, model.ErrInvalidState)
		}
		if e := s.ingestGovernance(ctx, tx, run.AssetID); e != nil {
			return nil, e
		}
		if req.PlanRevision < 1 || req.PlanRevision != run.SelectionRevision {
			return nil, fmt.Errorf("plan_revision must be the current selection revision (%d): %w", run.SelectionRevision, model.ErrConflict)
		}
		sel, selSHA, e := s.selectionRevision(ctx, tx, run, req.PlanRevision)
		if e != nil {
			return nil, e
		}
		policy, e := runPolicy(run)
		if e != nil {
			return nil, e
		}
		quotas, e := ingest.FrameQuota(segmentDurations(sel), profile.Sampling.MaxFrames, policy.MaxFramesPerSegment)
		if e != nil {
			return nil, e
		}
		_, analysisSHA, e := s.analysisRevision(ctx, tx, run, run.AnalysisRevision)
		if e != nil {
			return nil, e
		}
		prev := run.Version
		run.ProfileID, run.ProfileRevision, run.ProfileSHA256 = profile.ProfileID, profile.Revision, profileSHA
		input := ingest.Digest(map[string]any{"stage": ingest.TaskMediaPrepare, "source_sha256": sel.SourceSHA256, "probe_sha256": run.ProbeSHA256,
			"analysis_sha256": analysisSHA, "selection_sha256": selSHA, "policy_sha256": run.PolicySHA256, "profile_sha256": profileSHA, "frame_quotas": quotas})
		tk, e := s.queueIngestTask(ctx, tx, &run, ingest.TaskMediaPrepare, req.PlanRevision, input, "")
		if e != nil {
			return nil, e
		}
		run.State, run.Stage, run.ErrorCode, run.ErrorMessage = ingest.StateQueued, ingest.StagePrepare, "", ""
		run.Version++
		if e := tx.UpdateIngestRun(ctx, run, prev); e != nil {
			return nil, e
		}
		if e := auditTx(ctx, tx, actor, "ingest.prepare", runID, fmt.Sprintf("selection=%d profile=%s@%d sha256=%s", req.PlanRevision, profile.ProfileID, profile.Revision, profileSHA)); e != nil {
			return nil, e
		}
		started := IngestStart{Run: run, Task: tk}
		if waiting {
			started.Disposition = model.DispositionWaitingDrain
		}
		return started, nil
	})
	if err == nil {
		s.finishControl(wake)
	}
	if err == nil && !replayed {
		s.publishIngest(out.Run)
		s.publish("task_created", cloneTask(out.Task))
	}
	return out, err
}

// CancelIngestRun stops an active run and invalidates its in-flight task so
// later checkpoints or results from that execution are rejected.
func (s *Service) CancelIngestRun(ctx context.Context, actor, runID string, expected int, reason string) (model.IngestRun, error) {
	if actor == "" || len(reason) > 500 {
		return model.IngestRun{}, fmt.Errorf("reason must be at most 500 bytes: %w", model.ErrArgument)
	}
	var run model.IngestRun
	var cancelled *model.Task
	var wake []string
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		var e error
		run, e = loadRunForUpdate(ctx, tx, runID, expected, ingest.StateQueued, ingest.StateProcessing, ingest.StateAwaitingReview)
		if e != nil {
			return e
		}
		if run.CurrentTaskID != "" {
			b, e := tx.GetIngestBinding(ctx, run.CurrentTaskID)
			if e != nil {
				return e
			}
			if b.ExecutionID != "" {
				if ex, e := tx.GetIngestExecution(ctx, b.ExecutionID); e == nil {
					if e := s.revokeExecution(ctx, tx, ex, "cancelled", &wake); e != nil {
						return e
					}
				} else if !errors.Is(e, model.ErrNotFound) {
					return e
				}
			}
			b.Invalidated = true
			b.HandoverState = "draining"
			if e := tx.UpdateIngestBinding(ctx, b); e != nil {
				return e
			}
			tk, e := tx.GetTask(ctx, run.CurrentTaskID)
			if e != nil {
				return e
			}
			if tk.Status == model.TaskStatusQueued || tk.Status == model.TaskStatusClaimed || tk.Status == model.TaskStatusRunning {
				tk.Status, tk.LeaseUntil, tk.Message = model.TaskStatusCancelled, nil, "ingest run cancelled"
				if e := tx.UpdateTask(ctx, tk); e != nil {
					return e
				}
				saved, e := tx.GetTask(ctx, tk.TaskID)
				if e != nil {
					return e
				}
				cancelled = &saved
			}
		}
		prev := run.Version
		run.State, run.ErrorCode, run.ErrorMessage = ingest.StateCancelled, "cancelled", sanitizeReason(reason)
		run.Version++
		if e := tx.UpdateIngestRun(ctx, run, prev); e != nil {
			return e
		}
		return auditTx(ctx, tx, actor, "ingest.run.cancel", runID, sanitizeReason(reason))
	})
	if err == nil {
		s.finishControl(wake)
		s.publishIngest(run)
		if cancelled != nil {
			s.publish("task_updated", cloneTask(*cancelled))
		}
	}
	return run, err
}

// RetryIngestRun requeues the failed stage with the same fixed inputs, so
// verified checkpoints of earlier attempts stay reusable.
func (s *Service) RetryIngestRun(ctx context.Context, actor, runID string, expected int, stage, key string) (IngestStart, error) {
	return s.RetryIngestRunWithPolicy(ctx, actor, runID, expected, stage, key, "")
}

// RetryIngestRunWithPolicy queues the failed stage, or refuses while a
// resource is still occupied. replace_after_stop queues behind the barrier
// and does not clear the owner.
func (s *Service) RetryIngestRunWithPolicy(ctx context.Context, actor, runID string, expected int, stage, key, policy string) (IngestStart, error) {
	var out IngestStart
	replayed, err := s.ingestTx(ctx, "ingest.retry:"+runID, key, fmt.Sprintf("%d\n%s\n%s", expected, stage, policy), &out, func(tx *store.Store) (any, error) {
		run, e := loadRunForUpdate(ctx, tx, runID, expected, ingest.StateFailed)
		if e != nil {
			return nil, e
		}
		if stage != run.Stage || (stage != ingest.StageProbe && stage != ingest.StageSegment && stage != ingest.StagePrepare) {
			return nil, fmt.Errorf("only the failed stage %s can be retried: %w", run.Stage, model.ErrArgument)
		}
		if e := s.ingestGovernance(ctx, tx, run.AssetID); e != nil {
			return nil, e
		}
		old, e := tx.GetIngestBinding(ctx, run.CurrentTaskID)
		if e != nil {
			return nil, e
		}
		waiting, e := s.guardResourceBeforeQueue(ctx, tx, run, actor, policy)
		if e != nil {
			return nil, e
		}
		prev := run.Version
		tk, e := s.queueIngestTask(ctx, tx, &run, old.Stage, old.PlanRevision, old.InputSHA256, old.TaskID)
		if e != nil {
			return nil, e
		}
		run.State, run.ErrorCode, run.ErrorMessage = ingest.StateQueued, "", ""
		run.Version++
		if e := tx.UpdateIngestRun(ctx, run, prev); e != nil {
			if errors.Is(e, model.ErrConflict) {
				return nil, fmt.Errorf("another ingest run is active for this asset: %w", model.ErrConflict)
			}
			return nil, e
		}
		if e := auditTx(ctx, tx, actor, "ingest.run.retry", runID, "stage="+stage+" attempt_of="+old.TaskID); e != nil {
			return nil, e
		}
		started := IngestStart{Run: run, Task: tk}
		if waiting {
			started.Disposition = model.DispositionWaitingDrain
		}
		return started, nil
	})
	if err == nil && !replayed {
		s.publishIngest(out.Run)
		s.publish("task_created", cloneTask(out.Task))
	}
	return out, err
}

func (s *Service) ingestGovernance(ctx context.Context, tx *store.Store, assetID string) error {
	asset, err := tx.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if !assetVisibleForRole(asset, ingest.RoleIngester) {
		return fmt.Errorf("asset is locked, hidden or does not allow the ingester: %w", model.ErrForbidden)
	}
	return nil
}

func (s *Service) analysisRevision(ctx context.Context, st *store.Store, run model.IngestRun, rev int) (ingest.AnalysisPlan, string, error) {
	var plan ingest.AnalysisPlan
	row, err := st.GetPlanRevision(ctx, run.RunID, rev)
	if err != nil {
		return plan, "", err
	}
	if row.Kind != "analysis" || ingest.Digest(json.RawMessage(row.CanonicalJSON)) != row.SHA256 || json.Unmarshal([]byte(row.CanonicalJSON), &plan) != nil {
		return plan, "", fmt.Errorf("stored analysis plan failed verification: %w", model.ErrInvalidState)
	}
	return plan, row.SHA256, nil
}

func (s *Service) selectionRevision(ctx context.Context, st *store.Store, run model.IngestRun, rev int) (ingest.Selection, string, error) {
	var sel ingest.Selection
	row, err := st.GetPlanRevision(ctx, run.RunID, rev)
	if err != nil {
		return sel, "", err
	}
	if row.Kind != "selection" || ingest.Digest(json.RawMessage(row.CanonicalJSON)) != row.SHA256 || json.Unmarshal([]byte(row.CanonicalJSON), &sel) != nil {
		return sel, "", fmt.Errorf("stored selection failed verification: %w", model.ErrInvalidState)
	}
	return sel, row.SHA256, nil
}

func segmentDurations(sel ingest.Selection) []int64 {
	out := make([]int64, len(sel.SelectedSegments))
	for i, seg := range sel.SelectedSegments {
		out[i] = seg.EndUs - seg.StartUs
	}
	return out
}

func mustCanonical(v any) (string, string) {
	body, _ := json.Marshal(v)
	return string(body), ingest.Digest(json.RawMessage(body))
}

func dropFiles(files map[string]string, prefix string) {
	for k := range files {
		if strings.HasPrefix(k, prefix) {
			delete(files, k)
		}
	}
}

var absolutePath = regexp.MustCompile(`(?i)([a-z]:[\\/]|\\\\|/(home|users|mnt|tmp|var)/)[^\s"'<>|]*`)

// sanitizeReason keeps failure text actionable without echoing local paths.
func sanitizeReason(reason string) string {
	reason = absolutePath.ReplaceAllString(strings.TrimSpace(reason), "<path>")
	if len(reason) > 500 {
		reason = reason[:500]
	}
	return reason
}
