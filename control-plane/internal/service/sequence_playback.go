package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

type PlaybackClip struct {
	ID         string  `json:"id"`
	ResourceID string  `json:"resource_id"`
	At         int64   `json:"at"`
	Duration   int64   `json:"duration"`
	SourceIn   float64 `json:"source_in"`
}

type SequencePlayback struct {
	Mode           string         `json:"mode"`
	ContentVersion string         `json:"content_version"`
	Frames         int64          `json:"frames"`
	FPS            project.Rate   `json:"fps"`
	SegmentFrames  int64          `json:"segment_frames"`
	Video          []PlaybackClip `json:"video"`
	Audio          []PlaybackClip `json:"audio"`
}

func neutralPlayback(e project.Effects) bool {
	return e.Rotation == 0 && len(e.ParameterKeyframes) == 0 && e.GainDB == 0 && len(e.GainKeyframes) == 0 && e.FadeIn == 0 && e.FadeOut == 0 && e.Transition == 0 && (e.Scale == 0 || e.Scale == 1) && e.X == 0 && e.Y == 0 && e.CropLeft == 0 && e.CropRight == 0 && e.CropTop == 0 && e.CropBottom == 0
}

// Direct playback is an optimization of the existing selected-track semantics.
// Everything that needs composition, a nondefault stream or audio mixing stays
// on the shared renderer. It never changes the saved sequence or export graph.
func (s *Service) SequencePlaybackPlan(ctx context.Context, projectID, sequenceID string, req SequencePreviewRequest) (SequencePlayback, error) {
	view, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return SequencePlayback{}, err
	}
	plan := SequencePlayback{Mode: "direct", ContentVersion: view.ContentVersion, FPS: view.Sequence.FPS, SegmentFrames: int64(math.Ceil(view.Sequence.FPS.Value() * 2)), Video: []PlaybackClip{}, Audio: []PlaybackClip{}}
	if req.Session != view.Session || req.ExpectedRevision != view.Sequence.Revision || req.ContentVersion != view.ContentVersion {
		return plan, model.ErrConflict
	}
	if _, err = s.sequenceSources(ctx, projectID, view.Sequence); err != nil {
		return plan, err
	}
	for _, track := range sequenceTracks(view.Sequence) {
		for _, c := range track.Clips {
			plan.Frames = max(plan.Frames, c.At+c.Duration)
			if track.Kind == "subtitle" {
				plan.Mode = "segments"
				continue
			}
			r, err := s.ProjectResource(projectID, c.ResourceID)
			if err != nil {
				return plan, err
			}
			streams := project.Streams(r, track.Kind)
			if r.Draw != nil || r.Kind == "image" || len(streams) != 1 || streams[0] != c.StreamIndex || c.Speed != 1 || track.GainDB != 0 || !neutralPlayback(c.Effects) {
				plan.Mode = "segments"
			}
			clip := PlaybackClip{ID: c.ID, ResourceID: c.ResourceID, At: c.At, Duration: c.Duration, SourceIn: project.Seconds(c.SourceIn)}
			if track.Kind == "video" || track.Kind == "code" {
				plan.Video = append(plan.Video, clip)
			} else {
				plan.Audio = append(plan.Audio, clip)
			}
		}
	}
	for _, clips := range [][]PlaybackClip{plan.Video, plan.Audio} {
		slices.SortFunc(clips, func(a, b PlaybackClip) int {
			if a.At < b.At {
				return -1
			}
			if a.At > b.At {
				return 1
			}
			return strings.Compare(a.ID, b.ID)
		})
		for i := 1; i < len(clips); i++ {
			if clips[i].At < clips[i-1].At+clips[i-1].Duration {
				plan.Mode = "segments"
			}
		}
	}
	if plan.Frames < 1 {
		return plan, model.ErrArgument
	}
	// A valid movie can be opened immediately, without launching a still worker
	// or waiting for a second render/quality request.
	output := s.sequencePreviewDir(projectID, sequenceID, view.ContentVersion)
	if raw, e := os.ReadFile(filepath.Join(output, "program.json")); e == nil {
		var movie SequencePreview
		if json.Unmarshal(raw, &movie) == nil && movie.ContentVersion == view.ContentVersion {
			if digest, e := sequenceFileDigest(filepath.Join(output, "program.mp4")); e == nil && digest == movie.SHA256 {
				plan.Mode = "cached"
			}
		}
	}
	return plan, nil
}

type SequenceSegmentRequest struct {
	SequencePreviewRequest
	StartFrame int64 `json:"start_frame"`
}
type SequenceSegment struct {
	SequencePreview
	Key        string `json:"key"`
	StartFrame int64  `json:"start_frame"`
	Cached     bool   `json:"cached"`
}

func segmentInputs(seq project.Sequence, sources []sequenceSource, start, end int64) (string, []sequenceSource) {
	state := seq.SequenceState
	state.Name = ""
	state.Markers = nil
	state.Groups = nil
	state.Tracks = nil
	ids := map[string]bool{}
	tracks := sequenceTracks(seq)
	visualKey := func(track project.Track) string {
		if track.VisualGroup != "" {
			return "group:" + track.VisualGroup
		}
		return "track:" + track.ID
	}
	// Index adjacent transition starts once; segment requests also serve long
	// timelines, so finding handles must not scan all clips for every clip.
	transitions := map[string]map[int64]int64{}
	for _, track := range tracks {
		key := visualKey(track)
		if transitions[key] == nil {
			transitions[key] = map[int64]int64{}
		}
		for _, c := range track.Clips {
			transitions[key][c.At] = max(transitions[key][c.At], c.Effects.Transition)
		}
	}
	for _, track := range tracks {
		track.Name = ""
		track.Height = 0
		track.Locked = false
		track.LockOwner = ""
		ordered := slices.Clone(track.Clips)
		slices.SortFunc(ordered, func(a, b project.Clip) int {
			if a.At < b.At {
				return -1
			}
			if a.At > b.At {
				return 1
			}
			return 0
		})
		track.Clips = nil
		for _, c := range ordered {
			tail := transitions[visualKey(track)][c.At+c.Duration]
			if c.At >= end || c.At+c.Duration+tail <= start {
				continue
			}
			c.Name = ""
			c.Origin = project.Origin{}
			c.Code = nil
			track.Clips = append(track.Clips, c)
			ids[c.ResourceID] = true
		}
		if len(track.Clips) > 0 {
			state.Tracks = append(state.Tracks, track)
		}
	}
	selected := []sequenceSource{}
	// Dependencies of selected source-code pictures are inputs too. Dropping
	// these made bounded playback fail while a full export could still mix audio.
	for _, r := range sources {
		if ids[r.ID] && r.Draw != nil {
			for _, binding := range r.Draw.AudioBindings {
				ids[binding.ResourceID] = true
			}
		}
	}
	for _, r := range sources {
		if ids[r.ID] {
			selected = append(selected, r)
		}
	}
	key := skillHash(map[string]any{"state": state, "sources": selected, "start": start, "end": end, "renderer": "bounded-preview-v3-540p-source8-main31", "drawing_environment": drawingEnvironmentVersion()})
	return key, selected
}

func (s *Service) CreateSequenceSegment(ctx context.Context, projectID, sequenceID string, req SequenceSegmentRequest) (SequenceSegment, error) {
	var result SequenceSegment
	ctx, releaseExecution := s.trackProjectExecution(ctx)
	defer releaseExecution()
	view, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return result, err
	}
	if req.Session != view.Session || req.ContentVersion != view.ContentVersion || req.ExpectedRevision != view.Sequence.Revision {
		return result, model.ErrConflict
	}
	var frames int64
	for _, t := range sequenceTracks(view.Sequence) {
		for _, c := range t.Clips {
			frames = max(frames, c.At+c.Duration)
		}
	}
	count := int64(math.Ceil(view.Sequence.FPS.Value() * 2))
	if req.StartFrame < 0 || req.StartFrame >= frames || count < 1 {
		return result, model.ErrArgument
	}
	start := req.StartFrame / count * count
	end := min(frames, start+count)
	sources, err := s.sequenceSources(ctx, projectID, view.Sequence)
	if err != nil {
		return result, err
	}
	key, selected := segmentInputs(view.Sequence, sources, start, end)
	var release func()
	for {
		release, err = s.acquireScript("segment:" + projectID + ":" + sequenceID + ":" + key)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	defer release()
	output := filepath.Join(s.workspace.root, "sequences", projectID, sequenceID, "segments", key)
	if raw, e := os.ReadFile(filepath.Join(output, "program.json")); e == nil {
		if json.Unmarshal(raw, &result.SequencePreview) == nil && result.ContentVersion == key {
			if digest, e := sequenceFileDigest(filepath.Join(output, "program.mp4")); e == nil && digest == result.SHA256 {
				result.Cached = true
			}
		}
	}
	if !result.Cached {
		raw, e := s.sequencePreviewRequest(ctx, sequenceID, view, map[string]any{"operation": "segment", "sequence": view.Sequence, "resources": selected, "output_dir": output, "content_version": key, "window": map[string]int64{"start": start, "end": end}, "preview": true})
		if e != nil {
			return result, e
		}
		if err = json.Unmarshal(raw, &result.SequencePreview); err != nil {
			return result, err
		}
		if result.ContentVersion != key || result.Frames != end-start {
			return result, model.ErrConflict
		}
	}
	current, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return result, err
	}
	if current.Session != view.Session || current.ContentVersion != view.ContentVersion {
		return result, model.ErrConflict
	}
	result.Key = key
	result.StartFrame = start
	result.ContentVersion = view.ContentVersion
	meta, _ := json.Marshal(map[string]int64{"start": start, "end": end})
	if err = os.WriteFile(filepath.Join(output, "window.json"), meta, 0600); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Service) SequenceSegmentPath(ctx context.Context, projectID, sequenceID, version, key string) (string, error) {
	view, err := s.Timeline(ctx, projectID, sequenceID)
	if err != nil {
		return "", err
	}
	if view.ContentVersion != version {
		return "", model.ErrConflict
	}
	if len(key) != 64 || strings.ContainsAny(key, "/\\.") {
		return "", model.ErrArgument
	}
	output := filepath.Join(s.workspace.root, "sequences", projectID, sequenceID, "segments", key)
	raw, err := os.ReadFile(filepath.Join(output, "window.json"))
	if err != nil {
		return "", model.ErrNotFound
	}
	var window struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	}
	if json.Unmarshal(raw, &window) != nil {
		return "", model.ErrConflict
	}
	sources, err := s.sequenceSources(ctx, projectID, view.Sequence)
	if err != nil {
		return "", err
	}
	current, _ := segmentInputs(view.Sequence, sources, window.Start, window.End)
	if current != key {
		return "", model.ErrConflict
	}
	path := filepath.Join(output, "program.mp4")
	if _, err = os.Stat(path); err != nil {
		return "", fmt.Errorf("segment missing: %w", model.ErrNotFound)
	}
	return path, nil
}
