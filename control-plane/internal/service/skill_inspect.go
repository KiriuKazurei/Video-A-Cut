package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

func (s *Service) inspectSkillRender(ctx context.Context, r *skill.Run) (json.RawMessage, error) {
	if r.Plan == nil || r.Plan.FinalizedBy == "" {
		return nil, model.ErrForbidden
	}
	var source string
	var sourceSHA string
	var basicQuality json.RawMessage
	for _, a := range r.Artifacts {
		if a.Kind == "mp4" && a.PlanVersion == r.Plan.Version {
			p, _, err := s.skillArtifactPath(ctx, *r, a.ID)
			if err != nil {
				return nil, err
			}
			source = p
			sourceSHA = a.SHA
			basicQuality = a.BasicQuality
		}
	}
	if source == "" {
		return nil, model.ErrNotFound
	}
	dir, err := s.skillRunDir(*r)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(dir, newID("inspect"))
	if err = os.Mkdir(out, 0700); err != nil {
		return nil, err
	}
	cuts := []float64{}
	windows := editWindows(r.Plan)
	for i, window := range windows {
		if i > 0 {
			cuts = append(cuts, window.TimelineStart, windows[i-1].TimelineEnd)
		}
	}
	data, err := s.skillProcess(ctx, "inspect", map[string]any{"path": source, "output": out, "cuts": cuts, "max_frames": 48})
	if err != nil {
		return data, err
	}
	var report struct {
		Frames []string `json:"frames"`
	}
	if json.Unmarshal(data, &report) != nil || len(report.Frames) == 0 || len(report.Frames) > 48 {
		return nil, model.ErrInvalidState
	}
	inspection := &skill.Inspection{PlanVersion: r.Plan.Version, FrameIDs: []string{}, Verdict: "pending", Issues: []string{}}
	for _, name := range report.Frames {
		if filepath.Base(name) != name {
			return nil, model.ErrArgument
		}
		a, e := s.registerSkillArtifact(r, filepath.Join(out, name), name, "frame", r.Plan.Version)
		if e != nil {
			return nil, e
		}
		inspection.FrameIDs = append(inspection.FrameIDs, a.ID)
	}
	r.Inspection = inspection
	if !validBasicQuality(basicQuality, sourceSHA) {
		basicQuality = uncheckedBasicQuality(sourceSHA)
	}
	return json.Marshal(map[string]any{"inspection": inspection, "engineering": json.RawMessage(data), "basic_quality": basicQuality, "human_acceptance": "pending"})
}
