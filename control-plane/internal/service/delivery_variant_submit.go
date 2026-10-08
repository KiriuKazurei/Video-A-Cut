package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	dv "github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/deliveryvariant"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"path/filepath"
	"strings"
	"time"
)

func (s *Service) SubmitDeliveryVariant(ctx context.Context, agentID, taskID, outputDir string) error {
	job, err := s.st.GetDeliveryVariantJobByTaskID(ctx, taskID)
	if err != nil {
		return err
	}
	task, err := s.st.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.AgentID != agentID || task.Type != model.TaskTypeDeliveryVariant {
		return model.ErrForbidden
	}
	expected := strings.Join([]string{"delivery-variants", job.AssetID, job.VariantID}, "/")
	if outputDir != expected {
		return model.ErrArgument
	}
	if task.Status == model.TaskStatusSucceeded {
		if job.OutputDir != outputDir {
			return model.ErrConflict
		}
		return nil
	}
	if job.Invalidated || task.Status != model.TaskStatusClaimed && task.Status != model.TaskStatusRunning {
		return model.ErrConflict
	}
	if leaseExpired(task) {
		return model.ErrLeaseExpired
	}
	var input dv.Input
	if err = json.Unmarshal([]byte(job.InputJSON), &input); err != nil {
		return err
	}
	parent, err := s.verifyVariantInput(ctx, input)
	if err != nil {
		return err
	}
	output, err := s.previewPackageDir(outputDir)
	if err != nil {
		return err
	}
	read := func(name string) ([]byte, error) {
		p, _, e := s.previewPackageFile(output, name)
		if e != nil {
			return nil, e
		}
		return previewReadJSON(p)
	}
	manifestRaw, err := read("variant-manifest.json")
	if err != nil {
		return err
	}
	var manifest dv.Manifest
	if json.Unmarshal(manifestRaw, &manifest) != nil {
		return model.ErrArgument
	}
	receiptRaw, err := read("variant-receipt.json")
	if err != nil {
		return err
	}
	var receipt dv.Receipt
	if json.Unmarshal(receiptRaw, &receipt) != nil {
		return model.ErrArgument
	}
	reportRaw, err := read("variant-report.json")
	if err != nil {
		return err
	}
	var report dv.Report
	if json.Unmarshal(reportRaw, &report) != nil {
		return model.ErrArgument
	}
	wanted := dv.Binding{SchemaVersion: input.SchemaVersion, VariantID: job.VariantID, TaskID: job.TaskID, AssetID: job.AssetID, RunID: job.RunID, RevisionID: job.RevisionID, InputSHA256: job.InputSHA256, SpecSHA256: job.SpecSHA256}
	if manifest.Binding != wanted || receipt.Binding != wanted || report.Binding != wanted || receipt.ConverterVersion != "vac."+input.SpecID+"/1" {
		return model.ErrArgument
	}
	checks := map[string]bool{"source_fingerprint": false, "subtitle_text": false, "subtitle_timing": false, "ass_style": false, "self_contained": false}
	for _, c := range report.Checks {
		seen, ok := checks[c.Code]
		if !ok || seen || c.Status != "passed" {
			return model.ErrArgument
		}
		checks[c.Code] = true
	}
	for _, seen := range checks {
		if !seen {
			return model.ErrArgument
		}
	}
	sourceMap := map[string]dv.InputFile{}
	for _, f := range input.SourceFiles {
		sourceMap[f.Path] = f
	}
	if len(manifest.Artifacts) != len(sourceMap)+4 {
		return model.ErrArgument
	}
	keys, paths := map[string]bool{}, map[string]bool{}
	records := []model.DeliveryVariantArtifactRecord{}
	for _, a := range manifest.Artifacts {
		alias := strings.ToLower(a.Path)
		if !validTaskID(a.Key) || keys[a.Key] || paths[alias] || a.Size < 0 {
			return model.ErrArgument
		}
		keys[a.Key] = true
		paths[alias] = true
		p, info, e := s.previewPackageFile(output, a.Path)
		if e != nil || !info.Mode().IsRegular() || info.Size() != a.Size {
			return model.ErrArgument
		}
		sha, e := previewHashFile(ctx, p)
		if e != nil {
			return e
		}
		if sha != a.SHA256 {
			return model.ErrArgument
		}
		if f, ok := sourceMap[a.Path]; ok {
			if f.Kind != a.Kind || f.SHA256 != sha || f.Size != a.Size {
				return model.ErrArgument
			}
			if f.Kind == "srt" && a.Key != "srt" {
				return model.ErrArgument
			}
		} else {
			expectedFiles := map[string][3]string{"ass": {"subtitles.ass", "ass", "text/x-ssa; charset=utf-8"}, "style": {"ass-style.json", "ass_style", "application/json"}, "report": {"variant-report.json", "variant_report", "application/json"}, "receipt": {"variant-receipt.json", "variant_receipt", "application/json"}}
			f, ok := expectedFiles[a.Key]
			if !ok || a.Path != f[0] || a.Kind != f[1] || a.MIME != f[2] {
				return model.ErrArgument
			}
		}
		records = append(records, model.DeliveryVariantArtifactRecord{VariantID: job.VariantID, Key: a.Key, Kind: a.Kind, Path: a.Path, MIME: a.MIME, Size: a.Size, SHA256: sha})
	}
	for _, key := range []string{"ass", "srt", "style", "report", "receipt"} {
		if !keys[key] {
			return model.ErrArgument
		}
	}
	for path := range sourceMap {
		if !paths[strings.ToLower(path)] {
			return model.ErrArgument
		}
	}
	style, err := read("ass-style.json")
	if err != nil {
		return err
	}
	wantedStyle := dv.SpecBytes
	if input.StyleDocument != nil {
		wantedStyle = dv.StyleDocumentBytes(*input.StyleDocument)
	}
	if !bytes.Equal(style, wantedStyle) {
		return model.ErrArgument
	}
	edlPath, _, err := s.previewPackageFile(parent, "edl.json")
	if err != nil {
		return err
	}
	edl, err := previewReadJSON(edlPath)
	if err != nil {
		return err
	}
	duration, err := dv.TimelineDuration(edl)
	if err != nil {
		return model.ErrArgument
	}
	var cues []dv.Cue
	for _, f := range input.SourceFiles {
		if f.Kind == "srt" {
			p, _, e := s.previewPackageFile(parent, f.Path)
			if e != nil {
				return e
			}
			raw, e := previewReadJSON(p)
			if e != nil {
				return e
			}
			cues, e = dv.ParseSourceSRT(raw, duration)
			if e != nil {
				return model.ErrArgument
			}
		}
	}
	ass, err := read("subtitles.ass")
	if err != nil {
		return err
	}
	expectedASS := dv.ASS(cues)
	if input.StyleDocument != nil {
		if e := dv.ValidateStyleDocument(*input.StyleDocument, cues, duration); e != nil {
			return model.ErrArgument
		}
		expectedASS = dv.StyledASS(cues, *input.StyleDocument)
	}
	if string(ass) != expectedASS || report.SubtitleCount != len(cues) || report.DurationCentiseconds != (duration+5)/10 {
		return fmt.Errorf("ASS differs from approved SRT/style: %w", model.ErrArgument)
	}
	if _, err = s.verifyVariantInput(ctx, input); err != nil {
		return err
	}
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		currentTask, e := guardTaskMutation(ctx, tx, agentID, taskID)
		if e != nil {
			return e
		}
		if currentTask.Status != model.TaskStatusClaimed && currentTask.Status != model.TaskStatusRunning {
			return model.ErrConflict
		}
		if leaseExpired(currentTask) {
			return model.ErrLeaseExpired
		}
		if e = s.WorkflowBlocks(ctx, tx, taskID); e != nil {
			return e
		}
		bound, e := tx.GetDeliveryVariantJobByTaskID(ctx, taskID)
		if e != nil {
			return e
		}
		current, e := s.currentPreviewRevision(ctx, tx, bound.RunID)
		if e != nil {
			return e
		}
		if bound.Invalidated || current.RevisionID != bound.RevisionID {
			return model.ErrConflict
		}
		if _, e = s.verifyVariantInput(ctx, input); e != nil {
			return e
		}
		currentTask.Status = model.TaskStatusSucceeded
		currentTask.Progress = 1
		currentTask.Message = ""
		currentTask.LeaseUntil = nil
		currentTask.UpdatedAt = time.Now().UTC()
		if e = tx.UpdateTask(ctx, currentTask); e != nil {
			return e
		}
		if e = tx.UpdateDeliveryVariantJobSuccess(ctx, bound.VariantID, outputDir, sha256Hex(manifestRaw), string(reportRaw)); e != nil {
			return e
		}
		if e = tx.InsertDeliveryVariantArtifacts(ctx, records); e != nil {
			return e
		}
		return auditTx(ctx, tx, "agent:"+agentID, "delivery_variant.submit", bound.VariantID, taskID)
	})
	if err == nil {
		s.publishTaskDeliveryVariant(ctx, taskID)
		if saved, e := s.st.GetTask(ctx, taskID); e == nil {
			s.publish("task_updated", cloneTask(saved))
		}
	}
	return err
}
func (s *Service) ResolveDeliveryVariantFile(ctx context.Context, id, key string) (dv.ResolvedFile, error) {
	job, err := s.st.GetDeliveryVariantJob(ctx, id)
	if err != nil {
		return dv.ResolvedFile{}, err
	}
	asset, err := s.st.GetAsset(ctx, job.AssetID)
	if err != nil {
		return dv.ResolvedFile{}, err
	}
	if asset.Locked || !assetVisibleForRole(asset, "exporter") {
		return dv.ResolvedFile{}, model.ErrForbidden
	}
	task, err := s.st.GetTask(ctx, job.TaskID)
	if err != nil {
		return dv.ResolvedFile{}, err
	}
	if task.Status != model.TaskStatusSucceeded || job.OutputDir == "" {
		return dv.ResolvedFile{}, model.ErrNotFound
	}
	a, err := s.st.GetDeliveryVariantArtifact(ctx, id, key)
	if err != nil {
		return dv.ResolvedFile{}, err
	}
	pkg, err := s.previewPackageDir(job.OutputDir)
	if err != nil {
		return dv.ResolvedFile{}, err
	}
	absolute, info, err := s.previewPackageFile(pkg, a.Path)
	if err != nil {
		return dv.ResolvedFile{}, err
	}
	sha, err := previewHashFile(ctx, absolute)
	if err != nil {
		return dv.ResolvedFile{}, err
	}
	if info.Size() != a.Size || sha != a.SHA256 {
		return dv.ResolvedFile{}, model.ErrConflict
	}
	return dv.ResolvedFile{AbsolutePath: absolute, Name: a.Path, MIME: a.MIME, SHA256: a.SHA256, Size: a.Size}, nil
}
func (s *Service) DeliveryVariantZIPFiles(ctx context.Context, id string) ([]dv.ResolvedFile, error) {
	job, err := s.st.GetDeliveryVariantJob(ctx, id)
	if err != nil {
		return nil, err
	}
	records, err := s.st.ListDeliveryVariantArtifacts(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, model.ErrNotFound
	}
	files := make([]dv.ResolvedFile, 0, len(records)+1)
	for _, a := range records {
		file, e := s.ResolveDeliveryVariantFile(ctx, id, a.Key)
		if e != nil {
			return nil, e
		}
		files = append(files, file)
	}
	pkg, err := s.previewPackageDir(job.OutputDir)
	if err != nil {
		return nil, err
	}
	p, info, err := s.previewPackageFile(pkg, "variant-manifest.json")
	if err != nil {
		return nil, err
	}
	sha, err := previewHashFile(ctx, p)
	if err != nil {
		return nil, err
	}
	if sha != job.ManifestSHA256 {
		return nil, model.ErrConflict
	}
	return append(files, dv.ResolvedFile{AbsolutePath: p, Name: filepath.Base(p), MIME: "application/json", Size: info.Size(), SHA256: sha}), nil
}
