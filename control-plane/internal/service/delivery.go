package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

const maxManifestBytes = 1 << 20
const maxDeliveryFiles = 512

var deliveryKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)

// DeliveryFile is safe to return to a browser. It deliberately contains no
// filesystem path; the browser can address a file only by its registered key.
type DeliveryFile struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	PackagePath string `json:"package_path"`
	Mime        string `json:"mime"`
	Size        int64  `json:"size"`
	Playable    bool   `json:"playable"`
}

type deliveryManifest struct {
	SchemaVersion int `json:"schema_version"`
	Artifacts     []struct {
		Kind string `json:"kind"`
		Path string `json:"path"`
	} `json:"artifacts"`
}

// ConfigureDeliveryRoot is called once at startup. An empty root keeps file
// delivery disabled; a configured root must already exist and be a directory.
func (s *Service) ConfigureDeliveryRoot(root string) error {
	if root == "" {
		s.deliveryRoot = ""
		return nil
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("root must be absolute: %w", model.ErrArgument)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("root must be an existing directory: %w", model.ErrArgument)
	}
	s.deliveryRoot = real
	return nil
}

func (s *Service) deliveryPath(rel string, wantDir bool) (string, os.FileInfo, error) {
	if s.deliveryRoot == "" {
		return "", nil, fmt.Errorf("delivery root is not configured: %w", model.ErrInvalidState)
	}
	if !filepath.IsLocal(rel) {
		return "", nil, fmt.Errorf("delivery path must be relative to the configured root: %w", model.ErrArgument)
	}
	real, err := filepath.EvalSymlinks(filepath.Join(s.deliveryRoot, rel))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("delivery file is missing: %w", model.ErrNotFound)
		}
		return "", nil, err
	}
	within, err := filepath.Rel(s.deliveryRoot, real)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
		return "", nil, fmt.Errorf("delivery path escapes configured root: %w", model.ErrForbidden)
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", nil, err
	}
	if info.IsDir() != wantDir || (!wantDir && !info.Mode().IsRegular()) {
		return "", nil, fmt.Errorf("delivery path has wrong file type: %w", model.ErrArgument)
	}
	return real, info, nil
}

// ImportDelivery registers an existing CLI package below the configured root.
// Every declared artifact is checked before CreateAsset persists the map.
func (s *Service) ImportDelivery(ctx context.Context, assetID, packageDir string) (model.Asset, error) {
	if !validTaskID(assetID) {
		return model.Asset{}, fmt.Errorf("invalid asset id: %w", model.ErrArgument)
	}
	artifacts, err := s.validatePackage(packageDir)
	if err != nil {
		return model.Asset{}, err
	}
	if err := s.CreateAsset(ctx, model.Asset{AssetID: assetID, Status: model.AssetStatusIngested, Artifacts: artifacts}); err != nil {
		return model.Asset{}, err
	}
	s.audit(ctx, "human:webui", "delivery.import", assetID, packageDir)
	return s.GetAsset(ctx, assetID)
}

// validatePackage checks a CLI package below the delivery root and returns
// the artifact map (root-relative, slash-separated) it declares. It touches
// only the filesystem, so callers may run it before opening a transaction.
func (s *Service) validatePackage(packageDir string) (map[string]string, error) {
	if !filepath.IsLocal(packageDir) {
		return nil, fmt.Errorf("package_dir must be relative: %w", model.ErrArgument)
	}
	packagePath, _, err := s.deliveryPath(packageDir, true)
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(packagePath, "delivery-manifest.json")
	manifestRel, err := filepath.Rel(s.deliveryRoot, manifestPath)
	if err != nil {
		return nil, err
	}
	manifestReal, _, err := s.deliveryPath(manifestRel, false)
	if err != nil {
		return nil, err
	}
	if filepath.Dir(manifestReal) != packagePath {
		return nil, fmt.Errorf("manifest escapes package directory: %w", model.ErrForbidden)
	}
	f, err := os.Open(manifestReal)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxManifestBytes {
		return nil, fmt.Errorf("manifest exceeds size limit: %w", model.ErrArgument)
	}
	var manifest deliveryManifest
	if err := json.Unmarshal(raw, &manifest); err != nil || manifest.SchemaVersion != 1 || len(manifest.Artifacts) == 0 || len(manifest.Artifacts) > maxDeliveryFiles {
		return nil, fmt.Errorf("unsupported or invalid delivery manifest: %w", model.ErrArgument)
	}
	artifacts := map[string]string{"manifest": filepath.ToSlash(manifestRel)}
	counts := make(map[string]int)
	seenFiles := map[string]bool{manifestReal: true}
	for _, item := range manifest.Artifacts {
		if !deliveryKey.MatchString(item.Kind) || !filepath.IsLocal(item.Path) || item.Path == "." {
			return nil, fmt.Errorf("invalid artifact declaration: %w", model.ErrArgument)
		}
		candidate := filepath.Join(packagePath, item.Path)
		rel, err := filepath.Rel(s.deliveryRoot, candidate)
		if err != nil {
			return nil, err
		}
		real, _, err := s.deliveryPath(rel, false)
		if err != nil {
			return nil, err
		}
		inside, err := filepath.Rel(packagePath, real)
		if err != nil || !filepath.IsLocal(inside) || inside == "." {
			return nil, fmt.Errorf("artifact escapes package directory: %w", model.ErrForbidden)
		}
		if seenFiles[real] {
			return nil, fmt.Errorf("duplicate artifact file: %w", model.ErrArgument)
		}
		seenFiles[real] = true
		// A manifest may contain multiple source videos of the same kind.
		counts[item.Kind]++
		key := item.Kind
		if key == "manifest" || counts[item.Kind] > 1 {
			key = fmt.Sprintf("%s_%d", item.Kind, counts[item.Kind])
		}
		for _, exists := artifacts[key]; exists; _, exists = artifacts[key] {
			counts[item.Kind]++
			key = fmt.Sprintf("%s_%d", item.Kind, counts[item.Kind])
		}
		storedRel, err := filepath.Rel(s.deliveryRoot, real)
		if err != nil {
			return nil, err
		}
		artifacts[key] = filepath.ToSlash(storedRel)
	}
	return artifacts, nil
}

func fileMime(name string) (string, bool) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4":
		return "video/mp4", true
	case ".webm":
		return "video/webm", true
	case ".mov":
		return "video/quicktime", true
	case ".wav":
		return "audio/wav", true
	case ".mp3":
		return "audio/mpeg", true
	case ".ogg":
		return "audio/ogg", true
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".webp":
		return "image/webp", true
	default:
		return "application/octet-stream", false
	}
}

func (s *Service) packagePath(a model.Asset, real string, key string) (string, error) {
	if manifestRel := a.Artifacts["manifest"]; manifestRel != "" {
		manifestReal, _, err := s.deliveryPath(filepath.FromSlash(manifestRel), false)
		if err != nil {
			return "", err
		}
		actualRel, err := filepath.Rel(filepath.Dir(manifestReal), real)
		if err != nil || !filepath.IsLocal(actualRel) || actualRel == "." {
			return "", fmt.Errorf("registered file resolves outside package: %w", model.ErrForbidden)
		}
		// Preserve the original package-relative name used by XML and manifest.
		manifestDir := filepath.Dir(filepath.FromSlash(manifestRel))
		rootRel := filepath.FromSlash(a.Artifacts[key])
		within, err := filepath.Rel(manifestDir, rootRel)
		if err != nil || !filepath.IsLocal(within) || within == "." {
			return "", fmt.Errorf("registered file escapes package: %w", model.ErrForbidden)
		}
		return filepath.ToSlash(within), nil
	}
	return key + filepath.Ext(real), nil
}

// ListDeliveryFiles revalidates every registered path. Missing or escaped
// files fail the whole list rather than silently producing an incomplete ZIP.
func (s *Service) ListDeliveryFiles(ctx context.Context, assetID string) ([]DeliveryFile, error) {
	a, err := s.GetAsset(ctx, assetID)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(a.Artifacts))
	for key := range a.Artifacts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]DeliveryFile, 0, len(keys))
	for _, key := range keys {
		if !deliveryKey.MatchString(key) {
			return nil, fmt.Errorf("invalid registered file key: %w", model.ErrArgument)
		}
		path, info, err := s.deliveryPath(filepath.FromSlash(a.Artifacts[key]), false)
		if err != nil {
			return nil, err
		}
		mime, playable := fileMime(path)
		packageName, err := s.packagePath(a, path, key)
		if err != nil {
			return nil, err
		}
		out = append(out, DeliveryFile{Key: key, Name: info.Name(), PackagePath: packageName, Mime: mime, Size: info.Size(), Playable: playable})
	}
	return out, nil
}

// OpenDeliveryFile returns an already-open regular file, so handlers never
// construct filesystem paths from user-controlled URL segments.
func (s *Service) OpenDeliveryFile(ctx context.Context, assetID, key string) (*os.File, DeliveryFile, error) {
	if !deliveryKey.MatchString(key) {
		return nil, DeliveryFile{}, fmt.Errorf("invalid artifact key: %w", model.ErrArgument)
	}
	a, err := s.GetAsset(ctx, assetID)
	if err != nil {
		return nil, DeliveryFile{}, err
	}
	rel, ok := a.Artifacts[key]
	if !ok {
		return nil, DeliveryFile{}, fmt.Errorf("artifact key not registered: %w", model.ErrNotFound)
	}
	path, info, err := s.deliveryPath(filepath.FromSlash(rel), false)
	if err != nil {
		return nil, DeliveryFile{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, DeliveryFile{}, err
	}
	mime, playable := fileMime(path)
	packageName, err := s.packagePath(a, path, key)
	if err != nil {
		_ = f.Close()
		return nil, DeliveryFile{}, err
	}
	return f, DeliveryFile{Key: key, Name: info.Name(), PackagePath: packageName, Mime: mime, Size: info.Size(), Playable: playable}, nil
}
