package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

type DrawSaveRequest struct {
	Session          string                  `json:"session"`
	ExpectedRevision uint64                  `json:"expected_revision"`
	IdempotencyKey   string                  `json:"idempotency_key"`
	Composition      project.DrawComposition `json:"composition"`
}
type DrawActionRequest struct {
	Session        string `json:"session"`
	ContentVersion string `json:"content_version"`
}
type DrawRender struct {
	ContentVersion string  `json:"content_version"`
	SHA256         string  `json:"sha256"`
	Size           int64   `json:"size_bytes"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Frames         int64   `json:"frames"`
	Duration       float64 `json:"duration"`
	Engine         string  `json:"engine"`
	RuntimeVersion string  `json:"runtime_version"`
}
type DrawImportRequest struct {
	Session          string `json:"session"`
	ContentVersion   string `json:"content_version"`
	ExpectedRevision uint64 `json:"expected_revision"`
	IdempotencyKey   string `json:"idempotency_key"`
	SequenceID       string `json:"sequence_id"`
}

func (s *Service) DrawCompositions(projectID string) ([]project.DrawComposition, error) {
	w := s.workspace
	if w == nil {
		return nil, model.ErrInvalidState
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.document == nil || w.document.Project.ID != projectID {
		return nil, model.ErrNotFound
	}
	out := append([]project.DrawComposition{}, w.document.DrawCompositions...)
	return out, nil
}
func (s *Service) drawComposition(projectID, id string) (project.DrawComposition, error) {
	list, err := s.DrawCompositions(projectID)
	if err != nil {
		return project.DrawComposition{}, err
	}
	for _, c := range list {
		if c.ID == id {
			return c, nil
		}
	}
	return project.DrawComposition{}, model.ErrNotFound
}
func (s *Service) SaveDrawComposition(ctx context.Context, projectID string, req DrawSaveRequest) (project.Session, error) {
	w := s.workspace
	if w == nil {
		return project.Session{}, model.ErrInvalidState
	}
	if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 128 {
		return project.Session{}, model.ErrArgument
	}
	if err := w.lockMutation(ctx); err != nil {
		return project.Session{}, err
	}
	defer w.op.Unlock()
	w.mu.Lock()
	if w.document == nil || w.document.Project.ID != projectID || req.Session != w.session {
		w.mu.Unlock()
		return project.Session{}, model.ErrConflict
	}
	key := "draw-save:" + req.IdempotencyKey
	hash := skillHash(req)
	if old, ok := w.timelineReceipts[key]; ok {
		w.mu.Unlock()
		if old.Hash != hash {
			return project.Session{}, model.ErrConflict
		}
		return s.ProjectWorkspace(ctx)
	}
	if req.ExpectedRevision != w.revision {
		w.mu.Unlock()
		return project.Session{}, model.ErrConflict
	}
	c := req.Composition
	if c.ID == "" {
		c.ID = newID("draw")
	}
	for i := range c.Images {
		image := &c.Images[i]
		resource := slices.IndexFunc(w.document.Resources, func(r project.Resource) bool { return r.ID == image.ResourceID && r.Kind == "image" })
		if resource < 0 {
			w.mu.Unlock()
			return project.Session{}, fmt.Errorf("图片不属于当前工程: %w", model.ErrNotFound)
		}
		image.ResourceSHA = project.ResourceContentVersion(w.document.Resources[resource])
	}
	if err := c.Validate(); err != nil {
		w.mu.Unlock()
		return project.Session{}, fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	draft := cloneProject(w.document)
	i := slices.IndexFunc(draft.DrawCompositions, func(old project.DrawComposition) bool { return old.ID == c.ID })
	expected := c.Revision
	c.Revision = 1
	if i >= 0 {
		if expected != draft.DrawCompositions[i].Revision {
			w.mu.Unlock()
			return project.Session{}, model.ErrConflict
		}
		c.Revision = draft.DrawCompositions[i].Revision + 1
	} else if expected != 0 || len(draft.DrawCompositions) >= 16 {
		w.mu.Unlock()
		return project.Session{}, model.ErrArgument
	}
	c.ContentVersion = c.ContentHash()
	if i < 0 {
		draft.DrawCompositions = append(draft.DrawCompositions, c)
	} else {
		draft.DrawCompositions[i] = c
	}
	if err := draft.Validate(); err != nil {
		w.mu.Unlock()
		return project.Session{}, err
	}
	draft.Project.UpdatedAt = s.autoNow()
	w.document = draft
	w.revision++
	w.dirty = true
	if w.timelineReceipts == nil {
		w.timelineReceipts = map[string]timelineReceipt{}
	}
	w.timelineReceipts[key] = timelineReceipt{Hash: hash}
	w.mu.Unlock()
	s.publish("workspace.timeline.changed", map[string]any{"project_id": projectID, "draw_id": c.ID})
	return s.ProjectWorkspace(ctx)
}
func (s *Service) drawContext(projectID, id string, req DrawActionRequest) (project.DrawComposition, error) {
	c, err := s.drawComposition(projectID, id)
	if err != nil {
		return c, err
	}
	if req.Session != s.ProjectSessionID() || req.ContentVersion != c.ContentVersion {
		return c, model.ErrConflict
	}
	return c, nil
}
func (s *Service) drawDir(projectID, id, version string) string {
	return filepath.Join(s.workspace.root, "draw", skillHash([]string{projectID, id, version})[:32])
}
func (s *Service) CheckDrawComposition(ctx context.Context, projectID, id string, req DrawActionRequest) (json.RawMessage, error) {
	c, err := s.drawContext(projectID, id, req)
	if err != nil {
		return nil, err
	}
	assets, err := s.drawImageAssets(ctx, projectID, c)
	if err != nil {
		return nil, err
	}
	raw, err := s.scriptProcess(ctx, true, "draw-check", map[string]any{"composition": c, "assets": assets})
	if err != nil {
		return nil, err
	}
	if _, err = s.drawContext(projectID, id, req); err != nil {
		return nil, err
	}
	return raw, nil
}
func (s *Service) RenderDrawComposition(ctx context.Context, projectID, id string, req DrawActionRequest) (DrawRender, error) {
	var result DrawRender
	c, err := s.drawContext(projectID, id, req)
	if err != nil {
		return result, err
	}
	release, err := s.acquireScript("draw:" + projectID + ":" + id + ":" + c.ContentVersion)
	if err != nil {
		return result, err
	}
	defer release()
	dir := s.drawDir(projectID, id, c.ContentVersion)
	assets, err := s.drawImageAssets(ctx, projectID, c)
	if err != nil {
		return result, err
	}
	raw, err := s.scriptProcess(ctx, true, "draw-render", map[string]any{"composition": c, "output_dir": dir, "assets": assets})
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if result.ContentVersion != c.ContentVersion || result.Frames != c.DurationFrames || result.Width != c.Width || result.Height != c.Height || result.Engine != c.Engine {
		return result, model.ErrConflict
	}
	if _, err = s.drawContext(projectID, id, req); err != nil {
		return result, err
	}
	if _, err = s.drawImageAssets(ctx, projectID, c); err != nil {
		return result, err
	}
	s.publish("workspace.timeline.changed", map[string]any{"project_id": projectID, "draw_id": id, "rendered": true})
	return result, nil
}
func (s *Service) DrawRenderPath(projectID, id, version string) (string, DrawRender, error) {
	var report DrawRender
	c, err := s.drawComposition(projectID, id)
	if err != nil {
		return "", report, err
	}
	if version != c.ContentVersion {
		return "", report, model.ErrConflict
	}
	if _, err = s.drawImageAssets(context.Background(), projectID, c); err != nil {
		return "", report, err
	}
	dir := s.drawDir(projectID, id, version)
	raw, err := os.ReadFile(filepath.Join(dir, "program.json"))
	if err != nil {
		return "", report, model.ErrNotFound
	}
	if json.Unmarshal(raw, &report) != nil || report.ContentVersion != version {
		return "", report, model.ErrConflict
	}
	path := filepath.Join(dir, "program.mp4")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", report, model.ErrNotFound
	}
	sha, err := sequenceFileDigest(path)
	if err != nil {
		return "", report, err
	}
	if sha != report.SHA256 || info.Size() != report.Size {
		return "", report, model.ErrConflict
	}
	return path, report, nil
}

// Import commits one undoable Agent track group. Rendering never changes an unrelated sequence.
func (s *Service) ImportDrawComposition(ctx context.Context, actor, projectID, id string, req DrawImportRequest) (TimelineView, error) {
	path, report, err := s.DrawRenderPath(projectID, id, req.ContentVersion)
	if err != nil {
		return TimelineView{}, err
	}
	c, err := s.drawContext(projectID, id, DrawActionRequest{req.Session, req.ContentVersion})
	if err != nil {
		return TimelineView{}, err
	}
	if req.IdempotencyKey == "" {
		return TimelineView{}, model.ErrArgument
	}
	w := s.workspace
	if err = w.lockMutation(ctx); err != nil {
		return TimelineView{}, err
	}
	defer w.op.Unlock()
	w.mu.Lock()
	if w.document == nil || w.document.Project.ID != projectID || w.session != req.Session {
		w.mu.Unlock()
		return TimelineView{}, model.ErrConflict
	}
	draft := cloneProject(w.document)
	index := sequenceIndex(draft, req.SequenceID)
	if index < 0 {
		w.mu.Unlock()
		return TimelineView{}, model.ErrNotFound
	}
	key := "draw-import:" + req.IdempotencyKey
	hash := skillHash(req)
	if old, ok := w.timelineReceipts[key]; ok {
		view := timelineView(draft, draft.Sequences[index], w.session, w.revision)
		w.mu.Unlock()
		if old.Hash != hash {
			return TimelineView{}, model.ErrConflict
		}
		return view, nil
	}
	if draft.Sequences[index].Revision != req.ExpectedRevision {
		w.mu.Unlock()
		return TimelineView{}, model.ErrConflict
	}
	current := slices.IndexFunc(draft.DrawCompositions, func(x project.DrawComposition) bool { return x.ID == id })
	if current < 0 || draft.DrawCompositions[current].ContentVersion != req.ContentVersion {
		w.mu.Unlock()
		return TimelineView{}, model.ErrConflict
	}
	w.mu.Unlock()
	if err = s.captureProjectScripts(ctx, draft); err != nil {
		return TimelineView{}, err
	}
	resource, err := s.inspectResource(ctx, path)
	if err != nil {
		return TimelineView{}, err
	}
	if resource.Source.SHA256 != report.SHA256 {
		return TimelineView{}, model.ErrConflict
	}
	resource.ID = "res_" + skillHash([]string{projectID, id, req.ContentVersion})[:24]
	resource.AssetID = resource.ID
	resource.Name = c.Name + ".mp4"
	seq := &draft.Sequences[index]
	before := seq.Snapshot(draft)
	ri := slices.IndexFunc(draft.Resources, func(r project.Resource) bool { return r.ID == resource.ID })
	if ri < 0 {
		draft.Resources = append(draft.Resources, resource)
	} else {
		draft.Resources[ri] = resource
	}
	origin := project.Origin{Actor: actor, ArtifactID: id, SHA256: report.SHA256}
	if strings.HasPrefix(actor, "agent:") {
		origin.RunID = strings.TrimPrefix(actor, "agent:")
	}
	group := newID("grp")
	seq.Groups = append(seq.Groups, project.TrackGroup{ID: group, Name: "绘制 · " + c.Name, Origin: origin})
	if err = seq.Apply(draft, project.Operation{Type: "track.add", Kind: "video", GroupID: group, Name: c.Name}, origin); err != nil {
		return TimelineView{}, err
	}
	track := seq.Tracks[len(seq.Tracks)-1].ID
	if err = seq.Apply(draft, project.Operation{Type: "clip.insert", TrackID: track, ResourceID: resource.ID, At: 0}, origin); err != nil {
		return TimelineView{}, err
	}
	seq.ScopeGroup = group
	seq.Revision++
	seq.History = append(seq.History[:seq.HistoryCursor], project.EditRecord{ID: req.IdempotencyKey, Label: "绘制成片入轨", Actor: actor, Before: before, After: seq.Snapshot(draft)})
	seq.HistoryCursor = len(seq.History)
	if err = draft.Validate(); err != nil {
		return TimelineView{}, err
	}
	if err = s.materializeResource(ctx, resource, projectID); err != nil {
		return TimelineView{}, err
	}
	w.mu.Lock()
	w.document = draft
	w.revision++
	w.dirty = true
	draft.Project.UpdatedAt = s.autoNow()
	if w.timelineReceipts == nil {
		w.timelineReceipts = map[string]timelineReceipt{}
	}
	w.timelineReceipts[key] = timelineReceipt{Hash: hash}
	view := timelineView(draft, *seq, w.session, w.revision)
	w.mu.Unlock()
	if err = s.refreshProjectMedia(ctx); err != nil {
		return TimelineView{}, err
	}
	s.publish("workspace.timeline.changed", map[string]any{"project_id": projectID, "sequence_id": seq.ID})
	return view, nil
}
