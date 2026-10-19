package service

import (
	"context"
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

func (s *Service) cancelSkillExecutions(ctx context.Context, match func(skill.Run) bool) {
	rows, err := s.st.SkillList(ctx, "run")
	if err != nil {
		return
	}
	s.harnessMu.Lock()
	defer s.harnessMu.Unlock()
	for _, row := range rows {
		var r skill.Run
		if json.Unmarshal(row, &r) != nil || !match(r) {
			continue
		}
		for _, id := range []string{r.ID, "agent:" + r.ID} {
			if cancel := s.harnessCancel[id]; cancel != nil {
				cancel()
			}
		}
	}
}
