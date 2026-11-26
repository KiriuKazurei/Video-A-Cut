package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

func sequenceProgramStrategy(contentVersion string) string {
	return "sequence:" + contentVersion
}

func sequenceProgramFrameIDs(run *skill.Run, contentVersion string) []string {
	want := sequenceProgramStrategy(contentVersion)
	ids := []string{}
	for _, frame := range run.Frames {
		if frame.Strategy == want {
			ids = append(ids, frame.ID)
		}
	}
	return ids
}

func sequenceEndFrame(seq project.Sequence) int64 {
	var end int64
	for _, track := range sequenceTracks(seq) {
		for _, clip := range track.Clips {
			if clip.At+clip.Duration > end {
				end = clip.At + clip.Duration
			}
		}
	}
	return end
}

func (s *Service) sequenceMediaAllowed(projectID string, seq project.Sequence, snap harnessSnapshot) error {
	allowed := map[string]bool{}
	for _, item := range snap.Media {
		if item.ID != "" {
			allowed[item.ID] = true
		}
		if item.AssetID != "" {
			allowed[item.AssetID] = true
		}
	}
	for _, track := range sequenceTracks(seq) {
		for _, clip := range track.Clips {
			if clip.ResourceID == "" {
				continue
			}
			resource, err := s.ProjectResource(projectID, clip.ResourceID)
			if err != nil {
				return err
			}
			if resource.Draw != nil || (resource.Kind != "video" && resource.Kind != "audio") {
				continue
			}
			if !allowed[resource.ID] && !allowed[resource.AssetID] {
				return fmt.Errorf("sequence uses media outside the frozen visible set: %w", model.ErrForbidden)
			}
		}
	}
	return nil
}

// inspectSequenceFrames returns composed program frames for the run's sequence.
// Source frames and a single drawing canvas are not a substitute: subtitles,
// effects and stacked tracks are already flattened into each PNG.
func (s *Service) inspectSequenceFrames(ctx context.Context, r *skill.Run, snap harnessSnapshot, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		SequenceID     string  `json:"sequence_id"`
		Revision       *uint64 `json:"sequence_revision"`
		ContentVersion string  `json:"content_version"`
		Frames         []int64 `json:"frames"`
	}
	if err := decodeSkillArgs(args, &req); err != nil {
		return nil, err
	}
	if req.SequenceID == "" {
		req.SequenceID = r.SequenceID
	}
	if r.SequenceID == "" || req.SequenceID == "" {
		return nil, model.ErrArgument
	}
	if req.SequenceID != r.SequenceID || r.ProjectID == "" || r.ProjectID != s.CurrentProjectID() {
		return nil, model.ErrForbidden
	}
	if len(req.Frames) == 0 || len(req.Frames) > 48 {
		return nil, model.ErrArgument
	}
	seen := map[int64]bool{}
	for _, frame := range req.Frames {
		if frame < 0 || seen[frame] {
			return nil, model.ErrArgument
		}
		seen[frame] = true
	}
	view, err := s.Timeline(ctx, r.ProjectID, req.SequenceID)
	if err != nil {
		return nil, err
	}
	if req.Revision != nil && *req.Revision != view.Sequence.Revision {
		return nil, model.ErrConflict
	}
	if req.ContentVersion != "" && req.ContentVersion != view.ContentVersion {
		return nil, model.ErrConflict
	}
	end := sequenceEndFrame(view.Sequence)
	for _, frame := range req.Frames {
		if frame >= end {
			return nil, model.ErrArgument
		}
	}
	if err = s.sequenceMediaAllowed(r.ProjectID, view.Sequence, snap); err != nil {
		return nil, err
	}
	if r.FramesSpent+len(req.Frames) > r.Budget.MaxFrames {
		return nil, errFrameBudget
	}
	dir, err := s.skillRunDir(*r)
	if err != nil {
		return nil, err
	}
	staging := filepath.Join(dir, "sequence-frames")
	if err = os.MkdirAll(staging, 0700); err != nil {
		return nil, err
	}
	frames := []skill.Frame{}
	artifacts := []skill.Artifact{}
	for _, index := range req.Frames {
		preview, err := s.CreateSequenceFrame(ctx, r.ProjectID, req.SequenceID, SequenceFrameRequest{
			Session: view.Session, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion, Frame: index,
		})
		if err != nil {
			return nil, err
		}
		if preview.ContentVersion != view.ContentVersion || preview.Frame != index {
			return nil, model.ErrConflict
		}
		encoded, ok := strings.CutPrefix(preview.Image, "data:image/png;base64,")
		if !ok {
			return nil, model.ErrInvalidState
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(raw) < 8 || string(raw[:8]) != "\x89PNG\r\n\x1a\n" {
			return nil, model.ErrInvalidState
		}
		path := filepath.Join(staging, fmt.Sprintf("frame-%d.png", index))
		if err = os.WriteFile(path, raw, 0600); err != nil {
			return nil, err
		}
		artifact, err := s.registerSkillArtifact(r, path, fmt.Sprintf("sequence-%d.png", index), "frame", 0)
		if err != nil {
			return nil, err
		}
		frame := skill.Frame{
			ID: artifact.ID, MediaID: req.SequenceID, PTS: view.Sequence.FPS.Seconds(index),
			Score: 1, Strategy: sequenceProgramStrategy(view.ContentVersion), SHA: artifact.SHA,
		}
		frames = append(frames, frame)
		artifacts = append(artifacts, artifact)
		r.Frames = append(r.Frames, frame)
	}
	r.FramesSpent += len(frames)
	return json.Marshal(map[string]any{
		"sequence_id": req.SequenceID, "sequence_revision": view.Sequence.Revision,
		"content_version": view.ContentVersion, "frames": frames, "artifacts": artifacts,
	})
}
