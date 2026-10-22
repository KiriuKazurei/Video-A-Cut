package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

type projectWorkspace struct {
	mu               sync.Mutex
	op               sync.Mutex
	gate             sync.RWMutex
	document         *project.AvpDocument
	path             string
	session          string
	revision         uint64
	dirty            bool
	closing          bool
	root             string
	recent           []project.Recent
	policy           ProviderPolicy
	stop             chan struct{}
	done             chan struct{}
	undo             []resourceEdit
	undoCursor       int
	draining         map[string]string
	timelineReceipts map[string]timelineReceipt
}

type projectContextKey struct{}
type projectMutationContextKey struct{}

type resourceEdit struct {
	Before *project.Resource
	After  project.Resource
}

type ProviderPolicy struct {
	ExternalTTS bool `json:"external_tts"`
}

type ProjectCommand struct {
	Path            string `json:"path"`
	Name            string `json:"name,omitempty"`
	Decision        string `json:"decision,omitempty"` // save, discard; omitted requests a prompt
	StopTasks       bool   `json:"stop_tasks"`
	ExpectedSession string `json:"expected_session,omitempty"`
	RecoveryPath    string `json:"recovery_path,omitempty"`
}

func (s *Service) InspectProjectFile(ctx context.Context, path string) (project.Identity, error) {
	var doc project.AvpDocument
	if err := project.Read(path, "avp", &doc); err != nil {
		return doc.Project, fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	if err := doc.Validate(); err != nil {
		return doc.Project, fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	snapshot, err := decodeProjectSnapshot(doc.Runtime)
	if err != nil {
		return doc.Project, model.ErrArgument
	}
	if err = s.st.ValidateProjectSnapshot(ctx, snapshot); err != nil {
		return doc.Project, fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	return doc.Project, nil
}

func (s *Service) UpdateProjectWorkspaceState(ctx context.Context, session string, state map[string]any) (project.Session, error) {
	w := s.workspace
	if w == nil {
		return project.Session{}, model.ErrInvalidState
	}
	if err := w.lockMutation(ctx); err != nil {
		return project.Session{}, err
	}
	defer w.op.Unlock()
	w.mu.Lock()
	if w.document == nil || w.session != session {
		w.mu.Unlock()
		return project.Session{}, model.ErrConflict
	}
	if skillHash(w.document.Workspace) != skillHash(state) {
		w.document.Workspace = state
		w.dirty = true
		w.revision++
	}
	w.mu.Unlock()
	s.publish("workspace.state", nil)
	return s.ProjectWorkspace(ctx)
}

func (s *Service) ConfigureProjectWorkspace(ctx context.Context) error {
	if s.workspace != nil {
		return nil
	}
	if s.deliveryRoot == "" {
		return nil
	}
	root := filepath.Join(filepath.Dir(s.deliveryRoot), "workspace")
	if err := os.MkdirAll(filepath.Join(root, "recovery"), 0700); err != nil {
		return err
	}
	w := &projectWorkspace{root: root, session: newID("session"), recent: []project.Recent{}, stop: make(chan struct{}), done: make(chan struct{})}
	read := func(name string, value any) error {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, value)
	}
	if err := read("recent.json", &w.recent); err != nil {
		return err
	}
	if err := read("providers.json", &w.policy); err != nil {
		return err
	}
	if len(w.recent) > 10 {
		w.recent = w.recent[:10]
	}
	if err := s.resetProjectWork(ctx); err != nil {
		return err
	}
	s.workspace = w
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				_ = s.WriteProjectRecovery(ctx)
				cancel()
			}
		}
	}()
	return nil
}

func (s *Service) CloseProjectWorkspace() {
	if w := s.workspace; w != nil {
		select {
		case <-w.stop:
			return
		default:
			close(w.stop)
		}
		<-w.done
	}
}

func (s *Service) WorkspaceEnabled() bool { return s.workspace != nil }

func (s *Service) CurrentProjectID() string {
	if w := s.workspace; w != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.document != nil {
			return w.document.Project.ID
		}
	}
	return ""
}

func (s *Service) ProjectSessionID() string {
	if w := s.workspace; w != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.session
	}
	return ""
}

// BeginProjectRequest serializes the old HTTP surface with projection swaps.
// A request from an obsolete UI generation is rejected before any mutation.
func (s *Service) BeginProjectRequest(ctx context.Context, session string) (context.Context, func(), error) {
	w := s.workspace
	if w == nil {
		return ctx, func() {}, nil
	}
	w.gate.RLock()
	w.mu.Lock()
	if w.closing || (session != "" && session != w.session) {
		w.mu.Unlock()
		w.gate.RUnlock()
		return ctx, nil, fmt.Errorf("工程已切换，请读取当前工作区: %w", model.ErrConflict)
	}
	ctx, release := s.trackProjectOperation(ctx, "request:")
	if w.document != nil {
		ctx = context.WithValue(ctx, projectContextKey{}, w.document.Project.ID)
	}
	w.mu.Unlock()
	return ctx, func() { release(); w.gate.RUnlock() }, nil
}

func cloneProject(d *project.AvpDocument) *project.AvpDocument {
	if d == nil {
		return nil
	}
	raw, _ := json.Marshal(d)
	var copy project.AvpDocument
	_ = json.Unmarshal(raw, &copy)
	return &copy
}

func (s *Service) ProjectWorkspace(ctx context.Context) (project.Session, error) {
	w := s.workspace
	if w == nil {
		return project.Session{}, model.ErrInvalidState
	}
	w.mu.Lock()
	view := project.Session{ID: w.session, Path: w.path, Dirty: w.dirty, Revision: w.revision, Document: cloneProject(w.document), Recent: append([]project.Recent{}, w.recent...), Recoveries: []project.Recovery{}}
	w.mu.Unlock()
	if view.Document != nil {
		refreshResourceAvailability(view.Document.Resources)
		view.Document.FileReferences = nil
		for i := range view.Document.Scripts {
			view.Document.Scripts[i].Revisions = nil
		}
		view.Document.Runtime = nil
		for i := range view.Document.Sequences {
			view.Document.Sequences[i].History = nil
			view.Document.Sequences[i].HistoryCursor = 0
		}
	}
	count, err := s.RuntimeActivity(ctx)
	if err != nil {
		return view, err
	}
	rows, err := s.st.SkillList(ctx, "run")
	if err != nil {
		return view, err
	}
	for _, raw := range rows {
		var run skill.Run
		if json.Unmarshal(raw, &run) == nil && (run.State == "running" || run.State == "queued") {
			count++
		}
	}
	w.mu.Lock()
	count += len(w.draining)
	w.mu.Unlock()
	view.ActiveTasks = count
	if err := s.markRemovedResourceReferences(ctx, view.Document); err != nil {
		return view, err
	}
	entries, err := os.ReadDir(filepath.Join(w.root, "recovery"))
	if err != nil {
		return view, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var recovery project.Recovery
		if raw, e := os.ReadFile(filepath.Join(w.root, "recovery", entry.Name())); e == nil && json.Unmarshal(raw, &recovery) == nil {
			view.Recoveries = append(view.Recoveries, recovery)
		}
	}
	return view, nil
}

func (s *Service) markProjectDirty(name string) {
	w := s.workspace
	if w == nil {
		return
	}
	if strings.HasPrefix(name, "workspace.") || strings.HasPrefix(name, "capability.") || strings.HasPrefix(name, "audit.") || strings.HasPrefix(name, "profile.") || strings.HasPrefix(name, "skill.changed") || strings.HasPrefix(name, "tts.template.") {
		return
	}
	w.mu.Lock()
	if w.document != nil {
		w.dirty = true
		w.revision++
		w.document.Project.UpdatedAt = s.autoNow()
	}
	w.mu.Unlock()
}

func (s *Service) SetProviderPolicy(policy ProviderPolicy) (ProviderPolicy, error) {
	w := s.workspace
	if w == nil {
		return policy, model.ErrInvalidState
	}
	w.op.Lock()
	defer w.op.Unlock()
	if err := project.WriteAtomic(filepath.Join(w.root, "providers.json"), policy); err != nil {
		return policy, err
	}
	w.mu.Lock()
	w.policy = policy
	w.mu.Unlock()
	return policy, nil
}
func (s *Service) ProviderPolicy() ProviderPolicy {
	w := s.workspace
	if w == nil {
		return ProviderPolicy{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.policy
}

func (s *Service) captureProject(ctx context.Context) (*project.AvpDocument, uint64, error) {
	w := s.workspace
	w.mu.Lock()
	doc, revision := cloneProject(w.document), w.revision
	w.mu.Unlock()
	if doc == nil {
		return nil, revision, model.ErrInvalidState
	}
	snapshot, err := s.st.ProjectSnapshot(ctx)
	if err != nil {
		return nil, revision, err
	}
	doc.Runtime, err = json.Marshal(snapshot)
	if err != nil {
		return nil, revision, err
	}
	if err = s.captureProjectScripts(ctx, doc); err != nil {
		return nil, revision, err
	}
	if err = s.captureProjectOutputs(ctx, doc); err != nil {
		return nil, revision, err
	}
	doc.Upgrade()
	return doc, revision, nil
}

func (s *Service) WriteProjectRecovery(ctx context.Context) error {
	w := s.workspace
	if w == nil {
		return nil
	}
	w.op.Lock()
	defer w.op.Unlock()
	w.mu.Lock()
	dirty, path := w.dirty, w.path
	w.mu.Unlock()
	if !dirty {
		return nil
	}
	doc, _, err := s.captureProject(ctx)
	if err != nil {
		return err
	}
	base := filepath.Join(w.root, "recovery", doc.Project.ID)
	if err = project.WriteAtomic(base+".avp", doc); err != nil {
		return err
	}
	return project.WriteAtomic(base+".json", project.Recovery{Path: path, Name: doc.Project.Name, RecoveryPath: base + ".avp", UpdatedAt: s.autoNow()})
}

func (s *Service) saveProjectFile(ctx context.Context, target string) error {
	w := s.workspace
	doc, revision, err := s.captureProject(ctx)
	if err != nil {
		return err
	}
	if target == "" {
		w.mu.Lock()
		target = w.path
		w.mu.Unlock()
	}
	if !strings.EqualFold(filepath.Ext(target), ".avp") {
		return fmt.Errorf("请选择 .avp 保存位置: %w", model.ErrArgument)
	}
	for i := range doc.Resources {
		r := &doc.Resources[i]
		if r.Source.OriginalPath != "" {
			rel, e := filepath.Rel(filepath.Dir(target), r.Source.OriginalPath)
			if e == nil {
				r.Source.RelativePath = filepath.ToSlash(rel)
			}
		}
	}
	if err = project.WriteAtomic(target, doc); err != nil {
		return err
	}
	w.mu.Lock()
	if w.revision == revision {
		w.dirty = false
		w.document = doc
	}
	w.path = target
	w.mu.Unlock()
	_ = os.Remove(filepath.Join(w.root, "recovery", doc.Project.ID+".avp"))
	_ = os.Remove(filepath.Join(w.root, "recovery", doc.Project.ID+".json"))
	s.rememberProject(target, doc.Project.Name)
	return nil
}

func (s *Service) rememberProject(path, name string) {
	w := s.workspace
	w.mu.Lock()
	list := []project.Recent{{Path: path, Name: name, OpenedAt: s.autoNow()}}
	for _, item := range w.recent {
		if !strings.EqualFold(item.Path, path) && len(list) < 10 {
			list = append(list, item)
		}
	}
	w.recent = list
	w.mu.Unlock()
	if err := project.WriteAtomic(filepath.Join(w.root, "recent.json"), list); err != nil {
		// A global convenience index cannot turn a successful formal save/open
		// into a failed project operation or roll back the active projection.
		slog.Default().Warn("最近项目列表写入失败", "error", err)
	}
}

func (s *Service) ProjectFileCommand(ctx context.Context, action string, req ProjectCommand) (project.Session, error) {
	w := s.workspace
	if w == nil {
		return project.Session{}, model.ErrInvalidState
	}
	w.op.Lock()
	defer w.op.Unlock()
	if req.ExpectedSession != "" && req.ExpectedSession != s.ProjectSessionID() {
		return project.Session{}, model.ErrConflict
	}
	if action == "save" {
		if err := s.saveProjectFile(ctx, ""); err != nil {
			return project.Session{}, err
		}
		s.publish("workspace.saved", nil)
		return s.ProjectWorkspace(ctx)
	}
	if action == "undo" || action == "redo" {
		return s.moveResourceHistory(ctx, action)
	}
	var incoming *project.AvpDocument
	var snapshot store.ProjectSnapshot
	path := req.Path
	if action == "open" || action == "recover" {
		readPath := path
		if action == "recover" {
			readPath = req.RecoveryPath
			if !strings.HasPrefix(filepath.Clean(readPath), filepath.Join(w.root, "recovery")+string(filepath.Separator)) {
				return project.Session{}, model.ErrArgument
			}
		}
		incoming = &project.AvpDocument{}
		if err := project.Read(readPath, "avp", incoming); err != nil {
			return project.Session{}, fmt.Errorf("%v: %w", err, model.ErrArgument)
		}
		if err := incoming.Validate(); err != nil {
			return project.Session{}, fmt.Errorf("%v: %w", err, model.ErrArgument)
		}
		if len(incoming.Runtime) > 0 {
			decoder := json.NewDecoder(bytes.NewReader(incoming.Runtime))
			decoder.UseNumber()
			if err := decoder.Decode(&snapshot); err != nil {
				return project.Session{}, model.ErrArgument
			}
		}
		if path == "" {
			path = filepath.Join(filepath.Dir(readPath), incoming.Project.Name+".avp")
		}
		if err := s.st.ValidateProjectSnapshot(ctx, snapshot); err != nil {
			return project.Session{}, fmt.Errorf("工程运行记录损坏: %v: %w", err, model.ErrArgument)
		}
	} else if action == "new" {
		if strings.TrimSpace(req.Name) == "" || !strings.EqualFold(filepath.Ext(path), ".avp") {
			return project.Session{}, model.ErrArgument
		}
		if _, err := os.Stat(path); err == nil {
			return project.Session{}, fmt.Errorf("目标工程已存在，请另选位置: %w", model.ErrConflict)
		} else if !errors.Is(err, os.ErrNotExist) {
			return project.Session{}, err
		}
		now := s.autoNow()
		incoming = &project.AvpDocument{Format: "avp", SchemaVersion: project.SchemaVersion, Project: project.Identity{ID: newID("prj"), Name: strings.TrimSpace(req.Name), CreatedAt: now, UpdatedAt: now}, Resources: []project.Resource{}, Scripts: []project.AvsDocument{}, Sequences: []project.Sequence{project.NewSequence("主序列")}, Settings: map[string]any{}, Workspace: map[string]any{}}
	} else if action != "close" && action != "save-as" {
		return project.Session{}, model.ErrArgument
	}
	view, err := s.ProjectWorkspace(ctx)
	if err != nil {
		return view, err
	}
	if action != "save-as" && view.Dirty && req.Decision != "save" && req.Decision != "discard" {
		return view, fmt.Errorf("请保存或放弃当前工程修改: %w", model.ErrConflict)
	}
	if view.ActiveTasks > 0 && !req.StopTasks {
		return view, fmt.Errorf("请确认停止当前工程任务后切换: %w", model.ErrConflict)
	}
	w.mu.Lock()
	w.closing = true
	w.mu.Unlock()
	defer func() { w.mu.Lock(); w.closing = false; w.mu.Unlock() }()
	if err = s.stopProjectTasks(ctx); err != nil {
		return view, err
	}
	w.gate.Lock()
	defer w.gate.Unlock()
	if action != "save-as" && view.Dirty && req.Decision == "save" {
		if err = s.saveProjectFile(ctx, ""); err != nil {
			return view, err
		}
	}
	if action == "save-as" {
		if !strings.EqualFold(filepath.Ext(path), ".avp") || strings.EqualFold(path, view.Path) {
			return view, model.ErrArgument
		}
		incoming, _, err = s.captureProject(ctx)
		if err != nil {
			return view, err
		}
		oldID := incoming.Project.ID
		incoming.Project.ID = newID("prj")
		incoming.Project.CreatedAt = s.autoNow()
		if name := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))); name != "" {
			incoming.Project.Name = name
		}
		renameProjectOutputReferences(incoming, oldID)
		for i := range incoming.Resources {
			source := &incoming.Resources[i].Source
			if source.OriginalPath != "" {
				if relative, e := filepath.Rel(filepath.Dir(path), source.OriginalPath); e == nil {
					source.RelativePath = filepath.ToSlash(relative)
				}
			}
		}
		if snapshot, err = decodeProjectSnapshot(incoming.Runtime); err != nil {
			return view, err
		}
		snapshot = renameProjectSnapshot(snapshot, oldID, incoming.Project.ID)
		incoming.Runtime, _ = json.Marshal(snapshot)
		if err = project.WriteAtomic(path, incoming); err != nil {
			return view, err
		}
	}
	previous, err := s.st.ProjectSnapshot(ctx)
	if err != nil {
		return view, err
	}
	if err = s.st.RestoreProject(ctx, snapshot); err != nil {
		return view, err
	}
	rollback := func(e error) (project.Session, error) {
		restoreErr := s.st.RestoreProject(ctx, previous)
		return view, errors.Join(e, restoreErr)
	}
	if incoming != nil {
		incoming.Upgrade()
		if err = s.hydrateProject(ctx, incoming, path); err != nil {
			return rollback(err)
		}
		if action == "new" {
			if err = project.WriteAtomic(path, incoming); err != nil {
				return rollback(err)
			}
		}
		s.rememberProject(path, incoming.Project.Name)
	}
	w.mu.Lock()
	w.document = incoming
	w.path = path
	if incoming == nil {
		w.path = ""
	}
	w.session = newID("session")
	w.revision++
	w.dirty = action == "recover"
	w.undo = nil
	w.undoCursor = 0
	w.timelineReceipts = nil
	w.mu.Unlock()
	s.publish("workspace.changed", map[string]any{"session_id": s.ProjectSessionID(), "project_id": s.CurrentProjectID()})
	if incoming != nil {
		s.reconcileSequenceResults(ctx, incoming.Project.ID, s.ProjectSessionID())
	}
	return s.ProjectWorkspace(ctx)
}

func decodeProjectSnapshot(raw json.RawMessage) (store.ProjectSnapshot, error) {
	value := store.ProjectSnapshot{}
	if len(raw) == 0 {
		return value, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err := decoder.Decode(&value)
	return value, err
}

func renameProjectSnapshot(snapshot store.ProjectSnapshot, oldID, newID string) store.ProjectSnapshot {
	// Project references in this projection are JSON strings or scalar IDs.
	// Only exact quoted ID tokens are replaced; filesystem locations remain
	// references to completed output, rather than being invented on Save As.
	for table, data := range snapshot {
		for i := range data.Rows {
			for j, v := range data.Rows[i] {
				if text, ok := v.(string); ok {
					if text == oldID {
						data.Rows[i][j] = newID
					} else {
						data.Rows[i][j] = strings.ReplaceAll(text, `"`+oldID+`"`, `"`+newID+`"`)
					}
				}
			}
		}
		if table == "vac_edit_checkpoints" {
			bodyColumn, shaColumn := -1, -1
			for i, column := range data.Columns {
				if column == "body" {
					bodyColumn = i
				}
				if column == "sha" {
					shaColumn = i
				}
			}
			if bodyColumn >= 0 && shaColumn >= 0 {
				for i := range data.Rows {
					body, _ := data.Rows[i][bodyColumn].(string)
					sum := sha256.Sum256([]byte(body))
					data.Rows[i][shaColumn] = hex.EncodeToString(sum[:])
				}
			}
		}
		snapshot[table] = data
	}
	return snapshot
}
