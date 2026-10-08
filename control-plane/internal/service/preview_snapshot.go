package service

import (
	"context"
	"fmt"
	"os"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
)

type previewFileSnapshot struct {
	packageRef string
	path       string
	info       os.FileInfo
}

// Hash before acquiring SQLite's write lock. Keep the identity, size and mtime
// observed before hashing so a concurrent replacement or ordinary write fails
// the cheap recheck. This is not a content lock against hostile mtime restoration;
// the renderer and submit boundary still verify the complete fingerprints.
func (s *Service) verifyPreviewSnapshot(ctx context.Context, in preview.Input) ([]previewFileSnapshot, error) {
	var snapshot []previewFileSnapshot
	add := func(pkgRef, rel string) error {
		pkg, err := s.previewPackageDir(pkgRef)
		if err != nil {
			return err
		}
		_, info, err := s.previewPackageFile(pkg, rel)
		if err != nil {
			return err
		}
		snapshot = append(snapshot, previewFileSnapshot{pkgRef, rel, info})
		return nil
	}
	for _, file := range in.Files {
		if err := add(in.PackageRef, file.Path); err != nil {
			return nil, err
		}
	}
	if _, err := s.verifyPreviewInput(ctx, in); err != nil {
		return nil, err
	}
	if err := s.recheckPreviewSnapshot(ctx, snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *Service) recheckPreviewSnapshot(ctx context.Context, snapshot []previewFileSnapshot) error {
	for _, file := range snapshot {
		if err := ctx.Err(); err != nil {
			return err
		}
		pkg, err := s.previewPackageDir(file.packageRef)
		if err != nil {
			return err
		}
		_, now, err := s.previewPackageFile(pkg, file.path)
		if err != nil {
			return err
		}
		if !os.SameFile(file.info, now) || file.info.Size() != now.Size() || !file.info.ModTime().Equal(now.ModTime()) {
			return fmt.Errorf("preview input changed during validation: %s: %w", file.path, model.ErrConflict)
		}
	}
	return nil
}
