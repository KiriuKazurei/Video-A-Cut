package service

import (
	"context"
	"encoding/json"
	"fmt"
	dv "github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/deliveryvariant"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"reflect"
	"slices"
	"strings"
)

func (s *Service) freezeSubtitleVariant(ctx context.Context, id, run, rev string) (*preview.SubtitleVariantInput, error) {
	sub, in, e := s.subtitleVariantSnapshot(ctx, s.st, id, run, rev)
	if e != nil {
		return nil, e
	}
	if _, e = s.verifyVariantInput(ctx, in); e != nil {
		return nil, e
	}
	if e = s.verifySubtitleFile(ctx, sub); e != nil {
		return nil, e
	}
	return sub, nil
}

// subtitleVariantSnapshot reads only database state and may use a write transaction.
func (s *Service) subtitleVariantSnapshot(ctx context.Context, st *store.Store, id, run, rev string) (*preview.SubtitleVariantInput, dv.Input, error) {
	sub, e := s.subtitleVariantBinding(ctx, st, id, run, rev)
	if e != nil {
		return nil, dv.Input{}, e
	}
	job, e := st.GetDeliveryVariantJob(ctx, id)
	if e != nil {
		return nil, dv.Input{}, e
	}
	var in dv.Input
	e = json.Unmarshal([]byte(job.InputJSON), &in)
	return sub, in, e
}

func (s *Service) subtitleVariantBinding(ctx context.Context, st *store.Store, id, run, rev string) (*preview.SubtitleVariantInput, error) {
	job, e := st.GetDeliveryVariantJob(ctx, id)
	if e != nil {
		return nil, e
	}
	task, e := st.GetTask(ctx, job.TaskID)
	if e != nil {
		return nil, e
	}
	if job.RunID != run || job.RevisionID != rev || job.Invalidated || task.Status != model.TaskStatusSucceeded || job.OutputDir == "" {
		return nil, fmt.Errorf("ASS 必须属于当前导出版本且已完成: %w", model.ErrConflict)
	}
	var in dv.Input
	if e = json.Unmarshal([]byte(job.InputJSON), &in); e != nil {
		return nil, e
	}
	files, e := st.ListDeliveryVariantArtifacts(ctx, id)
	if e != nil {
		return nil, e
	}
	var ass preview.InputFile
	for _, f := range files {
		if f.Key == "ass" {
			ass = preview.InputFile{Kind: "ass", Path: f.Path, SHA256: f.SHA256, Size: f.Size}
		}
	}
	if ass.Path == "" {
		return nil, model.ErrInvalidState
	}
	families := []string{"Microsoft YaHei"}
	styleSHA := in.SpecSHA256
	if in.StyleDocument != nil {
		styleSHA = in.StyleSHA256
		familySet := map[string]bool{in.StyleDocument.Style.FontFamily: true}
		for _, o := range in.StyleDocument.Overrides {
			st, e := dv.ResolveStyle(in.StyleDocument.Style, o.Style)
			if e != nil {
				return nil, e
			}
			familySet[st.FontFamily] = true
		}
		families = []string{}
		for f := range familySet {
			families = append(families, f)
		}
		slices.Sort(families)
	}
	sub := &preview.SubtitleVariantInput{VariantID: id, PackageRef: job.OutputDir, ASS: ass, StyleSHA256: styleSHA, FontFamilies: families}
	return sub, nil
}

func (s *Service) verifySubtitleBindingAt(ctx context.Context, st *store.Store, in preview.Input) error {
	if in.SubtitleVariant == nil {
		return nil
	}
	sub, e := s.subtitleVariantBinding(ctx, st, in.SubtitleVariant.VariantID, in.RunID, in.RevisionID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(sub, in.SubtitleVariant) {
		return model.ErrConflict
	}
	return nil
}
func (s *Service) verifySubtitleFile(ctx context.Context, sub *preview.SubtitleVariantInput) error {
	pkg, e := s.previewPackageDir(sub.PackageRef)
	if e != nil {
		return e
	}
	path, info, e := s.previewPackageFile(pkg, sub.ASS.Path)
	if e != nil {
		return e
	}
	hash, e := previewHashFile(ctx, path)
	if e != nil {
		return e
	}
	if info.Size() != sub.ASS.Size || hash != sub.ASS.SHA256 {
		return fmt.Errorf("ASS 文件已改变: %w", model.ErrConflict)
	}
	return nil
}
func (s *Service) verifySubtitleBinding(ctx context.Context, in preview.Input) error {
	if in.SubtitleVariant == nil {
		return nil
	}
	sub, e := s.freezeSubtitleVariant(ctx, in.SubtitleVariant.VariantID, in.RunID, in.RevisionID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(sub, in.SubtitleVariant) {
		return model.ErrConflict
	}
	return nil
}
func previewRequestSubtitleMatches(job model.PreviewJob, req preview.StartRequest) bool {
	var in preview.Input
	if json.Unmarshal([]byte(job.InputJSON), &in) != nil {
		return false
	}
	id := ""
	if in.SubtitleVariant != nil {
		id = in.SubtitleVariant.VariantID
	}
	return id == req.SubtitleVariantID
}
func (s *Service) assPreviewWorkerReady(ctx context.Context, tx *store.Store, agent string, families []string) (bool, error) {
	cap, e := tx.GetActivePreviewCapability(ctx, agent)
	if e != nil {
		return false, e
	}
	var parsed preview.Capability
	if !cap.Ready || json.Unmarshal([]byte(cap.CapabilityJSON), &parsed) != nil || !parsed.Ready || parsed.ExecutionProtocol != PreviewExecutionProtocol || !slices.Contains(parsed.SubtitleFormats, "ass") {
		return false, nil
	}
	presetReady := false
	for _, p := range parsed.Presets {
		if p.PresetID == preview.PresetID && p.SHA256 == preview.DefaultPresetSHA256() {
			presetReady = true
		}
	}
	if !presetReady {
		return false, nil
	}
	vc, e := tx.GetActiveDeliveryVariantCapability(ctx, agent)
	if e != nil {
		return false, e
	}
	var fonts dv.Capability
	if !vc.Ready || json.Unmarshal([]byte(vc.CapabilityJSON), &fonts) != nil {
		return false, nil
	}
	available := map[string]bool{}
	for _, f := range fonts.Fonts {
		available[f.Family] = true
	}
	for _, f := range families {
		if !available[f] {
			return false, nil
		}
	}
	return true, nil
}
func (s *Service) checkASSPreviewCapability(ctx context.Context, families []string) (bool, error) {
	caps, e := s.st.ListActivePreviewCapabilities(ctx)
	if e != nil {
		return false, e
	}
	for _, c := range caps {
		ready, e := s.assPreviewWorkerReady(ctx, s.st, c.AgentID, families)
		if e == nil && ready {
			return true, nil
		}
	}
	return false, nil
}

func validFontReceipt(fonts []preview.Font, families []string) bool {
	seen := map[string]bool{}
	for _, f := range fonts {
		if !slices.Contains(families, f.Family) || len(f.SHA256) != 64 || f.SHA256 != strings.ToLower(f.SHA256) {
			return false
		}
		for _, r := range f.SHA256 {
			if !strings.ContainsRune("0123456789abcdef", r) {
				return false
			}
		}
		seen[f.Family] = true
	}
	for _, f := range families {
		if !seen[f] {
			return false
		}
	}
	return true
}
