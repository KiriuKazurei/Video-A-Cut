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

type drawImageAsset struct {
	BindingSHA string `json:"binding_sha256,omitempty"`
	ResourceID string `json:"resource_id"`
	SHA        string `json:"sha256"`
	Path       string `json:"path"`
}
type DrawImageResource struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	SHA      string         `json:"sha256"`
	Metadata map[string]any `json:"metadata"`
	Missing  bool           `json:"missing"`
	Changed  bool           `json:"changed"`
}

func (s *Service) DrawImageResources(projectID string) ([]DrawImageResource, error) {
	resources, err := s.ProjectResources(projectID)
	if err != nil {
		return nil, err
	}
	out := []DrawImageResource{}
	for _, r := range resources {
		if r.Kind == "image" && !r.Removed {
			metadata := map[string]any{}
			for _, key := range []string{"width", "height", "codec_name", "pix_fmt", "extension", "mime_type"} {
				if value, ok := r.Metadata[key]; ok {
					metadata[key] = value
				}
			}
			out = append(out, DrawImageResource{r.ID, r.Name, project.ResourceContentVersion(r), metadata, r.Missing, r.Changed})
		}
	}
	return out, nil
}

// Private filesystem inputs are resolved by Go from stable current-project IDs;
// neither a draw source nor an Agent supplies a media path.
func (s *Service) drawImageAssets(ctx context.Context, projectID string, c project.DrawComposition) ([]drawImageAsset, error) {
	assets := []drawImageAsset{}
	seen := map[string]bool{}
	for _, layer := range c.ImageBindings() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r, err := s.ProjectResource(projectID, layer.ResourceID)
		if err != nil {
			return nil, err
		}
		frozenRAW := false
		if c.Document != nil && project.IsCameraRaw(r.Source.OriginalPath) {
			for _, b := range c.Document.AssetBindings {
				if b.ResourceID == r.ID && b.SHA == layer.ResourceSHA && b.OriginalSHA == r.Source.SHA256 && b.RawDevelop != nil {
					if err = b.RawDevelop.Validate(); err != nil {
						return nil, err
					}
					recipe := *b.RawDevelop
					r.RawDevelop = &recipe
					frozenRAW = true
					break
				}
			}
		}
		if r.Kind != "image" || r.Removed || r.Missing || r.Changed || layer.ResourceSHA != project.ResourceContentVersion(r) {
			return nil, fmt.Errorf("图片资源 %s 缺失、移除或发生变化，请重新关联并保存构图: %w", layer.ResourceID, model.ErrConflict)
		}
		if seen[r.ID] {
			continue
		}
		if len(assets) >= 32 {
			return nil, fmt.Errorf("每个构图最多使用 32 份图片原件: %w", model.ErrArgument)
		}
		if project.IsCameraRaw(r.Source.OriginalPath) {
			sha, e := sequenceFileDigest(r.Source.OriginalPath)
			if e != nil || sha != r.Source.SHA256 {
				return nil, model.ErrConflict
			}
		}
		var path string
		if frozenRAW {
			path, err = s.drawingRawSnapshotPath(ctx, r)
		} else {
			path, err = s.imageRenderPath(ctx, projectID, r)
		}
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, model.ErrConflict
		}
		sha, err := sequenceFileDigest(path)
		if err != nil {
			return nil, err
		}
		if !project.IsCameraRaw(r.Source.OriginalPath) && sha != layer.ResourceSHA {
			return nil, fmt.Errorf("图片内容已改变，请重新关联: %w", model.ErrConflict)
		}
		seen[r.ID] = true
		assets = append(assets, drawImageAsset{BindingSHA: layer.ResourceSHA, ResourceID: r.ID, SHA: sha, Path: path})
	}
	return assets, nil
}

func (s *Service) drawingRawSnapshotPath(ctx context.Context, r project.Resource) (string, error) {
	dir := filepath.Join(s.workspace.root, "previews")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	path := filepath.Join(dir, project.ResourceContentVersion(r)+".png")
	for {
		if _, e := os.Stat(path); e == nil {
			return path, nil
		}
		release, e := s.acquireScript("raw-snapshot:" + path)
		if e != nil {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(20 * time.Millisecond):
				continue
			}
		}
		file, e := os.CreateTemp(dir, "raw-snapshot-*.png")
		if e != nil {
			release()
			return "", e
		}
		temp := file.Name()
		_ = file.Close()
		_, e = s.rawImageProcess(ctx, "develop", r.Source.OriginalPath, temp, r.RawDevelop)
		if e == nil {
			e = os.Rename(temp, path)
		}
		_ = os.Remove(temp)
		release()
		return path, e
	}
}
