package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

// Completed output stays an external reference, like source media. A fresh
// SQLite cache can still locate it; an absent output stays absent.
func (s *Service) captureProjectOutputs(ctx context.Context, doc *project.AvpDocument) error {
	if doc.FileReferences == nil {
		doc.FileReferences = map[string]string{}
	}
	record := func(ref string) {
		if !filepath.IsLocal(ref) {
			return
		}
		path, _, err := s.deliveryPath(filepath.FromSlash(ref), false)
		if err == nil {
			doc.FileReferences[filepath.ToSlash(filepath.Clean(ref))] = path
			return
		}
		path, _, err = s.deliveryPath(filepath.FromSlash(ref), true)
		if err == nil {
			doc.FileReferences[filepath.ToSlash(filepath.Clean(ref))] = path
		}
	}
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case string:
			record(value)
			if len(value) > 1 && (value[0] == '{' || value[0] == '[') {
				var nested any
				if json.Unmarshal([]byte(value), &nested) == nil {
					visit(nested)
				}
			}
		case []any:
			for _, item := range value {
				visit(item)
			}
		case map[string]any:
			for _, item := range value {
				visit(item)
			}
		}
	}
	var runtime any
	if err := json.Unmarshal(doc.Runtime, &runtime); err != nil {
		return err
	}
	visit(runtime)
	root := filepath.Join(s.deliveryRoot, "projects", doc.Project.ID)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(s.deliveryRoot, path)
		if err != nil {
			return err
		}
		record(rel)
		return nil
	})
}

func (s *Service) projectOutputReference(ref string, wantDir bool) (string, os.FileInfo, bool) {
	w := s.workspace
	if w == nil {
		return "", nil, false
	}
	w.mu.Lock()
	path := ""
	if w.document != nil {
		path = w.document.FileReferences[filepath.ToSlash(filepath.Clean(ref))]
	}
	w.mu.Unlock()
	if path == "" || !filepath.IsAbs(path) {
		return "", nil, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() != wantDir || (!wantDir && !info.Mode().IsRegular()) {
		return "", nil, false
	}
	return path, info, true
}

func renameProjectOutputReferences(doc *project.AvpDocument, oldID string) {
	oldPrefix := "projects/" + oldID + "/"
	for ref, path := range doc.FileReferences {
		if strings.HasPrefix(ref, oldPrefix) {
			doc.FileReferences["projects/"+doc.Project.ID+"/"+strings.TrimPrefix(ref, oldPrefix)] = path
		}
	}
}
