package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	dv "github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/deliveryvariant"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"path/filepath"
	"strings"
	"time"
)

func (s *Service) ReportDeliveryVariantCapability(ctx context.Context, agentID, role string, cap dv.Capability) error {
	if agentID == "" {
		return model.ErrArgument
	}
	if role != "exporter" {
		return model.ErrForbidden
	}
	raw, err := json.Marshal(cap)
	if err != nil {
		return err
	}
	err = s.st.PutDeliveryVariantCapability(ctx, agentID, cap.ProtocolVersion, cap.Ready && cap.ProtocolVersion == 1, string(raw), time.Now().UTC().Add(DefaultPreviewCapabilityExpiry))
	if err == nil {
		s.publish("capability.changed", map[string]any{"agent_id": agentID, "role": role})
	}
	return err
}
func variantCapability(raw string) bool { return variantCapabilityFor(raw, dv.SpecID) }
func variantCapabilityFor(raw string, id string) bool {
	var cap dv.Capability
	if json.Unmarshal([]byte(raw), &cap) != nil || !cap.Ready || cap.ProtocolVersion != 1 {
		return false
	}
	for _, spec := range cap.Specs {
		expected := dv.SpecSHA
		if id == dv.EditableSpecID {
			expected = dv.EditableSpecSHA
		}
		if spec.SpecID == id && spec.SHA256 == expected {
			return true
		}
	}
	return false
}
func (s *Service) hasVariantCapability(ctx context.Context, ids ...string) (bool, error) {
	id := dv.SpecID
	if len(ids) > 0 {
		id = ids[0]
	}
	caps, err := s.st.ListActiveDeliveryVariantCapabilities(ctx)
	if err != nil {
		return false, err
	}
	for _, cap := range caps {
		if cap.Ready && variantCapabilityFor(cap.CapabilityJSON, id) {
			return true, nil
		}
	}
	return false, nil
}
func (s *Service) deliveryVariantClaimable(ctx context.Context, tx *store.Store, agent string, task model.Task) (bool, error) {
	if task.Type != model.TaskTypeDeliveryVariant {
		return true, nil
	}
	job, err := tx.GetDeliveryVariantJobByTaskID(ctx, task.TaskID)
	if errors.Is(err, model.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cap, err := tx.GetActiveDeliveryVariantCapability(ctx, agent)
	if errors.Is(err, model.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var input dv.Input
	if json.Unmarshal([]byte(job.InputJSON), &input) != nil {
		return false, model.ErrArgument
	}
	return !job.Invalidated && cap.Ready && variantCapabilityFor(cap.CapabilityJSON, input.SpecID), nil
}
func (s *Service) variantView(ctx context.Context, st *store.Store, job model.DeliveryVariantJob, current string) (dv.View, error) {
	task, err := st.GetTask(ctx, job.TaskID)
	if err != nil {
		return dv.View{}, err
	}
	records, err := st.ListDeliveryVariantArtifacts(ctx, job.VariantID)
	if err != nil {
		return dv.View{}, err
	}
	artifacts := make([]dv.Artifact, 0, len(records))
	for _, a := range records {
		artifacts = append(artifacts, dv.Artifact{Key: a.Key, Kind: a.Kind, Path: a.Path, MIME: a.MIME, Size: a.Size, SHA256: a.SHA256})
	}
	var input dv.Input
	_ = json.Unmarshal([]byte(job.InputJSON), &input)
	return dv.View{StyleSHA256: input.StyleSHA256, VariantID: job.VariantID, TaskID: job.TaskID, AssetID: job.AssetID, RunID: job.RunID, RevisionID: job.RevisionID, SpecID: input.SpecID, Format: "ass", InputSHA256: job.InputSHA256, Status: task.Status, Progress: task.Progress, Message: task.Message, Invalidated: job.Invalidated, IsCurrent: !job.Invalidated && current != "" && job.RevisionID == current, Artifacts: artifacts}, nil
}
func (s *Service) variantInput(ctx context.Context, run model.WorkflowRun, rev model.Revision) (dv.Input, error) {
	pkg, err := s.previewPackageDir(rev.PackageRef)
	if err != nil {
		return dv.Input{}, err
	}
	files := map[string]dv.InputFile{}
	aliases := map[string]bool{}
	reserved := map[string]bool{"subtitles.ass": true, "ass-style.json": true, "variant-manifest.json": true, "variant-report.json": true, "variant-receipt.json": true}
	add := func(kind, path string) error {
		if f, exists := files[path]; exists {
			if f.Kind != kind {
				return model.ErrArgument
			}
			return nil
		}
		alias := strings.ToLower(path)
		if aliases[alias] || reserved[alias] || !preview.IsRelativePath(path) {
			return model.ErrArgument
		}
		aliases[alias] = true
		absolute, info, err := s.previewPackageFile(pkg, path)
		if err != nil || !info.Mode().IsRegular() {
			return model.ErrArgument
		}
		sha, err := previewHashFile(ctx, absolute)
		if err != nil {
			return err
		}
		files[path] = dv.InputFile{Kind: kind, Path: path, SHA256: sha, Size: info.Size()}
		return nil
	}
	if err = add("edl", "edl.json"); err != nil {
		return dv.Input{}, err
	}
	if err = add("delivery_manifest", "delivery-manifest.json"); err != nil {
		return dv.Input{}, err
	}
	manifestPath, _, err := s.previewPackageFile(pkg, "delivery-manifest.json")
	if err != nil {
		return dv.Input{}, err
	}
	raw, err := previewReadJSON(manifestPath)
	if err != nil {
		return dv.Input{}, err
	}
	var manifest deliveryManifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return dv.Input{}, model.ErrArgument
	}
	for _, a := range manifest.Artifacts {
		kind := a.Kind
		if kind == "subtitles" {
			kind = "srt"
		}
		if err = add(kind, a.Path); err != nil {
			return dv.Input{}, err
		}
	}
	var frozen []dv.InputFile
	count := 0
	for _, f := range files {
		frozen = append(frozen, f)
		if f.Kind == "srt" {
			count++
		}
	}
	if count != 1 {
		return dv.Input{}, fmt.Errorf("exactly one exported SRT required: %w", model.ErrInvalidState)
	}
	edlPath, _, _ := s.previewPackageFile(pkg, "edl.json")
	edlRaw, err := previewReadJSON(edlPath)
	if err != nil {
		return dv.Input{}, err
	}
	duration, err := dv.TimelineDuration(edlRaw)
	if err != nil {
		return dv.Input{}, model.ErrArgument
	}
	for _, f := range frozen {
		if f.Kind == "srt" {
			p, _, e := s.previewPackageFile(pkg, f.Path)
			if e != nil {
				return dv.Input{}, e
			}
			b, e := previewReadJSON(p)
			if e != nil {
				return dv.Input{}, e
			}
			if _, e = dv.ParseSourceSRT(b, duration); e != nil {
				return dv.Input{}, fmt.Errorf("invalid exported subtitles: %v: %w", e, model.ErrArgument)
			}
		}
	}
	input := dv.Input{SchemaVersion: 1, AssetID: run.AssetID, RunID: run.RunID, RevisionID: rev.RevisionID, ParentPackageRef: filepath.ToSlash(rev.PackageRef), EDLSHA256: files["edl.json"].SHA256, ParentManifestSHA256: files["delivery-manifest.json"].SHA256, Format: "ass", SpecID: dv.SpecID, SpecSHA256: dv.SpecSHA, Spec: dv.SpecBytes, SourceFiles: frozen}
	input.InputSHA256, err = dv.Fingerprint(input)
	return input, err
}
func (s *Service) verifyVariantInput(ctx context.Context, in dv.Input) (string, error) {
	sha, err := dv.Fingerprint(in)
	if err != nil || sha != in.InputSHA256 || !dv.ValidSpec(in.Spec, in.SpecSHA256) {
		return "", model.ErrArgument
	}
	return s.verifyPreviewInput(ctx, preview.Input{PackageRef: in.ParentPackageRef, Files: in.SourceFiles})
}
func (s *Service) StartDeliveryVariant(ctx context.Context, actor, runID string, req dv.StartRequest) (dv.View, bool, error) {
	if req.SpecID == dv.SpecID && (req.DraftVersion != 0 || req.StyleSHA256 != "") {
		return dv.View{}, false, model.ErrArgument
	}
	reqSpecSHA := dv.SpecSHA
	if req.SpecID == dv.EditableSpecID {
		reqSpecSHA = dv.EditableSpecSHA
	}

	if actor == "" || !validTaskID(runID) || req.RevisionID == "" || req.RequestID == "" || (req.SpecID != dv.SpecID && req.SpecID != dv.EditableSpecID) {
		return dv.View{}, false, model.ErrArgument
	}
	if old, e := s.st.GetDeliveryVariantJobByRequest(ctx, runID, req.RequestID); e == nil {
		if old.RevisionID != req.RevisionID || old.ExpectedVersion != req.ExpectedVersion || old.SpecSHA256 != reqSpecSHA || !variantRequestStyleMatches(old, req) {
			return dv.View{}, false, model.ErrConflict
		}
		current, _ := s.getLatestExportRevisionID(ctx, runID)
		view, e := s.variantView(ctx, s.st, old, current)
		return view, false, e
	} else if !errors.Is(e, model.ErrNotFound) {
		return dv.View{}, false, e
	}
	run, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return dv.View{}, false, err
	}
	if !preview.IsRelativePath(run.AssetID) || strings.Contains(run.AssetID, "/") {
		return dv.View{}, false, model.ErrArgument
	}
	if req.ExpectedVersion != run.Version {
		return dv.View{}, false, model.ErrConflict
	}
	rev, err := s.currentPreviewRevision(ctx, s.st, runID)
	if err != nil {
		return dv.View{}, false, err
	}
	if rev.RevisionID != req.RevisionID {
		return dv.View{}, false, model.ErrConflict
	}
	cap, err := s.hasVariantCapability(ctx, req.SpecID)
	if err != nil {
		return dv.View{}, false, err
	}
	if !cap {
		return dv.View{}, false, model.ErrInvalidState
	}
	input, err := s.variantInput(ctx, run, rev)
	if err != nil {
		return dv.View{}, false, err
	}
	if req.SpecID == dv.EditableSpecID {
		d, e := s.GetSubtitleStyle(ctx, runID)
		if e != nil {
			return dv.View{}, false, e
		}
		if d.DraftVersion < 1 || d.DraftVersion != req.DraftVersion || d.StyleSHA256 != req.StyleSHA256 {
			return dv.View{}, false, model.ErrConflict
		}
		input.SchemaVersion = 2
		input.SpecID = dv.EditableSpecID
		input.Spec = dv.EditableSpecBytes
		input.SpecSHA256 = dv.EditableSpecSHA
		input.StyleDocument = &d.Document
		input.StyleSHA256 = d.StyleSHA256
		input.InputSHA256, e = dv.Fingerprint(input)
		if e != nil {
			return dv.View{}, false, e
		}
	}
	input.VariantID = newID("dv")
	input.TaskID = newID("tkdv")
	raw, err := json.Marshal(input)
	if err != nil {
		return dv.View{}, false, err
	}
	now := time.Now().UTC()
	task := model.Task{TaskID: input.TaskID, AssetID: run.AssetID, Type: model.TaskTypeDeliveryVariant, AgentRole: "exporter", Status: model.TaskStatusQueued, UpdatedAt: now}
	job := model.DeliveryVariantJob{VariantID: input.VariantID, TaskID: task.TaskID, AssetID: run.AssetID, RunID: runID, RevisionID: rev.RevisionID, RequestID: req.RequestID, ExpectedVersion: req.ExpectedVersion, InputJSON: string(raw), InputSHA256: input.InputSHA256, SpecSHA256: input.SpecSHA256, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	var view dv.View
	created := false
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		if old, e := tx.GetDeliveryVariantJobByRequest(ctx, runID, req.RequestID); e == nil {
			if old.RevisionID != req.RevisionID || old.ExpectedVersion != req.ExpectedVersion || old.SpecSHA256 != reqSpecSHA || !variantRequestStyleMatches(old, req) {
				return model.ErrConflict
			}
			current, _ := s.previewRevisionID(ctx, tx, runID)
			view, e = s.variantView(ctx, tx, old, current)
			return e
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
		if req.SpecID == dv.EditableSpecID {
			v, raw, e := tx.GetSubtitleStyle(ctx, runID, rev.RevisionID)
			if e != nil {
				return e
			}
			var doc dv.StyleDocument
			if v != req.DraftVersion || json.Unmarshal([]byte(raw), &doc) != nil || dv.StyleHash(doc) != req.StyleSHA256 {
				return model.ErrConflict
			}
		}
		if _, e = s.verifyVariantInput(ctx, input); e != nil {
			return e
		}
		if old, e := tx.FindReusableDeliveryVariantJob(ctx, runID, rev.RevisionID, input.InputSHA256); e == nil {
			if e = tx.BindDeliveryVariantRequest(ctx, runID, req.RequestID, old.VariantID, rev.RevisionID, req.ExpectedVersion, reqSpecSHA); e != nil {
				return e
			}
			view, e = s.variantView(ctx, tx, old, current.RevisionID)
			return e
		} else if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		if e = tx.CreateTask(ctx, task); e != nil {
			return e
		}
		if e = tx.InsertDeliveryVariantJob(ctx, job); e != nil {
			return e
		}
		if e = auditTx(ctx, tx, actor, "delivery_variant.start", job.VariantID, task.TaskID); e != nil {
			return e
		}
		view, e = s.variantView(ctx, tx, job, current.RevisionID)
		created = true
		return e
	})
	if err != nil {
		return dv.View{}, false, err
	}
	if created {
		s.publish("task_created", cloneTask(task))
		s.publish("delivery_variant.changed", view)
	}
	return view, created, nil
}
func (s *Service) ListDeliveryVariants(ctx context.Context, runID string) (dv.List, error) {
	run, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return dv.List{}, err
	}
	current, _ := s.getLatestExportRevisionID(ctx, runID)
	cap, err := s.hasVariantCapability(ctx)
	if err != nil {
		return dv.List{}, err
	}
	checks := []dv.Check{}
	ready := current != "" && cap
	if current == "" {
		checks = append(checks, dv.Check{Code: "export_revision", Status: "blocked", Message: "需要当前已完成的导出版本"})
	}
	if !cap {
		checks = append(checks, dv.Check{Code: "worker_capability", Status: "blocked", Message: "等待支持 ASS 的导出 Worker"})
	}
	if current != "" {
		rev, e := s.st.GetRevision(ctx, current)
		if e != nil {
			return dv.List{}, e
		}
		if e = s.variantSubtitleReady(rev); e != nil {
			ready = false
			checks = append(checks, dv.Check{Code: "subtitles", Status: "blocked", Message: e.Error()})
		}
	}
	jobs, err := s.st.ListDeliveryVariantJobs(ctx, runID)
	if err != nil {
		return dv.List{}, err
	}
	views := make([]dv.View, 0, len(jobs))
	for _, job := range jobs {
		v, e := s.variantView(ctx, s.st, job, current)
		if e != nil {
			return dv.List{}, e
		}
		views = append(views, v)
	}
	editable, e := s.hasVariantCapability(ctx, dv.EditableSpecID)
	if e != nil {
		return dv.List{}, e
	}
	editableReady := current != "" && editable
	if editableReady {
		rev, e := s.st.GetRevision(ctx, current)
		if e != nil {
			return dv.List{}, e
		}
		if e = s.variantSubtitleReady(rev, true); e != nil {
			editableReady = false
		}
	}
	return dv.List{CanStartEditable: editableReady, RunID: runID, RevisionID: current, ExpectedVersion: run.Version, CanStart: ready, Checks: checks, Specs: []dv.SpecRef{{SpecID: dv.SpecID, SHA256: dv.SpecSHA}, {SpecID: dv.EditableSpecID, SHA256: dv.EditableSpecSHA}}, Variants: views}, nil
}

// Polling readiness reads only bounded metadata and subtitles; media is hashed at start/submit.
func (s *Service) variantSubtitleReady(rev model.Revision, editable ...bool) error {
	pkg, err := s.previewPackageDir(rev.PackageRef)
	if err != nil {
		return err
	}
	read := func(name string) ([]byte, error) {
		p, _, e := s.previewPackageFile(pkg, name)
		if e != nil {
			return nil, e
		}
		return previewReadJSON(p)
	}
	raw, err := read("delivery-manifest.json")
	if err != nil {
		return err
	}
	var manifest deliveryManifest
	if json.Unmarshal(raw, &manifest) != nil {
		return model.ErrArgument
	}
	srtPath := ""
	for _, a := range manifest.Artifacts {
		if a.Kind == "srt" || a.Kind == "subtitles" {
			if srtPath != "" && srtPath != a.Path {
				return model.ErrArgument
			}
			srtPath = a.Path
		}
	}
	if srtPath == "" {
		return fmt.Errorf("当前导出包缺少 SRT 字幕: %w", model.ErrInvalidState)
	}
	edl, err := read("edl.json")
	if err != nil {
		return err
	}
	duration, err := dv.TimelineDuration(edl)
	if err != nil {
		return err
	}
	srt, err := read(srtPath)
	if err != nil {
		return err
	}
	if len(editable) > 0 && editable[0] {
		cues, e := dv.ParseSourceSRT(srt, duration)
		if e != nil {
			return e
		}
		if len(cues) == 0 {
			return fmt.Errorf("当前导出没有字幕: %w", model.ErrInvalidState)
		}
		return nil
	}
	_, err = dv.ParseSRT(srt, duration)
	return err
}
func (s *Service) GetDeliveryVariant(ctx context.Context, id string) (dv.View, error) {
	job, err := s.st.GetDeliveryVariantJob(ctx, id)
	if err != nil {
		return dv.View{}, err
	}
	current, _ := s.getLatestExportRevisionID(ctx, job.RunID)
	return s.variantView(ctx, s.st, job, current)
}
func (s *Service) CancelDeliveryVariant(ctx context.Context, actor, id string) (dv.View, error) {
	if actor == "" {
		return dv.View{}, model.ErrArgument
	}
	job, err := s.st.GetDeliveryVariantJob(ctx, id)
	if err != nil {
		return dv.View{}, err
	}
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		t, e := tx.GetTask(ctx, job.TaskID)
		if e != nil {
			return e
		}
		if t.Status == model.TaskStatusSucceeded || t.Status == model.TaskStatusFailed || t.Status == model.TaskStatusCancelled {
			return nil
		}
		t.Status = model.TaskStatusCancelled
		t.LeaseUntil = nil
		t.Message = "ASS 生成已取消"
		t.UpdatedAt = time.Now().UTC()
		if e = tx.UpdateTask(ctx, t); e != nil {
			return e
		}
		return auditTx(ctx, tx, actor, "delivery_variant.cancel", id, t.TaskID)
	})
	if err != nil {
		return dv.View{}, err
	}
	s.publishTaskDeliveryVariant(ctx, job.TaskID)
	return s.GetDeliveryVariant(ctx, id)
}
func (s *Service) publishTaskDeliveryVariant(ctx context.Context, taskID string) {
	if job, err := s.st.GetDeliveryVariantJobByTaskID(ctx, taskID); err == nil {
		if view, e := s.GetDeliveryVariant(ctx, job.VariantID); e == nil {
			s.publish("delivery_variant.changed", view)
		}
	}
}
func (s *Service) invalidateDeliveryVariants(ctx context.Context, tx *store.Store, runID string) error {
	jobs, err := tx.ListDeliveryVariantJobs(ctx, runID)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if err = tx.InvalidateDeliveryVariantJob(ctx, job.VariantID); err != nil {
			return err
		}
		task, e := tx.GetTask(ctx, job.TaskID)
		if e != nil {
			return e
		}
		if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusClaimed || task.Status == model.TaskStatusRunning {
			task.Status = model.TaskStatusCancelled
			task.LeaseUntil = nil
			task.Message = "导出版本失效，附加格式任务已取消"
			task.UpdatedAt = time.Now().UTC()
			if e = tx.UpdateTask(ctx, task); e != nil {
				return e
			}
		}
	}
	return nil
}

func variantRequestStyleMatches(job model.DeliveryVariantJob, req dv.StartRequest) bool {
	var in dv.Input
	if json.Unmarshal([]byte(job.InputJSON), &in) != nil {
		return false
	}
	return in.StyleSHA256 == req.StyleSHA256
}
