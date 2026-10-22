package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

type SequencePreviewRequest struct {
	Session          string `json:"session"`
	ExpectedRevision uint64 `json:"expected_revision"`
	ContentVersion   string `json:"content_version"`
	Path             string `json:"path,omitempty"`
}
type SequencePreview struct {
	ContentVersion string       `json:"content_version"`
	SHA256         string       `json:"sha256"`
	Frames         int64        `json:"frames"`
	Duration       float64      `json:"duration"`
	Width          int          `json:"width"`
	Height         int          `json:"height"`
	FPS            project.Rate `json:"fps"`
	ScopeGroup     string       `json:"scope_group"`
}
type sequenceSource struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Path     string         `json:"path"`
	SHA256   string         `json:"sha256"`
	Metadata map[string]any `json:"metadata"`
}

func sequenceTracks(seq project.Sequence) []project.Track {
	tracks := []project.Track{}
	solo := map[string]bool{}
	for _, t := range seq.Tracks {
		if seq.ScopeGroup == "" || (seq.ScopeGroup == "original" && t.GroupID == "") || seq.ScopeGroup == t.GroupID {
			tracks = append(tracks, t)
			if t.Solo {
				solo[t.Kind] = true
			}
		}
	}
	return slices.DeleteFunc(tracks, func(t project.Track) bool {
		return t.Hidden || (t.Kind == "audio" && t.Muted) || (solo[t.Kind] && !t.Solo)
	})
}
func (s *Service) sequenceSources(projectID string, seq project.Sequence) ([]sequenceSource, error) {
	ids := map[string]bool{}
	for _, t := range sequenceTracks(seq) {
		for _, c := range t.Clips {
			ids[c.ResourceID] = true
		}
	}
	out := []sequenceSource{}
	for id := range ids {
		r, e := s.ProjectResource(projectID, id)
		if e != nil {
			return nil, e
		}
		if r.Missing || r.Removed || r.Changed {
			return nil, fmt.Errorf("资源缺失或已改变，请重新定位：%s: %w", r.Name, model.ErrConflict)
		}
		out = append(out, sequenceSource{r.ID, r.Name, r.Kind, r.Source.OriginalPath, r.Source.SHA256, r.Metadata})
	}
	slices.SortFunc(out, func(a, b sequenceSource) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return out, nil
}
func (s *Service) sequencePreviewDir(projectID, sequenceID, version string) string {
	return filepath.Join(s.workspace.root, "sequences", projectID, sequenceID, version)
}
func sequenceFileDigest(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (s *Service) CreateSequencePreview(ctx context.Context, projectID, sequenceID string, req SequencePreviewRequest) (SequencePreview, error) {
	var report SequencePreview
	view, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return report, err
	}
	if req.Session != view.Session || req.ExpectedRevision != view.Sequence.Revision || req.ContentVersion != view.ContentVersion {
		return report, model.ErrConflict
	}
	sources, err := s.sequenceSources(projectID, view.Sequence)
	if err != nil {
		return report, err
	}
	release, err := s.acquireScript("sequence:" + projectID + ":" + sequenceID + ":" + view.ContentVersion)
	if err != nil {
		return report, err
	}
	defer release()
	output := s.sequencePreviewDir(projectID, sequenceID, view.ContentVersion)
	raw, e := os.ReadFile(filepath.Join(output, "program.json"))
	if e == nil {
		e = json.Unmarshal(raw, &report)
	}
	validCache := e == nil && report.ContentVersion == view.ContentVersion
	if validCache {
		digest, err := sequenceFileDigest(filepath.Join(output, "program.mp4"))
		validCache = err == nil && digest == report.SHA256
	}
	if !validCache {
		raw, err = s.scriptProcess(ctx, true, "sequence", map[string]any{"sequence": view.Sequence, "resources": sources, "output_dir": output, "content_version": view.ContentVersion})
		if err != nil {
			return report, err
		}
		if err = json.Unmarshal(raw, &report); err != nil {
			return report, err
		}
	}
	current, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return report, err
	}
	if current.Session != view.Session || current.ContentVersion != report.ContentVersion {
		return report, fmt.Errorf("序列已改变，旧节目结果已丢弃: %w", model.ErrConflict)
	}
	if _, err = s.sequenceSources(projectID, current.Sequence); err != nil {
		return report, err
	}
	if _, err = os.Stat(filepath.Join(output, "program.mp4")); err != nil {
		return report, err
	}
	return report, nil
}

func (s *Service) SequencePreviewPath(ctx context.Context, projectID, sequenceID, version string) (string, error) {
	view, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return "", err
	}
	if version != view.ContentVersion {
		return "", model.ErrConflict
	}
	if _, err = s.sequenceSources(projectID, view.Sequence); err != nil {
		return "", err
	}
	path := filepath.Join(s.sequencePreviewDir(projectID, sequenceID, version), "program.mp4")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", model.ErrNotFound
	}
	return path, nil
}
func (s *Service) ExportSequence(ctx context.Context, projectID, sequenceID string, req SequencePreviewRequest) (SequencePreview, error) {
	if !filepath.IsAbs(req.Path) || !slices.Contains([]string{".mp4", ".MP4"}, filepath.Ext(req.Path)) {
		return SequencePreview{}, model.ErrArgument
	}
	report, err := s.CreateSequencePreview(ctx, projectID, sequenceID, req)
	if err != nil {
		return report, err
	}
	path, err := s.SequencePreviewPath(ctx, projectID, sequenceID, report.ContentVersion)
	if err != nil {
		return report, err
	}
	w := s.workspace
	if err = w.lockMutation(ctx); err != nil {
		return report, err
	}
	defer w.op.Unlock()
	current, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return report, err
	}
	if current.Session != req.Session || current.ContentVersion != req.ContentVersion {
		return report, model.ErrConflict
	}
	digest, err := sequenceFileDigest(path)
	if err != nil {
		return report, err
	}
	if digest != report.SHA256 {
		return report, model.ErrConflict
	}
	// Export copies the verified program composition rather than reinterpreting
	// the tracks through another renderer.
	if err = project.CopyAtomic(req.Path, path); err != nil {
		return report, err
	}
	return report, nil
}

func (s *Service) ResourceWaveform(ctx context.Context, projectID, id string, stream int) (json.RawMessage, error) {
	r, err := s.ProjectResource(projectID, id)
	if err != nil {
		return nil, err
	}
	if r.Missing || r.Removed || r.Changed || !slices.Contains(project.Streams(r, "audio"), stream) {
		return nil, model.ErrArgument
	}
	output := filepath.Join(s.workspace.root, "previews", r.Source.SHA256+"-audio-"+strconv.Itoa(stream)+".json")
	if raw, e := os.ReadFile(output); e == nil {
		return raw, nil
	}
	raw, err := s.scriptProcess(ctx, true, "waveform", map[string]any{"path": r.Source.OriginalPath, "stream_index": stream, "duration": r.Metadata["duration"]})
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return nil, err
	}
	if err = project.WriteAtomic(output, json.RawMessage(raw)); err != nil {
		return nil, err
	}
	return raw, nil
}

// loudnessCacheVersion is part of the cache key; bump it when the worker's
// bucket plan or encoding changes so stale envelopes are not served.
const loudnessCacheVersion = "loudness-v2"

func loudnessCachePath(root, sha string, stream int) string {
	return filepath.Join(root, "previews", sha+"-"+loudnessCacheVersion+"-"+strconv.Itoa(stream)+".json")
}

// ResourceLoudness returns a per-stream peak/RMS envelope cached by source
// hash, stream index and encoding version.
func (s *Service) ResourceLoudness(ctx context.Context, projectID, id string, stream int) (json.RawMessage, error) {
	r, err := s.ProjectResource(projectID, id)
	if err != nil {
		return nil, err
	}
	if r.Missing || r.Removed || r.Changed || !slices.Contains(project.Streams(r, "audio"), stream) {
		return nil, model.ErrArgument
	}
	output := loudnessCachePath(s.workspace.root, r.Source.SHA256, stream)
	if raw, e := os.ReadFile(output); e == nil && json.Valid(raw) {
		return raw, nil
	}
	release, err := s.acquireScript("loudness:" + output)
	if err != nil {
		return nil, err
	}
	defer release()
	if raw, e := os.ReadFile(output); e == nil && json.Valid(raw) {
		return raw, nil
	}
	raw, err := s.scriptProcess(ctx, true, "loudness", map[string]any{"path": r.Source.OriginalPath, "stream_index": stream, "duration": r.Metadata["duration"]})
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return nil, err
	}
	if err = project.WriteAtomic(output, json.RawMessage(raw)); err != nil {
		return nil, err
	}
	return raw, nil
}
func (s *Service) ResourceThumbnail(ctx context.Context, projectID, id string, at float64) (string, error) {
	r, err := s.ProjectResource(projectID, id)
	if err != nil {
		return "", err
	}
	if r.Missing || r.Removed || r.Changed || (r.Kind != "video" && r.Kind != "image") || !finiteScriptTime(at) || at < 0 || (r.Kind == "video" && at >= r.Out) {
		return "", model.ErrArgument
	}
	output := filepath.Join(s.workspace.root, "previews", r.Source.SHA256+"-"+skillHash(at)+".png")
	if _, err = os.Stat(output); err == nil {
		return output, nil
	}
	release, err := s.acquireScript("thumbnail:" + output)
	if err != nil {
		return "", err
	}
	defer release()
	if err = os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return "", err
	}
	stream := 0
	if indexes := project.Streams(r, "video"); len(indexes) > 0 {
		stream = indexes[0]
	}
	temporary := output + ".tmp.png"
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-ss", strconv.FormatFloat(at, 'f', 9, 64), "-protocol_whitelist", "file,pipe", "-i", r.Source.OriginalPath, "-map", fmt.Sprintf("0:%d", stream), "-frames:v", "1", "-vf", "scale=160:-2", "-threads", "1", temporary)
	configureMediaCommand(cmd)
	cmd.Env = toolEnvironment()
	if err = cmd.Run(); err != nil {
		return "", err
	}
	if err = os.Rename(temporary, output); err != nil {
		return "", err
	}
	return output, nil
}
