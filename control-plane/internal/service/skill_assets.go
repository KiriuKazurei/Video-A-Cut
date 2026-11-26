package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

// errImportApproval pauses the run until a person imports the file or places it
// on the configured allow-list. It is not a revoked grant.
var errImportApproval = fmt.Errorf("asset import requires user approval: %w", model.ErrInvalidState)

var libraryEntryID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// resolveBuiltinLibrary is the extension point for a published library entry.
// A future catalog resolves id/version to one trusted file and the caller
// imports that path with the same hash dedup and visible-set update.
func (s *Service) resolveBuiltinLibrary(id, version string) (string, error) {
	if !libraryEntryID.MatchString(id) || (version != "" && !libraryEntryID.MatchString(version)) {
		return "", model.ErrArgument
	}
	return "", fmt.Errorf("builtin library entry %q is not published: %w", id, model.ErrArgument)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err = io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func resourceByPath(list []project.Resource, path string) (project.Resource, bool) {
	for _, item := range list {
		if !item.Removed && strings.EqualFold(item.Source.OriginalPath, path) {
			return item, true
		}
	}
	return project.Resource{}, false
}

func resourceBySHA(list []project.Resource, sha string) (project.Resource, bool) {
	if sha == "" {
		return project.Resource{}, false
	}
	for _, item := range list {
		if !item.Removed && strings.EqualFold(item.Source.SHA256, sha) {
			return item, true
		}
	}
	return project.Resource{}, false
}

// allowlistedImportPath accepts a regular file inside a configured ingest root.
// A path that resolves into a root only by crossing a symlink is rejected.
func (s *Service) allowlistedImportPath(abs string) (string, bool, error) {
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false, err
	}
	for _, root := range s.ingest.roots {
		base, err := filepath.EvalSymlinks(root.Path)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(base, real)
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			continue
		}
		source, err := ingest.ResolveSource(root, filepath.ToSlash(rel))
		if err != nil {
			return "", false, err
		}
		return source.Path, true, nil
	}
	return "", false, nil
}

func (s *Service) freezeRunMedia(ctx context.Context, r *skill.Run, resource project.Resource) error {
	if resource.Kind != "video" && resource.Kind != "audio" || resource.AssetID == "" {
		return nil
	}
	var snap harnessSnapshot
	if err := s.st.SkillSnapshot(ctx, r.ID, &snap); err != nil {
		return err
	}
	present := false
	for _, item := range snap.Media {
		if item.ID == resource.ID {
			present = true
		}
	}
	if !present {
		view, err := s.ProjectVisibleSet(ctx, r.ProjectID)
		if err != nil {
			return err
		}
		var found *skill.Media
		for i := range view.Media {
			if view.Media[i].ID == resource.ID {
				found = &view.Media[i]
				break
			}
		}
		if found == nil {
			return model.ErrInvalidState
		}
		snap.Media = append(snap.Media, *found)
		if snap.Paths == nil {
			snap.Paths = map[string]string{}
		}
		snap.Paths[found.ID] = "@resource/" + found.ID
		if err = s.st.ReplaceSkillSnapshot(ctx, r.ID, snap); err != nil {
			return err
		}
	}
	p, err := s.GetProject(ctx, r.ProjectID)
	if err != nil {
		return err
	}
	ids := []string{}
	for _, item := range snap.Media {
		if item.AssetID != "" {
			ids = append(ids, item.AssetID)
		}
	}
	slices.Sort(ids)
	p.Visible = ids
	r.VisibleSHA = projectSHA(p, snap.Media)
	return nil
}

func skillAssetView(resource project.Resource, imported, deduped bool, visibleSHA string) map[string]any {
	return map[string]any{
		"imported": imported, "deduped": deduped, "visible_set_sha": visibleSHA,
		"resource": map[string]any{
			"id": resource.ID, "asset_id": resource.AssetID, "kind": resource.Kind,
			"name": resource.Name, "sha256": resource.Source.SHA256, "in": resource.In, "out": resource.Out,
		},
	}
}

func (s *Service) importAuthorizedFile(ctx context.Context, r *skill.Run, abs string) (map[string]any, error) {
	list, err := s.ProjectResources(r.ProjectID)
	if err != nil {
		return nil, err
	}
	if existing, ok := resourceByPath(list, abs); ok {
		if err = s.freezeRunMedia(ctx, r, existing); err != nil {
			return nil, err
		}
		return skillAssetView(existing, false, true, r.VisibleSHA), nil
	}
	sha, err := fileSHA256(abs)
	if err != nil {
		return nil, err
	}
	if existing, ok := resourceBySHA(list, sha); ok {
		if err = s.freezeRunMedia(ctx, r, existing); err != nil {
			return nil, err
		}
		return skillAssetView(existing, false, true, r.VisibleSHA), nil
	}
	if _, err = s.ImportProjectResource(ctx, r.ProjectID, abs); err != nil {
		return nil, err
	}
	list, err = s.ProjectResources(r.ProjectID)
	if err != nil {
		return nil, err
	}
	created, ok := resourceBySHA(list, sha)
	if !ok {
		created, ok = resourceByPath(list, abs)
	}
	if !ok {
		return nil, model.ErrInvalidState
	}
	if err = s.freezeRunMedia(ctx, r, created); err != nil {
		return nil, err
	}
	return skillAssetView(created, true, false, r.VisibleSHA), nil
}

// importSkillAsset reuses project import. Configured ingest roots and files
// already in the project are covered by the existing authorization. Any other
// path stops at a decision and does not copy the file. library_id is reserved
// for a future built-in catalog entry.
func (s *Service) importSkillAsset(ctx context.Context, r *skill.Run, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Path           string `json:"path"`
		RootID         string `json:"root_id"`
		RelativePath   string `json:"relative_path"`
		LibraryID      string `json:"library_id"`
		LibraryVersion string `json:"library_version"`
	}
	if err := decodeSkillArgs(args, &req); err != nil {
		return nil, err
	}
	if !s.WorkspaceEnabled() || r.ProjectID == "" || r.ProjectID != s.CurrentProjectID() {
		return nil, model.ErrForbidden
	}
	modes := 0
	if req.Path != "" {
		modes++
	}
	if req.RootID != "" || req.RelativePath != "" {
		modes++
	}
	if req.LibraryID != "" || req.LibraryVersion != "" {
		modes++
	}
	if modes != 1 {
		return nil, model.ErrArgument
	}
	abs := ""
	switch {
	case req.LibraryID != "" || req.LibraryVersion != "":
		if req.LibraryID == "" {
			return nil, model.ErrArgument
		}
		trusted, err := s.resolveBuiltinLibrary(req.LibraryID, req.LibraryVersion)
		if err != nil {
			return nil, err
		}
		abs = trusted
	case req.RootID != "" || req.RelativePath != "":
		if req.RootID == "" || req.RelativePath == "" {
			return nil, model.ErrArgument
		}
		root, err := ingest.FindRoot(s.ingest.roots, req.RootID)
		if err != nil {
			return nil, err
		}
		source, err := ingest.ResolveSource(root, req.RelativePath)
		if err != nil {
			return nil, err
		}
		abs = source.Path
	default:
		if !filepath.IsAbs(req.Path) {
			return nil, model.ErrArgument
		}
		var err error
		abs, err = filepath.Abs(req.Path)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("import file does not exist: %w", model.ErrNotFound)
			}
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("import path must not be a symlink: %w", model.ErrForbidden)
		}
		if !info.Mode().IsRegular() {
			return nil, model.ErrArgument
		}
		list, err := s.ProjectResources(r.ProjectID)
		if err != nil {
			return nil, err
		}
		if existing, ok := resourceByPath(list, abs); ok {
			if err = s.freezeRunMedia(ctx, r, existing); err != nil {
				return nil, err
			}
			return json.Marshal(skillAssetView(existing, false, true, r.VisibleSHA))
		}
		allowed, ok, err := s.allowlistedImportPath(abs)
		if err != nil {
			return nil, err
		}
		if !ok {
			decision, err := json.Marshal(map[string]any{
				"imported": false, "deduped": false,
				"decision": map[string]any{
					"kind": "approve_asset_import", "code": "outside_authorized_scope",
					"name": filepath.Base(abs), "bytes": info.Size(),
					"message": "该文件不在已授权的工程素材或导入白名单内。请用户在 VAC 资源工作区导入，或把文件放入已配置的导入根目录后，用新的幂等键重试。",
				},
			})
			if err != nil {
				return nil, err
			}
			return decision, fmt.Errorf("outside_authorized_scope: %w", errImportApproval)
		}
		abs = allowed
	}
	view, err := s.importAuthorizedFile(ctx, r, abs)
	if err != nil {
		return nil, err
	}
	if req.LibraryID != "" {
		view["library_id"] = req.LibraryID
		if req.LibraryVersion != "" {
			view["library_version"] = req.LibraryVersion
		}
	}
	return json.Marshal(view)
}
