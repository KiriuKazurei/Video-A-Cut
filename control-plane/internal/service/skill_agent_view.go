package service

import (
	"context"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

func (s *Service) ProjectAgentView(ctx context.Context, id, role string) (skill.VisibleSet, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return skill.VisibleSet{}, err
	}
	if !p.ExternalAuthorized {
		return skill.VisibleSet{}, model.ErrForbidden
	}
	for _, assetID := range p.Visible {
		a, e := s.st.GetAsset(ctx, assetID)
		if e != nil {
			return skill.VisibleSet{}, e
		}
		if !assetVisibleForRole(a, role) {
			return skill.VisibleSet{}, model.ErrForbidden
		}
	}
	v, err := s.ProjectVisibleSet(ctx, id)
	if err != nil {
		return v, err
	}
	if !v.Confirmed {
		return v, model.ErrForbidden
	}
	return v, nil
}
