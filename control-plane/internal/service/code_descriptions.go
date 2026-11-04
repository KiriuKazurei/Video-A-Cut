package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

func (s *Service) codeRunSources(r skill.Run) map[string]project.DrawComposition {
	out := map[string]project.DrawComposition{}
	if s.workspace == nil || len(r.CodeTargets) == 0 {
		return out
	}
	w := s.workspace
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.document == nil || w.document.Project.ID != r.ProjectID {
		return out
	}
	for _, sequence := range w.document.Sequences {
		if sequence.ID != r.SequenceID {
			continue
		}
		for _, track := range sequence.Tracks {
			for _, clip := range track.Clips {
				if _, ok := r.CodeTargets[clip.ID]; !ok {
					continue
				}
				for _, resource := range w.document.Resources {
					if resource.ID != clip.ResourceID || resource.Draw == nil {
						continue
					}
					source := resource.Draw.Snapshot
					if resource.Draw.Mode == "follow" {
						for _, composition := range w.document.DrawCompositions {
							if composition.ID == resource.Draw.CompositionID {
								source = composition
							}
						}
					}
					out[clip.ID] = source
				}
			}
		}
	}
	// The HTTP/model consumer owns this snapshot; never share mutable source maps.
	raw, _ := json.Marshal(out)
	_ = json.Unmarshal(raw, &out)
	return out
}

// Schedule only missing descriptions. Code remains usable while the existing
// Agent runtime queues/executes this scoped task. No separate model client.
func (s *Service) scheduleCodeDescriptions(projectID, sequenceID string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		view, err := s.Timeline(ctx, projectID, sequenceID)
		if err != nil {
			return
		}
		targets := map[string]string{}
		descriptions := []map[string]any{}
		for _, t := range view.Sequence.Tracks {
			if t.Kind != "code" || t.Locked {
				continue
			}
			for _, c := range t.Clips {
				if c.Code != nil && c.Code.Description == "" && c.Code.RunID == "" && c.Code.DescriptionState != "failed" && len(targets) < 32 {
					targets[c.ID] = c.Code.ContentVersion
					descriptions = append(descriptions, map[string]any{"clip_id": c.ID, "resource_id": c.ResourceID, "code_revision": c.Code.Revision})
				}
			}
		}
		if len(targets) == 0 {
			return
		}
		visible, err := s.ProjectVisibleSet(ctx, projectID)
		if err != nil {
			s.failCodeDescriptions(projectID, sequenceID, view.Session, targets)
			return
		}
		raw, _ := json.Marshal(descriptions)
		request := skill.RunRequest{ProjectID: projectID, SequenceID: sequenceID, Flow: "custom", SkillID: "vac-draw-pipeline", Strategy: "draw", Mode: "external_mcp", VisibleSHA: visible.SHA, CodeTargets: targets, CodeAction: "describe", Goal: fmt.Sprintf("补齐以下代码条的自然语言画面描述：%s。先timeline.read及resources.read读取源码；每条单独使用timeline.edit提交code.script，mode=describe、clip_ids、code_revision和description。不得更改画面、参数或其他条。失败条保留，继续其他条，最后run.finish。", raw), IdempotencyKey: "code-describe:" + skillHash([]any{view.Session, targets})[:32]}
		profiles, _ := s.ListProcessingProfiles(ctx, 100, 0)
		for _, p := range profiles {
			if p.ContentMode == "configured" && p.Narration.Endpoint != "" {
				request.Mode = "builtin_api"
				request.ProfileID = p.ProfileID
				request.ProfileRevision = p.Revision
				break
			}
		}
		run, err := s.CreateSkillRun(ctx, "human:desktop", request)
		if err != nil {
			s.failCodeDescriptions(projectID, sequenceID, view.Session, targets)
			s.publish("workspace.code.description.failed", map[string]any{"project_id": projectID, "sequence_id": sequenceID, "message": err.Error()})
			return
		}
		for attempt := 0; attempt < 3; attempt++ {
			latest, e := s.Timeline(ctx, projectID, sequenceID)
			if e != nil || latest.Session != view.Session {
				return
			}
			operations := []project.Operation{}
			for _, t := range latest.Sequence.Tracks {
				for _, c := range t.Clips {
					if expected, ok := targets[c.ID]; ok && c.Code.ContentVersion == expected && c.Code.RunID == "" {
						operations = append(operations, project.Operation{Type: "code.job", ClipIDs: []string{c.ID}, CodeRunID: run.ID, Unlinked: true})
					}
				}
			}
			if len(operations) == 0 {
				return
			}
			_, err = s.TimelineCommand(ctx, "human:desktop", projectID, sequenceID, project.Command{Session: view.Session, ExpectedRevision: latest.Sequence.Revision, IdempotencyKey: "code-job:" + run.ID, Label: "补齐代码条描述", Operations: operations})
			if err == nil {
				break
			}
		}
		if err == nil && request.Mode == "builtin_api" {
			go func() { _, _ = s.RunBuiltinAgent(context.Background(), "human:desktop", run.ID, run.Version) }()
		}
		if err == nil {
			s.scheduleCodeDescriptions(projectID, sequenceID)
		}
	}()
}

func (s *Service) failCodeDescriptions(projectID, sequenceID, session string, targets map[string]string) {
	if s.workspace == nil {
		return
	}
	w := s.workspace
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.lockMutation(ctx); err != nil {
		return
	}
	defer w.op.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.document == nil || w.document.Project.ID != projectID || w.session != session {
		return
	}
	i := sequenceIndex(w.document, sequenceID)
	if i < 0 {
		return
	}
	changed := false
	for ti := range w.document.Sequences[i].Tracks {
		for ci := range w.document.Sequences[i].Tracks[ti].Clips {
			c := &w.document.Sequences[i].Tracks[ti].Clips[ci]
			if expected, ok := targets[c.ID]; ok && c.Code != nil && c.Code.Description == "" && c.Code.RunID == "" && project.CodeTargetVersion(w.document, *c) == expected {
				c.Code.DescriptionState = "failed"
				c.Code.DescriptionError = "描述任务未能创建，请配置Agent后重试"
				changed = true
			}
		}
	}
	if changed {
		w.dirty = true
	}
}
