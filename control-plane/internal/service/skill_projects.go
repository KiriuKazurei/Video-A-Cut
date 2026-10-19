package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

func skillHash(v any) string {
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func validFlow(v string) bool { return v == "lightweight" || v == "visual_heavy" || v == "custom" }
func sortedIDs(ids []string) ([]string, error) {
	out := append([]string{}, ids...)
	slices.Sort(out)
	for i, id := range out {
		if !validTaskID(id) || (i > 0 && id == out[i-1]) {
			return nil, model.ErrArgument
		}
	}
	return out, nil
}
func (s *Service) ListProjects(ctx context.Context) ([]json.RawMessage, error) {
	return s.st.SkillList(ctx, "project")
}
func (s *Service) GetProject(ctx context.Context, id string) (skill.Project, error) {
	var p skill.Project
	err := s.st.SkillGet(ctx, "project", id, &p)
	return p, err
}

type ProjectRequest struct {
	ProfileID           string            `json:"profile_id,omitempty"`
	ProfileRevision     int               `json:"profile_revision,omitempty"`
	AcceptTranscription bool              `json:"accept_transcription"`
	Name                string            `json:"name"`
	Flow                string            `json:"flow"`
	ExpectedVersion     int               `json:"expected_version"`
	Assets              []string          `json:"assets"`
	Visible             []string          `json:"visible"`
	Defaults            map[string]string `json:"default_skills"`
	Disabled            []string          `json:"disabled_skills"`
	AcceptExternal      bool              `json:"accept_external"`
}

func (s *Service) SaveProject(ctx context.Context, actor, id string, req ProjectRequest) (skill.Project, error) {
	var p skill.Project
	if err := requireHumanConfirm(actor, "manage project"); err != nil {
		return p, err
	}
	if strings.TrimSpace(req.Name) == "" || len(req.Name) > 256 || !validFlow(req.Flow) {
		return p, model.ErrArgument
	}
	assets, err := sortedIDs(req.Assets)
	if err != nil {
		return p, err
	}
	if req.ProfileID != "" {
		if req.ProfileRevision < 1 {
			return p, model.ErrArgument
		}
		if _, _, err = s.GetProcessingProfile(ctx, req.ProfileID, req.ProfileRevision); err != nil {
			return p, err
		}
	}
	visible, err := sortedIDs(req.Visible)
	if err != nil {
		return p, err
	}
	for _, v := range visible {
		if !slices.Contains(assets, v) {
			return p, model.ErrArgument
		}
	}
	if len(assets) > 200 || len(req.Disabled) > 100 {
		return p, model.ErrArgument
	}
	if id == "" {
		if req.ExpectedVersion != 0 {
			return p, model.ErrArgument
		}
		id = newID("prj")
	}
	if !validTaskID(id) || id == "legacy" {
		return p, model.ErrArgument
	}
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		if req.ExpectedVersion > 0 {
			if err := tx.SkillGet(ctx, "project", id, &p); err != nil {
				return err
			}
			if p.Version != req.ExpectedVersion {
				return model.ErrConflict
			}
		}
		for _, a := range assets {
			if _, err := tx.GetAsset(ctx, a); err != nil {
				return err
			}
		}
		for flow, sk := range req.Defaults {
			if !validFlow(flow) {
				return model.ErrArgument
			}
			var pack skill.Package
			if err := tx.SkillGet(ctx, "skill", sk, &pack); err != nil {
				return err
			}
			if !pack.Enabled || !slices.Contains(pack.Flows, flow) {
				return model.ErrArgument
			}
		}
		now := s.autoNow()
		if p.CreatedAt.IsZero() {
			p.CreatedAt = now
		}
		p.ID = id
		p.Name = req.Name
		p.Flow = req.Flow
		p.Assets = assets
		p.Visible = visible
		p.Defaults = req.Defaults
		p.Disabled = req.Disabled
		p.ExternalAuthorized = req.AcceptExternal
		p.ProfileID = req.ProfileID
		p.ProfileRevision = req.ProfileRevision
		p.TranscriptionAuthorized = req.AcceptTranscription
		p.Version = req.ExpectedVersion + 1
		p.UpdatedAt = now
		p.ConfirmedSHA = ""
		p.ConfirmedBy = ""
		if err := tx.SkillPut(ctx, "project", id, req.ExpectedVersion, p); err != nil {
			return err
		}
		return auditTx(ctx, tx, actor, "project.save", id, fmt.Sprint(p.Version))
	})
	if err != nil {
		return p, err
	}
	// DB is authoritative. A mirror failure is reported; it is not rolled back as a fake success.
	s.cancelSkillExecutions(ctx, func(r skill.Run) bool { return r.ProjectID == id })
	if err = s.mirrorProject(p); err != nil {
		return p, err
	}
	s.publish("project.changed", map[string]any{"project_id": id})
	return p, nil
}
func (s *Service) projectPath(id string) (string, error) {
	if s.deliveryRoot == "" || !validTaskID(id) {
		return "", model.ErrInvalidState
	}
	rel := filepath.Join("projects", id)
	return s.harnessDirectory(rel)
}

// Create one component at a time, checking existing junctions before writing.
func (s *Service) harnessDirectory(rel string) (string, error) {
	if s.deliveryRoot == "" || !filepath.IsLocal(rel) {
		return "", model.ErrArgument
	}
	cur := s.deliveryRoot
	for _, part := range strings.Split(filepath.Clean(rel), string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			if err = os.Mkdir(cur, 0700); err != nil {
				return "", err
			}
			info, err = os.Lstat(cur)
		}
		if err != nil {
			return "", err
		}
		real, e := filepath.EvalSymlinks(cur)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Clean(real), filepath.Clean(cur)) {
			return "", model.ErrForbidden
		}
	}
	return cur, nil
}
func (s *Service) mirrorProject(p skill.Project) error {
	root, err := s.projectPath(p.ID)
	if err != nil {
		return err
	}
	for _, sub := range []string{"media", "scripts", "keyframes", "summaries", "skill-runs", "renders", "delivery"} {
		if _, err = s.harnessDirectory(filepath.Join("projects", p.ID, sub)); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(root, "project.json")
	if info, e := os.Lstat(path); e == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return model.ErrForbidden
	}
	return os.WriteFile(path, raw, 0600)
}

type harnessSnapshot struct {
	Package skill.Package     `json:"skill"`
	Media   []skill.Media     `json:"media"`
	Paths   map[string]string `json:"paths"`
}

func (s *Service) projectMedia(ctx context.Context, p skill.Project) ([]skill.Media, map[string]string, error) {
	assets := []model.Asset{}
	for _, id := range p.Visible {
		a, err := s.st.GetAsset(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		if a.Locked || !a.AgentVisible {
			return nil, nil, model.ErrForbidden
		}
		assets = append(assets, a)
	}
	list, err := s.scriptMediaFromAssets(assets)
	if err != nil {
		return nil, nil, err
	}
	media := []skill.Media{}
	paths := map[string]string{}
	for _, m := range list {
		if err = ctx.Err(); err != nil {
			return nil, nil, err
		}
		f, err := os.Open(m.Path)
		if err != nil {
			return nil, nil, err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return nil, nil, err
		}
		media = append(media, skill.Media{ID: m.ID, AssetID: m.AssetID, Name: m.Name, In: m.In, Out: m.Out, SHA: hex.EncodeToString(h.Sum(nil))})
		paths[m.ID] = m.Source
	}
	return media, paths, nil
}
func projectSHA(p skill.Project, media []skill.Media) string {
	return skillHash(struct {
		Version int
		Project string
		Visible []string
		Media   []skill.Media
	}{2, p.ID, p.Visible, media})
}
func (s *Service) ProjectVisibleSet(ctx context.Context, id string) (skill.VisibleSet, error) {
	p, err := s.GetProject(ctx, id)
	if err != nil {
		return skill.VisibleSet{}, err
	}
	media, _, err := s.projectMedia(ctx, p)
	if err != nil {
		return skill.VisibleSet{}, err
	}
	sha := projectSHA(p, media)
	return skill.VisibleSet{ProjectID: id, ScopeVersion: 2, SHA: sha, Confirmed: sha == p.ConfirmedSHA, Media: media}, nil
}
func (s *Service) ConfirmProjectVisible(ctx context.Context, actor, id, sha string, version int) (skill.VisibleSet, error) {
	if err := requireHumanConfirm(actor, "confirm project visible set"); err != nil {
		return skill.VisibleSet{}, err
	}
	view, err := s.ProjectVisibleSet(ctx, id)
	if err != nil {
		return view, err
	}
	if sha == "" || sha != view.SHA {
		return view, model.ErrConflict
	}
	var p skill.Project
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		if err := tx.SkillGet(ctx, "project", id, &p); err != nil {
			return err
		}
		if p.Version != version {
			return model.ErrConflict
		}
		p.Version++
		p.ConfirmedSHA = sha
		p.ConfirmedBy = actor
		p.UpdatedAt = s.autoNow()
		if err := tx.SkillPut(ctx, "project", id, version, p); err != nil {
			return err
		}
		return auditTx(ctx, tx, actor, "project.visible.confirm", id, sha)
	})
	if err == nil {
		view.Confirmed = true
		s.publish("project.changed", map[string]any{"project_id": id})
		err = s.mirrorProject(p)
	}
	return view, err
}

// ConfigureSkillHarness adds a read-only compatibility project; no legacy consent is fabricated.
func (s *Service) ConfigureSkillHarness(ctx context.Context) error {
	var legacy skill.Project
	if err := s.st.SkillGet(ctx, "project", "legacy", &legacy); errors.Is(err, model.ErrNotFound) {
		list, e := s.st.ListLibraryAssets(ctx)
		if e != nil {
			return e
		}
		ids := []string{}
		for _, a := range list {
			ids = append(ids, a.AssetID)
		}
		now := time.Now().UTC()
		legacy = skill.Project{ID: "legacy", Name: "兼容素材库", Flow: "custom", Version: 1, Assets: ids, Visible: []string{}, Defaults: map[string]string{}, Disabled: []string{}, CreatedAt: now, UpdatedAt: now}
		if e = s.st.SkillPut(ctx, "project", "legacy", 0, legacy); e != nil {
			return e
		}
	} else if err != nil {
		return err
	}
	// A process restart cannot silently replay a possibly billable tool.
	runs, err := s.st.SkillList(ctx, "run")
	if err != nil {
		return err
	}
	for _, raw := range runs {
		var r skill.Run
		if json.Unmarshal(raw, &r) != nil {
			return model.ErrInvalidState
		}
		if err = s.st.EnsureSkillCheckpoint(ctx, r.ID); err != nil {
			return err
		}
		if r.State == "running" {
			v := r.Version
			r.Version++
			r.State = "waiting_human"
			r.Reason = "runtime_restarted"
			if err = s.st.SkillPut(ctx, "run", r.ID, v, r); err != nil {
				return err
			}
		}
	}
	return s.loadBuiltinSkills(ctx)
}
