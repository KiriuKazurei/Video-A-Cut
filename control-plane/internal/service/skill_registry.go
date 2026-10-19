package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

var skillTools = []string{"media.list", "media.get_frames", "media.transcribe", "summaries.get", "summaries.submit", "scripts.propose", "renders.create", "renders.inspect", "renders.review", "artifacts.list", "artifacts.get", "run.finish"}

func packageSHA(p skill.Package) string { p.Enabled = false; p.SHA = ""; return skillHash(p) }
func (s *Service) loadBuiltinSkills(ctx context.Context) error {
	for _, p := range skill.Builtins() {
		p.SHA = packageSHA(p)
		// Keep disabled settings and immutable run snapshots across upgrades.
		var old skill.Package
		err := s.st.SkillGet(ctx, "skill", p.ID, &old)
		if err == nil {
			if old.Builtin && old.SHA != p.SHA {
				p.Enabled = old.Enabled
				if e := s.st.SkillSetEnabled(ctx, p.ID, p); e != nil {
					return e
				}
			}
			continue
		}
		if err != model.ErrNotFound {
			return err
		}
		p.SHA = packageSHA(p)
		if err = s.st.SkillPut(ctx, "skill", p.ID, 0, p); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) ListSkills(ctx context.Context) ([]json.RawMessage, error) {
	return s.st.SkillList(ctx, "skill")
}
func (s *Service) GetSkill(ctx context.Context, id string) (skill.Package, error) {
	var p skill.Package
	err := s.st.SkillGet(ctx, "skill", id, &p)
	return p, err
}
func validateSkill(p skill.Package) error {
	if !validTaskID(p.ID) || p.Name == "" || p.Version == "" || len(p.Content) == 0 || len(p.Content) > 128<<10 || len(p.Resources) > 64 || len(p.Flows) == 0 || len(p.Modes) == 0 {
		return model.ErrArgument
	}
	if p.Runtime != "instructions_only" && p.Runtime != "harness" {
		return model.ErrArgument
	}
	for _, flow := range p.Flows {
		if !validFlow(flow) {
			return model.ErrArgument
		}
	}
	for _, mode := range p.Modes {
		if mode != "external_mcp" && mode != "builtin_api" {
			return model.ErrArgument
		}
	}
	for _, tool := range p.Tools {
		if !slices.Contains(skillTools, tool) {
			return model.ErrArgument
		}
	}
	total := len(p.Content)
	for key, body := range p.Resources {
		if !filepath.IsLocal(key) || strings.Contains(key, "\\") || len(body) > 128<<10 {
			return model.ErrArgument
		}
		total += len(body)
	}
	if total > 1<<20 {
		return model.ErrArgument
	}
	return nil
}
func (s *Service) InstallSkill(ctx context.Context, actor string, p skill.Package) (skill.Package, error) {
	if err := requireHumanConfirm(actor, "install skill"); err != nil {
		return p, err
	}
	if err := validateSkill(p); err != nil {
		return p, err
	}
	p.Source = "local_path"
	p.Builtin = false
	p.SHA = packageSHA(p)
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		// An install is not an overwrite. Disable/enable and immutable versions are separate.
		if err := tx.SkillPut(ctx, "skill", p.ID, 0, p); err != nil {
			return model.ErrConflict
		}
		return auditTx(ctx, tx, actor, "skill.install", p.ID, p.SHA)
	})
	if err == nil {
		s.publish("skill.changed", map[string]string{"skill_id": p.ID})
	}
	return p, err
}
func (s *Service) ImportSkill(ctx context.Context, actor, dir string) (skill.Package, error) {
	var p skill.Package
	if err := requireHumanConfirm(actor, "import local skill"); err != nil {
		return p, err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return p, model.ErrArgument
	}
	for cur := root; ; cur = filepath.Dir(cur) {
		info, e := os.Lstat(cur)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return p, model.ErrForbidden
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	resources := map[string]string{}
	total := int64(0)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return model.ErrForbidden
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return model.ErrForbidden
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		// Import only declared text instructions. Never execute or implicitly expose helpers.
		if rel != "skill.json" && !strings.HasSuffix(rel, ".md") && rel != "LICENSE" {
			return nil
		}
		total += info.Size()
		if total > 1<<20 || info.Size() > 128<<10 || len(resources) > 64 {
			return model.ErrArgument
		}
		body, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		resources[rel] = string(body)
		return nil
	})
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal([]byte(resources["skill.json"]), &p); err != nil {
		return p, fmt.Errorf("local skill requires skill.json: %w", model.ErrArgument)
	}
	p.Content = resources["SKILL.md"]
	delete(resources, "skill.json")
	delete(resources, "SKILL.md")
	p.Resources = resources
	return s.InstallSkill(ctx, actor, p)
}
func (s *Service) EnableSkill(ctx context.Context, actor, id string, enabled bool) (skill.Package, error) {
	var p skill.Package
	if err := requireHumanConfirm(actor, "manage skill"); err != nil {
		return p, err
	}
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		if err := tx.SkillGet(ctx, "skill", id, &p); err != nil {
			return err
		}
		p.Enabled = enabled
		// Registry rows have a store version separate from the package semantic version.
		if err := tx.SkillSetEnabled(ctx, id, p); err != nil {
			return err
		}
		return auditTx(ctx, tx, actor, "skill.enabled", id, fmt.Sprint(enabled))
	})
	if err == nil {
		if !enabled {
			s.cancelSkillExecutions(ctx, func(r skill.Run) bool { return r.SkillID == id })
		}
		s.publish("skill.changed", map[string]string{"skill_id": id})
	}
	return p, err
}
func (s *Service) ValidateSkill(ctx context.Context, id string) (map[string]any, error) {
	p, err := s.GetSkill(ctx, id)
	if err != nil {
		return nil, err
	}
	checks := map[string]any{"metadata_valid": validateSkill(p) == nil, "enabled": p.Enabled, "content_intact": packageSHA(p) == p.SHA}
	for _, tool := range []string{"ffmpeg", "ffprobe", "python", "node"} {
		_, e := exec.LookPath(tool)
		checks[tool] = e == nil
	}
	return checks, nil
}
