package service

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/scriptsyntax"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

type ScriptFileImport struct {
	Path       string            `json:"path"`
	ScriptOnly bool              `json:"script_only"`
	Bindings   map[string]string `json:"bindings"`
	Files      map[string]string `json:"files"`
}

func (s *Service) InspectScriptFile(path string) (project.AvsDocument, error) {
	var doc project.AvsDocument
	if err := project.Read(path, "avs", &doc); err != nil {
		return doc, fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	if err := doc.Validate(); err != nil {
		return doc, fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	var segments []ScriptSegment
	if err := json.Unmarshal(doc.Segments, &segments); err != nil {
		return doc, model.ErrArgument
	}
	known := map[string]bool{}
	for _, b := range doc.Bindings {
		known[b.ID] = true
	}
	for _, seg := range segments {
		if seg.MediaID != "" && !known[seg.MediaID] {
			doc.Bindings = append(doc.Bindings, project.Binding{ID: seg.MediaID, Kind: "video", Name: seg.MediaID, Unresolved: true})
			known[seg.MediaID] = true
		}
	}
	// Explicit media names in the syntax are dependencies too, even in an
	// ungenerated draft without segments.
	for _, paragraph := range scriptsyntax.Parse(doc.Body).Paragraphs {
		if doc.SchemaVersion >= 2 && len(doc.References) > 0 {
			break
		}
		if paragraph.Video != "" && !known[paragraph.Video] {
			found := false
			for _, binding := range doc.Bindings {
				if binding.Name == paragraph.Video {
					found = true
				}
			}
			if !found {
				doc.Bindings = append(doc.Bindings, project.Binding{ID: paragraph.Video, Name: paragraph.Video, Kind: "video", Unresolved: true})
			}
			known[paragraph.Video] = true
		}
	}
	return doc, nil
}

func (s *Service) ImportScriptFile(ctx context.Context, projectID string, req ScriptFileImport) (project.Session, error) {
	w := s.workspace
	if w == nil {
		return project.Session{}, model.ErrInvalidState
	}
	if err := w.lockMutation(ctx); err != nil {
		return project.Session{}, err
	}
	defer w.op.Unlock()
	if projectID == "" || s.CurrentProjectID() != projectID {
		return project.Session{}, model.ErrNotFound
	}
	doc, err := s.InspectScriptFile(req.Path)
	if err != nil {
		return project.Session{}, err
	}
	if len(doc.Bindings) > 0 && !req.ScriptOnly && len(req.Bindings) == 0 && len(req.Files) == 0 {
		return project.Session{}, fmt.Errorf("请选择关联资源或仅导入脚本: %w", model.ErrArgument)
	}
	list, _ := s.ProjectResources(projectID)
	newResources := []project.Resource{}
	for i := range doc.Bindings {
		b := &doc.Bindings[i]
		b.ResourceID = ""
		b.Unresolved = true
		if req.ScriptOnly {
			continue
		}
		resourceID := req.Bindings[b.ID]
		if file := req.Files[b.ID]; file != "" {
			r, err := s.inspectResource(ctx, file)
			if err != nil {
				return project.Session{}, err
			}
			if r.Kind != "video" && r.Kind != "audio" {
				return project.Session{}, model.ErrArgument
			}
			for _, old := range append(list, newResources...) {
				if !old.Removed && strings.EqualFold(old.Source.OriginalPath, r.Source.OriginalPath) {
					r = old
					break
				}
			}
			if r.AssetID == "" {
				r.AssetID = r.ID
				newResources = append(newResources, r)
			}
			resourceID = r.ID
		}
		for _, r := range append(list, newResources...) {
			if r.ID == resourceID && !r.Removed && !r.Missing && (r.Kind == "video" || r.Kind == "audio") {
				b.ResourceID, b.Unresolved = r.ID, false
			}
		}
	}
	var segments []ScriptSegment
	_ = json.Unmarshal(doc.Segments, &segments)
	for i := range segments {
		seg := &segments[i]
		for j := range doc.Bindings {
			binding := &doc.Bindings[j]
			if binding.ID == seg.MediaID {
				binding.SegmentIndexes = append(binding.SegmentIndexes, i)
				if binding.Unresolved {
					seg.MediaID = ""
				} else {
					seg.MediaID = binding.ResourceID
				}
				break
			}
		}
		if seg.Voice != nil {
			seg.Voice.AcceptExternal = false
		}
	}
	for i := range doc.Bindings {
		binding := &doc.Bindings[i]
		if !binding.Unresolved {
			for _, resource := range append(list, newResources...) {
				if resource.ID == binding.ResourceID {
					name := filepath.Base(resource.Source.OriginalPath)
					if doc.SchemaVersion >= 2 && len(doc.References) > 0 {
						temporary := &project.AvpDocument{Scripts: []project.AvsDocument{doc}}
						project.RenameResourceReferences(temporary, binding.ID, name)
						doc.Body = temporary.Scripts[0].Body
						doc.References = temporary.Scripts[0].References
					} else {
						doc.Body = strings.ReplaceAll(doc.Body, "{@"+binding.Name+"}", "{@"+name+"}")
					}
					binding.Name = name
				}
			}
		}
	}
	doc.Segments, _ = json.Marshal(segments)
	doc.SchemaVersion = project.SchemaVersion
	for i := range doc.References {
		ref := &doc.References[i]
		ref.Unresolved = true
		if req.ScriptOnly {
			continue
		}
		for _, b := range doc.Bindings {
			if b.ID == ref.ResourceID || b.ID == ref.TargetID {
				if b.Unresolved {
					continue
				}
				ref.ResourceID = b.ResourceID
				if ref.Kind == "video" || ref.Kind == "audio" {
					ref.TargetID = b.ResourceID
					ref.SequenceID = ""
					ref.ClipID = ""
					ref.Unresolved = false
				}
			}
		}
	}
	if !req.ScriptOnly {
		w.mu.Lock()
		parent := cloneProject(w.document)
		w.mu.Unlock()
		parent.Resources = append(parent.Resources, newResources...)
		for i := range doc.References {
			ref := &doc.References[i]
			ref.Unresolved = false
			if _, e := project.ResolveReference(parent, *ref); e != nil {
				ref.Unresolved = true
			}
		}
	}
	doc.ID = newID("scr")
	r, err := s.inspectResource(ctx, req.Path)
	if err != nil {
		return project.Session{}, err
	}
	r.AssetID, r.ScriptID = r.ID, doc.ID
	r.Metadata["title"] = doc.Title
	for _, resource := range append(newResources, r) {
		if err = s.materializeResource(ctx, resource, projectID); err != nil {
			return project.Session{}, err
		}
	}
	w.mu.Lock()
	w.document.Resources = append(w.document.Resources, newResources...)
	w.document.Resources = append(w.document.Resources, r)
	w.document.Scripts = append(w.document.Scripts, doc)
	copy := cloneProject(w.document)
	w.mu.Unlock()
	s.rememberResourceEdit(nil, r)
	if err = s.hydrateScript(ctx, copy, doc); err != nil {
		return project.Session{}, err
	}
	if err = s.refreshProjectMedia(ctx); err != nil {
		return project.Session{}, err
	}
	s.publish("resource.changed", map[string]any{"project_id": projectID, "resource_id": r.ID})
	return s.ProjectWorkspace(ctx)
}

func (s *Service) hydrateScript(ctx context.Context, parent *project.AvpDocument, doc project.AvsDocument) error {
	if _, err := s.st.GetScript(ctx, doc.ID); err == nil {
		return nil
	} else if !isNotFound(err) {
		return err
	}
	var assetID string
	for _, r := range parent.Resources {
		if r.ScriptID == doc.ID {
			assetID = r.AssetID
			break
		}
	}
	if assetID == "" {
		return fmt.Errorf("脚本缺少工程资源: %w", model.ErrArgument)
	}
	now := s.autoNow()
	sc := model.Script{ScriptID: doc.ID, AssetID: assetID, Title: doc.Title, Body: doc.Body, Status: model.ScriptDraft, CreatedAt: now, UpdatedAt: now}
	set, err := loadVisibleSet(ctx, s.st)
	if err != nil {
		return err
	}
	sc.VisibleSetSHA = set.SHA
	var segments []ScriptSegment
	if err = json.Unmarshal(doc.Segments, &segments); err != nil {
		return err
	}
	w := ScriptWorkbench{Script: sc, Version: 1, Document: scriptsyntax.Parse(doc.Body), Segments: segments, Sources: []ScriptSource{}, Questions: []string{}, TTSEnabled: doc.TTSEnabled, SubtitlesEnabled: doc.SubtitlesEnabled, AllowWebResearch: doc.AllowWebResearch, References: doc.References}
	if len(doc.Sources) > 0 {
		_ = json.Unmarshal(doc.Sources, &w.Sources)
	}
	raw, _ := json.Marshal(w)
	return s.st.Transaction(ctx, func(tx *store.Store) error {
		if err := tx.InsertScript(ctx, sc); err != nil {
			return err
		}
		if err := tx.PutScriptWorkbench(ctx, sc.ScriptID, 0, string(raw)); err != nil {
			return err
		}
		return tx.InsertScriptRevision(ctx, sc.ScriptID, store.ScriptRevision{Version: 1, Action: "import", Actor: "human:workspace", Snapshot: string(raw), CreatedAt: now})
	})
}

func (s *Service) exportScriptDocument(ctx context.Context, id string) (project.AvsDocument, error) {
	w, err := s.GetScriptWorkbench(ctx, id)
	if err != nil {
		return project.AvsDocument{}, err
	}
	doc := project.AvsDocument{Format: "avs", SchemaVersion: project.SchemaVersion, ID: id, Title: w.Script.Title, Body: w.Script.Body, TTSEnabled: w.TTSEnabled, SubtitlesEnabled: w.SubtitlesEnabled, AllowWebResearch: w.AllowWebResearch, Bindings: []project.Binding{}, References: w.References}
	doc.Segments, _ = json.Marshal(w.Segments)
	doc.Sources, _ = json.Marshal(w.Sources)
	revisions, err := s.ScriptRevisions(ctx, id)
	if err != nil {
		return doc, err
	}
	doc.Revisions, _ = json.Marshal(revisions)
	list, _ := s.ProjectResources(s.CurrentProjectID())
	seen := map[string]bool{}
	for _, seg := range w.Segments {
		if seg.MediaID == "" || seen[seg.MediaID] {
			continue
		}
		seen[seg.MediaID] = true
		b := project.Binding{ID: seg.MediaID, Name: seg.MediaID, Kind: "video", Unresolved: true}
		for _, r := range list {
			if r.ID == seg.MediaID {
				b.ResourceID, b.Name, b.Kind, b.Unresolved = r.ID, filepath.Base(r.Source.OriginalPath), r.Kind, r.Removed || r.Missing
			}
		}
		doc.Bindings = append(doc.Bindings, b)
	}
	for _, ref := range w.References {
		if ref.ResourceID == "" || seen[ref.ResourceID] {
			continue
		}
		for _, r := range list {
			if r.ID == ref.ResourceID && (r.Kind == "video" || r.Kind == "audio") {
				doc.Bindings = append(doc.Bindings, project.Binding{ID: r.ID, ResourceID: r.ID, Name: r.Name, Kind: r.Kind, Unresolved: r.Missing || r.Removed})
				seen[r.ID] = true
			}
		}
	}
	// A draft can bind explicit syntax before it has generated any segments.
	// Preserve those bindings as well as pending import descriptors.
	if ws := s.workspace; ws != nil {
		ws.mu.Lock()
		for _, old := range ws.document.Scripts {
			if old.ID == id {
				for _, binding := range old.Bindings {
					usedInBody := strings.Contains(doc.Body, "{@"+binding.Name+"}")
					if (binding.Unresolved || usedInBody) && !seen[binding.ID] && !seen[binding.ResourceID] {
						doc.Bindings = append(doc.Bindings, binding)
						seen[binding.ID] = true
						if binding.ResourceID != "" {
							seen[binding.ResourceID] = true
						}
					}
				}
			}
		}
		ws.mu.Unlock()
	}
	return doc, nil
}

func (s *Service) captureProjectScripts(ctx context.Context, doc *project.AvpDocument) error {
	all, err := s.st.ListScripts(ctx, "")
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, script := range doc.Scripts {
		known[script.ID] = true
	}
	for _, script := range all {
		if !known[script.ScriptID] {
			current, err := s.exportScriptDocument(ctx, script.ScriptID)
			if err != nil {
				return err
			}
			doc.Scripts = append(doc.Scripts, current)
		}
	}
	for i, sc := range doc.Scripts {
		current, err := s.exportScriptDocument(ctx, sc.ID)
		if err != nil {
			return err
		}
		doc.Scripts[i] = current
	}
	return nil
}

func (s *Service) ExportScriptFile(ctx context.Context, projectID, id, path string) error {
	if projectID == "" || projectID != s.CurrentProjectID() || !strings.EqualFold(filepath.Ext(path), ".avs") {
		return model.ErrArgument
	}
	doc, err := s.exportScriptDocument(ctx, id)
	if err != nil {
		return err
	}
	return project.WriteAtomic(path, doc)
}

// Resolve pending AVS dependencies without rereading or modifying the source
// AVS. The imported script remains a separately editable project document.
func (s *Service) BindProjectScript(ctx context.Context, projectID, id string, req ScriptFileImport) (project.Session, error) {
	w := s.workspace
	if w == nil {
		return project.Session{}, model.ErrInvalidState
	}
	if err := w.lockMutation(ctx); err != nil {
		return project.Session{}, err
	}
	defer w.op.Unlock()
	if projectID == "" || projectID != s.CurrentProjectID() {
		return project.Session{}, model.ErrNotFound
	}
	doc, err := s.exportScriptDocument(ctx, id)
	if err != nil {
		return project.Session{}, err
	}
	resources, _ := s.ProjectResources(projectID)
	var segments []ScriptSegment
	if err = json.Unmarshal(doc.Segments, &segments); err != nil {
		return project.Session{}, err
	}
	added := []project.Resource{}
	for i := range doc.Bindings {
		binding := &doc.Bindings[i]
		oldResource := binding.ResourceID
		selected := req.Bindings[binding.ID]
		if file := req.Files[binding.ID]; file != "" {
			r, err := s.inspectResource(ctx, file)
			if err != nil {
				return project.Session{}, err
			}
			if r.Kind != "video" && r.Kind != "audio" {
				return project.Session{}, model.ErrArgument
			}
			for _, current := range append(resources, added...) {
				if !current.Removed && strings.EqualFold(current.Source.OriginalPath, r.Source.OriginalPath) {
					r = current
					break
				}
			}
			if r.AssetID == "" {
				r.AssetID = r.ID
				added = append(added, r)
			}
			selected = r.ID
		}
		if selected == "" {
			continue
		}
		found := false
		for _, resource := range append(resources, added...) {
			if resource.ID != selected || resource.Removed || resource.Missing || (resource.Kind != "video" && resource.Kind != "audio") {
				continue
			}
			found = true
			name := filepath.Base(resource.Source.OriginalPath)
			if doc.SchemaVersion >= 2 && len(doc.References) > 0 {
				for j := range doc.References {
					ref := &doc.References[j]
					if ref.Unresolved && ref.Kind == "" && ref.TargetID == "" && ref.Name == binding.Name {
						for _, other := range doc.Bindings {
							if other.ID != binding.ID && other.Name == binding.Name {
								return project.Session{}, fmt.Errorf("同名旧素材绑定请逐项重新选择: %w", model.ErrConflict)
							}
						}
						ref.Kind = resource.Kind
						ref.TargetID = binding.ID
						ref.ResourceID = binding.ID
					}
				}
				temporary := &project.AvpDocument{Scripts: []project.AvsDocument{doc}}
				project.RenameResourceReferences(temporary, binding.ID, name)
				if oldResource != "" {
					project.RenameResourceReferences(temporary, oldResource, name)
				}
				doc.Body = temporary.Scripts[0].Body
				doc.References = temporary.Scripts[0].References
				for i := range doc.References {
					ref := &doc.References[i]
					if (oldResource != "" && ref.ResourceID == oldResource) || ref.ResourceID == binding.ID || ref.TargetID == binding.ID {
						ref.ResourceID = resource.ID
						if ref.Kind == "video" || ref.Kind == "audio" {
							ref.TargetID = resource.ID
							ref.Unresolved = false
						}
					}
				}
			} else {
				doc.Body = strings.ReplaceAll(doc.Body, "{@"+binding.Name+"}", "{@"+name+"}")
			}
			for j := range segments {
				pending := false
				for _, index := range binding.SegmentIndexes {
					if index == j {
						pending = true
					}
				}
				if segments[j].MediaID == binding.ID || (oldResource != "" && segments[j].MediaID == oldResource) || (binding.Unresolved && segments[j].MediaID == "" && pending) {
					segments[j].MediaID = resource.ID
				}
			}
			binding.Name = name
			binding.ResourceID = resource.ID
			binding.Unresolved = false
		}
		if !found {
			return project.Session{}, model.ErrArgument
		}
	}
	for _, resource := range added {
		if err = s.materializeResource(ctx, resource, projectID); err != nil {
			return project.Session{}, err
		}
	}
	w.mu.Lock()
	w.document.Resources = append(w.document.Resources, added...)
	w.mu.Unlock()
	if err = s.refreshProjectMedia(ctx); err != nil {
		return project.Session{}, err
	}
	workbench, err := s.GetScriptWorkbench(ctx, id)
	if err != nil {
		return project.Session{}, err
	}
	visible, err := s.VisibleSet(ctx)
	if err != nil {
		return project.Session{}, err
	}
	_, err = s.SaveScriptWorkbench(context.WithValue(ctx, projectMutationContextKey{}, w), "human:window", id, SaveScriptRequest{ExpectedVersion: workbench.Version, VisibleSetSHA: visible.SHA256, Title: doc.Title, Body: doc.Body, References: doc.References, Segments: segments, TTSEnabled: doc.TTSEnabled, SubtitlesEnabled: doc.SubtitlesEnabled, AllowWebResearch: doc.AllowWebResearch})
	if err != nil {
		return project.Session{}, err
	}
	doc.Segments, _ = json.Marshal(segments)
	w.mu.Lock()
	for i := range w.document.Scripts {
		if w.document.Scripts[i].ID == id {
			w.document.Scripts[i] = doc
		}
	}
	w.mu.Unlock()
	s.publish("resource.changed", map[string]any{"project_id": projectID})
	return s.ProjectWorkspace(ctx)
}

func (s *Service) projectScriptBindingsReady(ctx context.Context, id string) error {
	if !s.WorkspaceEnabled() {
		return nil
	}
	doc, err := s.exportScriptDocument(ctx, id)
	if err != nil {
		return err
	}
	for _, binding := range doc.Bindings {
		if binding.Unresolved {
			return fmt.Errorf("请先关联脚本素材 %s: %w", binding.Name, model.ErrConflict)
		}
	}
	ws := s.workspace
	ws.mu.Lock()
	_, err = project.BindReferences(ws.document, doc.Body, doc.References, "", true)
	ws.mu.Unlock()
	if err != nil {
		return fmt.Errorf("%v: %w", err, model.ErrConflict)
	}
	return nil
}
