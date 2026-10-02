package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func (s *Service) hashUnderRoot(rel string) (string, error) {
	if s.deliveryRoot == "" {
		return "", fmt.Errorf("delivery root is not configured: %w", model.ErrInvalidState)
	}
	rel = filepath.ToSlash(rel)
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
		return "", fmt.Errorf("package path is not relative: %w", model.ErrArgument)
	}
	full, _, err := s.deliveryPath(filepath.FromSlash(rel), false)
	if err != nil {
		return "", err
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("package file %s: %w", rel, model.ErrNotFound)
		}
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Service) readPackageEDL(packageRef string) (map[string]any, string, error) {
	rel := filepath.ToSlash(packageRef) + "/edl.json"
	sum, err := s.hashUnderRoot(rel)
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(filepath.Join(s.deliveryRoot, filepath.FromSlash(rel)))
	if err != nil {
		return nil, "", err
	}
	var edl map[string]any
	if err := json.Unmarshal(raw, &edl); err != nil || edl == nil {
		return nil, "", fmt.Errorf("stored edl is not an object: %w", model.ErrInvalidState)
	}
	return edl, sum, nil
}

func evidenceHash(root, packageRef string) (string, error) {
	rel := filepath.ToSlash(packageRef) + "/samples/evidence-manifest.json"
	full := filepath.Join(root, filepath.FromSlash(rel))
	real, resolveErr := filepath.EvalSymlinks(full)
	if resolveErr == nil {
		within, e := filepath.Rel(root, real)
		if e != nil || !filepath.IsLocal(within) {
			return "", fmt.Errorf("evidence escapes root: %w", model.ErrForbidden)
		}
		full = real
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func joinRoot(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

func narrationSource(line map[string]any) string {
	for _, key := range []string{"source_scene_label", "source_scene", "source_scene_id"} {
		if value, ok := line[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func asMapSlice(v any) []map[string]any {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if ok {
			out = append(out, m)
		}
	}
	return out
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("package contains symlink: %w", model.ErrForbidden)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			in.Close()
			return err
		}
		_, err = io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		return err
	})
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
