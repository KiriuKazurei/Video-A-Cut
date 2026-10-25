package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

func (s *Service) GetSkillRun(ctx context.Context, actor, id string) (skill.Run, error) {
	var r skill.Run
	err := s.st.SkillGet(ctx, "run", id, &r)
	if err == nil && s.WorkspaceEnabled() && (s.CurrentProjectID() == "" || r.ProjectID != s.CurrentProjectID()) {
		return r, model.ErrNotFound
	}
	r.FramesSpent = max(r.FramesSpent, len(r.Frames))
	if err == nil && !isHumanActor(actor) && r.Owner != actor {
		err = model.ErrForbidden
	}
	return r, err
}
func (s *Service) ListSkillRuns(ctx context.Context, actor, project string) ([]skill.Run, error) {
	if s.WorkspaceEnabled() {
		current := s.CurrentProjectID()
		if current == "" {
			return []skill.Run{}, nil
		}
		if project != "" && project != current {
			return nil, model.ErrNotFound
		}
		project = current
	}
	list, err := s.st.SkillList(ctx, "run")
	if err != nil {
		return nil, err
	}
	out := []skill.Run{}
	for _, raw := range list {
		var r skill.Run
		if err = json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		if (project == "" || r.ProjectID == project) && (isHumanActor(actor) || r.Owner == actor) {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *Service) CreateSkillRun(ctx context.Context, actor string, req skill.RunRequest) (skill.Run, error) {
	var r skill.Run
	if actor == "" || len(req.Goal) == 0 || len(req.Goal) > 64<<10 || len(req.IdempotencyKey) == 0 || len(req.IdempotencyKey) > 128 {
		return r, model.ErrArgument
	}
	if req.Mode != "external_mcp" && req.Mode != "builtin_api" {
		return r, model.ErrArgument
	}
	p, err := s.GetProject(ctx, req.ProjectID)
	if err != nil {
		return r, err
	}
	if req.Flow != "" {
		if !validFlow(req.Flow) {
			return r, model.ErrArgument
		}
		p.Flow = req.Flow
	}
	if req.ProfileID == "" && p.ProfileID != "" {
		req.ProfileID = p.ProfileID
		req.ProfileRevision = p.ProfileRevision
	}
	if !isHumanActor(actor) && (!p.ExternalAuthorized || req.Mode != "external_mcp") {
		return r, model.ErrForbidden
	}
	if !isHumanActor(actor) && !s.WorkspaceEnabled() {
		if req.AgentRole == "" {
			return r, model.ErrForbidden
		}
		for _, assetID := range p.Visible {
			a, e := s.st.GetAsset(ctx, assetID)
			if e != nil {
				return r, e
			}
			if !assetVisibleForRole(a, req.AgentRole) {
				return r, model.ErrForbidden
			}
		}
	}
	view, err := s.ProjectVisibleSet(ctx, p.ID)
	if err != nil {
		return r, err
	}
	if !view.Confirmed || req.VisibleSHA != view.SHA {
		return r, fmt.Errorf("confirm the nonempty project visible set: %w", model.ErrForbidden)
	}
	sk := req.SkillID
	if sk == "" {
		sk = p.Defaults[p.Flow]
	}
	pack, err := s.GetSkill(ctx, sk)
	if err != nil {
		return r, err
	}
	if req.Strategy == "" && drawPackage(sk) {
		req.Strategy = "draw"
	}
	if req.Strategy != "" && req.Strategy != "edit" && req.Strategy != "draw" {
		return r, model.ErrArgument
	}
	if req.Strategy == "draw" && (!s.WorkspaceEnabled() || !slices.Contains(pack.Tools, "draw.save")) {
		return r, model.ErrForbidden
	}
	if len(view.Media) == 0 && req.Strategy != "draw" {
		return r, fmt.Errorf("editing requires source media: %w", model.ErrForbidden)
	}
	if !pack.Enabled || slices.Contains(p.Disabled, sk) || !slices.Contains(pack.Flows, p.Flow) || !slices.Contains(pack.Modes, req.Mode) {
		return r, model.ErrForbidden
	}
	if req.Budget.MaxSteps == 0 {
		req.Budget.MaxSteps = 24
	}
	if req.Budget.MaxSeconds == 0 {
		req.Budget.MaxSeconds = 900
	}
	if req.Budget.MaxFrames == 0 {
		req.Budget.MaxFrames = 24
	}
	if req.Budget.MaxSteps < 1 || req.Budget.MaxSteps > 64 || req.Budget.MaxSeconds < 1 || req.Budget.MaxSeconds > 3600 || req.Budget.MaxFrames < 1 || req.Budget.MaxFrames > 48 {
		return r, model.ErrArgument
	}
	if req.Mode == "builtin_api" && !isHumanActor(actor) {
		return r, model.ErrForbidden
	}
	if req.ProfileID != "" && !isHumanActor(actor) && !s.WorkspaceEnabled() && (req.ProfileID != p.ProfileID || req.ProfileRevision != p.ProfileRevision) {
		return r, model.ErrForbidden
	}
	profileSHA := ""
	if req.ProfileID != "" {
		profile, sha, e := s.GetProcessingProfile(ctx, req.ProfileID, req.ProfileRevision)
		if e != nil {
			return r, e
		}
		if req.Mode == "builtin_api" && profile.ContentMode != "configured" {
			return r, model.ErrArgument
		}
		req.ProfileRevision = profile.Revision
		if req.Mode == "builtin_api" && strings.TrimSpace(os.Getenv(profile.Narration.TokenEnv)) == "" {
			return r, fmt.Errorf("missing Agent API key environment variable: %w", model.ErrInvalidState)
		}
		profileSHA = sha
		ok, e := s.st.ProfileConsent(ctx, profile.ProfileID, profile.Revision, sha)
		if e != nil {
			return r, e
		}
		projectGrant := p.ExternalAuthorized && req.ProfileID == p.ProfileID && req.ProfileRevision == p.ProfileRevision
		if profile.External() && (!ok || (!s.WorkspaceEnabled() && !(req.AcceptExternal && isHumanActor(actor) || projectGrant))) {
			return r, model.ErrForbidden
		}
	} else if req.Mode == "builtin_api" {
		return r, fmt.Errorf("builtin Agent requires a configured profile: %w", model.ErrArgument)
	}
	_, paths, err := s.projectMedia(ctx, p)
	if err != nil {
		return r, err
	}
	scope := "skill-run:" + actor
	sequenceID := req.SequenceID
	if w := s.workspace; w != nil {
		w.mu.Lock()
		if w.document == nil || w.document.Project.ID != req.ProjectID {
			w.mu.Unlock()
			return r, model.ErrNotFound
		}
		if sequenceID == "" {
			sequenceID, _ = w.document.Workspace["active_sequence"].(string)
			if sequenceIndex(w.document, sequenceID) < 0 && len(w.document.Sequences) > 0 {
				sequenceID = w.document.Sequences[0].ID
			}
		}
		valid := sequenceIndex(w.document, sequenceID) >= 0
		w.mu.Unlock()
		if !valid {
			return r, fmt.Errorf("请选择当前工程序列: %w", model.ErrArgument)
		}
	}
	hash := skillHash(req)
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		old, id, e := tx.SkillRequest(ctx, scope, req.IdempotencyKey)
		if e == nil {
			if old != hash {
				return model.ErrConflict
			}
			return tx.SkillGet(ctx, "run", id, &r)
		}
		if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		var cur skill.Project
		if e = tx.SkillGet(ctx, "project", p.ID, &cur); e != nil {
			return e
		}
		if cur.Version != p.Version || cur.ConfirmedSHA != view.SHA {
			return model.ErrConflict
		}
		var currentPack skill.Package
		if e = tx.SkillGet(ctx, "skill", pack.ID, &currentPack); e != nil {
			return e
		}
		if !currentPack.Enabled || currentPack.SHA != pack.SHA {
			return model.ErrConflict
		}
		now := s.autoNow()
		r = skill.Run{Strategy: req.Strategy, ID: newID("skr"), ProjectID: p.ID, ProjectVersion: p.Version, SkillID: pack.ID, SkillSHA: pack.SHA, SkillVersion: pack.Version, Mode: req.Mode, Flow: p.Flow, VisibleSHA: view.SHA, State: "queued", Owner: actor, Version: 1, Budget: req.Budget, Goal: req.Goal, ProfileID: req.ProfileID, ProfileRevision: req.ProfileRevision, ProfileSHA: profileSHA, ExternalAuthorized: req.AcceptExternal || p.ExternalAuthorized, Frames: []skill.Frame{}, Artifacts: []skill.Artifact{}, CreatedAt: now, UpdatedAt: now}
		r.TranscriptionAuthorized = (p.TranscriptionAuthorized && req.ProfileID == p.ProfileID && req.ProfileRevision == p.ProfileRevision) || (isHumanActor(actor) && req.AcceptTranscription)
		if s.WorkspaceEnabled() {
			r.TranscriptionAuthorized = true
			r.ExternalAuthorized = true
		}
		r.AgentRole = req.AgentRole
		r.SequenceID = sequenceID
		if req.AgentRole != "" {
			until := s.autoNow().Add(2 * time.Minute)
			r.LeaseUntil = &until
		}
		if e = tx.SkillPut(ctx, "run", r.ID, 0, r); e != nil {
			return e
		}
		if e = tx.PutSkillSnapshot(ctx, r.ID, harnessSnapshot{Package: pack, Media: view.Media, Paths: paths}); e != nil {
			return e
		}
		if e = tx.SkillRemember(ctx, scope, req.IdempotencyKey, hash, r.ID); e != nil {
			return e
		}
		return auditTx(ctx, tx, actor, "skill.run.create", r.ID, pack.ID+"@"+pack.Version)
	})
	if err == nil {
		s.publish("skill.run.changed", r)
	}
	return r, err
}
func (s *Service) checkSkillRun(ctx context.Context, r skill.Run) (harnessSnapshot, error) {
	var snap harnessSnapshot
	if r.AgentRole != "" && r.LeaseUntil != nil && s.autoNow().After(*r.LeaseUntil) {
		return snap, model.ErrLeaseExpired
	}
	p, err := s.GetProject(ctx, r.ProjectID)
	if err != nil {
		return snap, err
	}
	if (!s.WorkspaceEnabled() && (p.Version != r.ProjectVersion || p.ConfirmedSHA != r.VisibleSHA)) || slices.Contains(p.Disabled, r.SkillID) {
		return snap, model.ErrConflict
	}
	if r.AgentRole != "" && !s.WorkspaceEnabled() {
		for _, assetID := range p.Visible {
			a, e := s.st.GetAsset(ctx, assetID)
			if e != nil {
				return snap, e
			}
			if !assetVisibleForRole(a, r.AgentRole) {
				return snap, model.ErrForbidden
			}
		}
	}
	pack, err := s.GetSkill(ctx, r.SkillID)
	if err != nil {
		return snap, err
	}
	if !pack.Enabled {
		return snap, model.ErrForbidden
	}
	if err = s.st.SkillSnapshot(ctx, r.ID, &snap); err != nil {
		return snap, err
	}
	if s.WorkspaceEnabled() {
		media, _, err := s.projectMedia(ctx, p)
		if err != nil {
			return snap, err
		}
		current := map[string]skill.Media{}
		for _, item := range media {
			current[item.ID] = item
		}
		for _, frozen := range snap.Media {
			item, ok := current[frozen.ID]
			if !ok || item.SHA != frozen.SHA || item.In != frozen.In || item.Out != frozen.Out {
				return snap, model.ErrConflict
			}
		}
	}
	if snap.Package.SHA != r.SkillSHA || packageSHA(snap.Package) != r.SkillSHA {
		return snap, model.ErrConflict
	}
	view, err := s.ProjectVisibleSet(ctx, p.ID)
	if err != nil {
		return snap, err
	}
	if !s.WorkspaceEnabled() && (!view.Confirmed || view.SHA != r.VisibleSHA) {
		return snap, model.ErrConflict
	}
	if r.ProfileID != "" {
		profile, sha, e := s.GetProcessingProfile(ctx, r.ProfileID, r.ProfileRevision)
		if e != nil {
			return snap, e
		}
		ok, e := s.st.ProfileConsent(ctx, profile.ProfileID, profile.Revision, sha)
		if e != nil {
			return snap, e
		}
		if sha != r.ProfileSHA || (profile.External() && (!ok || !r.ExternalAuthorized)) {
			return snap, model.ErrForbidden
		}
	}
	return snap, nil
}
func (s *Service) SkillRunContext(ctx context.Context, actor, id string) (map[string]any, error) {
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	check := r
	if isHumanActor(actor) {
		check.LeaseUntil = nil
	}
	snap, err := s.checkSkillRun(ctx, check)
	if err != nil {
		return nil, err
	}
	drawings, _ := s.DrawCompositions(r.ProjectID)
	imageResources, _ := s.DrawImageResources(r.ProjectID)
	result := map[string]any{"image_resources": imageResources, "draw_compositions": drawings, "run": r, "skill": snap.Package, "media": snap.Media, "tools": snap.Package.Tools}
	if s.WorkspaceEnabled() && r.SequenceID != "" {
		view, e := s.Timeline(ctx, r.ProjectID, r.SequenceID)
		if e != nil {
			return nil, e
		}
		refs, e := s.ReferenceDirectory(r.ProjectID, r.SequenceID)
		if e != nil {
			return nil, e
		}
		result["sequence"] = view
		if !isHumanActor(actor) {
			view.History = nil
			result["sequence"] = view
		}
		result["references"] = refs
	}
	return result, nil
}
func (s *Service) ClaimSkillRun(ctx context.Context, actor, id string, version int, roles ...string) (skill.Run, error) {
	var r skill.Run
	if !strings.HasPrefix(actor, "agent:") {
		return r, model.ErrForbidden
	}
	if len(roles) != 1 || roles[0] == "" {
		return r, model.ErrForbidden
	}
	if err := s.st.SkillGet(ctx, "run", id, &r); err != nil {
		return skill.Run{}, err
	}
	// Validate the new session's role before exposing or transferring a run.
	if _, err := s.ProjectAgentView(ctx, r.ProjectID, roles[0]); err != nil {
		return skill.Run{}, err
	}
	check := r
	check.AgentRole, check.LeaseUntil = roles[0], nil
	if _, err := s.checkSkillRun(ctx, check); err != nil {
		return skill.Run{}, err
	}
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		if err := tx.SkillGet(ctx, "run", id, &r); err != nil {
			return err
		}
		if r.Version != version || r.Mode != "external_mcp" || !r.ExternalAuthorized || r.State != "queued" {
			return model.ErrConflict
		}
		expired := r.LeaseUntil != nil && s.autoNow().After(*r.LeaseUntil)
		if !isHumanActor(r.Owner) && r.Owner != actor && !expired {
			return model.ErrForbidden
		}
		r.Owner = actor
		r.AgentRole = roles[0]
		until := s.autoNow().Add(2 * time.Minute)
		r.LeaseUntil = &until
		r.Version++
		return tx.SkillPut(ctx, "run", id, version, r)
	})
	if err == nil {
		s.publish("skill.run.changed", r)
	}
	return r, err
}
func (s *Service) UpdateSkillRun(ctx context.Context, actor, id, action string, version int) (skill.Run, error) {
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return r, err
	}
	if r.Version != version {
		return r, model.ErrConflict
	}
	if action != "cancel" {
		if err = requireHumanConfirm(actor, action); err != nil {
			return r, err
		}
		check := r
		check.LeaseUntil = nil
		if _, err = s.checkSkillRun(ctx, check); err != nil {
			return r, err
		}
	}
	if r.State == "succeeded" || r.State == "cancelled" {
		return r, model.ErrInvalidState
	}
	switch action {
	case "finalize":
		if r.Plan == nil || r.State != "waiting_human" || r.Reason != "plan_confirmation" {
			return r, model.ErrInvalidState
		}
		now := s.autoNow()
		r.Plan.FinalizedBy = actor
		r.Plan.FinalizedAt = &now
		r.State = "queued"
		r.Reason = ""
	case "resume":
		if r.State != "waiting_human" || r.Reason == "plan_confirmation" {
			return r, model.ErrInvalidState
		}
		r.State = "queued"
		r.Reason = ""
	case "cancel":
		r.State = "cancelled"
		r.Reason = "user_cancelled"
	default:
		return r, model.ErrArgument
	}
	r.Version++
	if r.AgentRole != "" && action != "cancel" {
		until := s.autoNow().Add(2 * time.Minute)
		r.LeaseUntil = &until
	}
	if action != "cancel" {
		r.BudgetStepBase, r.BudgetElapsedBase = r.Steps, r.ElapsedMilliseconds
	}
	r.UpdatedAt = s.autoNow()
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		if err := tx.SkillPut(ctx, "run", id, version, r); err != nil {
			return err
		}
		return auditTx(ctx, tx, actor, "skill.run."+action, id, fmt.Sprint(r.Version))
	})
	if err == nil {
		if action == "cancel" {
			s.harnessMu.Lock()
			if cancel := s.harnessCancel[id]; cancel != nil {
				cancel()
			}
			if cancel := s.harnessCancel["agent:"+id]; cancel != nil {
				cancel()
			}
			s.harnessMu.Unlock()
		}
		s.publish("skill.run.changed", r)
	}
	return r, err
}
func (s *Service) skillRunDir(r skill.Run) (string, error) {
	if !validTaskID(r.ID) || !validTaskID(r.ProjectID) {
		return "", model.ErrArgument
	}
	full, err := s.harnessDirectory(filepath.Join("projects", r.ProjectID, "skill-runs", r.ID, "files"))
	return filepath.Dir(full), err
}
func (s *Service) SkillRunSteps(ctx context.Context, actor, id string) ([]json.RawMessage, error) {
	if err := requireHumanConfirm(actor, "read operation steps"); err != nil {
		return nil, err
	}
	if _, err := s.GetSkillRun(ctx, actor, id); err != nil {
		return nil, err
	}
	return s.st.SkillSteps(ctx, id)
}
func (s *Service) SkillRunRevisions(ctx context.Context, actor, id string) ([]json.RawMessage, error) {
	if err := requireHumanConfirm(actor, "read operation revisions"); err != nil {
		return nil, err
	}
	if _, err := s.GetSkillRun(ctx, actor, id); err != nil {
		return nil, err
	}
	return s.st.SkillRevisions(ctx, id)
}
