package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

func (s *Service) drawingResourceSnapshot(projectID string, r project.Resource) (project.DrawComposition, error) {
	if r.Draw == nil || r.Removed || r.Missing {
		return project.DrawComposition{}, model.ErrNotFound
	}
	c := r.Draw.Snapshot
	if r.Draw.Mode == "follow" {
		return s.drawComposition(projectID, r.Draw.CompositionID)
	}
	return c, nil
}
func (s *Service) checkDrawingResourceSnapshot(ctx context.Context, projectID string, r project.Resource, c project.DrawComposition) error {
	current, e := s.ProjectResource(projectID, r.ID)
	if e != nil {
		return e
	}
	now, e := s.drawingResourceSnapshot(projectID, current)
	if e != nil {
		return e
	}
	if now.ContentVersion != c.ContentVersion {
		return model.ErrConflict
	}
	_, e = s.drawImageAssets(ctx, projectID, c)
	return e
}
func (s *Service) DrawResourceThumbnail(ctx context.Context, projectID string, r project.Resource, at float64) (string, error) {
	c, e := s.drawingResourceSnapshot(projectID, r)
	if e != nil {
		return "", e
	}
	assets, e := s.drawImageAssets(ctx, projectID, c)
	if e != nil {
		return "", e
	}
	frame := c.FPS.Frames(at)
	if frame < 0 || frame >= c.DurationFrames {
		return "", model.ErrArgument
	}
	directory := s.drawDir(projectID, r.ID, c.ContentVersion+drawingEnvironmentVersion()+fmt.Sprint(frame))
	path := filepath.Join(directory, "frame.png")
	if _, e = os.Stat(path); e == nil {
		return path, s.checkDrawingResourceSnapshot(ctx, projectID, r, c)
	}
	release, e := s.acquireScript("draw-thumbnail:" + directory)
	if e != nil {
		return "", e
	}
	defer release()
	doc := c.UnifiedDocument()
	_, e = s.scriptProcess(ctx, true, "draw-source-frame", map[string]any{"document": doc, "assets": assets, "output_dir": directory, "frame": frame, "scale": 160 / float64(c.Width)})
	if e != nil {
		return "", e
	}
	return path, s.checkDrawingResourceSnapshot(ctx, projectID, r, c)
}

// Source monitor compatibility proxy, produced lazily. Source insertion and
// editing/export never depend on this optional, disposable MP4.
func (s *Service) DrawResourcePreview(ctx context.Context, projectID string, r project.Resource) (string, error) {
	c, e := s.drawingResourceSnapshot(projectID, r)
	if e != nil {
		return "", e
	}
	assets, e := s.drawImageAssets(ctx, projectID, c)
	if e != nil {
		return "", e
	}
	directory := s.drawDir(projectID, r.ID, c.ContentVersion+drawingEnvironmentVersion()+"source-monitor")
	path := filepath.Join(directory, "program.mp4")
	for {
		if _, e = os.Stat(path); e == nil {
			return path, s.checkDrawingResourceSnapshot(ctx, projectID, r, c)
		}
		release, err := s.acquireScript("draw-source-monitor:" + directory)
		if err != nil {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(20 * time.Millisecond):
				continue
			}
		}
		doc := c.UnifiedDocument()
		c.Document = &doc
		c.NormalizeDocument()
		_, e = s.scriptProcess(ctx, true, "draw-render", map[string]any{"composition": c, "assets": assets, "output_dir": directory})
		release()
		if e != nil {
			return "", e
		}
		return path, s.checkDrawingResourceSnapshot(ctx, projectID, r, c)
	}
}
