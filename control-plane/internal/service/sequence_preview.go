package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

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
	BasicQuality   json.RawMessage `json:"basic_quality,omitempty"`
	ContentVersion string          `json:"content_version"`
	SHA256         string          `json:"sha256"`
	Frames         int64           `json:"frames"`
	Duration       float64         `json:"duration"`
	Width          int             `json:"width"`
	Height         int             `json:"height"`
	FPS            project.Rate    `json:"fps"`
	ScopeGroup     string          `json:"scope_group"`
}

type SequenceFrameRequest struct {
	Session          string `json:"session"`
	ExpectedRevision uint64 `json:"expected_revision"`
	ContentVersion   string `json:"content_version"`
	Frame            int64  `json:"frame"`
}
type SequenceFramePreview struct {
	ContentVersion string         `json:"content_version"`
	Frame          int64          `json:"frame"`
	Image          string         `json:"image"`
	WorkerPID      int            `json:"worker_pid,omitempty"`
	Diagnostics    map[string]any `json:"diagnostics,omitempty"`
}

func (s *Service) CloseSequenceFrame(projectID, session, sequenceID string) error {
	if s.CurrentProjectID() != projectID || s.ProjectSessionID() != session {
		return model.ErrConflict
	}
	return s.closePreviewProcess("sequence:" + session + ":" + sequenceID)
}

func (s *Service) sequencePreviewRequest(ctx context.Context, sequenceID string, view TimelineView, payload map[string]any) ([]byte, error) {
	var result []byte
	p, err := s.startPreviewProcess("sequence:"+view.Session+":"+sequenceID, drawingEnvironmentVersion(), "sequence-preview.mjs")
	if err != nil {
		return result, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	requestID := newID("frame")
	payload["request_id"] = requestID
	raw, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	if _, err = p.input.Write(append(raw, '\n')); err != nil {
		p.stop()
		return result, err
	}
	// A cancelled HTTP seek may finish its one in-flight frame. Drain its reply
	// before admitting the next request, keeping the compiled browser alive.
	timeout := 45 * time.Second
	if payload["operation"] == "segment" {
		timeout = 90 * time.Second
	}
	wait := time.NewTimer(timeout)
	defer wait.Stop()
	for {
		select {
		case <-wait.C:
			p.stop()
			return result, context.DeadlineExceeded
		case raw, ok := <-p.responses:
			if !ok {
				return result, fmt.Errorf("节目预览进程退出: %w", model.ErrInvalidState)
			}
			var status struct {
				OK        bool   `json:"ok"`
				RequestID string `json:"request_id"`
				Message   string `json:"message"`
			}
			if err = json.Unmarshal(raw, &status); err != nil {
				return result, err
			}
			if status.RequestID != requestID {
				continue
			}
			if !status.OK {
				return result, fmt.Errorf("%s: %w", status.Message, model.ErrInvalidState)
			}
			result = raw
			return result, ctx.Err()
		}
	}
}

func (s *Service) persistentSequenceFrame(ctx context.Context, projectID, sequenceID string, view TimelineView, sources []sequenceSource, frame int64) (SequenceFramePreview, error) {
	var result SequenceFramePreview
	raw, err := s.sequencePreviewRequest(ctx, sequenceID, view, map[string]any{"session": view.Session, "sequence": view.Sequence, "resources": sources, "frame": frame, "content_version": view.ContentVersion})
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if result.Frame != frame || result.ContentVersion != view.ContentVersion || !strings.HasPrefix(result.Image, "data:image/png;base64,") {
		return result, model.ErrConflict
	}
	return result, nil
}

// Current-frame work reuses a sequence-owned worker and never waits behind the
// full-movie render/export lock. Superseded HTTP results do not destroy its cache.
func (s *Service) CreateSequenceFrame(ctx context.Context, projectID, sequenceID string, req SequenceFrameRequest) (SequenceFramePreview, error) {
	var result SequenceFramePreview
	if s.workspace == nil {
		return result, model.ErrInvalidState
	}
	s.workspace.gate.RLock()
	defer s.workspace.gate.RUnlock()
	view, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return result, err
	}
	if req.Session != view.Session || req.ExpectedRevision != view.Sequence.Revision || req.ContentVersion != view.ContentVersion {
		return result, model.ErrConflict
	}
	var end int64
	for _, track := range sequenceTracks(view.Sequence) {
		for _, clip := range track.Clips {
			if clip.At+clip.Duration > end {
				end = clip.At + clip.Duration
			}
		}
	}
	if req.Frame < 0 || req.Frame >= end {
		return result, model.ErrArgument
	}
	sources, err := s.sequenceSources(ctx, projectID, view.Sequence)
	if err != nil {
		return result, err
	}
	output := ""
	// Reopening an already rendered composition must not recompile its drawing
	// sources just to show a still. Extract from the verified same-version movie.
	cachedDir := s.sequencePreviewDir(projectID, sequenceID, view.ContentVersion)
	var cached SequencePreview
	metadata, cacheErr := os.ReadFile(filepath.Join(cachedDir, "program.json"))
	cachedMovie := false
	if cacheErr == nil && json.Unmarshal(metadata, &cached) == nil && cached.ContentVersion == view.ContentVersion {
		digest, digestErr := sequenceFileDigest(filepath.Join(cachedDir, "program.mp4"))
		cachedMovie = digestErr == nil && digest == cached.SHA256
	}
	if cachedMovie {
		parent := filepath.Join(s.workspace.root, "sequence-frames")
		if err = os.MkdirAll(parent, 0700); err != nil {
			return result, err
		}
		output, err = os.MkdirTemp(parent, "frame-")
		if err != nil {
			return result, err
		}
		defer os.RemoveAll(output)
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-y", "-ss", strconv.FormatFloat(view.Sequence.FPS.Seconds(req.Frame), 'f', 9, 64), "-protocol_whitelist", "file,pipe", "-i", filepath.Join(cachedDir, "program.mp4"), "-frames:v", "1", "-threads", "1", filepath.Join(output, "frame.png"))
		configureMediaCommand(cmd)
		cmd.Env = toolEnvironment()
		if err = cmd.Run(); err != nil {
			return result, err
		}
	} else {
		result, err = s.persistentSequenceFrame(ctx, projectID, sequenceID, view, sources, req.Frame)
		if err != nil {
			return result, err
		}
	}
	current, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return result, err
	}
	if current.Session != view.Session || current.ContentVersion != view.ContentVersion {
		return result, model.ErrConflict
	}
	if _, err = s.sequenceSources(ctx, projectID, current.Sequence); err != nil {
		return result, err
	}
	if cachedMovie {
		raw, err := os.ReadFile(filepath.Join(output, "frame.png"))
		if err != nil {
			return result, err
		}
		result = SequenceFramePreview{ContentVersion: view.ContentVersion, Frame: req.Frame, Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw), Diagnostics: map[string]any{"backend": "cached-movie"}}
	}
	return result, nil
}

type sequenceSource struct {
	Draw     *project.DrawDocument `json:"draw_document,omitempty"`
	Assets   []drawImageAsset      `json:"assets,omitempty"`
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Kind     string                `json:"kind"`
	Path     string                `json:"path"`
	SHA256   string                `json:"sha256"`
	Metadata map[string]any        `json:"metadata"`
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
		return (t.Hidden && (t.Kind != "code" || t.Muted)) || (t.Kind == "audio" && t.Muted) || (solo[t.Kind] && !t.Solo)
	})
}
func (s *Service) sequenceSources(ctx context.Context, projectID string, seq project.Sequence) ([]sequenceSource, error) {
	ids := map[string]bool{}
	for _, t := range sequenceTracks(seq) {
		for _, c := range t.Clips {
			ids[c.ResourceID] = true
		}
	}
	out := []sequenceSource{}
	audioSources := map[string]sequenceSource{}
	for id := range ids {
		r, e := s.ProjectResource(projectID, id)
		if e != nil {
			return nil, e
		}
		if r.Missing || r.Removed || r.Changed {
			return nil, fmt.Errorf("资源缺失或已改变，请重新定位：%s: %w", r.Name, model.ErrConflict)
		}
		if r.Draw != nil {
			c := r.Draw.Snapshot
			if r.Draw.Mode == "follow" {
				c, e = s.drawComposition(projectID, r.Draw.CompositionID)
				if e != nil {
					return nil, e
				}
			}
			assets, e := s.drawImageAssets(ctx, projectID, c)
			if e != nil {
				return nil, e
			}
			doc := c.UnifiedDocument()
			for _, a := range doc.AudioBindings {
				audio, e := s.ProjectResource(projectID, a.ResourceID)
				if e != nil {
					return nil, e
				}
				if audio.Missing || audio.Changed || audio.Removed || !slices.Contains(project.Streams(audio, "audio"), a.StreamIndex) || (a.ResourceSHA != "" && a.ResourceSHA != audio.Source.SHA256) {
					return nil, model.ErrConflict
				}
				audioSources[a.ResourceID] = sequenceSource{ID: audio.ID, Name: audio.Name, Kind: audio.Kind, Path: audio.Source.OriginalPath, SHA256: audio.Source.SHA256, Metadata: audio.Metadata}
			}
			out = append(out, sequenceSource{ID: r.ID, Name: r.Name, Kind: "video", SHA256: c.ContentVersion, Metadata: map[string]any{"duration": c.FPS.Seconds(c.DurationFrames), "streams": []map[string]any{{"index": 0, "codec_type": "video"}}}, Draw: &doc, Assets: assets})
			continue
		}
		path := r.Source.OriginalPath
		sha := r.Source.SHA256
		if project.IsCameraRaw(path) {
			path, e = s.imageRenderPath(ctx, projectID, r)
			if e != nil {
				return nil, e
			}
			sha, e = sequenceFileDigest(path)
			if e != nil {
				return nil, e
			}
		}
		out = append(out, sequenceSource{ID: r.ID, Name: r.Name, Kind: r.Kind, Path: path, SHA256: sha, Metadata: r.Metadata})
	}
	for id, a := range audioSources {
		if !ids[id] {
			out = append(out, a)
		}
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
	sources, err := s.sequenceSources(ctx, projectID, view.Sequence)
	if err != nil {
		return report, err
	}
	// The desktop preview and Agent export can request the exact same movie.
	// Join the owned render, then validate/reuse its cache instead of reporting
	// an unchanged composition as a human-blocking context conflict.
	var release func()
	for {
		release, err = s.acquireScript("sequence:" + projectID + ":" + sequenceID + ":" + view.ContentVersion)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
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
	if _, err = s.sequenceSources(ctx, projectID, current.Sequence); err != nil {
		return report, err
	}
	if _, err = os.Stat(filepath.Join(output, "program.mp4")); err != nil {
		return report, err
	}
	if !validBasicQuality(report.BasicQuality, report.SHA256) {
		data, checkErr := s.scriptProcess(ctx, true, "quality-sequence", map[string]any{"path": filepath.Join(output, "program.mp4"), "sha256": report.SHA256, "duration": report.Duration, "sequence": current.Sequence, "resources": sources})
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		var quality struct {
			BasicQuality json.RawMessage `json:"basic_quality"`
		}
		if checkErr == nil && json.Unmarshal(data, &quality) == nil && validBasicQuality(quality.BasicQuality, report.SHA256) {
			report.BasicQuality = quality.BasicQuality
		} else {
			report.BasicQuality = uncheckedBasicQuality(report.SHA256)
		}
	}
	latest, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return report, err
	}
	if latest.Session != view.Session || latest.ContentVersion != report.ContentVersion {
		return report, fmt.Errorf("序列已改变，旧检查结果已丢弃: %w", model.ErrConflict)
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
	if _, err = s.sequenceSources(ctx, projectID, view.Sequence); err != nil {
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
	if r.Draw != nil {
		return s.DrawResourceThumbnail(ctx, projectID, r, at)
	}
	output := filepath.Join(s.workspace.root, "previews", project.ResourceContentVersion(r)+"-"+skillHash(at)+".png")
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
	source, err := s.imageRenderPath(ctx, projectID, r)
	if err != nil {
		return "", err
	}
	temporary := output + ".tmp.png"
	defer os.Remove(temporary)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-ss", strconv.FormatFloat(at, 'f', 9, 64), "-protocol_whitelist", "file,pipe", "-i", source, "-map", fmt.Sprintf("0:%d", stream), "-frames:v", "1", "-vf", "scale=160:-2", "-threads", "1", temporary)
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
