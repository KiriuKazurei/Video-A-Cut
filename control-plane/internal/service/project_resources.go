package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

func resourceKind(path string) string {
	if project.IsCameraRaw(path) {
		return "image"
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".mkv", ".mov", ".avi", ".webm", ".m4v", ".mpeg", ".mpg", ".ts", ".flv":
		return "video"
	case ".wav", ".mp3", ".m4a", ".aac", ".flac", ".ogg", ".opus", ".wma":
		return "audio"
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".tif", ".tiff", ".jpe", ".jepg":
		return "image"
	case ".srt", ".ass", ".vtt":
		return "subtitle"
	case ".txt", ".md", ".markdown":
		return "document"
	case ".avs":
		return "script"
	}
	return ""
}

func (s *Service) ProjectResources(projectID string) ([]project.Resource, error) {
	w := s.workspace
	if w == nil {
		return nil, model.ErrInvalidState
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.document == nil || w.document.Project.ID != projectID {
		return nil, model.ErrNotFound
	}
	copy := cloneProject(w.document)
	refreshResourceAvailability(copy.Resources)
	return copy.Resources, nil
}

func refreshResourceAvailability(resources []project.Resource) {
	for i := range resources {
		r := &resources[i]
		if r.Kind == "script" {
			continue
		}
		info, err := os.Stat(r.Source.OriginalPath)
		r.Missing = r.Removed || err != nil || (err == nil && !info.Mode().IsRegular())
		if !r.Missing {
			r.Changed = info.Size() != r.Source.Size || info.ModTime().UnixNano() != r.Source.ModifiedNS
		}
	}
}

func (s *Service) ProjectResource(projectID, id string) (project.Resource, error) {
	list, err := s.ProjectResources(projectID)
	if err != nil {
		return project.Resource{}, err
	}
	for _, r := range list {
		if r.ID == id {
			return r, nil
		}
	}
	return project.Resource{}, model.ErrNotFound
}

func (s *Service) registeredResource(assetID string) (project.Resource, bool) {
	if s.workspace == nil {
		return project.Resource{}, false
	}
	list, err := s.ProjectResources(s.CurrentProjectID())
	if err != nil {
		return project.Resource{}, false
	}
	for _, r := range list {
		if r.AssetID == assetID {
			return r, true
		}
	}
	return project.Resource{}, false
}

func (s *Service) sourceReference(ref string) (string, os.FileInfo, error) {
	if !strings.HasPrefix(ref, "@resource/") {
		return "", nil, model.ErrArgument
	}
	r, err := s.ProjectResource(s.CurrentProjectID(), strings.TrimPrefix(ref, "@resource/"))
	if err != nil {
		return "", nil, err
	}
	if r.Removed {
		return "", nil, model.ErrNotFound
	}
	info, err := os.Stat(r.Source.OriginalPath)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, model.ErrArgument
	}
	return r.Source.OriginalPath, info, nil
}

func (s *Service) inspectResource(ctx context.Context, path string) (project.Resource, error) {
	var r project.Resource
	abs, err := filepath.Abs(path)
	if err != nil {
		return r, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return r, err
	}
	if !info.Mode().IsRegular() {
		return r, model.ErrArgument
	}
	r = project.Resource{ID: newID("res"), Kind: resourceKind(abs), Name: filepath.Base(abs), Source: project.Source{OriginalPath: abs, Size: info.Size(), ModifiedNS: info.ModTime().UnixNano()}, Metadata: map[string]any{}}
	if r.Kind == "" {
		return r, fmt.Errorf("不支持的资源格式: %w", model.ErrArgument)
	}
	f, err := os.Open(abs)
	if err != nil {
		return r, err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return r, err
	}
	r.Source.SHA256 = hex.EncodeToString(h.Sum(nil))
	r.Metadata["extension"] = strings.ToLower(filepath.Ext(abs))
	r.Metadata["mime_type"] = mime.TypeByExtension(filepath.Ext(abs))
	if r.Kind == "document" || r.Kind == "subtitle" {
		raw, err := os.ReadFile(abs)
		if err != nil {
			return r, err
		}
		if !utf8.Valid(raw) {
			return r, fmt.Errorf("请将文稿或字幕保存为 UTF-8: %w", model.ErrArgument)
		}
		r.Metadata["encoding"] = "UTF-8"
		r.Metadata["characters"] = utf8.RuneCount(raw)
		r.Metadata["lines"] = strings.Count(string(raw), "\n") + 1
	}
	if project.IsCameraRaw(abs) {
		metadata, err := s.rawImageProcess(ctx, "inspect", abs, "", nil)
		if err != nil {
			return r, err
		}
		for k, v := range metadata {
			r.Metadata[k] = v
		}
		recipe := project.DefaultRawDevelop()
		r.RawDevelop = &recipe
	} else if r.Kind == "video" || r.Kind == "audio" || r.Kind == "image" {
		probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(probeCtx, "ffprobe", "-v", "error", "-show_format", "-show_streams", "-of", "json", abs)
		hideTTSCommand(cmd)
		var out scriptOutput
		cmd.Stdout = &out
		cmd.Stderr = &scriptOutput{}
		if err = cmd.Run(); err != nil {
			return r, fmt.Errorf("读取媒体元信息: %w", err)
		}
		var probe struct {
			Streams []map[string]any `json:"streams"`
			Format  map[string]any   `json:"format"`
		}
		if err = json.Unmarshal(out.Bytes(), &probe); err != nil {
			return r, err
		}
		r.Metadata["streams"] = probe.Streams
		r.Metadata["format"] = probe.Format
		if duration, ok := probe.Format["duration"].(string); ok {
			r.Out, _ = strconv.ParseFloat(duration, 64)
			r.Metadata["duration"] = r.Out
		}
		for _, stream := range probe.Streams {
			if stream["codec_type"] == "video" {
				for _, key := range []string{"width", "height", "codec_name", "avg_frame_rate", "r_frame_rate", "pix_fmt"} {
					r.Metadata[key] = stream[key]
				}
				break
			}
		}
		if (r.Kind == "video" || r.Kind == "audio") && r.Out <= 0 {
			return r, model.ErrArgument
		}
	}
	after, err := os.Stat(abs)
	if err != nil || after.Size() != info.Size() || after.ModTime() != info.ModTime() {
		return r, fmt.Errorf("导入期间文件已改变: %w", model.ErrConflict)
	}
	return r, nil
}

func (s *Service) ImportProjectResource(ctx context.Context, projectID, path string) (project.Session, error) {
	w := s.workspace
	if w == nil {
		return project.Session{}, model.ErrInvalidState
	}
	if err := w.lockMutation(ctx); err != nil {
		return project.Session{}, err
	}
	defer w.op.Unlock()
	if projectID != s.CurrentProjectID() || projectID == "" {
		return project.Session{}, model.ErrNotFound
	}
	if resourceKind(path) == "script" {
		return project.Session{}, fmt.Errorf("请使用脚本关联导入接口: %w", model.ErrArgument)
	}
	r, err := s.inspectResource(ctx, path)
	if err != nil {
		return project.Session{}, err
	}
	list, _ := s.ProjectResources(projectID)
	for _, old := range list {
		if !old.Removed && strings.EqualFold(old.Source.OriginalPath, r.Source.OriginalPath) {
			return s.ProjectWorkspace(ctx)
		}
	}
	if r.Kind == "video" || r.Kind == "audio" {
		r.AssetID = r.ID
		if err = s.materializeResource(ctx, r, projectID); err != nil {
			return project.Session{}, err
		}
	}
	w.mu.Lock()
	w.document.Resources = append(w.document.Resources, r)
	w.mu.Unlock()
	s.rememberResourceEdit(nil, r)
	if err = s.refreshProjectMedia(ctx); err != nil {
		return project.Session{}, err
	}
	s.publish("resource.changed", map[string]any{"project_id": projectID, "resource_id": r.ID})
	return s.ProjectWorkspace(ctx)
}

type ResourcePatch struct {
	RawDevelop *project.RawDevelop `json:"raw_develop,omitempty"`
	Name       *string             `json:"name,omitempty"`
	Notes      *string             `json:"notes,omitempty"`
	In         *float64            `json:"in,omitempty"`
	Out        *float64            `json:"out,omitempty"`
	Path       string              `json:"path,omitempty"`
	Removed    *bool               `json:"removed,omitempty"`
}

func (s *Service) UpdateProjectResource(ctx context.Context, projectID, id string, patch ResourcePatch) (project.Session, error) {
	w := s.workspace
	if w == nil {
		return project.Session{}, model.ErrInvalidState
	}
	if err := w.lockMutation(ctx); err != nil {
		return project.Session{}, err
	}
	defer w.op.Unlock()
	r, err := s.ProjectResource(projectID, id)
	if err != nil {
		return project.Session{}, err
	}
	previous := r
	contentChanged := patch.Path != "" || patch.Removed != nil || patch.RawDevelop != nil
	if patch.Path != "" {
		next, err := s.inspectResource(ctx, patch.Path)
		if err != nil {
			return project.Session{}, err
		}
		if next.Kind != r.Kind {
			return project.Session{}, model.ErrArgument
		}
		if !project.IsCameraRaw(next.Source.OriginalPath) {
			r.RawDevelop = nil
		} else if r.RawDevelop == nil {
			r.RawDevelop = next.RawDevelop
		}
		r.Source, r.Metadata, r.Missing, r.Changed = next.Source, next.Metadata, false, false
		if r.Out > next.Out {
			r.Out = next.Out
		}
		if r.In >= r.Out {
			r.In = 0
		}
	}
	if patch.RawDevelop != nil {
		if !project.IsCameraRaw(r.Source.OriginalPath) {
			return project.Session{}, model.ErrArgument
		}
		if err := patch.RawDevelop.Validate(); err != nil {
			return project.Session{}, fmt.Errorf("%v: %w", err, model.ErrArgument)
		}
		recipe := *patch.RawDevelop
		r.RawDevelop = &recipe
	}
	if patch.Name != nil {
		if strings.TrimSpace(*patch.Name) == "" {
			return project.Session{}, model.ErrArgument
		}
		r.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Notes != nil {
		r.Notes = *patch.Notes
	}
	if patch.In != nil {
		r.In = *patch.In
	}
	if patch.Out != nil {
		r.Out = *patch.Out
	}
	if r.In < 0 || r.Out < r.In || !finiteScriptTime(r.In) || !finiteScriptTime(r.Out) {
		return project.Session{}, model.ErrArgument
	}
	if duration, ok := r.Metadata["duration"].(float64); ok && r.Out > duration {
		return project.Session{}, model.ErrArgument
	}
	if patch.Removed != nil {
		r.Removed = *patch.Removed
		r.Missing = r.Removed
	}
	if contentChanged && r.AssetID != "" {
		if err := s.materializeResource(ctx, r, projectID); err != nil {
			return project.Session{}, err
		}
	}
	w.mu.Lock()
	for i := range w.document.Resources {
		if w.document.Resources[i].ID == id {
			w.document.Resources[i] = r
		}
	}
	w.mu.Unlock()
	s.rememberResourceEdit(&previous, r)
	if contentChanged {
		if err = s.refreshProjectMedia(ctx); err != nil {
			return project.Session{}, err
		}
		if project.ResourceContentVersion(previous) != project.ResourceContentVersion(r) || previous.Removed != r.Removed {
			s.cancelSkillExecutions(ctx, func(run skill.Run) bool { return run.ProjectID == projectID && s.skillRunUsesResource(ctx, run, id) })
		}
	}
	s.publish("resource.changed", map[string]any{"project_id": projectID, "resource_id": id})
	return s.ProjectWorkspace(ctx)
}

func (s *Service) materializeResource(ctx context.Context, r project.Resource, projectID string) error {
	if r.AssetID == "" {
		return nil
	}
	dir, err := s.harnessDirectory(filepath.Join("projects", projectID, "references", r.ID))
	if err != nil {
		return err
	}
	video := []any{}
	if r.Kind == "video" || r.Kind == "audio" {
		video = append(video, map[string]any{"id": r.ID, "src": r.Source.OriginalPath, "in": 0, "out": r.Metadata["duration"], "start": 0})
	}
	edl := map[string]any{"version": 1, "timeline": map[string]any{"fps": 30, "sample_rate": 48000}, "video": video, "voice": []any{}, "music": []any{}, "narration": []any{}, "subtitle": []any{}}
	if err = project.WriteAtomic(filepath.Join(dir, "edl.json"), edl); err != nil {
		return err
	}
	rel, _ := filepath.Rel(s.deliveryRoot, filepath.Join(dir, "edl.json"))
	a, err := s.st.GetAsset(ctx, r.AssetID)
	if err != nil && !isNotFound(err) {
		return err
	}
	if isNotFound(err) {
		return s.st.CreateAsset(ctx, model.Asset{AssetID: r.AssetID, InputKind: model.InputKindEDLPackage, Status: model.AssetStatusIngested, AgentVisible: true, HumanApproved: true, AllowedAgents: []string{"recognizer", "narrator", "exporter", "ingester", "editor"}, Artifacts: map[string]string{"edl": filepath.ToSlash(rel)}})
	}
	if a.Artifacts == nil {
		a.Artifacts = map[string]string{}
	}
	a.Artifacts["edl"] = filepath.ToSlash(rel)
	return s.st.UpdateAsset(ctx, a)
}

func (s *Service) resetProjectWork(ctx context.Context) error {
	// Opening a file never replays work or renews an old process lease.
	if _, err := s.st.DB().ExecContext(ctx, "UPDATE tasks SET status='cancelled', lease_expires_at=NULL, message='工程重新打开，任务已停止' WHERE status IN ('queued','claimed','running')"); err != nil {
		return err
	}
	previews, err := s.st.ListTTSPreviews(ctx)
	if err != nil {
		return err
	}
	for _, preview := range previews {
		if preview.Status == "queued" || preview.Status == "running" {
			preview.Status = "cancelled"
			preview.UpdatedAt = s.autoNow()
			if err := s.st.PutTTSPreview(ctx, preview); err != nil {
				return err
			}
		}
	}
	rows, err := s.st.SkillList(ctx, "run")
	if err != nil {
		return err
	}
	for _, raw := range rows {
		var run skill.Run
		if err := json.Unmarshal(raw, &run); err != nil {
			return err
		}
		if run.State == "queued" || run.State == "running" {
			before := run.Version
			run.Version++
			run.State = "waiting_human"
			run.Reason = "runtime_restarted"
			run.LeaseUntil = nil
			if err := s.st.SkillPut(ctx, "run", run.ID, before, run); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Service) hydrateProject(ctx context.Context, doc *project.AvpDocument, path string) error {
	if err := s.resetProjectWork(ctx); err != nil {
		return err
	}

	for i := range doc.Resources {
		r := &doc.Resources[i]
		if r.Kind != "script" {
			resolved := project.ResolveSource(path, r.Source)
			info, err := os.Stat(resolved)
			r.Missing = err != nil || (err == nil && !info.Mode().IsRegular()) || r.Removed
			if !r.Missing {
				r.Changed = info.Size() != r.Source.Size || info.ModTime().UnixNano() != r.Source.ModifiedNS
				r.Source.OriginalPath = resolved
			}
		}
		if err := s.materializeResource(ctx, *r, doc.Project.ID); err != nil {
			return err
		}
	}
	var p skill.Project
	err := s.st.SkillGet(ctx, "project", doc.Project.ID, &p)
	if err != nil && !isNotFound(err) {
		return err
	}
	if isNotFound(err) {
		p = skill.Project{ID: doc.Project.ID, Name: doc.Project.Name, Flow: "lightweight", Version: 1, CreatedAt: doc.Project.CreatedAt, UpdatedAt: doc.Project.UpdatedAt, Assets: []string{}, Visible: []string{}, Defaults: map[string]string{"lightweight": "video-use", "visual_heavy": "vac-visual-summary"}, Disabled: []string{}}
	}
	p.Name = doc.Project.Name
	p.ExternalAuthorized = true
	p.TranscriptionAuthorized = true
	p.Assets = []string{}
	p.Visible = []string{}
	for _, r := range doc.Resources {
		if r.AssetID != "" {
			p.Assets = append(p.Assets, r.AssetID)
			if !r.Removed && !r.Missing {
				p.Visible = append(p.Visible, r.AssetID)
			}
		}
	}
	slices.Sort(p.Assets)
	slices.Sort(p.Visible)
	media, _, err := s.projectMedia(ctx, p)
	if err != nil {
		return err
	}
	p.ConfirmedSHA = projectSHA(p, media)
	raw, _ := json.Marshal(p)
	if _, err = s.st.DB().ExecContext(ctx, "INSERT INTO vac_projects(id,version,body) VALUES (?,?,?) ON CONFLICT(id) DO UPDATE SET version=excluded.version,body=excluded.body", p.ID, p.Version, string(raw)); err != nil {
		return err
	}
	for _, sc := range doc.Scripts {
		if err = s.hydrateScript(ctx, doc, sc); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) refreshProjectMedia(ctx context.Context) error {
	projectID := s.CurrentProjectID()
	if projectID == "" {
		return model.ErrInvalidState
	}
	list, _ := s.ProjectResources(projectID)
	var p skill.Project
	if err := s.st.SkillGet(ctx, "project", projectID, &p); err != nil {
		return err
	}
	p.Assets = []string{}
	p.Visible = []string{}
	for _, r := range list {
		if r.AssetID != "" {
			p.Assets = append(p.Assets, r.AssetID)
			if !r.Removed && !r.Missing {
				p.Visible = append(p.Visible, r.AssetID)
			}
		}
	}
	slices.Sort(p.Assets)
	slices.Sort(p.Visible)
	media, _, err := s.projectMedia(ctx, p)
	if err != nil {
		return err
	}
	p.ConfirmedSHA = projectSHA(p, media)
	p.ExternalAuthorized = true
	p.TranscriptionAuthorized = true
	raw, _ := json.Marshal(p)
	_, err = s.st.DB().ExecContext(ctx, "UPDATE vac_projects SET body=? WHERE id=?", string(raw), p.ID)
	return err
}

func (s *Service) ProjectResourcePath(projectID, id string, proxy bool) (string, error) {
	r, err := s.ProjectResource(projectID, id)
	if err != nil {
		return "", err
	}
	if r.Removed {
		return "", model.ErrNotFound
	}
	if proxy || project.IsCameraRaw(r.Source.OriginalPath) {
		if r.Changed {
			return "", model.ErrConflict
		}
		ext := ".mp4"
		if r.Kind == "audio" {
			ext = ".mp3"
		} else if r.Kind == "image" {
			ext = ".png"
		}
		path := filepath.Join(s.workspace.root, "previews", project.ResourceContentVersion(r)+ext)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return "", model.ErrNotFound
		}
		return path, nil
	}
	info, err := os.Stat(r.Source.OriginalPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", model.ErrNotFound
	}
	return r.Source.OriginalPath, nil
}

func (s *Service) CreateResourcePreview(ctx context.Context, projectID, id string) (string, error) {
	r, err := s.ProjectResource(projectID, id)
	if err != nil {
		return "", err
	}
	if r.Missing || r.Removed {
		return "", model.ErrNotFound
	}
	if r.Kind != "video" && r.Kind != "audio" && r.Kind != "image" {
		return "", model.ErrArgument
	}
	if r.Changed {
		return "", fmt.Errorf("源文件已改变，请更新资源引用: %w", model.ErrConflict)
	}
	if path, err := s.ProjectResourcePath(projectID, id, true); err == nil {
		return path, nil
	}
	source := r.Source.OriginalPath
	_, err = os.Stat(source)
	if err != nil {
		return "", err
	}
	// Resource-bin thumbnails and the source monitor may request the same
	// RAW derivative together. Join the owned conversion instead of reporting
	// a transient busy error or decoding the same sensor image twice.
	var release func()
	for {
		release, err = s.acquireScript("proxy:" + id)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	defer release()
	if path, err := s.ProjectResourcePath(projectID, id, true); err == nil {
		return path, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.harnessMu.Lock()
	if s.harnessCancel == nil {
		s.harnessCancel = map[string]context.CancelFunc{}
	}
	s.harnessCancel["proxy:"+id] = cancel
	s.harnessMu.Unlock()
	defer func() { s.harnessMu.Lock(); delete(s.harnessCancel, "proxy:"+id); s.harnessMu.Unlock() }()
	dir := filepath.Join(s.workspace.root, "previews")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	ext := ".mp4"
	args := []string{"-v", "error", "-nostdin", "-y", "-i", source}
	if r.Kind == "audio" {
		ext = ".mp3"
		args = append(args, "-vn", "-c:a", "libmp3lame", "-b:a", "160k")
	} else if r.Kind == "image" {
		ext = ".png"
		args = append(args, "-frames:v", "1", "-update", "1")
	} else {
		args = append(args, "-map", "0:v:0", "-map", "0:a:0?", "-vf", "scale='min(1280,iw)':-2", "-c:v", "libx264", "-preset", "fast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart")
	}
	path := filepath.Join(dir, project.ResourceContentVersion(r)+ext)
	temporary, err := os.CreateTemp(dir, r.Source.SHA256+".partial-*"+ext)
	if err != nil {
		return "", err
	}
	temp := temporary.Name()
	if err = temporary.Close(); err != nil {
		return "", err
	}
	defer os.Remove(temp)
	args = append(args, temp)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	configureMediaCommand(cmd)
	var out scriptOutput
	cmd.Stderr = &out
	if project.IsCameraRaw(source) {
		if _, err = s.rawImageProcess(ctx, "develop", source, temp, r.RawDevelop); err != nil {
			return "", err
		}
	} else if err = cmd.Run(); err != nil {
		return "", fmt.Errorf("生成预览代理: %v %s", err, out.String())
	}
	current, err := s.ProjectResource(projectID, id)
	if err != nil || current.Missing || current.Changed || project.ResourceContentVersion(current) != project.ResourceContentVersion(r) {
		return "", model.ErrConflict
	}
	if err = os.Rename(temp, path); err != nil {
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
			return path, nil
		}
		return "", err
	}
	return path, nil
}

func (s *Service) stopProjectTasks(ctx context.Context) error {
	ctx, cancelWait := context.WithTimeout(ctx, 30*time.Second)
	defer cancelWait()
	s.harnessMu.Lock()
	for _, cancel := range s.harnessCancel {
		cancel()
	}
	s.harnessMu.Unlock()
	rows, err := s.st.SkillList(ctx, "run")
	if err != nil {
		return err
	}
	for _, raw := range rows {
		var r skill.Run
		if json.Unmarshal(raw, &r) != nil {
			return model.ErrInvalidState
		}
		if r.State == "running" || r.State == "queued" {
			if _, err = s.UpdateSkillRun(ctx, "human:workspace", r.ID, "cancel", r.Version); err != nil {
				return err
			}
		}
	}
	previews, err := s.st.ListTTSPreviews(ctx)
	if err != nil {
		return err
	}
	for _, preview := range previews {
		if preview.Status == "running" || preview.Status == "queued" {
			if _, err = s.CancelTTSPreview(ctx, "human:workspace", preview.JobID); err != nil {
				return err
			}
		}
	}
	// Existing workers acknowledge cancellation through their existing control
	// protocol. Do not drop their records before the execution is drained.
	tasks, err := s.st.ListActiveTasks(ctx)
	if err != nil {
		return err
	}
	var wake []string
	if err = s.st.Transaction(ctx, func(tx *store.Store) error {
		for _, task := range tasks {
			executions, err := tx.ListIngestExecutionsByTask(ctx, task.TaskID)
			if err != nil {
				return err
			}
			for _, execution := range executions {
				if err := s.revokeExecution(ctx, tx, execution, "project_closing", &wake); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	s.finishControl(wake)
	for _, task := range tasks {
		if task.AgentID != "" && (task.Status == model.TaskStatusClaimed || task.Status == model.TaskStatusRunning) {
			s.workspace.mu.Lock()
			if s.workspace.draining == nil {
				s.workspace.draining = map[string]string{}
			}
			s.workspace.draining[task.TaskID] = task.AgentID
			s.workspace.mu.Unlock()
		}
		task.Status = model.TaskStatusCancelled
		task.LeaseUntil = nil
		task.Message = "工程关闭或切换"
		if err = s.st.UpdateTask(ctx, task); err != nil {
			return err
		}
	}
	for {
		s.harnessMu.Lock()
		active := len(s.harnessCancel)
		s.harnessMu.Unlock()
		if runtime := s.ttsRuntime; runtime != nil {
			runtime.mu.Lock()
			active += len(runtime.cancel)
			runtime.mu.Unlock()
		}
		s.workspace.mu.Lock()
		active += len(s.workspace.draining)
		s.workspace.mu.Unlock()
		if active == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return nil
}

func (s *Service) trackProjectExecution(ctx context.Context) (context.Context, func()) {
	return s.trackProjectOperation(ctx, "native:")
}

func (s *Service) trackProjectOperation(ctx context.Context, prefix string) (context.Context, func()) {
	if !s.WorkspaceEnabled() {
		return ctx, func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	key := prefix + newID("operation")
	s.harnessMu.Lock()
	if s.harnessCancel == nil {
		s.harnessCancel = map[string]context.CancelFunc{}
	}
	s.harnessCancel[key] = cancel
	s.harnessMu.Unlock()
	return ctx, func() { cancel(); s.harnessMu.Lock(); delete(s.harnessCancel, key); s.harnessMu.Unlock() }
}

func (w *projectWorkspace) lockMutation(ctx context.Context) error {
	for !w.op.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := ctx.Err(); err != nil {
		w.op.Unlock()
		return err
	}
	return nil
}
