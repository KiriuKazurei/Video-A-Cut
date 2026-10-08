package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

var _ preview.Application = (*Service)(nil)

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func (s *Service) getLatestExportRevisionID(ctx context.Context, runID string) (string, error) {
	return s.previewRevisionID(ctx, s.st, runID)
}

func (s *Service) previewRevisionID(ctx context.Context, st *store.Store, runID string) (string, error) {
	rev, err := s.currentPreviewRevision(ctx, st, runID)
	if errors.Is(err, model.ErrConflict) || errors.Is(err, model.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return rev.RevisionID, nil
}

func (s *Service) projectPreviewView(ctx context.Context, job model.PreviewJob, latestExportRevID string) (preview.View, error) {
	return s.projectPreviewViewAt(ctx, s.st, job, latestExportRevID)
}

func (s *Service) projectPreviewViewAt(ctx context.Context, st *store.Store, job model.PreviewJob, latestExportRevID string) (preview.View, error) {
	tk, err := st.GetTask(ctx, job.TaskID)
	if err != nil {
		return preview.View{}, fmt.Errorf("service: get preview task %s: %w", job.TaskID, err)
	}
	artRecords, err := st.ListPreviewArtifacts(ctx, job.PreviewID)
	if err != nil {
		return preview.View{}, fmt.Errorf("service: list preview artifacts %s: %w", job.PreviewID, err)
	}
	artifacts := make([]preview.Artifact, 0, len(artRecords))
	for _, ar := range artRecords {
		artifacts = append(artifacts, preview.Artifact{
			Key:    ar.Key,
			Kind:   ar.Kind,
			Path:   ar.Path,
			MIME:   ar.MIME,
			Size:   ar.Size,
			SHA256: ar.SHA256,
		})
	}
	var media *preview.Media
	if job.MediaJSON != "" {
		var m preview.Media
		if err := json.Unmarshal([]byte(job.MediaJSON), &m); err == nil {
			media = &m
		}
	}
	var frozen preview.Input
	_ = json.Unmarshal([]byte(job.InputJSON), &frozen)
	subtitleID, styleSHA := "", ""
	if frozen.SubtitleVariant != nil {
		subtitleID = frozen.SubtitleVariant.VariantID
		styleSHA = frozen.SubtitleVariant.StyleSHA256
	}
	isCurrent := !job.Invalidated && (latestExportRevID != "") && (job.RevisionID == latestExportRevID)
	return preview.View{
		SubtitleVariantID: subtitleID, StyleSHA256: styleSHA,
		PreviewID:   job.PreviewID,
		TaskID:      job.TaskID,
		AssetID:     job.AssetID,
		RunID:       job.RunID,
		RevisionID:  job.RevisionID,
		PresetID:    preview.PresetID,
		InputSHA256: job.InputSHA256,
		Status:      tk.Status,
		Progress:    tk.Progress,
		Message:     tk.Message,
		Invalidated: job.Invalidated,
		IsCurrent:   isCurrent,
		Artifacts:   artifacts,
		Media:       media,
	}, nil
}

// StartPreview starts or reuses an MP4 preview job for a workflow run's exported revision.
// Returns view, created (true if a new preview task was created, false if reused), and error.
func (s *Service) StartPreview(ctx context.Context, actor, runID string, req preview.StartRequest) (preview.View, bool, error) {
	if actor == "" {
		return preview.View{}, false, fmt.Errorf("service: actor is required: %w", model.ErrArgument)
	}
	if runID == "" || !validTaskID(runID) {
		return preview.View{}, false, fmt.Errorf("service: invalid run id %q: %w", runID, model.ErrArgument)
	}
	if req.RevisionID == "" {
		return preview.View{}, false, fmt.Errorf("service: revision_id is required: %w", model.ErrArgument)
	}
	if req.RequestID == "" {
		return preview.View{}, false, fmt.Errorf("service: request_id is required: %w", model.ErrArgument)
	}
	if req.PresetID == "" {
		req.PresetID = preview.PresetID
	}
	if req.PresetID != preview.PresetID {
		return preview.View{}, false, fmt.Errorf("service: unsupported preset %q: %w", req.PresetID, model.ErrArgument)
	}

	presetBytes, presetSHA, err := preview.GetPreset(req.PresetID)
	if err != nil {
		return preview.View{}, false, err
	}
	// Recover an existing transport request before checking mutable prerequisites.
	if existing, e := s.st.GetPreviewJobByRequest(ctx, runID, req.RequestID); e == nil {
		if existing.RevisionID != req.RevisionID || existing.ExpectedVersion != req.ExpectedVersion || existing.PresetSHA256 != presetSHA || !previewRequestSubtitleMatches(existing, req) {
			return preview.View{}, false, model.ErrConflict
		}
		current, _ := s.getLatestExportRevisionID(ctx, runID)
		view, e := s.projectPreviewView(ctx, existing, current)
		return view, false, e
	} else if !errors.Is(e, model.ErrNotFound) {
		return preview.View{}, false, e
	}

	run, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return preview.View{}, false, err
	}
	if run.Status == model.WorkflowCancelled {
		return preview.View{}, false, fmt.Errorf("service: workflow %s is cancelled: %w", runID, model.ErrConflict)
	}
	if req.ExpectedVersion != run.Version {
		return preview.View{}, false, fmt.Errorf("service: expected version %d does not match run version %d: %w", req.ExpectedVersion, run.Version, model.ErrConflict)
	}

	asset, err := s.st.GetAsset(ctx, run.AssetID)
	if err != nil {
		return preview.View{}, false, err
	}
	if asset.Locked {
		return preview.View{}, false, fmt.Errorf("service: asset %s is locked: %w", asset.AssetID, model.ErrForbidden)
	}
	if !assetVisibleForRole(asset, "exporter") {
		return preview.View{}, false, fmt.Errorf("service: asset %s does not allow exporter: %w", asset.AssetID, model.ErrForbidden)
	}
	if asset.Status != model.AssetStatusExported {
		return preview.View{}, false, fmt.Errorf("service: asset %s is not in exported status: %w", asset.AssetID, model.ErrInvalidState)
	}
	latestExportRevID, err := s.getLatestExportRevisionID(ctx, runID)
	if err != nil {
		return preview.View{}, false, err
	}
	if latestExportRevID == "" || req.RevisionID != latestExportRevID {
		return preview.View{}, false, model.ErrConflict
	}

	hasCap, err := s.CheckPreviewCapability(ctx, req.PresetID)
	if err != nil {
		return preview.View{}, false, err
	}
	if !hasCap {
		return preview.View{}, false, fmt.Errorf("service: no active worker capable of preset %s: %w", req.PresetID, model.ErrInvalidState)
	}

	// Build PreviewInput from export package files
	rev, err := s.st.GetRevision(ctx, req.RevisionID)
	if err != nil {
		return preview.View{}, false, err
	}
	pkgAbs, err := s.previewPackageDir(rev.PackageRef)
	if err != nil {
		return preview.View{}, false, fmt.Errorf("service: invalid export package path %s: %w", rev.PackageRef, err)
	}

	manifestPath, _, err := s.previewPackageFile(pkgAbs, "delivery-manifest.json")
	if err != nil {
		return preview.View{}, false, err
	}
	manifestBytes, err := previewReadJSON(manifestPath)
	if err != nil {
		return preview.View{}, false, fmt.Errorf("service: read delivery-manifest.json: %w", err)
	}
	manifestSHA := sha256Hex(manifestBytes)
	var delivManifest deliveryManifest
	if err := json.Unmarshal(manifestBytes, &delivManifest); err != nil {
		return preview.View{}, false, fmt.Errorf("service: parse delivery-manifest.json: %w", err)
	}

	edlPath, _, err := s.previewPackageFile(pkgAbs, "edl.json")
	if err != nil {
		return preview.View{}, false, err
	}
	edlBytes, err := previewReadJSON(edlPath)
	if err != nil {
		return preview.View{}, false, fmt.Errorf("service: read edl.json: %w", err)
	}
	edlSHA := sha256Hex(edlBytes)
	var edlMap map[string]any
	if err := json.Unmarshal(edlBytes, &edlMap); err != nil {
		return preview.View{}, false, fmt.Errorf("service: parse edl.json: %w", err)
	}

	filesMap := make(map[string]preview.InputFile)
	aliases := make(map[string]string)
	addFile := func(kind, relPath string) error {
		clean := filepath.ToSlash(relPath)
		if _, exists := filesMap[clean]; exists {
			return nil
		}
		if !preview.IsRelativePath(clean) {
			return fmt.Errorf("invalid file path %q: %w", clean, preview.ErrInvalidInput)
		}
		alias := strings.ToLower(clean)
		if previous, exists := aliases[alias]; exists && previous != clean {
			return model.ErrArgument
		}
		aliases[alias] = clean
		abs, info, err := s.previewPackageFile(pkgAbs, clean)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("missing or irregular file %s: %w", clean, model.ErrNotFound)
		}
		digest, err := previewHashFile(ctx, abs)
		if err != nil {
			return fmt.Errorf("read file %s: %w", clean, err)
		}
		filesMap[clean] = preview.InputFile{
			Kind:   kind,
			Path:   clean,
			SHA256: digest,
			Size:   info.Size(),
		}
		return nil
	}

	if err := addFile("edl", "edl.json"); err != nil {
		return preview.View{}, false, err
	}
	if err := addFile("delivery_manifest", "delivery-manifest.json"); err != nil {
		return preview.View{}, false, err
	}

	for _, art := range delivManifest.Artifacts {
		mapped := ""
		switch art.Kind {
		case "source_video", "video":
			mapped = "source_video"
		case "packaged_source":
			mapped = "packaged_source"
		case "game_audio":
			mapped = "game_audio"
		case "voice":
			mapped = "voice"
		case "music":
			mapped = "music"
		case "subtitles", "srt":
			mapped = "srt"
		}
		if mapped != "" {
			if err := addFile(mapped, art.Path); err != nil {
				return preview.View{}, false, err
			}
		}
	}

	// Conventional stem and subtitle fallback
	for _, pair := range [][2]string{
		{"game_audio", "audio_a1.wav"},
		{"voice", "audio_a2.wav"},
		{"music", "audio_a3.wav"},
		{"srt", "subtitles.srt"},
	} {
		kind, name := pair[0], pair[1]
		if _, exists := filesMap[name]; !exists {
			if _, err := os.Stat(filepath.Join(pkgAbs, name)); err == nil {
				if err = addFile(kind, name); err != nil {
					return preview.View{}, false, err
				}
			}
		}
	}

	// EDL video and audio references fallback
	if videoClips, ok := edlMap["video"].([]any); ok {
		for _, v := range videoClips {
			if clip, ok := v.(map[string]any); ok {
				if src, ok := clip["src"].(string); ok && src != "" {
					if _, exists := filesMap[src]; !exists {
						if _, err := os.Stat(filepath.Join(pkgAbs, filepath.FromSlash(src))); err == nil {
							if err = addFile("source_video", src); err != nil {
								return preview.View{}, false, err
							}
						}
					}
				}
			}
		}
	}
	for _, trackKey := range []string{"game_audio", "voice", "music"} {
		if audioClips, ok := edlMap[trackKey].([]any); ok {
			for _, a := range audioClips {
				if clip, ok := a.(map[string]any); ok {
					if src, ok := clip["src"].(string); ok && src != "" {
						if _, exists := filesMap[src]; !exists {
							if _, err := os.Stat(filepath.Join(pkgAbs, filepath.FromSlash(src))); err == nil {
								if err = addFile("packaged_source", src); err != nil {
									return preview.View{}, false, err
								}
							}
						}
					}
				}
			}
		}
	}

	var files []preview.InputFile
	var hasVideo, hasGameAudio, hasVoice, hasMusic bool
	for _, f := range filesMap {
		files = append(files, f)
		switch f.Kind {
		case "source_video":
			hasVideo = true
		case "game_audio":
			hasGameAudio = true
		case "voice":
			hasVoice = true
		case "music":
			hasMusic = true
		}
	}
	if !hasVideo || !hasGameAudio || !hasVoice || !hasMusic {
		return preview.View{}, false, fmt.Errorf("service: missing required source media or audio stems: %w", model.ErrInvalidState)
	}

	previewIn := preview.Input{
		SchemaVersion:  preview.ProtocolVersion,
		PreviewID:      "",
		TaskID:         "",
		AssetID:        asset.AssetID,
		RunID:          run.RunID,
		RevisionID:     rev.RevisionID,
		PackageRef:     filepath.ToSlash(rev.PackageRef),
		EDLSHA256:      edlSHA,
		ManifestSHA256: manifestSHA,
		PresetID:       preview.PresetID,
		PresetSHA256:   presetSHA,
		Preset:         json.RawMessage(presetBytes),
		Files:          files,
	}
	if req.SubtitleVariantID != "" {
		sub, e := s.freezeSubtitleVariant(ctx, req.SubtitleVariantID, runID, rev.RevisionID)
		if e != nil {
			return preview.View{}, false, e
		}
		ok, e := s.checkASSPreviewCapability(ctx, sub.FontFamilies)
		if e != nil {
			return preview.View{}, false, e
		}
		if !ok {
			return preview.View{}, false, fmt.Errorf("等待支持 ASS 且具有所选字体的预览 Worker: %w", model.ErrInvalidState)
		}
		previewIn.SchemaVersion = 2
		previewIn.SubtitleVariant = sub
	}
	inputSHA, err := preview.Fingerprint(previewIn)
	if err != nil {
		return preview.View{}, false, fmt.Errorf("service: compute preview fingerprint: %w", err)
	}
	previewIn.InputSHA256 = inputSHA

	previewID := newID("prev")
	taskID := newID("tkprev")
	previewIn.PreviewID = previewID
	previewIn.TaskID = taskID
	inputJSON, err := json.Marshal(previewIn)
	if err != nil {
		return preview.View{}, false, fmt.Errorf("service: encode preview input: %w", err)
	}

	now := time.Now().UTC()
	tk := model.Task{
		TaskID:    taskID,
		AssetID:   asset.AssetID,
		Type:      model.TaskTypePreview,
		AgentRole: "exporter",
		Status:    model.TaskStatusQueued,
		Progress:  0,
		UpdatedAt: now,
	}
	job := model.PreviewJob{
		PreviewID:       previewID,
		TaskID:          taskID,
		AssetID:         asset.AssetID,
		RunID:           run.RunID,
		RevisionID:      rev.RevisionID,
		RequestID:       req.RequestID,
		ExpectedVersion: req.ExpectedVersion,
		InputJSON:       string(inputJSON),
		InputSHA256:     inputSHA,
		PresetSHA256:    presetSHA,
		Invalidated:     false,
		CreatedBy:       actor,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	var createdView preview.View
	snapshot, err := s.verifyPreviewSnapshot(ctx, previewIn)
	if err != nil {
		return preview.View{}, false, err
	}
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		// Concurrency double check for request_id
		if dup, e := tx.GetPreviewJobByRequest(ctx, runID, req.RequestID); e == nil {
			if dup.RevisionID != req.RevisionID || dup.ExpectedVersion != req.ExpectedVersion || dup.PresetSHA256 != presetSHA || !previewRequestSubtitleMatches(dup, req) {
				return model.ErrConflict
			}
			current, _ := s.previewRevisionID(ctx, tx, runID)
			v, e := s.projectPreviewViewAt(ctx, tx, dup, current)
			if e != nil {
				return e
			}
			createdView = v
			return nil
		} else if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		fresh, e := tx.GetWorkflow(ctx, runID)
		if e != nil {
			return e
		}
		current, e := s.currentPreviewRevision(ctx, tx, runID)
		if e != nil {
			return e
		}
		if fresh.Version != req.ExpectedVersion || current.RevisionID != req.RevisionID {
			return model.ErrConflict
		}
		if e = s.recheckPreviewSnapshot(ctx, snapshot); e != nil {
			return e
		}
		if e = s.verifySubtitleBindingAt(ctx, tx, previewIn); e != nil {
			return e
		}
		// Concurrency double check for reusable job
		if r, e := tx.FindReusablePreviewJob(ctx, runID, rev.RevisionID, inputSHA); e == nil {
			if e = tx.BindPreviewRequest(ctx, runID, req.RequestID, r.PreviewID, req.RevisionID, req.ExpectedVersion, presetSHA); e != nil {
				return e
			}
			v, e := s.projectPreviewViewAt(ctx, tx, r, current.RevisionID)
			if e != nil {
				return e
			}
			createdView = v
			return nil
		} else if !errors.Is(e, model.ErrNotFound) {
			return e
		}

		if err := tx.CreateTask(ctx, tk); err != nil {
			return fmt.Errorf("service: create preview task: %w", err)
		}
		if err := tx.InsertPreviewJob(ctx, job); err != nil {
			return fmt.Errorf("service: insert preview job: %w", err)
		}
		if err := auditTx(ctx, tx, actor, "preview.start", previewID, "task="+taskID); err != nil {
			return err
		}

		createdView, e = s.projectPreviewViewAt(ctx, tx, job, rev.RevisionID)
		if e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		return preview.View{}, false, err
	}

	if createdView.PreviewID != previewID {
		// Reused in race condition
		return createdView, false, nil
	}

	s.publish("task_created", cloneTask(tk))
	s.publish("preview_updated", createdView)
	return createdView, true, nil
}

// Start satisfies preview.Application.
func (s *Service) Start(ctx context.Context, actor, runID string, req preview.StartRequest) (preview.View, error) {
	v, _, err := s.StartPreview(ctx, actor, runID, req)
	return v, err
}

// ListPreviews returns current revision, readiness checks, and historical previews for a run.
func (s *Service) ListPreviews(ctx context.Context, runID string) (preview.List, error) {
	run, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return preview.List{}, err
	}
	latestExportRevID, err := s.getLatestExportRevisionID(ctx, runID)
	if err != nil {
		return preview.List{}, err
	}

	checks := make([]preview.Check, 0, 4)
	canStart := true

	// Check 1: workflow status
	if run.Status == model.WorkflowCancelled {
		checks = append(checks, preview.Check{
			Code:    "workflow_status",
			Status:  "blocked",
			Message: "workflow is cancelled",
		})
		canStart = false
	} else {
		checks = append(checks, preview.Check{
			Code:    "workflow_status",
			Status:  "passed",
			Message: "workflow is active",
		})
	}

	// Check 2: export revision
	if latestExportRevID == "" {
		checks = append(checks, preview.Check{
			Code:    "export_revision",
			Status:  "blocked",
			Message: "no completed export revision",
		})
		canStart = false
	} else {
		checks = append(checks, preview.Check{
			Code:    "export_revision",
			Status:  "passed",
			Message: fmt.Sprintf("revision %s is exported", latestExportRevID),
		})
	}

	// Check 3: governance
	asset, err := s.st.GetAsset(ctx, run.AssetID)
	if err != nil {
		checks = append(checks, preview.Check{
			Code:    "governance",
			Status:  "blocked",
			Message: err.Error(),
		})
		canStart = false
	} else if asset.Locked {
		checks = append(checks, preview.Check{
			Code:    "governance",
			Status:  "blocked",
			Message: "asset is locked",
		})
		canStart = false
	} else if !assetVisibleForRole(asset, "exporter") {
		checks = append(checks, preview.Check{
			Code:    "governance",
			Status:  "blocked",
			Message: "exporter role not allowed",
		})
		canStart = false
	} else {
		checks = append(checks, preview.Check{
			Code:    "governance",
			Status:  "passed",
			Message: "governance policy allows exporter",
		})
	}

	// Check 4: worker capability
	hasCap, err := s.CheckPreviewCapability(ctx, preview.PresetID)
	if err != nil || !hasCap {
		checks = append(checks, preview.Check{
			Code:    "worker_capability",
			Status:  "blocked",
			Message: "no active worker capable of preset " + preview.PresetID,
		})
		canStart = false
	} else {
		checks = append(checks, preview.Check{
			Code:    "worker_capability",
			Status:  "passed",
			Message: "worker capability available for preset " + preview.PresetID,
		})
	}

	jobs, err := s.st.ListPreviewJobs(ctx, runID)
	if err != nil {
		return preview.List{}, err
	}
	views := make([]preview.View, 0, len(jobs))
	for _, j := range jobs {
		v, err := s.projectPreviewView(ctx, j, latestExportRevID)
		if err == nil {
			views = append(views, v)
		}
	}

	return preview.List{
		RunID:           run.RunID,
		RevisionID:      latestExportRevID,
		ExpectedVersion: run.Version,
		CanStart:        canStart,
		Checks:          checks,
		Previews:        views,
	}, nil
}

// List satisfies preview.Application.
func (s *Service) List(ctx context.Context, runID string) (preview.List, error) {
	return s.ListPreviews(ctx, runID)
}

// GetPreview returns the current projected view for a preview job.
func (s *Service) GetPreview(ctx context.Context, previewID string) (preview.View, error) {
	job, err := s.st.GetPreviewJob(ctx, previewID)
	if err != nil {
		return preview.View{}, err
	}
	latestExportRevID, _ := s.getLatestExportRevisionID(ctx, job.RunID)
	return s.projectPreviewView(ctx, job, latestExportRevID)
}

// Get satisfies preview.Application.
func (s *Service) Get(ctx context.Context, previewID string) (preview.View, error) {
	return s.GetPreview(ctx, previewID)
}

// CancelPreview cancels an active preview task. Idempotent if already terminal.
func (s *Service) CancelPreview(ctx context.Context, actor, previewID string) (preview.View, error) {
	job, err := s.st.GetPreviewJob(ctx, previewID)
	if err != nil {
		return preview.View{}, err
	}
	tk, err := s.st.GetTask(ctx, job.TaskID)
	if err != nil {
		return preview.View{}, err
	}
	latestExportRevID, _ := s.getLatestExportRevisionID(ctx, job.RunID)

	if tk.Status == model.TaskStatusSucceeded || tk.Status == model.TaskStatusFailed || tk.Status == model.TaskStatusCancelled {
		return s.projectPreviewView(ctx, job, latestExportRevID)
	}

	var savedTask model.Task
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		current, err := tx.GetTask(ctx, job.TaskID)
		if err != nil {
			return err
		}
		if current.Status == model.TaskStatusSucceeded || current.Status == model.TaskStatusFailed || current.Status == model.TaskStatusCancelled {
			savedTask = current
			return nil
		}
		current.Status = model.TaskStatusCancelled
		current.LeaseUntil = nil
		current.Message = "preview cancelled by " + actor
		current.UpdatedAt = time.Now().UTC()
		if err := tx.UpdateTask(ctx, current); err != nil {
			return err
		}
		savedTask = current
		return auditTx(ctx, tx, actor, "preview.cancel", previewID, "task="+job.TaskID)
	})
	if err != nil {
		return preview.View{}, err
	}

	s.publish("task_updated", cloneTask(savedTask))
	view, err := s.projectPreviewView(ctx, job, latestExportRevID)
	if err == nil {
		s.publish("preview_updated", view)
	}
	return view, err
}

// Cancel satisfies preview.Application.
func (s *Service) Cancel(ctx context.Context, actor, previewID string) (preview.View, error) {
	return s.CancelPreview(ctx, actor, previewID)
}

// PreviewTaskInput is the legacy port. Preview callers must use GetTaskInputScoped;
// an identity without a claim scope fails closed.
func (s *Service) PreviewTaskInput(ctx context.Context, agentID, taskID string) (preview.Input, error) {
	in, err := s.GetTaskInputScoped(ctx, ExecutionScope{AgentID: agentID, Role: "exporter", TaskID: taskID})
	if err != nil {
		return preview.Input{}, err
	}
	prevIn, ok := in.Preview.(*preview.Input)
	if !ok || prevIn == nil {
		return preview.Input{}, model.ErrInvalidState
	}
	return *prevIn, nil
}

// TaskInput satisfies preview.Application.
func (s *Service) TaskInput(ctx context.Context, agentID, taskID string) (preview.Input, error) {
	return s.PreviewTaskInput(ctx, agentID, taskID)
}

// SubmitPreview is the legacy port and rejects an identity without a claim scope.
// Preview callers must use SubmitPreviewScoped.
func (s *Service) SubmitPreview(ctx context.Context, agentID, taskID, previewDir string) error {
	return s.SubmitPreviewScoped(ctx, ExecutionScope{AgentID: agentID, Role: "exporter", TaskID: taskID}, previewDir)
}

// SubmitPreviewScoped binds registration (including replay) to the current attempt.
func (s *Service) SubmitPreviewScoped(ctx context.Context, scope ExecutionScope, previewDir string) error {
	agentID, taskID := scope.AgentID, scope.TaskID
	if agentID == "" || taskID == "" || previewDir == "" {
		return fmt.Errorf("service: agent_id, task_id and preview_dir are required: %w", model.ErrArgument)
	}
	bound, err := s.st.GetPreviewJobByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	var task model.Task
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		auth, e := authorizeExecution(ctx, tx, scope, opSubmit)
		task = auth.Task
		return e
	})
	if err != nil {
		return err
	}
	if task.AgentID != agentID || task.Type != model.TaskTypePreview {
		return model.ErrForbidden
	}
	if task.Status == model.TaskStatusSucceeded {
		if filepathSlash(previewDir) != bound.OutputDir {
			return model.ErrConflict
		}
		return nil
	}
	if bound.Invalidated || (task.Status != model.TaskStatusClaimed && task.Status != model.TaskStatusRunning) {
		return model.ErrConflict
	}
	if leaseExpired(task) {
		return model.ErrLeaseExpired
	}
	var frozen preview.Input
	if err = json.Unmarshal([]byte(bound.InputJSON), &frozen); err != nil {
		return err
	}
	sourcePkg, err := s.verifyPreviewInput(ctx, frozen)
	if err != nil {
		return err
	}
	previewRel := filepath.FromSlash(previewDir)
	previewAbs, err := s.previewPackageDir(previewRel)
	if err != nil {
		return fmt.Errorf("service: invalid preview dir %s: %w", previewDir, err)
	}

	// 1. Read and validate preview-manifest.json
	readOutput := func(name string) ([]byte, error) {
		path, _, e := s.previewPackageFile(previewAbs, name)
		if e != nil {
			return nil, e
		}
		return previewReadJSON(path)
	}
	manifestBytes, err := readOutput(preview.ManifestFilename)
	if err != nil {
		return fmt.Errorf("service: read %s: %w", preview.ManifestFilename, model.ErrArgument)
	}
	var manifest preview.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("service: parse %s: %w", preview.ManifestFilename, model.ErrArgument)
	}
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("service: unsupported manifest schema %d: %w", manifest.SchemaVersion, model.ErrArgument)
	}

	// 2. Read and validate preview-receipt.json
	receiptBytes, err := readOutput(preview.ReceiptFilename)
	if err != nil {
		return fmt.Errorf("service: read %s: %w", preview.ReceiptFilename, model.ErrArgument)
	}
	var receipt preview.Receipt
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		return fmt.Errorf("service: parse %s: %w", preview.ReceiptFilename, model.ErrArgument)
	}
	if receipt.SchemaVersion != 1 {
		return fmt.Errorf("service: unsupported receipt schema %d: %w", receipt.SchemaVersion, model.ErrArgument)
	}

	// 3. Read and validate render-report.json
	reportBytes, err := readOutput(preview.ReportFilename)
	if err != nil {
		return fmt.Errorf("service: read %s: %w", preview.ReportFilename, model.ErrArgument)
	}
	var report preview.Report
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		return fmt.Errorf("service: parse %s: %w", preview.ReportFilename, model.ErrArgument)
	}
	if report.SchemaVersion != 1 {
		return fmt.Errorf("service: unsupported report schema %d: %w", report.SchemaVersion, model.ErrArgument)
	}
	for _, chk := range report.Checks {
		if chk.Status != "passed" {
			return fmt.Errorf("service: render report check %s failed (%s): %w", chk.Code, chk.Message, model.ErrArgument)
		}
	}

	// 4. Verify physical files and hashes
	mp4Path, mp4Info, err := s.previewPackageFile(previewAbs, preview.MediaFilename)
	if err != nil || !mp4Info.Mode().IsRegular() || mp4Info.Size() == 0 {
		return fmt.Errorf("service: missing or empty %s: %w", preview.MediaFilename, model.ErrArgument)
	}
	mp4SHA, err := previewHashFile(ctx, mp4Path)
	if err != nil {
		return fmt.Errorf("service: read %s: %w", preview.MediaFilename, err)
	}
	repSHA := sha256Hex(reportBytes)
	recSHA := sha256Hex(receiptBytes)
	manSHA := sha256Hex(manifestBytes)

	// Validate manifest declares artifacts
	var foundMP4, foundReport, foundReceipt bool
	if len(manifest.Artifacts) != 3 {
		return model.ErrArgument
	}
	keys := map[string]bool{}
	for _, a := range manifest.Artifacts {
		if keys[a.Key] {
			return model.ErrArgument
		}
		keys[a.Key] = true
		switch a.Key {
		case "mp4":
			if a.Path != preview.MediaFilename || a.SHA256 != mp4SHA || a.Size != mp4Info.Size() {
				return fmt.Errorf("service: manifest artifact mp4 mismatch: %w", model.ErrArgument)
			}
			foundMP4 = true
		case "report":
			if a.Path != preview.ReportFilename || a.SHA256 != repSHA || a.Size != int64(len(reportBytes)) {
				return fmt.Errorf("service: manifest artifact report mismatch: %w", model.ErrArgument)
			}
			foundReport = true
		case "receipt":
			if a.Path != preview.ReceiptFilename || a.SHA256 != recSHA || a.Size != int64(len(receiptBytes)) {
				return fmt.Errorf("service: manifest artifact receipt mismatch: %w", model.ErrArgument)
			}
			foundReceipt = true
		}
	}
	if !foundMP4 || !foundReport || !foundReceipt {
		return fmt.Errorf("service: manifest missing required artifacts (mp4, report, receipt): %w", model.ErrArgument)
	}

	// 5. Probe final.mp4 with configured prober
	prober := s.MediaProber()
	probedMedia, err := prober.ProbeMedia(ctx, mp4Path)
	if err != nil {
		return fmt.Errorf("service: probe %s: %w", preview.MediaFilename, err)
	}
	if probedMedia.Width <= 0 || probedMedia.Height <= 0 || probedMedia.DurationSeconds <= 0 {
		return fmt.Errorf("service: probed media has invalid dimensions or duration: %w", model.ErrArgument)
	}
	if err = s.validatePreviewMedia(ctx, frozen, report, receipt, probedMedia, sourcePkg); err != nil {
		return err
	}
	if _, err = s.verifyPreviewInput(ctx, frozen); err != nil {
		return err
	}
	mediaJSON, err := json.Marshal(probedMedia)
	if err != nil {
		return fmt.Errorf("service: encode media json: %w", err)
	}

	// 6. Transactional update
	var savedTask model.Task
	var job model.PreviewJob
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		tk, err := guardTaskMutation(ctx, tx, agentID, taskID)
		if err != nil {
			return err
		}
		if _, err = authorizeExecution(ctx, tx, scope, opSubmit); err != nil {
			return err
		}
		if tk.Status == model.TaskStatusSucceeded {
			j, e := tx.GetPreviewJobByTaskID(ctx, taskID)
			if e != nil {
				return e
			}
			if j.OutputDir != filepathSlash(previewDir) {
				return model.ErrConflict
			}
			job = j
			savedTask = tk
			return nil
		}
		if tk.Type != model.TaskTypePreview {
			return fmt.Errorf("service: task %s is type %q, not preview: %w", taskID, tk.Type, model.ErrArgument)
		}
		if tk.Status != model.TaskStatusClaimed && tk.Status != model.TaskStatusRunning {
			return fmt.Errorf("service: task %s status %q is not active: %w", taskID, tk.Status, model.ErrInvalidState)
		}
		if leaseExpired(tk) {
			return fmt.Errorf("service: task %s: %w", taskID, model.ErrLeaseExpired)
		}
		if err := s.WorkflowBlocks(ctx, tx, taskID); err != nil {
			return err
		}
		asset, err := tx.GetAsset(ctx, tk.AssetID)
		if err != nil {
			return err
		}
		if !assetVisibleForRole(asset, tk.AgentRole) {
			return fmt.Errorf("service: asset %s not visible for %s: %w", asset.AssetID, tk.AgentRole, model.ErrForbidden)
		}

		j, err := tx.GetPreviewJobByTaskID(ctx, taskID)
		if err != nil {
			return err
		}
		if j.Invalidated {
			return fmt.Errorf("service: preview job %s is invalidated: %w", j.PreviewID, model.ErrConflict)
		}
		current, e := s.currentPreviewRevision(ctx, tx, j.RunID)
		if e != nil {
			return e
		}
		if current.RevisionID != j.RevisionID {
			return model.ErrConflict
		}
		if manifest.PreviewID != j.PreviewID || manifest.TaskID != j.TaskID || manifest.InputSHA256 != j.InputSHA256 || manifest.PresetSHA256 != j.PresetSHA256 {
			return fmt.Errorf("service: preview manifest identity mismatch: %w", model.ErrArgument)
		}
		if receipt.PreviewID != j.PreviewID || receipt.TaskID != j.TaskID || receipt.InputSHA256 != j.InputSHA256 || receipt.PresetSHA256 != j.PresetSHA256 {
			return fmt.Errorf("service: preview receipt identity mismatch: %w", model.ErrArgument)
		}
		if report.InputSHA256 != j.InputSHA256 {
			return fmt.Errorf("service: preview report input_sha256 mismatch: %w", model.ErrArgument)
		}

		tk.Status = model.TaskStatusSucceeded
		tk.Progress = 1.0
		tk.LeaseUntil = nil
		tk.Message = ""
		tk.UpdatedAt = time.Now().UTC()
		if err := tx.UpdateTask(ctx, tk); err != nil {
			return err
		}

		outDirSlash := filepathSlash(previewDir)
		if err := tx.UpdatePreviewJobSuccess(ctx, j.PreviewID, outDirSlash, manSHA, string(mediaJSON)); err != nil {
			return err
		}

		artifacts := []model.PreviewArtifactRecord{
			{PreviewID: j.PreviewID, Key: "mp4", Kind: "preview", Path: preview.MediaFilename, MIME: "video/mp4", Size: mp4Info.Size(), SHA256: mp4SHA},
			{PreviewID: j.PreviewID, Key: "report", Kind: "render_report", Path: preview.ReportFilename, MIME: "application/json", Size: int64(len(reportBytes)), SHA256: repSHA},
			{PreviewID: j.PreviewID, Key: "receipt", Kind: "receipt", Path: preview.ReceiptFilename, MIME: "application/json", Size: int64(len(receiptBytes)), SHA256: recSHA},
		}
		if err := tx.InsertPreviewArtifacts(ctx, artifacts); err != nil {
			return err
		}

		savedTask = tk
		job = j
		return auditTx(ctx, tx, "agent:"+agentID, "preview.submit", j.PreviewID, "task="+taskID)
	})
	if err != nil {
		return err
	}

	s.publish("task_updated", cloneTask(savedTask))
	latestExportRevID, _ := s.getLatestExportRevisionID(ctx, job.RunID)
	view, err := s.projectPreviewView(ctx, job, latestExportRevID)
	if err == nil {
		s.publish("preview_updated", view)
	}
	return nil
}

// Submit satisfies preview.Application.
func (s *Service) Submit(ctx context.Context, agentID, taskID, previewDir string) error {
	return s.SubmitPreview(ctx, agentID, taskID, previewDir)
}

// ResolvePreviewFile returns safe physical file details for controlled keys ("mp4", "report", "receipt").
func (s *Service) ResolvePreviewFile(ctx context.Context, previewID, key string) (preview.ResolvedFile, error) {
	if key != "mp4" && key != "report" && key != "receipt" {
		return preview.ResolvedFile{}, fmt.Errorf("service: controlled key must be mp4, report or receipt, got %q: %w", key, model.ErrArgument)
	}
	job, err := s.st.GetPreviewJob(ctx, previewID)
	if err != nil {
		return preview.ResolvedFile{}, err
	}
	if job.OutputDir == "" {
		return preview.ResolvedFile{}, fmt.Errorf("service: preview %s has not produced output: %w", previewID, model.ErrNotFound)
	}
	art, err := s.st.GetPreviewArtifact(ctx, previewID, key)
	if err != nil {
		return preview.ResolvedFile{}, err
	}

	pkg, err := s.previewPackageDir(job.OutputDir)
	if err != nil {
		return preview.ResolvedFile{}, err
	}
	absPath, info, err := s.previewPackageFile(pkg, art.Path)
	if err != nil {
		return preview.ResolvedFile{}, err
	}

	return preview.ResolvedFile{
		AbsolutePath: absPath,
		Name:         art.Path,
		MIME:         art.MIME,
		Size:         info.Size(),
		SHA256:       art.SHA256,
	}, nil
}

// ResolveFile satisfies preview.Application.
func (s *Service) ResolveFile(ctx context.Context, previewID, key string) (preview.ResolvedFile, error) {
	return s.ResolvePreviewFile(ctx, previewID, key)
}

func (s *Service) publishTaskPreview(ctx context.Context, taskID string) {
	job, err := s.st.GetPreviewJobByTaskID(ctx, taskID)
	if err != nil {
		return
	}
	latestExportRevID, _ := s.getLatestExportRevisionID(ctx, job.RunID)
	view, err := s.projectPreviewView(ctx, job, latestExportRevID)
	if err == nil {
		s.publish("preview_updated", view)
	}
}
