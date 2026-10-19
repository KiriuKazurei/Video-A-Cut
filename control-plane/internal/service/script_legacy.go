package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// LegacyScriptInput is additive and omitted for pre-script workflow runs.
// Sources are delivery-root references supplied by Go, never model paths.
type LegacyScriptInput struct {
	ID        string            `json:"script_id"`
	Version   int               `json:"version"`
	SHA       string            `json:"visible_set_sha"`
	Workbench ScriptWorkbench   `json:"workbench"`
	Media     []ScriptMedia     `json:"media"`
	Sources   map[string]string `json:"sources"`
}

func (s *Service) legacyScriptInput(ctx context.Context, st *store.Store, runID string) (*LegacyScriptInput, error) {
	a, err := st.GetWorkflowAuto(ctx, runID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if a.Scope.ScriptID == "" {
		return nil, nil
	}
	w, err := workbenchAt(ctx, st, a.Scope.ScriptID)
	if err != nil {
		return nil, err
	}
	if w.Version != a.Scope.ScriptVersion || w.Script.Status != model.ScriptFinalized || w.Script.VisibleSetSHA != a.Scope.VisibleSetSHA {
		return nil, model.ErrConflict
	}
	return &LegacyScriptInput{ID: w.Script.ScriptID, Version: w.Version, SHA: w.Script.VisibleSetSHA, Workbench: w}, nil
}
func (s *Service) boundLegacyScript(ctx context.Context, runID string) (*LegacyScriptInput, error) {
	in, err := s.legacyScriptInput(ctx, s.st, runID)
	if err != nil || in == nil {
		return in, err
	}
	media, err := s.ScriptMediaCatalogue(ctx)
	if err != nil {
		return nil, err
	}
	if err = validateScriptPlan(in.Workbench, media, true); err != nil {
		return nil, err
	}
	in.Media = media
	in.Sources = map[string]string{}
	for _, m := range media {
		in.Sources[m.ID] = m.Source
	}
	for _, seg := range in.Workbench.Segments {
		if scriptOption(seg.TTSEnabled, in.Workbench.TTSEnabled) && seg.Narration != "" && seg.Voice != nil && seg.Voice.Settings.External() && !seg.Voice.AcceptExternal {
			return nil, fmt.Errorf("script voice requires external consent: %w", model.ErrForbidden)
		}
	}
	return in, nil
}
