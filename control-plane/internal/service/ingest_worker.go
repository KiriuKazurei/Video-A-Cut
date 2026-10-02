package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// IngestCapability is the ingester's local observation sent with heartbeat.
type IngestCapability struct {
	RootsSHA256       string   `json:"roots_sha256"`
	FFmpegReady       bool     `json:"ffmpeg_ready"`
	FFprobeReady      bool     `json:"ffprobe_ready"`
	Operations        []string `json:"operations"`
	WorkerVersion     string   `json:"worker_version"`
	ExecutionProtocol int      `json:"execution_protocol,omitempty"`
	AgentID           string   `json:"agent_id,omitempty"`
}

var ingestOperations = []string{ingest.TaskMediaProbe, ingest.TaskSegment, ingest.TaskMediaPrepare}

// ReportIngestCapability stores a 90-second capability report. Only the
// ingester role may report one; the model-profile capability is separate.
func (s *Service) ReportIngestCapability(ctx context.Context, agentID, role string, c IngestCapability) error {
	if role != ingest.RoleIngester || len(c.RootsSHA256) != 64 || !isHex(c.RootsSHA256) || len(c.Operations) > 8 || !printableLimited(c.WorkerVersion, 1, 64) {
		return fmt.Errorf("ingest_capability is only accepted from an ingester with a roots fingerprint: %w", model.ErrArgument)
	}
	for _, op := range c.Operations {
		if !containsString(ingestOperations, op) {
			return fmt.Errorf("unknown ingest operation: %w", model.ErrArgument)
		}
	}
	c.AgentID = agentID
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := s.st.PutIngestCapability(ctx, agentID, c.RootsSHA256, string(body)); err != nil {
		return err
	}
	s.publish("capability.changed", map[string]any{"role": role})
	return nil
}

// ingestClaimable decides whether a queued ingest task may go to agentID.
// Tasks without a binding are not ingest tasks and pass through.
func (s *Service) ingestClaimable(ctx context.Context, tx *store.Store, agentID string, tk model.Task) (bool, error) {
	b, err := tx.GetIngestBinding(ctx, tk.TaskID)
	if errors.Is(err, model.ErrNotFound) {
		return !ingest.IsTaskType(tk.Type), nil
	}
	if err != nil {
		return false, err
	}
	run, err := tx.GetIngestRun(ctx, b.RunID)
	if err != nil {
		return false, err
	}
	if b.Invalidated || run.CurrentTaskID != tk.TaskID || (run.State != ingest.StateQueued && run.State != ingest.StateProcessing) {
		return false, nil
	}
	if b.RootsSHA256 != s.ingest.rootsSHA {
		return false, nil
	}
	roots, body, err := tx.IngestCapability(ctx, agentID)
	if errors.Is(err, model.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var c IngestCapability
	if roots != b.RootsSHA256 || json.Unmarshal([]byte(body), &c) != nil || !c.FFmpegReady || !c.FFprobeReady || !containsString(c.Operations, tk.Type) {
		return false, nil
	}
	if c.ExecutionProtocol < ExecutionProtocolVersion {
		return false, fmt.Errorf("ingester execution protocol %d is older than %d; upgrade the worker before it can take ingest tasks: %w", c.ExecutionProtocol, ExecutionProtocolVersion, model.ErrProtocolUpgrade)
	}
	return true, nil
}

// ingestBlocks rejects writes for invalidated, superseded or cancelled
// ingest tasks. Non-ingest tasks pass.
func ingestBlocks(ctx context.Context, tx *store.Store, taskID string) error {
	b, err := tx.GetIngestBinding(ctx, taskID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	run, err := tx.GetIngestRun(ctx, b.RunID)
	if err != nil {
		return err
	}
	if b.Invalidated || run.State == ingest.StateCancelled || run.CurrentTaskID != taskID {
		return fmt.Errorf("service: ingest task %s was cancelled or superseded: %w", taskID, model.ErrConflict)
	}
	return nil
}

// ingestProgress keeps ingest progress monotonic within one execution and
// below 100% until the result is registered.
func ingestProgress(ctx context.Context, tx *store.Store, tk model.Task, p float64) float64 {
	if _, err := tx.GetIngestBinding(ctx, tk.TaskID); err != nil {
		return p
	}
	if p > 0.99 {
		p = 0.99
	}
	if p < tk.Progress {
		return tk.Progress
	}
	return p
}

// NoteIngestFailure fails the run when its current task fails.
func (s *Service) NoteIngestFailure(ctx context.Context, tx *store.Store, tk model.Task, reason string) error {
	b, err := tx.GetIngestBinding(ctx, tk.TaskID)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	run, err := tx.GetIngestRun(ctx, b.RunID)
	if err != nil || b.Invalidated || run.CurrentTaskID != tk.TaskID || (run.State != ingest.StateProcessing && run.State != ingest.StateQueued) {
		return err
	}
	prev := run.Version
	run.State, run.ErrorCode, run.ErrorMessage = ingest.StateFailed, "task_failed", sanitizeReason(reason)
	run.Version++
	return tx.UpdateIngestRun(ctx, run, prev)
}

func (s *Service) publishTaskIngest(ctx context.Context, taskID string) {
	b, err := s.st.GetIngestBinding(ctx, taskID)
	if err != nil {
		return
	}
	if run, err := s.st.GetIngestRun(ctx, b.RunID); err == nil {
		s.publishIngest(run)
	}
}

// IngestSourceRef is the controlled source reference given to the ingester.
type IngestSourceRef struct {
	SourceID      string `json:"source_id"`
	RootID        string `json:"root_id"`
	RelativePath  string `json:"relative_path"`
	SourceVersion string `json:"source_version"`
	SizeBytes     int64  `json:"size_bytes"`
	// Decimal string: nanosecond mtimes exceed 2^53 and the MCP output path
	// round-trips numbers through float64.
	MTimeNs     string `json:"mtime_ns"`
	SHA256      string `json:"sha256,omitempty"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
	SourceDir   string `json:"source_dir"`
}

// IngestInput is get_task_input for media_probe, segment and media_prepare.
type IngestInput struct {
	RunID               string                   `json:"run_id"`
	Stage               string                   `json:"stage"`
	ExecutionID         string                   `json:"execution_id"`
	InputSHA256         string                   `json:"input_sha256"`
	PolicySHA256        string                   `json:"policy_sha256"`
	RootsSHA256         string                   `json:"roots_sha256"`
	Policy              ingest.Policy            `json:"policy"`
	Source              IngestSourceRef          `json:"source"`
	OutputDir           string                   `json:"output_dir"`
	PackageDir          string                   `json:"package_dir"`
	Probe               *ingest.Probe            `json:"probe,omitempty"`
	ProbeSHA256         string                   `json:"probe_sha256,omitempty"`
	AnalysisRevision    int                      `json:"analysis_revision,omitempty"`
	AnalysisSHA256      string                   `json:"analysis_sha256,omitempty"`
	Analysis            *ingest.AnalysisPlan     `json:"analysis,omitempty"`
	SelectionRevision   int                      `json:"selection_revision,omitempty"`
	SelectionSHA256     string                   `json:"selection_sha256,omitempty"`
	Selection           *ingest.Selection        `json:"selection,omitempty"`
	FrameQuotas         []int                    `json:"frame_quotas,omitempty"`
	Provenance          *ingest.Provenance       `json:"provenance,omitempty"`
	Checkpoints         []model.IngestCheckpoint `json:"checkpoints"`
	Generation          int                      `json:"generation,omitempty"`
	RuntimeInstanceID   string                   `json:"runtime_instance_id,omitempty"`
	ControlVersion      int                      `json:"control_version,omitempty"`
	ExecutionProtocol   int                      `json:"execution_protocol,omitempty"`
	ResourceKeys        []string                 `json:"resource_keys"`
	PreviousExecutionID string                   `json:"previous_execution_id,omitempty"`
}

func ingestOutputDir(b model.IngestTaskBinding) string {
	return "ingest/" + b.RunID + "/" + b.TaskID + "/" + b.ExecutionID
}

func sourceDir(sourceID string) string { return "ingest/sources/" + sourceID }

func (s *Service) ingestTaskInput(ctx context.Context, tk model.Task, b model.IngestTaskBinding) (TaskInput, error) {
	run, err := s.st.GetIngestRun(ctx, b.RunID)
	if err != nil {
		return TaskInput{}, err
	}
	if b.Invalidated || run.CurrentTaskID != tk.TaskID || run.State == ingest.StateCancelled {
		return TaskInput{InputKind: "media_ingest", Cancelled: true, TaskID: tk.TaskID, AssetID: tk.AssetID, EDL: map[string]any{}, NarrationApprovals: []string{}}, nil
	}
	policy, err := runPolicy(run)
	if err != nil {
		return TaskInput{}, err
	}
	src, err := s.st.GetSource(ctx, run.SourceID)
	if err != nil {
		return TaskInput{}, err
	}
	in := IngestInput{RunID: run.RunID, Stage: b.Stage, ExecutionID: b.ExecutionID, InputSHA256: b.InputSHA256, PolicySHA256: run.PolicySHA256,
		Generation: b.ExecutionSeq, ExecutionProtocol: ExecutionProtocolVersion,
		RootsSHA256: b.RootsSHA256, Policy: policy, OutputDir: ingestOutputDir(b), PackageDir: ingestOutputDir(b) + "/package",
		Source: IngestSourceRef{SourceID: src.SourceID, RootID: src.RootID, RelativePath: src.RelativePath, SourceVersion: src.SourceVersion,
			SizeBytes: src.SizeBytes, MTimeNs: strconv.FormatInt(src.MTimeNs, 10), SHA256: src.SHA256, SnapshotRef: src.SnapshotRef, SourceDir: sourceDir(src.SourceID)},
		Checkpoints: []model.IngestCheckpoint{}}
	in.ResourceKeys = []string{sourceSnapshotKey(run.SourceID)}
	if resource, err := s.st.GetExecutionResource(ctx, in.ResourceKeys[0]); err == nil && resource.OwnerExecutionID != b.ExecutionID {
		in.PreviousExecutionID = resource.OwnerExecutionID
	}
	if ex, err := s.st.GetIngestExecution(ctx, b.ExecutionID); err == nil {
		in.RuntimeInstanceID = ex.RuntimeInstanceID
		in.Generation = ex.Generation
		if ctrl, err := s.st.GetExecutionControl(ctx, ex.ExecutionID); err == nil {
			in.ControlVersion = ctrl.ControlVersion
		}
	}
	if b.Stage != ingest.TaskMediaProbe {
		pr, err := s.runProbe(run)
		if err != nil {
			return TaskInput{}, err
		}
		in.Probe, in.ProbeSHA256 = &pr, run.ProbeSHA256
		plan, sum, err := s.analysisRevision(ctx, s.st, run, run.AnalysisRevision)
		if err != nil {
			return TaskInput{}, err
		}
		in.Analysis, in.AnalysisSHA256, in.AnalysisRevision = &plan, sum, run.AnalysisRevision
	}
	if b.Stage == ingest.TaskMediaPrepare {
		sel, sum, err := s.selectionRevision(ctx, s.st, run, b.PlanRevision)
		if err != nil {
			return TaskInput{}, err
		}
		profile, profileSHA, err := s.GetProcessingProfile(ctx, run.ProfileID, run.ProfileRevision)
		if err != nil || profileSHA != run.ProfileSHA256 {
			return TaskInput{}, fmt.Errorf("bound content profile changed: %w", model.ErrConflict)
		}
		quotas, err := ingest.FrameQuota(segmentDurations(sel), profile.Sampling.MaxFrames, policy.MaxFramesPerSegment)
		if err != nil {
			return TaskInput{}, err
		}
		in.Selection, in.SelectionSHA256, in.SelectionRevision, in.FrameQuotas = &sel, sum, b.PlanRevision, quotas
		in.Provenance = &ingest.Provenance{SchemaVersion: 1, SourceID: src.SourceID, SourceSHA256: src.SHA256, SourceVersion: src.SourceVersion,
			RunID: run.RunID, AnalysisRevision: run.AnalysisRevision, AnalysisSHA256: in.AnalysisSHA256, SelectionRevision: b.PlanRevision,
			SelectionSHA256: sum, PolicySHA256: run.PolicySHA256, ProbeSHA256: run.ProbeSHA256, ProfileID: run.ProfileID,
			ProfileRevision: run.ProfileRevision, ProfileSHA256: run.ProfileSHA256, FrameQuotaVersion: policy.FrameQuotaVersion}
	}
	bindings, err := s.st.ListIngestBindings(ctx, run.RunID)
	if err != nil {
		return TaskInput{}, err
	}
	for _, other := range bindings {
		if other.Stage != b.Stage || other.InputSHA256 != b.InputSHA256 {
			continue
		}
		cps, err := s.st.ListCheckpoints(ctx, other.TaskID)
		if err != nil {
			return TaskInput{}, err
		}
		for _, cp := range cps {
			if cp.InputSHA256 == b.InputSHA256 && cp.PolicySHA256 == run.PolicySHA256 {
				in.Checkpoints = append(in.Checkpoints, cp)
			}
		}
	}
	return TaskInput{InputKind: "media_ingest", Ingest: &in, TaskID: tk.TaskID, AssetID: tk.AssetID, EDL: map[string]any{}, NarrationApprovals: []string{}}, nil
}

// CheckpointRequest is save_ingest_checkpoint.
type CheckpointRequest struct {
	TaskID            string `json:"task_id"`
	ExecutionID       string `json:"execution_id"`
	RuntimeInstanceID string `json:"runtime_instance_id"`
	Generation        int    `json:"generation"`
	Sequence          int    `json:"sequence"`
	InputSHA256       string `json:"input_sha256"`
	Kind              string `json:"kind"`
	ItemIndex         int    `json:"item_index"`
	Ref               string `json:"ref"`
	SHA256            string `json:"sha256"`
	ManifestVersion   int    `json:"manifest_version,omitempty"`
	JournalVersion    int    `json:"journal_version,omitempty"`
	SourceExecutionID string `json:"source_execution_id,omitempty"`
	Summary           string `json:"summary,omitempty"`
}

var checkpointKinds = map[string]string{ingest.TaskMediaProbe: "copy_chunk", ingest.TaskSegment: "scan_chunk", ingest.TaskMediaPrepare: "segment_media"}

// SaveIngestCheckpoint registers one verified block or segment manifest of
// the current execution. Sequences are dense per task; a repeat of the same
// checkpoint is accepted.
func (s *Service) SaveIngestCheckpoint(ctx context.Context, agentID, role string, req CheckpointRequest) (int, error) {
	scope := ExecutionScope{AgentID: agentID, Role: role, RuntimeInstanceID: req.RuntimeInstanceID, TaskID: req.TaskID,
		ExecutionID: req.ExecutionID, Generation: req.Generation, InputSHA256: req.InputSHA256}
	// Identity is decided before any file read. A stale execution never hashes.
	replay := false
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		auth, err := authorizeExecution(ctx, tx, scope, opCheckpoint)
		if err != nil {
			return err
		}
		if req.Sequence < 1 || req.ItemIndex < 0 || len(req.SHA256) != 64 || !isHex(req.SHA256) {
			return fmt.Errorf("checkpoint sequence, item_index and sha256 are required: %w", model.ErrArgument)
		}
		if auth.Task.Status != model.TaskStatusClaimed && auth.Task.Status != model.TaskStatusRunning {
			return fmt.Errorf("task is not active: %w", model.ErrInvalidState)
		}
		if checkpointKinds[auth.Binding.Stage] != req.Kind {
			return fmt.Errorf("checkpoint kind does not belong to %s: %w", auth.Binding.Stage, model.ErrArgument)
		}
		if !strings.HasPrefix(req.Ref, ingestOutputDir(auth.Binding)+"/checkpoints/") || !strings.HasSuffix(req.Ref, ".json") {
			return fmt.Errorf("checkpoint ref must be a manifest in this execution's checkpoints directory: %w", model.ErrArgument)
		}
		existing, err := tx.ListCheckpoints(ctx, req.TaskID)
		if err != nil {
			return err
		}
		if req.Sequence <= len(existing) {
			prior := existing[req.Sequence-1]
			if prior.Ref == req.Ref && prior.SHA256 == req.SHA256 && prior.ExecutionID == auth.Execution.ExecutionID && scope.Generation == auth.Execution.Generation {
				replay = true
				return nil
			}
			return fmt.Errorf("checkpoint sequence already used: %w", model.ErrConflict)
		}
		if req.Sequence != len(existing)+1 {
			return fmt.Errorf("checkpoint sequence must be %d: %w", len(existing)+1, model.ErrConflict)
		}
		return nil
	})
	if err != nil || replay {
		return req.Sequence, err
	}
	sum, size, hashErr := s.hashSmall(req.Ref, 1<<20)
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		auth, err := authorizeExecution(ctx, tx, scope, opCheckpoint)
		if err != nil {
			return err
		}
		existing, err := tx.ListCheckpoints(ctx, req.TaskID)
		if err != nil {
			return err
		}
		if req.Sequence <= len(existing) {
			prior := existing[req.Sequence-1]
			if prior.Ref == req.Ref && prior.SHA256 == req.SHA256 && prior.ExecutionID == auth.Execution.ExecutionID {
				replay = true
				return nil
			}
			return fmt.Errorf("checkpoint sequence already used: %w", model.ErrConflict)
		}
		if req.Sequence != len(existing)+1 {
			return fmt.Errorf("checkpoint sequence must be %d: %w", len(existing)+1, model.ErrConflict)
		}
		if hashErr != nil {
			return hashErr
		}
		if sum != req.SHA256 || size == 0 {
			return fmt.Errorf("checkpoint manifest hash does not match: %w", model.ErrConflict)
		}
		manifest, journal := req.ManifestVersion, req.JournalVersion
		if manifest == 0 {
			manifest = 1
		}
		return tx.InsertCheckpoint(ctx, model.IngestCheckpoint{TaskID: req.TaskID, Sequence: req.Sequence, ExecutionID: req.ExecutionID,
			InputSHA256: req.InputSHA256, PolicySHA256: auth.Run.PolicySHA256, Kind: req.Kind, ItemIndex: req.ItemIndex, Ref: req.Ref, SHA256: req.SHA256,
			ManifestVersion: manifest, JournalVersion: journal, SourceExecutionID: req.SourceExecutionID, ItemStatus: "registered", Summary: req.Summary, CreatedAt: time.Now().UTC()})
	})
	return req.Sequence, err
}

// hashSmall hashes a bounded file below the delivery root.
func (s *Service) hashSmall(rel string, max int64) (string, int64, error) {
	full, info, err := s.deliveryPath(filepath.FromSlash(rel), false)
	if err != nil {
		return "", 0, err
	}
	if info.Size() > max {
		return "", 0, fmt.Errorf("file exceeds the %d byte limit: %w", max, model.ErrArgument)
	}
	f, err := os.Open(full)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, max+1))
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func (s *Service) readSmall(rel string, max int64) ([]byte, string, error) {
	full, info, err := s.deliveryPath(filepath.FromSlash(rel), false)
	if err != nil {
		return nil, "", err
	}
	if info.Size() > max {
		return nil, "", fmt.Errorf("%s exceeds the %d byte limit: %w", path.Base(rel), max, model.ErrArgument)
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

// IngestResultRequest is submit_ingest_result.
type IngestResultRequest struct {
	TaskID            string `json:"task_id"`
	ExecutionID       string `json:"execution_id"`
	RuntimeInstanceID string `json:"runtime_instance_id"`
	Generation        int    `json:"generation"`
	PackageDir        string `json:"package_dir"`
	InputSHA256       string `json:"input_sha256"`
	RequestID         string `json:"request_id,omitempty"`
}

// ingestRegistration is computed from files before the transaction and
// applied inside it after the guards are rechecked.
type ingestRegistration struct {
	apply func(ctx context.Context, tx *store.Store, run *model.IngestRun, asset *model.Asset) error
	sum   string
}

var snapshotExt = regexp.MustCompile(`^\.[a-z0-9]{1,8}$`)

// submitReplay accepts a second submit of the same execution's already
// committed package. It still requires the instance and generation, and it
// does not read the package. A different execution keeps the guard error.
func submitReplay(ctx context.Context, tx *store.Store, scope ExecutionScope, req IngestResultRequest, authErr error) error {
	ex, err := tx.GetIngestExecution(ctx, scope.ExecutionID)
	if err != nil || ex.AgentID != scope.AgentID || ex.Role != scope.Role || ex.RuntimeInstanceID != scope.RuntimeInstanceID || ex.TaskID != scope.TaskID || ex.Generation != scope.Generation || ex.InputSHA256 != scope.InputSHA256 {
		return authErr
	}
	b, err := tx.GetIngestBinding(ctx, scope.TaskID)
	if err != nil || b.ExecutionID != ex.ExecutionID || b.ExecutionSeq != ex.Generation {
		return authErr
	}
	if ex.Status != model.ExecSucceeded {
		return authErr
	}
	if b.ResultRef == req.PackageDir && b.InputSHA256 == req.InputSHA256 && req.ExecutionID == ex.ExecutionID {
		return nil
	}
	return fmt.Errorf("task already registered a different result: %w", model.ErrConflict)
}

// SubmitIngestResult validates the published package of the current
// execution and registers the result, next state and audit in one
// transaction. Replaying the same package is a no-op.
func (s *Service) SubmitIngestResult(ctx context.Context, agentID, role string, req IngestResultRequest) error {
	scope := ExecutionScope{AgentID: agentID, Role: role, RuntimeInstanceID: req.RuntimeInstanceID, TaskID: req.TaskID,
		ExecutionID: req.ExecutionID, Generation: req.Generation, InputSHA256: req.InputSHA256}
	// The guard runs before any receipt or package read. Only the current
	// execution reaches the file checks below.
	proceed := false
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		_, err := authorizeExecution(ctx, tx, scope, opSubmit)
		if err == nil {
			proceed = true
			return nil
		}
		if !errors.Is(err, model.ErrStaleExecution) && !errors.Is(err, model.ErrInvalidState) {
			return err
		}
		return submitReplay(ctx, tx, scope, req, err)
	})
	if err != nil || !proceed {
		return err
	}
	b, err := s.st.GetIngestBinding(ctx, req.TaskID)
	if err != nil {
		return err
	}
	if b.ExecutionID != req.ExecutionID || b.ExecutionSeq != req.Generation {
		return staleExecution("execution_id is stale; the task was claimed again")
	}
	if req.PackageDir != ingestOutputDir(b)+"/package" {
		return fmt.Errorf("package_dir is not the current execution's package: %w", model.ErrConflict)
	}
	run, err := s.st.GetIngestRun(ctx, b.RunID)
	if err != nil {
		return err
	}
	receiptRaw, _, err := s.readSmall(req.PackageDir+"/worker-receipt.json", 64*1024)
	if err != nil {
		return err
	}
	receipt, err := ingest.ParseReceipt(receiptRaw)
	if err != nil {
		return err
	}
	if receipt.TaskID != b.TaskID || receipt.ExecutionID != b.ExecutionID || receipt.Stage != b.Stage || receipt.InputSHA256 != b.InputSHA256 || receipt.PolicySHA256 != run.PolicySHA256 {
		return fmt.Errorf("worker-receipt.json does not match the fixed task input: %w", model.ErrConflict)
	}
	var reg ingestRegistration
	switch b.Stage {
	case ingest.TaskMediaProbe:
		reg, err = s.probeRegistration(ctx, run, req.PackageDir)
	case ingest.TaskSegment:
		reg, err = s.segmentRegistration(ctx, run, req.PackageDir)
	case ingest.TaskMediaPrepare:
		reg, err = s.prepareRegistration(ctx, run, b, req.PackageDir, receipt.WorkerVersion)
	default:
		err = fmt.Errorf("unknown ingest stage: %w", model.ErrInvalidState)
	}
	if err != nil {
		return err
	}
	var savedTask model.Task
	var savedAsset model.Asset
	var savedRun model.IngestRun
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		scope := ExecutionScope{AgentID: agentID, Role: role, RuntimeInstanceID: req.RuntimeInstanceID, TaskID: req.TaskID,
			ExecutionID: req.ExecutionID, Generation: req.Generation, InputSHA256: req.InputSHA256}
		auth, err := authorizeExecution(ctx, tx, scope, opSubmit)
		if err != nil {
			return err
		}
		tk, b2, run := auth.Task, auth.Binding, auth.Run
		if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
			return fmt.Errorf("task is not active: %w", model.ErrInvalidState)
		}
		if run.State != ingest.StateProcessing {
			return fmt.Errorf("ingest run is %s: %w", run.State, model.ErrInvalidState)
		}
		asset, err := tx.GetAsset(ctx, tk.AssetID)
		if err != nil {
			return err
		}
		prev := run.Version
		if err := reg.apply(ctx, tx, &run, &asset); err != nil {
			return err
		}
		run.ErrorCode, run.ErrorMessage = "", ""
		run.Version++
		if err := tx.UpdateIngestRun(ctx, run, prev); err != nil {
			return err
		}
		if err := tx.UpdateAsset(ctx, asset); err != nil {
			return err
		}
		tk.Status, tk.Progress, tk.LeaseUntil = model.TaskStatusSucceeded, 1, nil
		tk.Artifacts = map[string]string{"package": req.PackageDir}
		if err := tx.UpdateTask(ctx, tk); err != nil {
			return err
		}
		b2.ResultRef, b2.ResultSHA256 = req.PackageDir, reg.sum
		b2.ExecutionState = model.ExecSucceeded
		if err := tx.UpdateIngestBinding(ctx, b2); err != nil {
			return err
		}
		auth.Execution.Status = model.ExecSucceeded
		now := time.Now().UTC()
		auth.Execution.SubmittedAt = &now
		auth.Execution.ResultRequestID = req.RequestID
		if err := tx.UpdateIngestExecution(ctx, auth.Execution); err != nil {
			return err
		}
		if err := s.releaseResourceIfOwner(ctx, tx, run, auth.Execution.ExecutionID); err != nil {
			return err
		}
		if req.RequestID != "" {
			content := req.InputSHA256 + "\n" + req.PackageDir + "\n" + reg.sum
			if existing, err := tx.GetExecutionRequest(ctx, req.RuntimeInstanceID, req.RequestID, opSubmit); errors.Is(err, model.ErrNotFound) {
				if err := tx.InsertExecutionRequest(ctx, model.ExecutionRequest{
					RuntimeInstanceID: req.RuntimeInstanceID, RequestID: req.RequestID, Operation: opSubmit,
					AgentID: agentID, ExecutionID: req.ExecutionID, ContentSHA256: content, ResultStatus: "committed", CreatedAt: now,
				}); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if existing.ContentSHA256 != content {
				_ = tx.UpdateExecutionRequestStatus(ctx, req.RuntimeInstanceID, req.RequestID, opSubmit, "conflict", existing.ContentSHA256)
				return fmt.Errorf("submit request content changed: %w", model.ErrConflict)
			} else if err := tx.UpdateExecutionRequestStatus(ctx, req.RuntimeInstanceID, req.RequestID, opSubmit, "committed", content); err != nil {
				return err
			}
		}
		if err := auditTx(ctx, tx, "agent:"+agentID, "ingest.result", run.RunID, b2.Stage+" task="+tk.TaskID+" sha256="+reg.sum); err != nil {
			return err
		}
		savedRun = run
		if savedTask, err = tx.GetTask(ctx, tk.TaskID); err != nil {
			return err
		}
		savedAsset, err = tx.GetAsset(ctx, tk.AssetID)
		return err
	})
	if err != nil {
		if ingestBlocks(ctx, s.st, req.TaskID) != nil {
			s.audit(ctx, "agent:"+agentID, "stale_ingest.reject", req.TaskID, sanitizeReason(err.Error()))
		}
		return err
	}
	s.publishIngest(savedRun)
	s.publish("task_updated", cloneTask(savedTask))
	s.publish("asset_updated", cloneAsset(savedAsset))
	return nil
}

func (s *Service) probeRegistration(ctx context.Context, run model.IngestRun, pkg string) (ingestRegistration, error) {
	src, err := s.st.GetSource(ctx, run.SourceID)
	if err != nil {
		return ingestRegistration{}, err
	}
	policy, err := runPolicy(run)
	if err != nil {
		return ingestRegistration{}, err
	}
	snapRaw, snapSum, err := s.readSmall(pkg+"/snapshot.json", 64*1024)
	if err != nil {
		return ingestRegistration{}, err
	}
	snap, err := ingest.ParseSnapshot(snapRaw)
	if err != nil {
		return ingestRegistration{}, err
	}
	if snap.SourceID != src.SourceID || snap.SourceVersion != src.SourceVersion || snap.SizeBytes != src.SizeBytes || snap.MTimeNs != src.MTimeNs {
		return ingestRegistration{}, fmt.Errorf("the recording changed while it was copied; register it again: %w", model.ErrConflict)
	}
	ext := strings.ToLower(path.Ext(src.RelativePath))
	if !snapshotExt.MatchString(ext) || snap.Snapshot != sourceDir(src.SourceID)+"/snapshot"+ext || len(snap.SHA256) != 64 || !isHex(snap.SHA256) {
		return ingestRegistration{}, fmt.Errorf("snapshot.json does not name the controlled snapshot: %w", model.ErrArgument)
	}
	if src.SnapshotRef != "" && (src.SnapshotRef != snap.Snapshot || src.SHA256 != snap.SHA256) {
		return ingestRegistration{}, fmt.Errorf("published snapshot is immutable: %w", model.ErrConflict)
	}
	_, info, err := s.deliveryPath(filepath.FromSlash(snap.Snapshot), false)
	if err != nil {
		return ingestRegistration{}, err
	}
	if info.Size() != src.SizeBytes {
		return ingestRegistration{}, fmt.Errorf("snapshot length differs from the source: %w", model.ErrConflict)
	}
	probeRaw, _, err := s.readSmall(pkg+"/probe.json", 1<<20)
	if err != nil {
		return ingestRegistration{}, err
	}
	pr, err := ingest.ParseProbe(probeRaw, policy, src.SizeBytes)
	if err != nil {
		return ingestRegistration{}, err
	}
	if !strings.Contains(pr.Container, "matroska") && !strings.Contains(pr.Container, "mp4") && !strings.Contains(pr.Container, "mov") {
		return ingestRegistration{}, fmt.Errorf("container %q is not supported (MKV/MP4/MOV): %w", pr.Container, model.ErrArgument)
	}
	probeJSON, probeSHA := mustCanonical(pr)
	return ingestRegistration{sum: snapSum, apply: func(ctx context.Context, tx *store.Store, run *model.IngestRun, asset *model.Asset) error {
		if err := tx.PublishSource(ctx, src.SourceID, snap.Snapshot, snap.SHA256, pr.ProbeVersion); err != nil {
			return err
		}
		run.ProbeJSON, run.ProbeSHA256 = probeJSON, probeSHA
		run.State, run.Stage = ingest.StateAwaitingReview, ingest.StageProbe
		return nil
	}}, nil
}

func (s *Service) segmentRegistration(ctx context.Context, run model.IngestRun, pkg string) (ingestRegistration, error) {
	policy, err := runPolicy(run)
	if err != nil {
		return ingestRegistration{}, err
	}
	plan, planSHA, err := s.analysisRevision(ctx, s.st, run, run.AnalysisRevision)
	if err != nil {
		return ingestRegistration{}, err
	}
	raw, sum, err := s.readSmall(pkg+"/segments.json", policy.MaxDetectionJSONBytes)
	if err != nil {
		return ingestRegistration{}, err
	}
	doc, err := ingest.ParseSegments(raw, plan, planSHA, policy)
	if err != nil {
		return ingestRegistration{}, err
	}
	thumbs := map[string]string{}
	for _, c := range doc.Candidates {
		if c.Thumbnail == "" {
			continue
		}
		rel := pkg + "/" + c.Thumbnail
		if _, info, err := s.deliveryPath(filepath.FromSlash(rel), false); err != nil || info.Size() > 2<<20 {
			return ingestRegistration{}, fmt.Errorf("thumbnail for %s is missing or too large: %w", c.SegmentID, model.ErrArgument)
		}
		thumbs["thumb_"+c.SegmentID] = rel
	}
	analysisRev := run.AnalysisRevision
	return ingestRegistration{sum: sum, apply: func(ctx context.Context, tx *store.Store, run *model.IngestRun, asset *model.Asset) error {
		if run.AnalysisRevision != analysisRev {
			return fmt.Errorf("analysis plan changed during registration: %w", model.ErrConflict)
		}
		run.SegmentsRef, run.SegmentsSHA256 = pkg+"/segments.json", sum
		dropFiles(run.Files, "thumb_")
		for k, v := range thumbs {
			run.Files[k] = v
		}
		run.State, run.Stage = ingest.StateAwaitingReview, ingest.StageSegmentReview
		return nil
	}}, nil
}

func (s *Service) prepareRegistration(ctx context.Context, run model.IngestRun, b model.IngestTaskBinding, pkg, workerVersion string) (ingestRegistration, error) {
	src, err := s.st.GetSource(ctx, run.SourceID)
	if err != nil {
		return ingestRegistration{}, err
	}
	policy, err := runPolicy(run)
	if err != nil {
		return ingestRegistration{}, err
	}
	sel, selSHA, err := s.selectionRevision(ctx, s.st, run, b.PlanRevision)
	if err != nil {
		return ingestRegistration{}, err
	}
	plan, planSHA, err := s.analysisRevision(ctx, s.st, run, run.AnalysisRevision)
	if err != nil {
		return ingestRegistration{}, err
	}
	profile, profileSHA, err := s.GetProcessingProfile(ctx, run.ProfileID, run.ProfileRevision)
	if err != nil || profileSHA != run.ProfileSHA256 {
		return ingestRegistration{}, fmt.Errorf("bound content profile changed: %w", model.ErrConflict)
	}
	quotas, err := ingest.FrameQuota(segmentDurations(sel), profile.Sampling.MaxFrames, policy.MaxFramesPerSegment)
	if err != nil {
		return ingestRegistration{}, err
	}
	artifacts, err := s.validatePackage(filepath.FromSlash(pkg))
	if err != nil {
		return ingestRegistration{}, err
	}
	if artifacts["edl"] == "" || artifacts["source_map"] == "" || artifacts["ingest_provenance"] == "" {
		return ingestRegistration{}, fmt.Errorf("prepared package must declare edl, source_map and ingest_provenance: %w", model.ErrArgument)
	}
	mapRaw, _, err := s.readSmall(pkg+"/source-map.json", 4<<20)
	if err != nil {
		return ingestRegistration{}, err
	}
	smap, err := ingest.ParseSourceMap(mapRaw, sel, selSHA, quotas, plan.GameAudioStreamIndex != nil)
	if err != nil {
		return ingestRegistration{}, err
	}
	if smap.VideoStreamIndex != plan.VideoStreamIndex || !sameIntPtr(smap.GameAudioStreamIndex, plan.GameAudioStreamIndex) || smap.SourceID != src.SourceID {
		return ingestRegistration{}, fmt.Errorf("source-map.json streams differ from the analysis plan: %w", model.ErrArgument)
	}
	files := map[string]string{}
	for _, seg := range smap.Segments {
		sum, _, err := s.hashSmall(pkg+"/"+seg.Media, 4<<30)
		if err != nil {
			return ingestRegistration{}, err
		}
		if sum != seg.MediaSHA256 {
			return ingestRegistration{}, fmt.Errorf("prepared media %s hash differs from source-map: %w", seg.SegmentID, model.ErrConflict)
		}
		files["media_"+seg.SegmentID] = pkg + "/" + seg.Media
	}
	edl, edlSHA, err := s.readPackageEDL(pkg)
	if err != nil {
		return ingestRegistration{}, err
	}
	if err := ingest.CheckEDL(edl, smap); err != nil {
		return ingestRegistration{}, err
	}
	provRaw, _, err := s.readSmall(pkg+"/ingest-provenance.json", 64*1024)
	if err != nil {
		return ingestRegistration{}, err
	}
	want := ingest.Provenance{SchemaVersion: 1, SourceID: src.SourceID, SourceSHA256: src.SHA256, SourceVersion: src.SourceVersion, RunID: run.RunID,
		AnalysisRevision: run.AnalysisRevision, AnalysisSHA256: planSHA, SelectionRevision: b.PlanRevision, SelectionSHA256: selSHA,
		PolicySHA256: run.PolicySHA256, ProbeSHA256: run.ProbeSHA256, ProfileID: run.ProfileID, ProfileRevision: run.ProfileRevision,
		ProfileSHA256: run.ProfileSHA256, FrameQuotaVersion: policy.FrameQuotaVersion, WorkerVersion: workerVersion}
	if err := ingest.ParseProvenance(provRaw, want); err != nil {
		return ingestRegistration{}, err
	}
	return ingestRegistration{sum: edlSHA, apply: func(ctx context.Context, tx *store.Store, run *model.IngestRun, asset *model.Asset) error {
		if _, err := tx.ActiveWorkflow(ctx, asset.AssetID); err == nil {
			return fmt.Errorf("asset has an active content workflow; its input cannot be replaced: %w", model.ErrConflict)
		} else if !errors.Is(err, model.ErrNotFound) {
			return err
		}
		asset.Artifacts = cloneStringMap(artifacts)
		asset.Status = model.AssetStatusIngested
		asset.InputKind = model.InputKindEDLPackage
		asset.IngestRunID = run.RunID
		dropFiles(run.Files, "media_")
		for k, v := range files {
			run.Files[k] = v
		}
		run.PackageRef = pkg
		run.State, run.Stage = ingest.StateReady, ingest.StageReady
		return nil
	}}, nil
}

func sameIntPtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
