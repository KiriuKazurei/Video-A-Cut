package service

import (
	"context"
	"strings"
)

// RuntimeActivity lets the desktop ask about exit without reading SQLite itself.
func (s *Service) RuntimeActivity(ctx context.Context) (int, error) {
	tasks, err := s.st.ListActiveTasks(ctx)
	if err != nil {
		return 0, err
	}
	previews, err := s.st.ListTTSPreviews(ctx)
	if err != nil {
		return 0, err
	}
	count := len(tasks)
	s.harnessMu.Lock(); for key := range s.harnessCancel { if strings.HasPrefix(key,"native:") || strings.HasPrefix(key,"proxy:") { count++ } }; s.harnessMu.Unlock()
	for _, preview := range previews {
		if preview.Status == "queued" || preview.Status == "running" {
			count++
		}
	}
	return count, nil
}
