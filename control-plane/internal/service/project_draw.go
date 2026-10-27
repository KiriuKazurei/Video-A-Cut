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
type DrawRenderRequest struct {
	DrawActionRequest
	Format string `json:"format,omitempty"`
}
type DrawRender struct {
	Format             string  `json:"format,omitempty"`
	Alpha              string  `json:"alpha,omitempty"`
	ColorSpace         string  `json:"color_space,omitempty"`
	EnvironmentVersion string  `json:"environment_version,omitempty"`
	ContentVersion     string  `json:"content_version"`
	SHA256             string  `json:"sha256"`
	Size               int64   `json:"size_bytes"`
	Width              int     `json:"width"`
	Height             int     `json:"height"`
	Frames             int64   `json:"frames"`
	Duration           float64 `json:"duration"`
	Engine             string  `json:"engine"`
	RuntimeVersion     string  `json:"runtime_version"`
}
type DrawImportRequest struct {
	SourceMode       string `json:"source_mode,omitempty"`
	Session          string `json:"session"`
	ContentVersion   string `json:"content_version"`
	ExpectedRevision uint64 `json:"expected_revision"`
	IdempotencyKey   string `json:"idempotency_key"`
	SequenceID       string `json:"sequence_id"`
}
type drawEdit struct{ Before, After project.DrawComposition }
type drawEditHistory struct {
	Edits  []drawEdit
	Cursor int
}
type DrawHistory struct {
	CanUndo bool `json:"can_undo"`
	CanRedo bool `json:"can_redo"`
}
type DrawHistoryRequest struct {
	Session             string `json:"session"`
	ExpectedRevision    uint64 `json:"expected_revision"`
	CompositionRevision uint64 `json:"composition_revision"`
	IdempotencyKey      string `json:"idempotency_key"`
	Action              string `json:"action"`
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
	out := []project.DrawComposition{}
	raw, _ := json.Marshal(w.document.DrawCompositions)
	_ = json.Unmarshal(raw, &out)
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
	var c project.DrawComposition
	raw, _ := json.Marshal(req.Composition)
	_ = json.Unmarshal(raw, &c)
	if c.Document != nil {
		if err := project.ValidateDrawImageBindings(c.Images); err != nil {
			w.mu.Unlock()
			return project.Session{}, fmt.Errorf("composition images: %v: %w", err, model.ErrArgument)
		}
		for _, source := range c.Document.Sources {
			if err := project.ValidateDrawImageBindings(source.ImageBindings); err != nil {
				w.mu.Unlock()
				return project.Session{}, fmt.Errorf("source %s image bindings: %v: %w", source.ID, err, model.ErrArgument)
			}
		}
	}
	c.NormalizeDocument()
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
	if c.Document != nil {
		for i := range c.Document.Nodes {
			n := &c.Document.Nodes[i]
			if n.Kind != "image" {
				continue
			}
			ri := slices.IndexFunc(w.document.Resources, func(r project.Resource) bool { return r.ID == n.ResourceID && r.Kind == "image" })
			if ri < 0 {
				w.mu.Unlock()
				return project.Session{}, model.ErrNotFound
			}
			n.ResourceSHA = project.ResourceContentVersion(w.document.Resources[ri])
		}
	}
	if err := bindDrawingAssets(w.document, &c); err != nil {
		w.mu.Unlock()
		return project.Session{}, err
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
	var previous *project.DrawComposition
	if i < 0 {
		draft.DrawCompositions = append(draft.DrawCompositions, c)
	} else {
		old := draft.DrawCompositions[i]
		previous = &old
		draft.DrawCompositions[i] = c
	}
	if err := draft.Validate(); err != nil {
		w.mu.Unlock()
		return project.Session{}, err
	}
	draft.Project.UpdatedAt = s.autoNow()
	if previous != nil {
		if w.drawHistories == nil {
			w.drawHistories = map[string]*drawEditHistory{}
		}
		h := w.drawHistories[c.ID]
		if h == nil {
			h = &drawEditHistory{}
			w.drawHistories[c.ID] = h
		}
		h.Edits = append(h.Edits[:h.Cursor], drawEdit{Before: *previous, After: c})
		if len(h.Edits) > 32 {
			h.Edits = h.Edits[len(h.Edits)-32:]
		}
		h.Cursor = len(h.Edits)
	}
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
func (s *Service) DrawingHistory(projectID, id string) (DrawHistory, error) {
	w := s.workspace
	if w == nil {
		return DrawHistory{}, model.ErrInvalidState
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.document == nil || w.document.Project.ID != projectID || !slices.ContainsFunc(w.document.DrawCompositions, func(c project.DrawComposition) bool { return c.ID == id }) {
		return DrawHistory{}, model.ErrNotFound
	}
	h := w.drawHistories[id]
	if h == nil {
		return DrawHistory{}, nil
	}
	return DrawHistory{h.Cursor > 0, h.Cursor < len(h.Edits)}, nil
}

// Human history changes the complete scene atomically; it never rewinds runs,
// budgets, disk media or frozen source resources. Like resource history it is
// scoped to the current open session, not persisted in the AVP.
func (s *Service) MoveDrawingHistory(ctx context.Context, projectID, id string, req DrawHistoryRequest) (project.Session, error) {
	w := s.workspace
	if w == nil {
		return project.Session{}, model.ErrInvalidState
	}
	if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 128 || req.Action != "undo" && req.Action != "redo" {
		return project.Session{}, model.ErrArgument
	}
	if e := w.lockMutation(ctx); e != nil {
		return project.Session{}, e
	}
	defer w.op.Unlock()
	w.mu.Lock()
	if w.document == nil || w.document.Project.ID != projectID || w.session != req.Session {
		w.mu.Unlock()
		return project.Session{}, model.ErrConflict
	}
	key := "draw-history:" + req.IdempotencyKey
	hash := skillHash([]any{projectID, id, req})
	if old, ok := w.timelineReceipts[key]; ok {
		w.mu.Unlock()
		if old.Hash != hash {
			return project.Session{}, model.ErrConflict
		}
		return s.ProjectWorkspace(ctx)
	}
	i := slices.IndexFunc(w.document.DrawCompositions, func(c project.DrawComposition) bool { return c.ID == id })
	if i < 0 || w.revision != req.ExpectedRevision || w.document.DrawCompositions[i].Revision != req.CompositionRevision {
		w.mu.Unlock()
		return project.Session{}, model.ErrConflict
	}
	h := w.drawHistories[id]
	if h == nil || req.Action == "undo" && h.Cursor == 0 || req.Action == "redo" && h.Cursor == len(h.Edits) {
		w.mu.Unlock()
		return project.Session{}, model.ErrInvalidState
	}
	index := h.Cursor
	if req.Action == "undo" {
		index--
	}
	target := h.Edits[index].After
	if req.Action == "undo" {
		target = h.Edits[index].Before
	}
	var c project.DrawComposition
	raw, _ := json.Marshal(target)
	_ = json.Unmarshal(raw, &c)
	draft := cloneProject(w.document)
	c.Revision = draft.DrawCompositions[i].Revision + 1
	if e := bindDrawingAssets(draft, &c); e != nil {
		w.mu.Unlock()
		return project.Session{}, e
	}
	c.NormalizeDocument()
	c.ContentVersion = c.ContentHash()
	draft.DrawCompositions[i] = c
	if e := draft.Validate(); e != nil {
		w.mu.Unlock()
		return project.Session{}, e
	}
	draft.Project.UpdatedAt = s.autoNow()
	w.document = draft
	w.revision++
	w.dirty = true
	if req.Action == "undo" {
		h.Cursor--
	} else {
		h.Cursor++
	}
	if w.timelineReceipts == nil {
		w.timelineReceipts = map[string]timelineReceipt{}
	}
	w.timelineReceipts[key] = timelineReceipt{Hash: hash}
	w.mu.Unlock()
	s.publish("workspace.timeline.changed", map[string]any{"project_id": projectID, "draw_id": id})
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
	return s.RenderDrawCompositionFormat(ctx, projectID, id, DrawRenderRequest{DrawActionRequest: req})
}
func drawFormat(format string) (string, string, error) {
	switch format {
	case "", "mp4":
		return "mp4", ".mp4", nil
	case "webm-alpha":
		return format, ".webm", nil
	case "prores4444":
		return format, ".mov", nil
	default:
		return "", "", model.ErrArgument
	}
}
func (s *Service) RenderDrawCompositionFormat(ctx context.Context, projectID, id string, req DrawRenderRequest) (DrawRender, error) {
	var result DrawRender
	format, _, err := drawFormat(req.Format)
	if err != nil {
		return result, err
	}
	environment := drawingEnvironmentVersion()
	c, err := s.drawContext(projectID, id, req.DrawActionRequest)
	if err != nil {
		return result, err
	}
	release, err := s.acquireScript("draw:" + projectID + ":" + id + ":" + c.ContentVersion + ":" + format)
	if err != nil {
		return result, err
	}
	defer release()
	dir := s.drawDir(projectID, id, c.ContentVersion)
	if format != "mp4" {
		dir = filepath.Join(dir, format)
		if c.Document == nil {
			document := c.UnifiedDocument()
			c.Document = &document
			c.NormalizeDocument()
		}
	}
	assets, err := s.drawImageAssets(ctx, projectID, c)
	if err != nil {
		return result, err
	}
	raw, err := s.scriptProcess(ctx, true, "draw-render", map[string]any{"composition": c, "output_dir": dir, "assets": assets, "environment_version": environment, "format": format})
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if result.ContentVersion != c.ContentVersion || result.Frames != c.DurationFrames || result.Width != c.Width || result.Height != c.Height || result.Engine != c.Engine || result.EnvironmentVersion != environment || drawingEnvironmentVersion() != environment {
		return result, model.ErrConflict
	}
	if _, err = s.drawContext(projectID, id, req.DrawActionRequest); err != nil {
		return result, err
	}
	if _, err = s.drawImageAssets(ctx, projectID, c); err != nil {
		return result, err
	}
	s.publish("workspace.timeline.changed", map[string]any{"project_id": projectID, "draw_id": id, "rendered": true})
	return result, nil
}
func (s *Service) DrawRenderPath(projectID, id, version string) (string, DrawRender, error) {
	return s.DrawRenderPathFormat(projectID, id, version, "mp4")
}
func (s *Service) DrawRenderPathFormat(projectID, id, version, format string) (string, DrawRender, error) {
	var report DrawRender
	format, extension, err := drawFormat(format)
	if err != nil {
		return "", report, err
	}
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
	if format != "mp4" {
		dir = filepath.Join(dir, format)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "program.json"))
	if err != nil {
		return "", report, model.ErrNotFound
	}
	if json.Unmarshal(raw, &report) != nil || report.ContentVersion != version || report.EnvironmentVersion != drawingEnvironmentVersion() {
		return "", report, model.ErrConflict
	}
	path := filepath.Join(dir, "program"+extension)
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

func (s *Service) ExportDrawComposition(ctx context.Context, projectID, id string, req DrawRenderRequest, target string) (DrawRender, error) {
	_, extension, err := drawFormat(req.Format)
	if err != nil || !filepath.IsAbs(target) || !strings.EqualFold(filepath.Ext(target), extension) {
		return DrawRender{}, model.ErrArgument
	}
	report, err := s.RenderDrawCompositionFormat(ctx, projectID, id, req)
	if err != nil {
		return report, err
	}
	w := s.workspace
	if err = w.lockMutation(ctx); err != nil {
		return report, err
	}
	defer w.op.Unlock()
	if _, err = s.drawContext(projectID, id, req.DrawActionRequest); err != nil {
		return report, err
	}
	path, _, err := s.DrawRenderPathFormat(projectID, id, req.ContentVersion, req.Format)
	if err != nil {
		return report, err
	}
	if err = project.CopyAtomic(target, path); err != nil {
		return report, err
	}
	return report, nil
}

// Import commits one undoable Agent track group. Rendering never changes an unrelated sequence.
func (s *Service) ImportDrawComposition(ctx context.Context, actor, projectID, id string, req DrawImportRequest) (TimelineView, error) {
	var path string
	var report DrawRender
	var err error
	if req.SourceMode == "" || req.SourceMode == "rendered" {
		path, report, err = s.DrawRenderPath(projectID, id, req.ContentVersion)
		if err != nil {
			return TimelineView{}, err
		}
	} else if req.SourceMode != "snapshot" && req.SourceMode != "follow" {
		return TimelineView{}, model.ErrArgument
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
	var resource project.Resource
	if path != "" {
		resource, err = s.inspectResource(ctx, path)
		if err != nil {
			return TimelineView{}, err
		}
		if resource.Source.SHA256 != report.SHA256 {
			return TimelineView{}, model.ErrConflict
		}
		resource.ID = "res_" + skillHash([]string{projectID, id, req.ContentVersion})[:24]
		resource.AssetID = resource.ID
		resource.Name = c.Name + ".mp4"
	} else {
		// Convert legacy source when freezing, without changing the working
		// composition or the original AVP file.
		frozen := c
		doc := frozen.UnifiedDocument()
		frozen.Document = &doc
		frozen.NormalizeDocument()
		if err = bindDrawingAssets(draft, &frozen); err != nil {
			return TimelineView{}, err
		}
		frozen.ContentVersion = frozen.ContentHash()
		resource = project.Resource{ID: "res_" + skillHash([]string{projectID, id, req.ContentVersion, req.SourceMode})[:24], Kind: "video", Name: c.Name + " · 源构图", Source: project.Source{SHA256: c.ContentVersion}, Out: c.FPS.Seconds(c.DurationFrames), Metadata: map[string]any{"duration": c.FPS.Seconds(c.DurationFrames), "width": c.Width, "height": c.Height, "avg_frame_rate": fmt.Sprintf("%d/%d", c.FPS.Num, c.FPS.Den), "streams": []map[string]any{{"index": 0, "codec_type": "video", "codec_name": "vac-draw"}}}, Draw: &project.DrawSourceReference{CompositionID: c.ID, Mode: req.SourceMode, Snapshot: c}}
		resource.Draw.Snapshot = frozen
		resource.Source.SHA256 = frozen.ContentVersion
		if _, err = s.drawImageAssets(ctx, projectID, c); err != nil {
			return TimelineView{}, err
		}
	}
	seq := &draft.Sequences[index]
	before := seq.Snapshot(draft)
	ri := slices.IndexFunc(draft.Resources, func(r project.Resource) bool { return r.ID == resource.ID })
	if ri < 0 {
		draft.Resources = append(draft.Resources, resource)
	} else {
		draft.Resources[ri] = resource
	}
	origin := project.Origin{Actor: actor, ArtifactID: id, SHA256: resource.Source.SHA256}
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
