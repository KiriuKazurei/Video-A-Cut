package service

import (
	"context"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

func (s *Service) rememberResourceEdit(before *project.Resource, after project.Resource) {
	w := s.workspace
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.undo = append(w.undo[:w.undoCursor], resourceEdit{Before: before, After: after})
	w.undoCursor = len(w.undo)
}

// Resource history changes the document only. It never rewinds run budgets,
// provider activity or append-only operation records.
func (s *Service) moveResourceHistory(ctx context.Context, action string) (project.Session, error) {
	w := s.workspace
	w.mu.Lock()
	if w.document == nil || (action == "undo" && w.undoCursor == 0) || (action == "redo" && w.undoCursor == len(w.undo)) {
		w.mu.Unlock()
		return s.ProjectWorkspace(ctx)
	}
	index := w.undoCursor
	if action == "undo" {
		index--
	}
	edit := w.undo[index]
	target := edit.After
	if action == "undo" {
		if edit.Before != nil {
			target = *edit.Before
		} else {
			target.Removed, target.Missing = true, true
		}
	}
	var previous project.Resource
	for _, resource := range w.document.Resources {
		if resource.ID == target.ID {
			previous = resource
		}
	}
	if previous.ID == "" {
		w.mu.Unlock()
		return project.Session{}, model.ErrInvalidState
	}
	w.mu.Unlock()
	contentChanged := previous.Source != target.Source || previous.Removed != target.Removed || project.ResourceContentVersion(previous) != project.ResourceContentVersion(target)
	if contentChanged && target.AssetID != "" {
		if err := s.materializeResource(ctx, target, s.CurrentProjectID()); err != nil {
			return project.Session{}, err
		}
	}
	w.mu.Lock()
	for i := range w.document.Resources {
		if w.document.Resources[i].ID == target.ID {
			w.document.Resources[i] = target
		}
	}
	if action == "undo" {
		w.undoCursor--
	} else {
		w.undoCursor++
	}
	w.mu.Unlock()
	if contentChanged {
		if err := s.refreshProjectMedia(ctx); err != nil {
			return project.Session{}, err
		}
		if project.ResourceContentVersion(previous) != project.ResourceContentVersion(target) || previous.Removed != target.Removed {
			s.cancelSkillExecutions(ctx, func(run skill.Run) bool {
				return run.ProjectID == s.CurrentProjectID() && s.skillRunUsesResource(ctx, run, target.ID)
			})
		}
	}
	s.publish("resource.changed", map[string]any{"project_id": s.CurrentProjectID(), "resource_id": target.ID})
	return s.ProjectWorkspace(ctx)
}
