package service

import (
	"context"
	"encoding/json"
	"fmt"
	dv "github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/deliveryvariant"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"math"
	"slices"
	"strings"
)

func (s *Service) subtitleSource(ctx context.Context, runID string) (dv.SubtitleStyleDraft, error) {
	run, e := s.st.GetWorkflow(ctx, runID)
	if e != nil {
		return dv.SubtitleStyleDraft{}, e
	}
	asset, e := s.st.GetAsset(ctx, run.AssetID)
	if e != nil {
		return dv.SubtitleStyleDraft{}, e
	}
	if asset.Locked {
		return dv.SubtitleStyleDraft{}, model.ErrForbidden
	}
	rev, e := s.currentPreviewRevision(ctx, s.st, runID)
	if e != nil {
		return dv.SubtitleStyleDraft{}, e
	}
	pkg, e := s.previewPackageDir(rev.PackageRef)
	if e != nil {
		return dv.SubtitleStyleDraft{}, e
	}
	read := func(rel string) ([]byte, error) {
		p, _, e := s.previewPackageFile(pkg, rel)
		if e != nil {
			return nil, e
		}
		return previewReadJSON(p)
	}
	edl, e := read("edl.json")
	if e != nil {
		return dv.SubtitleStyleDraft{}, e
	}
	duration, e := dv.TimelineDuration(edl)
	if e != nil {
		return dv.SubtitleStyleDraft{}, fmt.Errorf("invalid timeline: %w", model.ErrArgument)
	}
	manifest, e := read("delivery-manifest.json")
	if e != nil {
		return dv.SubtitleStyleDraft{}, e
	}
	var m deliveryManifest
	if e = json.Unmarshal(manifest, &m); e != nil {
		return dv.SubtitleStyleDraft{}, e
	}
	srt := ""
	for _, a := range m.Artifacts {
		kind, path := a.Kind, a.Path
		if kind == "srt" || kind == "subtitles" {
			if srt != "" {
				return dv.SubtitleStyleDraft{}, model.ErrArgument
			}
			srt = path
		}
	}
	if srt == "" {
		return dv.SubtitleStyleDraft{}, fmt.Errorf("当前导出缺少 SRT 字幕: %w", model.ErrInvalidState)
	}
	raw, e := read(srt)
	if e != nil {
		return dv.SubtitleStyleDraft{}, e
	}
	cues, e := dv.ParseSourceSRT(raw, duration)
	if e != nil {
		return dv.SubtitleStyleDraft{}, fmt.Errorf("字幕无效: %v: %w", e, model.ErrArgument)
	}
	if len(cues) == 0 {
		return dv.SubtitleStyleDraft{}, fmt.Errorf("当前导出没有字幕: %w", model.ErrInvalidState)
	}
	var timeline struct {
		Timeline struct {
			FPS float64 `json:"fps"`
		} `json:"timeline"`
		Video []struct {
			Src        string  `json:"src"`
			TimelineIn float64 `json:"timeline_in"`
		} `json:"video"`
	}
	_ = json.Unmarshal(edl, &timeline)
	if len(timeline.Video) == 0 {
		return dv.SubtitleStyleDraft{}, model.ErrArgument
	}
	slices.SortStableFunc(timeline.Video, func(a, b struct {
		Src        string  `json:"src"`
		TimelineIn float64 `json:"timeline_in"`
	}) int {
		aa, bb := math.Round(a.TimelineIn*timeline.Timeline.FPS), math.Round(b.TimelineIn*timeline.Timeline.FPS)
		if aa < bb {
			return -1
		}
		if aa > bb {
			return 1
		}
		return 0
	})
	mediaPath, _, e := s.previewPackageFile(pkg, timeline.Video[0].Src)
	if e != nil {
		return dv.SubtitleStyleDraft{}, e
	}
	media, e := s.MediaProber().ProbeMedia(ctx, mediaPath)
	if e != nil {
		return dv.SubtitleStyleDraft{}, fmt.Errorf("无法读取字幕画布: %v: %w", e, model.ErrInvalidState)
	}
	if media.Width < 2 || media.Height < 2 {
		return dv.SubtitleStyleDraft{}, model.ErrArgument
	}
	w, h := float64(media.Width), float64(media.Height)
	scale := math.Min(1, math.Min(1280/w, 720/h))
	cw, ch := int(math.Round(w*scale))/2*2, int(math.Round(h*scale))/2*2
	if cw < 2 || ch < 2 {
		return dv.SubtitleStyleDraft{}, model.ErrArgument
	}
	x := int(math.Round(720 * float64(cw) / float64(ch)))
	if x < 180 || x > 10000 {
		return dv.SubtitleStyleDraft{}, fmt.Errorf("当前画布宽高比超出基础字幕编辑范围: %w", model.ErrArgument)
	}
	out := dv.SubtitleStyleDraft{AssetID: run.AssetID, RunID: runID, RevisionID: rev.RevisionID, DurationMS: duration, Cues: []dv.SourceCue{}, Document: dv.StyleDocument{SourceSRTSHA256: sha256Hex(raw), PlayResX: x, PlayResY: 720, Style: dv.DefaultStyle(), Overrides: []dv.SubtitleCueOverride{}}}
	for i, c := range cues {
		out.Cues = append(out.Cues, dv.SourceCue{CueID: i + 1, StartMS: c.Start * 10, EndMS: c.End * 10, Text: c.Text})
	}
	return out, nil
}
func draftCues(d dv.SubtitleStyleDraft) []dv.Cue {
	out := make([]dv.Cue, len(d.Cues))
	for i, c := range d.Cues {
		out[i] = dv.Cue{Start: c.StartMS / 10, End: c.EndMS / 10, Text: c.Text}
	}
	return out
}
func (s *Service) GetSubtitleStyle(ctx context.Context, runID string) (dv.SubtitleStyleDraft, error) {
	d, e := s.subtitleSource(ctx, runID)
	if e != nil {
		return d, e
	}
	v, raw, e := s.st.GetSubtitleStyle(ctx, runID, d.RevisionID)
	if e != nil {
		return d, e
	}
	if v > 0 {
		var saved dv.StyleDocument
		if e = json.Unmarshal([]byte(raw), &saved); e != nil {
			return d, e
		}
		if saved.SourceSRTSHA256 != d.Document.SourceSRTSHA256 || saved.PlayResX != d.Document.PlayResX {
			return d, model.ErrConflict
		}
		d.Document = saved
		d.DraftVersion = v
	}
	d.StyleSHA256 = dv.StyleHash(d.Document)
	return d, nil
}
func (s *Service) SaveSubtitleStyle(ctx context.Context, actor, runID string, req dv.SaveStyleRequest) (dv.SubtitleStyleDraft, error) {
	if actor == "" {
		return dv.SubtitleStyleDraft{}, model.ErrArgument
	}
	d, e := s.subtitleSource(ctx, runID)
	if e != nil {
		return d, e
	}
	asset, e := s.st.GetAsset(ctx, d.AssetID)
	if e != nil {
		return d, e
	}
	if asset.Locked {
		return d, model.ErrForbidden
	}
	if req.RevisionID != d.RevisionID || req.Document.SourceSRTSHA256 != d.Document.SourceSRTSHA256 || req.Document.PlayResX != d.Document.PlayResX || req.Document.PlayResY != 720 {
		return d, model.ErrConflict
	}
	if e = dv.ValidateStyleDocument(req.Document, draftCues(d), d.DurationMS); e != nil {
		return d, fmt.Errorf("%v: %w", e, model.ErrArgument)
	}
	if len(dv.StyledASS(draftCues(d), req.Document)) > 1024*1024 {
		return d, model.ErrArgument
	}
	e = s.st.Transaction(ctx, func(tx *store.Store) error {
		rev, e := s.currentPreviewRevision(ctx, tx, runID)
		if e != nil {
			return e
		}
		if rev.RevisionID != d.RevisionID {
			return model.ErrConflict
		}
		v, _, e := tx.GetSubtitleStyle(ctx, runID, d.RevisionID)
		if e != nil {
			return e
		}
		if v != req.ExpectedDraftVersion {
			return model.ErrConflict
		}
		if e = tx.PutSubtitleStyle(ctx, runID, d.RevisionID, v, string(dv.StyleDocumentBytes(req.Document))); e != nil {
			return e
		}
		return auditTx(ctx, tx, actor, "subtitle_style.save", runID, d.RevisionID)
	})
	if e != nil {
		return d, e
	}
	d.Document = req.Document
	d.DraftVersion = req.ExpectedDraftVersion + 1
	d.StyleSHA256 = dv.StyleHash(d.Document)
	s.publish("subtitle_style.changed", map[string]any{"run_id": runID, "revision_id": d.RevisionID})
	return d, nil
}
func (s *Service) SubtitleFonts(ctx context.Context) ([]dv.FontFamily, error) {
	caps, e := s.st.ListActiveDeliveryVariantCapabilities(ctx)
	if e != nil {
		return nil, e
	}
	fonts := map[string]dv.FontFamily{}
	for _, c := range caps {
		var cap dv.Capability
		if c.Ready && json.Unmarshal([]byte(c.CapabilityJSON), &cap) == nil {
			for _, f := range cap.Fonts {
				if strings.TrimSpace(f.Family) != "" {
					fonts[f.Family] = f
				}
			}
		}
	}
	out := []dv.FontFamily{}
	for _, f := range fonts {
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b dv.FontFamily) int { return strings.Compare(a.Family, b.Family) })
	return out, nil
}
