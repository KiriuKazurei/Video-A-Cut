package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/scriptsyntax"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

type TimelineHistory struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Actor   string `json:"actor"`
	Applied bool   `json:"applied"`
}
type TimelineView struct {
	Session         string            `json:"session"`
	ProjectRevision uint64            `json:"project_revision"`
	Sequence        project.Sequence  `json:"sequence"`
	History         []TimelineHistory `json:"history"`
	ContentVersion  string            `json:"content_version"`
}
type timelineReceipt struct {
	Hash string
	View TimelineView
}
type SequenceRequest struct {
	Session          string `json:"session"`
	ExpectedRevision uint64 `json:"expected_revision"`
	IdempotencyKey   string `json:"idempotency_key"`
	Action           string `json:"action"`
	SequenceID       string `json:"sequence_id"`
	Name             string `json:"name"`
}

func timelineView(doc *project.AvpDocument, seq project.Sequence, session string, revision uint64) TimelineView {
	history := []TimelineHistory{}
	for i, h := range seq.History {
		history = append(history, TimelineHistory{h.ID, h.Label, h.Actor, i < seq.HistoryCursor})
	}
	seq.History = nil
	return TimelineView{session, revision, seq, history, sequenceContentVersion(doc, seq)}
}
func sequenceContentVersion(doc *project.AvpDocument, seq project.Sequence) string {
	// UI state, marker labels and undo history do not change picture or sound.
	raw, _ := json.Marshal(seq.SequenceState)
	var state project.SequenceState
	_ = json.Unmarshal(raw, &state)
	state.Name = ""
	state.Markers = nil
	state.Groups = nil
	sources := map[string]string{}
	for i := range state.Tracks {
		t := &state.Tracks[i]
		t.Name = ""
		t.Height = 0
		t.Locked = false
		for j := range t.Clips {
			c := &t.Clips[j]
			c.Name = ""
			c.Origin = project.Origin{}
			for _, r := range doc.Resources {
				if r.ID == c.ResourceID {
					sources[r.ID] = project.ResourceContentVersion(r)
				}
			}
		}
	}
	return skillHash(map[string]any{"state": state, "sources": sources})
}
func sequenceIndex(d *project.AvpDocument, id string) int {
	return slices.IndexFunc(d.Sequences, func(s project.Sequence) bool { return s.ID == id })
}

func (s *Service) Timeline(ctx context.Context, projectID, sequenceID string) (TimelineView, error) {
	w := s.workspace
	if w == nil {
		return TimelineView{}, model.ErrInvalidState
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.document == nil || w.document.Project.ID != projectID {
		return TimelineView{}, model.ErrNotFound
	}
	i := sequenceIndex(w.document, sequenceID)
	if i < 0 {
		return TimelineView{}, model.ErrNotFound
	}
	doc := cloneProject(w.document)
	return timelineView(doc, doc.Sequences[i], w.session, w.revision), nil
}
func (s *Service) ReferenceDirectory(projectID, sequenceID string) ([]project.ReferenceCandidate, error) {
	w := s.workspace
	if w == nil {
		return nil, model.ErrInvalidState
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.document == nil || w.document.Project.ID != projectID {
		return nil, model.ErrNotFound
	}
	if sequenceID != "" && sequenceIndex(w.document, sequenceID) < 0 {
		return nil, model.ErrNotFound
	}
	return project.ReferenceDirectory(w.document, sequenceID), nil
}
func (s *Service) ResolveProjectReferences(projectID string, refs []project.Reference) ([]project.ReferenceCandidate, error) {
	w := s.workspace
	if w == nil {
		return nil, model.ErrInvalidState
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.document == nil || w.document.Project.ID != projectID {
		return nil, model.ErrNotFound
	}
	out := []project.ReferenceCandidate{}
	for _, ref := range refs {
		r, err := project.ResolveReference(w.document, ref)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", err, model.ErrConflict)
		}
		r.ID = ref.ID
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) ManageSequence(ctx context.Context, projectID string, req SequenceRequest) (project.Session, error) {
	w := s.workspace
	if w == nil {
		return project.Session{}, model.ErrInvalidState
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
	if req.IdempotencyKey == "" {
		w.mu.Unlock()
		return project.Session{}, model.ErrArgument
	}
	key := "manage:" + req.IdempotencyKey
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
	draft := cloneProject(w.document)
	i := sequenceIndex(draft, req.SequenceID)
	switch req.Action {
	case "create":
		name := strings.TrimSpace(req.Name)
		if name == "" {
			name = "新序列"
		}
		draft.Sequences = append(draft.Sequences, project.NewSequence(name))
	case "duplicate":
		if i < 0 {
			w.mu.Unlock()
			return project.Session{}, model.ErrNotFound
		}
		raw, _ := json.Marshal(draft.Sequences[i])
		var seq project.Sequence
		_ = json.Unmarshal(raw, &seq)
		seq.ID = project.TimelineID("seq")
		seq.Name += " 副本"
		seq.Revision = 1
		seq.History = nil
		seq.HistoryCursor = 0
		// References in another sequence must never silently follow duplicated clips.
		links := map[string]string{}
		for t := range seq.Tracks {
			seq.Tracks[t].ID = project.TimelineID("trk")
			for c := range seq.Tracks[t].Clips {
				clip := &seq.Tracks[t].Clips[c]
				clip.ID = project.TimelineID("clip")
				if clip.LinkID != "" {
					if links[clip.LinkID] == "" {
						links[clip.LinkID] = project.TimelineID("link")
					}
					clip.LinkID = links[clip.LinkID]
				}
			}
		}
		for j := range seq.Markers {
			seq.Markers[j].ID = project.TimelineID("mark")
		}
		draft.Sequences = append(draft.Sequences, seq)
	case "delete":
		if i < 0 {
			w.mu.Unlock()
			return project.Session{}, model.ErrNotFound
		}
		if len(draft.Sequences) == 1 {
			w.mu.Unlock()
			return project.Session{}, fmt.Errorf("请至少保留一条序列: %w", model.ErrArgument)
		}
		draft.Sequences = slices.Delete(draft.Sequences, i, i+1)
	default:
		w.mu.Unlock()
		return project.Session{}, model.ErrArgument
	}
	w.document = draft
	if w.timelineReceipts == nil {
		w.timelineReceipts = map[string]timelineReceipt{}
	}
	w.timelineReceipts[key] = timelineReceipt{Hash: hash}
	w.revision++
	w.dirty = true
	w.mu.Unlock()
	s.publish("workspace.timeline.changed", map[string]any{"project_id": projectID, "sequence_id": req.SequenceID})
	return s.ProjectWorkspace(ctx)
}

func (s *Service) TimelineCommand(ctx context.Context, actor, projectID, sequenceID string, req project.Command) (TimelineView, error) {
	w := s.workspace
	if w == nil {
		return TimelineView{}, model.ErrInvalidState
	}
	if err := w.lockMutation(ctx); err != nil {
		return TimelineView{}, err
	}
	defer w.op.Unlock()
	if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 128 || len(req.Operations) == 0 || len(req.Operations) > 512 {
		return TimelineView{}, model.ErrArgument
	}
	w.mu.Lock()
	if w.document == nil || w.document.Project.ID != projectID || req.Session != w.session {
		w.mu.Unlock()
		return TimelineView{}, model.ErrConflict
	}
	key := sequenceID + ":" + req.IdempotencyKey
	hash := skillHash(req)
	if receipt, ok := w.timelineReceipts[key]; ok {
		w.mu.Unlock()
		if receipt.Hash != hash {
			return TimelineView{}, model.ErrConflict
		}
		return receipt.View, nil
	}
	draft := cloneProject(w.document)
	index := sequenceIndex(draft, sequenceID)
	if index < 0 {
		w.mu.Unlock()
		return TimelineView{}, model.ErrNotFound
	}
	if draft.Sequences[index].Revision != req.ExpectedRevision {
		w.mu.Unlock()
		return TimelineView{}, fmt.Errorf("序列已更新，请刷新后重试: %w", model.ErrConflict)
	}
	if len(w.timelineReceipts) >= 10000 {
		w.mu.Unlock()
		return TimelineView{}, fmt.Errorf("请保存并重新打开工程以继续编辑: %w", model.ErrInvalidState)
	}
	w.mu.Unlock()
	if err := s.captureProjectScripts(ctx, draft); err != nil {
		return TimelineView{}, err
	}
	seq := &draft.Sequences[index]
	before := seq.Snapshot(draft)
	if len(req.Operations) == 1 && (req.Operations[0].Type == "history.undo" || req.Operations[0].Type == "history.redo") {
		undo := req.Operations[0].Type == "history.undo"
		cursor := seq.HistoryCursor
		if undo {
			cursor--
		}
		if cursor < 0 || cursor >= len(seq.History) {
			return TimelineView{}, model.ErrInvalidState
		}
		record := seq.History[cursor]
		restore := record.After
		previous := record.Before
		if undo {
			restore, previous = previous, restore
		}
		seq.SequenceState = restore.State
		for i := range draft.Resources {
			r := &draft.Resources[i]
			a, b := restore.SourceMarkers[r.ID], previous.SourceMarkers[r.ID]
			if skillHash(a) != skillHash(b) {
				if skillHash(r.Markers) != skillHash(b) {
					return TimelineView{}, fmt.Errorf("素材标记在其他序列中已改变: %w", model.ErrConflict)
				}
				r.Markers = a
			}
		}
		for _, m := range seq.Markers {
			project.RenameMarkerReferences(draft, m.ID, m.Notes)
		}
		for _, r := range draft.Resources {
			for _, m := range r.Markers {
				project.RenameMarkerReferences(draft, m.ID, m.Notes)
			}
		}
		if undo {
			seq.HistoryCursor--
		} else {
			seq.HistoryCursor++
		}
	} else {
		origin := project.Origin{Actor: actor}
		if strings.HasPrefix(actor, "agent:") {
			origin.RunID = strings.TrimPrefix(actor, "agent:")
		}
		pasteLinks := map[string]string{}
		oldRate := seq.FPS
		if !seq.Configured {
		outer:
			for _, op := range req.Operations {
				ids := []string{op.ResourceID}
				for _, c := range op.Clips {
					ids = append(ids, c.ResourceID)
				}
				for _, id := range ids {
					for _, r := range draft.Resources {
						if r.ID == id && r.Kind == "video" {
							seq.ConfigureResource(r)
							break outer
						}
					}
				}
			}
		}
		for _, op := range req.Operations {
			op = project.RebaseOperation(op, oldRate, seq.FPS)
			if op.Type == "clip.paste" {
				for i := range op.Clips {
					c := &op.Clips[i]
					if c.LinkID != "" {
						if pasteLinks[c.LinkID] == "" {
							pasteLinks[c.LinkID] = project.TimelineID("link")
						}
						c.LinkID = pasteLinks[c.LinkID]
					}
				}
			}
			expanded, err := s.expandSubtitleInsert(ctx, draft, seq, op)
			if err != nil {
				return TimelineView{}, err
			}
			for _, operation := range expanded {
				if err := seq.Apply(draft, operation, origin); err != nil {
					return TimelineView{}, fmt.Errorf("%v: %w", err, model.ErrArgument)
				}
			}
		}
		if err := draft.Validate(); err != nil {
			return TimelineView{}, fmt.Errorf("%v: %w", err, model.ErrArgument)
		}
		label := strings.TrimSpace(req.Label)
		if label == "" {
			label = req.Operations[0].Type
		}
		seq.History = append(seq.History[:seq.HistoryCursor], project.EditRecord{ID: req.IdempotencyKey, Label: label, Actor: actor, Before: before, After: seq.Snapshot(draft)})
		if len(seq.History) > 100 {
			seq.History = seq.History[len(seq.History)-100:]
		}
		seq.HistoryCursor = len(seq.History)
	}
	if err := draft.Validate(); err != nil {
		return TimelineView{}, fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	if err := s.syncTimelineScripts(ctx, draft); err != nil {
		return TimelineView{}, err
	}
	seq.Revision++
	w.mu.Lock()
	w.document = draft
	w.revision++
	w.dirty = true
	draft.Project.UpdatedAt = s.autoNow()
	view := timelineView(cloneProject(draft), draft.Sequences[index], w.session, w.revision)
	if w.timelineReceipts == nil {
		w.timelineReceipts = map[string]timelineReceipt{}
	}
	w.timelineReceipts[key] = timelineReceipt{hash, view}
	w.mu.Unlock()
	s.publish("workspace.timeline.changed", map[string]any{"project_id": projectID, "sequence_id": sequenceID, "revision": seq.Revision, "content_version": view.ContentVersion})
	return view, nil
}

// Marker rename and its bound script spelling commit in one SQLite transaction
// before exposing the new in-memory sequence. No external AVS is written.
func (s *Service) syncTimelineScripts(ctx context.Context, doc *project.AvpDocument) error {
	return s.st.Transaction(ctx, func(tx *store.Store) error {
		for _, sc := range doc.Scripts {
			w, err := workbenchAt(ctx, tx, sc.ID)
			if err != nil {
				return err
			}
			if w.Script.Body == sc.Body && skillHash(w.References) == skillHash(sc.References) {
				continue
			}
			old := w.Version
			w.Script.Body = sc.Body
			w.References = sc.References
			w.Document = scriptsyntax.Parse(sc.Body)
			w.Script.UpdatedAt = s.autoNow()
			if err = tx.UpdateScript(ctx, w.Script); err != nil {
				return err
			}
			raw, _ := json.Marshal(w)
			if err = tx.PutScriptWorkbenchMetadata(ctx, sc.ID, old, string(raw)); err != nil {
				return err
			}
		}
		return nil
	})
}

type FrameAnchor struct {
	SequenceID string        `json:"sequence_id"`
	ClipID     string        `json:"clip_id"`
	Frame      int64         `json:"frame"`
	SourceTime *project.Time `json:"source_time,omitempty"`
}
type FrameModificationRequest struct {
	Session          string      `json:"session"`
	ExpectedRevision uint64      `json:"expected_revision"`
	IdempotencyKey   string      `json:"idempotency_key"`
	Anchor           FrameAnchor `json:"anchor"`
	Instruction      string      `json:"instruction"`
}
type FrameModificationResult struct {
	Enabled   bool              `json:"enabled"`
	Resource  *project.Resource `json:"resource,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}
