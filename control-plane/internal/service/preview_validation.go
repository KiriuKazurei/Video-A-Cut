package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// All transports, task guards and view projections use the same current input.
func (s *Service) currentPreviewRevision(ctx context.Context, st *store.Store, runID string) (model.Revision, error) {
	run, err := st.GetWorkflow(ctx, runID)
	if err != nil {
		return model.Revision{}, err
	}
	latest, err := st.ListWorkflows(ctx, run.AssetID, 1)
	if err != nil {
		return model.Revision{}, err
	}
	if run.Status == model.WorkflowCancelled || len(latest) != 1 || latest[0].RunID != runID {
		return model.Revision{}, model.ErrConflict
	}
	asset, err := st.GetAsset(ctx, run.AssetID)
	if err != nil {
		return model.Revision{}, err
	}
	if asset.Status != model.AssetStatusExported || !assetVisibleForRole(asset, "exporter") {
		return model.Revision{}, model.ErrConflict
	}
	rev, err := st.GetRevision(ctx, run.CurrentRevisionID)
	if err != nil {
		return model.Revision{}, err
	}
	if rev.AssetID != asset.AssetID || filepath.Clean(filepath.FromSlash(asset.Artifacts["edl"])) != filepath.Join(filepath.FromSlash(rev.PackageRef), "edl.json") {
		return model.Revision{}, model.ErrConflict
	}
	stages, err := st.ListStages(ctx, runID)
	if err != nil {
		return model.Revision{}, err
	}
	for _, stage := range stages {
		if stage.Stage != model.TaskTypeExport || stage.Invalidated || stage.OutputRevisionID != rev.RevisionID {
			continue
		}
		task, e := st.GetTask(ctx, stage.TaskID)
		if e == nil && task.Status == model.TaskStatusSucceeded {
			return rev, nil
		}
	}
	return model.Revision{}, model.ErrConflict
}

// Reject redirected components, even if the link target is elsewhere inside deliveryRoot.
func (s *Service) previewPackageDir(rel string) (string, error) {
	if !preview.IsRelativePath(filepath.ToSlash(rel)) {
		return "", model.ErrArgument
	}
	cursor := s.deliveryRoot
	for _, part := range strings.Split(filepath.FromSlash(rel), string(filepath.Separator)) {
		cursor = filepath.Join(cursor, part)
		info, err := os.Lstat(cursor)
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return "", model.ErrForbidden
		}
	}
	physical, _, err := s.deliveryPath(filepath.FromSlash(rel), true)
	return physical, err
}

func (s *Service) previewPackageFile(pkgAbs, rel string) (string, os.FileInfo, error) {
	if !preview.IsRelativePath(rel) {
		return "", nil, model.ErrArgument
	}
	path := filepath.Join(pkgAbs, filepath.FromSlash(rel))
	rootRel, err := filepath.Rel(s.deliveryRoot, path)
	if err != nil || !filepath.IsLocal(rootRel) {
		return "", nil, model.ErrForbidden
	}
	cursor := s.deliveryRoot
	for _, part := range strings.Split(rootRel, string(filepath.Separator)) {
		cursor = filepath.Join(cursor, part)
		info, e := os.Lstat(cursor)
		if e != nil {
			return "", nil, e
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return "", nil, model.ErrForbidden
		}
	}
	physical, info, err := s.deliveryPath(rootRel, false)
	if err != nil {
		return "", nil, err
	}
	within, err := filepath.Rel(pkgAbs, physical)
	if err != nil || !filepath.IsLocal(within) || !strings.EqualFold(filepath.Clean(path), physical) {
		return "", nil, model.ErrForbidden
	}
	return physical, info, nil
}

func previewReadJSON(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxManifestBytes {
		return nil, fmt.Errorf("preview JSON exceeds limit: %w", model.ErrArgument)
	}
	return b, nil
}

func previewHashFile(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		n, e := f.Read(buffer)
		if n > 0 {
			hash.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Service) verifyPreviewInput(ctx context.Context, input preview.Input) (string, error) {
	pkg, err := s.previewPackageDir(input.PackageRef)
	if err != nil {
		return "", err
	}
	aliases := map[string]bool{}
	for _, f := range input.Files {
		alias := strings.ToLower(f.Path)
		if aliases[alias] {
			return "", model.ErrArgument
		}
		aliases[alias] = true
		path, info, e := s.previewPackageFile(pkg, f.Path)
		if e != nil {
			return "", e
		}
		hash, e := previewHashFile(ctx, path)
		if e != nil {
			return "", e
		}
		if hash != f.SHA256 || info.Size() != f.Size {
			return "", fmt.Errorf("preview input changed: %s: %w", f.Path, model.ErrConflict)
		}
	}
	if e := s.verifySubtitleBinding(ctx, input); e != nil {
		return "", e
	}
	return pkg, nil
}

func (s *Service) validatePreviewMedia(ctx context.Context, input preview.Input, report preview.Report, receipt preview.Receipt, media preview.Media, pkg string) error {
	required := map[string]bool{"source_fingerprint": false, "timeline_frames": false, "audio_alignment": false, "subtitles": false, "media_streams": false}
	for _, c := range report.Checks {
		seen, ok := required[c.Code]
		if !ok || seen || c.Status != "passed" {
			return model.ErrArgument
		}
		required[c.Code] = true
	}
	for _, seen := range required {
		if !seen {
			return fmt.Errorf("missing preview check: %w", model.ErrArgument)
		}
	}
	if receipt.RendererVersion == "" || receipt.FFmpegVersion == "" {
		return model.ErrArgument
	}
	var edl struct {
		Timeline struct {
			FPS float64 `json:"fps"`
		}
		Video []struct {
			Src        string  `json:"src"`
			In         float64 `json:"in"`
			Out        float64 `json:"out"`
			TimelineIn float64 `json:"timeline_in"`
		} `json:"video"`
	}
	edlPath, _, err := s.previewPackageFile(pkg, "edl.json")
	if err != nil {
		return err
	}
	b, err := previewReadJSON(edlPath)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &edl); err != nil {
		return err
	}
	fps := edl.Timeline.FPS
	const maxSafeInteger = 9007199254740991
	if fps <= 0 || math.IsNaN(fps) || math.IsInf(fps, 0) || fps > maxSafeInteger || fps != math.Trunc(fps) || len(edl.Video) == 0 {
		return model.ErrArgument
	}
	sources := map[string]bool{}
	hasSRT := false
	for _, f := range input.Files {
		if f.Kind == "source_video" || f.Kind == "packaged_source" {
			sources[f.Path] = true
		}
		if f.Kind == "srt" {
			hasSRT = true
		}
	}
	var frames int64
	// Match the renderer: frame order chooses the first clip's canvas.
	sort.SliceStable(edl.Video, func(i, j int) bool {
		return math.Round(edl.Video[i].TimelineIn*fps) < math.Round(edl.Video[j].TimelineIn*fps)
	})
	for _, cut := range edl.Video {
		if !sources[cut.Src] || cut.In < 0 || cut.Out <= cut.In || cut.TimelineIn < 0 {
			return model.ErrArgument
		}
		start, in, out := math.Round(cut.TimelineIn*fps), math.Round(cut.In*fps), math.Round(cut.Out*fps)
		end := start + (out - in)
		if out <= in || start < float64(frames) || math.IsNaN(end) || math.IsInf(end, 0) || start > maxSafeInteger || in > maxSafeInteger || out > maxSafeInteger || end > maxSafeInteger {
			return model.ErrArgument
		}
		if int64(end) > frames {
			frames = int64(end)
		}
	}
	sourcePath, _, err := s.previewPackageFile(pkg, edl.Video[0].Src)
	if err != nil {
		return err
	}
	source, err := s.MediaProber().ProbeMedia(ctx, sourcePath)
	if err != nil {
		return err
	}
	if source.Width < 2 || source.Height < 2 {
		return model.ErrArgument
	}
	scale := math.Min(1, math.Min(1280/float64(source.Width), 720/float64(source.Height)))
	width := int(math.Round(float64(source.Width) * scale))
	width -= width % 2
	height := int(math.Round(float64(source.Height) * scale))
	height -= height % 2
	samples := math.Round(float64(frames) * 48000 / fps)
	if samples > maxSafeInteger || width < 2 || height < 2 {
		return model.ErrArgument
	}
	targetSamples := int64(samples)
	duration := float64(frames) / fps
	if frames <= 0 || media.Width != width || media.Height != height || media.FPS != fps || media.FrameCount != frames || media.VideoCodec != "h264" || media.AudioCodec != "aac" || media.SampleRate != 48000 || media.Channels != 2 || math.IsNaN(media.DurationSeconds) || math.IsInf(media.DurationSeconds, 0) || math.Abs(media.DurationSeconds-duration) > 0.025 {
		return fmt.Errorf("preview media does not match frozen timeline/preset: %w", model.ErrArgument)
	}
	if report.TargetFrames != frames || report.TargetSamples != targetSamples || report.Media.Width != media.Width || report.Media.Height != media.Height || report.Media.FPS != media.FPS || report.Media.FrameCount != media.FrameCount || report.Media.VideoCodec != media.VideoCodec || report.Media.AudioCodec != media.AudioCodec || report.Media.SampleRate != media.SampleRate || report.Media.Channels != media.Channels || math.IsNaN(report.Media.DurationSeconds) || math.Abs(report.Media.DurationSeconds-duration) > 0.025 {
		return model.ErrArgument
	}
	if err := preview.ValidateAudioInterval(media.AudioInterval, targetSamples, media.SampleRate, fps); err != nil {
		return fmt.Errorf("preview audio timing: %v: %w", err, model.ErrArgument)
	}
	// Older published reports may omit interval metadata. They still need a
	// fresh successful probe; reported timing, when present, must match it.
	if reported := report.Media.AudioInterval; reported != nil {
		if err := preview.ValidateAudioInterval(reported, targetSamples, media.SampleRate, fps); err != nil {
			return fmt.Errorf("reported audio timing: %v: %w", err, model.ErrArgument)
		}
		if reported.StartPTS != media.AudioInterval.StartPTS || reported.DurationTS != media.AudioInterval.DurationTS || reported.TimeBase != media.AudioInterval.TimeBase {
			return fmt.Errorf("reported audio timing does not match probe: %w", model.ErrArgument)
		}
	}
	if input.SubtitleVariant != nil {
		if !validFontReceipt(receipt.Fonts, input.SubtitleVariant.FontFamilies) {
			return model.ErrArgument
		}
		hasSRT = false
	}
	if hasSRT && (receipt.Font == nil || receipt.Font.Family != "Microsoft YaHei" || len(receipt.Font.SHA256) != 64) {
		return model.ErrArgument
	}
	if hasSRT {
		if _, err := hex.DecodeString(receipt.Font.SHA256); err != nil || receipt.Font.SHA256 != strings.ToLower(receipt.Font.SHA256) {
			return model.ErrArgument
		}
	}
	return nil
}
