package service

import (
	"context"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"os"
	"path/filepath"
)

func (s *Service) WorkflowEvidencePath(ctx context.Context, runID, revisionID, key string) (string, error) {
	run, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return "", err
	}
	if revisionID != run.CurrentRevisionID {
		return "", fmt.Errorf("evidence revision is not current: %w", model.ErrConflict)
	}
	rev, err := s.st.GetRevision(ctx, revisionID)
	if err != nil {
		return "", err
	}
	edl, err := s.readRevision(rev)
	if err != nil {
		return "", err
	}
	files, err := s.evidenceFiles(rev.PackageRef, edl)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if file["key"] == key {
			path, _, err := s.deliveryPath(filepath.FromSlash(rev.PackageRef+"/"+file["path"]), false)
			return path, err
		}
	}
	return "", fmt.Errorf("evidence key missing: %w", model.ErrNotFound)
}

func (s *Service) WorkflowPackageFiles(ctx context.Context, runID string) (map[string]string, error) {
	run, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != model.WorkflowReadyAcceptance {
		return nil, fmt.Errorf("no final package: %w", model.ErrInvalidState)
	}
	rev, err := s.st.GetRevision(ctx, run.CurrentRevisionID)
	if err != nil {
		return nil, err
	}
	edl, err := s.readRevision(rev)
	if err != nil {
		return nil, err
	}
	if _, err := s.evidenceFiles(rev.PackageRef, edl); err != nil {
		return nil, err
	}
	artifacts, err := s.validatePackage(filepath.FromSlash(rev.PackageRef))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	base, _, err := s.deliveryPath(filepath.FromSlash(rev.PackageRef), true)
	if err != nil {
		return nil, err
	}
	for _, rel := range artifacts {
		path, _, err := s.deliveryPath(filepath.FromSlash(rel), false)
		if err != nil {
			return nil, err
		}
		name, err := filepath.Rel(base, path)
		if err != nil || !filepath.IsLocal(name) {
			return nil, model.ErrForbidden
		}
		out[filepath.ToSlash(name)] = path
	}
	return out, nil
}

// Called before HTTP starts. Unregistered snapshots are retained in quarantine;
// a process crash must never make an orphan look like an accepted revision.
func (s *Service) RecoverRevisionFiles(ctx context.Context) error {
	if s.deliveryRoot == "" {
		return nil
	}
	known, err := s.st.RevisionPackages(ctx)
	if err != nil {
		return err
	}
	root := filepath.Join(s.deliveryRoot, "revisions")
	assets, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if !asset.IsDir() {
			continue
		}
		dir := filepath.Join(root, asset.Name())
		if _, _, err := s.deliveryPath(filepath.Join("revisions", asset.Name()), true); err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			rel := "revisions/" + asset.Name() + "/" + entry.Name()
			if known[rel] {
				continue
			}
			source, _, err := s.deliveryPath(filepath.FromSlash(rel), true)
			if err != nil {
				return err
			}
			dest := filepath.Join(s.deliveryRoot, ".recovery", newID("orphan"))
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			if _, _, err := s.deliveryPath(".recovery", true); err != nil {
				return err
			}
			if err := os.Rename(source, dest); err != nil {
				return err
			}
			s.audit(ctx, "system", "revision.quarantine", rel, "retained at "+filepath.Base(dest))
		}
	}
	return nil
}
