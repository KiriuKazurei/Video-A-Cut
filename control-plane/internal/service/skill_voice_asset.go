package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

// VoiceSynthesizeRequest asks the open project to render one narration asset.
// The Agent places it afterwards with timeline.edit clip.insert. scripts.propose
// still clears flat-plan voice settings; this tool does not write those fields.
type VoiceSynthesizeRequest struct {
	Text     string       `json:"text"`
	Settings tts.Settings `json:"settings"`
	Name     string       `json:"name,omitempty"`
}

func narrationAssetName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "旁白", nil
	}
	if len([]rune(name)) > 80 || strings.ContainsAny(name, `/\`+string(os.PathSeparator)+"\x00") {
		return "", fmt.Errorf("旁白名称无效: %w", model.ErrArgument)
	}
	return name, nil
}

// projectGeneratedFile creates vac-generated next to the open AVP and rejects
// symlink escapes. Callers never supply the directory.
func (s *Service) projectGeneratedFile(name string) (string, error) {
	w := s.workspace
	if w == nil || !filepath.IsAbs(w.path) {
		return "", model.ErrInvalidState
	}
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return "", model.ErrArgument
	}
	projectDir := filepath.Dir(w.path)
	dir := filepath.Join(projectDir, "vac-generated")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", model.ErrForbidden
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", model.ErrForbidden
	}
	realProject, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		return "", model.ErrForbidden
	}
	if !strings.EqualFold(filepath.Clean(realDir), filepath.Clean(dir)) {
		return "", model.ErrForbidden
	}
	rel, err := filepath.Rel(realProject, realDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", model.ErrForbidden
	}
	return filepath.Join(dir, name), nil
}

func (s *Service) synthesizeSkillVoice(ctx context.Context, r *skill.Run, args json.RawMessage) (json.RawMessage, error) {
	var req VoiceSynthesizeRequest
	if err := decodeSkillArgs(args, &req); err != nil {
		return nil, err
	}
	if !s.WorkspaceEnabled() || r.ProjectID == "" || r.ProjectID != s.CurrentProjectID() {
		return nil, fmt.Errorf("配音只能写入当前打开的工程: %w", model.ErrForbidden)
	}
	name, err := narrationAssetName(req.Name)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(req.Text)
	if !tts.ValidText(text, 1000, true) {
		return nil, fmt.Errorf("旁白为 1～1000 字符: %w", model.ErrArgument)
	}
	if err = req.Settings.Validate(); err != nil {
		return nil, fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	if req.Settings.External() && !s.ProviderPolicy().ExternalTTS {
		return nil, fmt.Errorf("请在服务商设置中启用云端配音: %w", model.ErrForbidden)
	}
	runtime := s.ttsRuntime
	if runtime == nil || !runtime.ready {
		return nil, fmt.Errorf("TTS 运行环境未配置: %w", model.ErrInvalidState)
	}
	output, err := s.projectGeneratedFile(newID("voice") + ".wav")
	if err != nil {
		return nil, err
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(output)
		}
	}()
	jobCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	stop := context.AfterFunc(runtime.ctx, cancel)
	defer stop()
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return nil, model.ErrInvalidState
	}
	runtime.wg.Add(1)
	runtime.mu.Unlock()
	defer runtime.wg.Done()
	select {
	case runtime.gate <- struct{}{}:
		defer func() { <-runtime.gate }()
	case <-jobCtx.Done():
		return nil, jobCtx.Err()
	}
	result, err := s.runTTSProcess(jobCtx, map[string]any{"text": text, "settings": req.Settings, "output": output})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ttsErrorMessage(err), model.ErrInvalidState)
	}
	if r.ProjectID != s.CurrentProjectID() {
		return nil, fmt.Errorf("工程已切换，旁白未导入: %w", model.ErrConflict)
	}
	resource, err := s.importGeneratedAudio(ctx, r.ProjectID, output, name)
	if err != nil {
		return nil, err
	}
	if !finiteScriptTime(resource.Out) || resource.Out <= 0 || resource.Out > 600 {
		return nil, fmt.Errorf("旁白时长无效: %w", model.ErrInvalidState)
	}
	if result.Duration > 0 && (result.Duration > resource.Out+0.5 || resource.Out > result.Duration+0.5) {
		return nil, fmt.Errorf("旁白时长与文件不一致: %w", model.ErrInvalidState)
	}
	remove = false
	return json.Marshal(map[string]any{
		"asset_id":    resource.ID,
		"resource_id": resource.ID,
		"duration":    resource.Out,
		"name":        resource.Name,
	})
}

func (s *Service) importGeneratedAudio(ctx context.Context, projectID, path, name string) (project.Resource, error) {
	var resource project.Resource
	w := s.workspace
	if w == nil {
		return resource, model.ErrInvalidState
	}
	if err := w.lockMutation(ctx); err != nil {
		return resource, err
	}
	defer w.op.Unlock()
	if projectID != s.CurrentProjectID() {
		return resource, model.ErrNotFound
	}
	resource, err := s.inspectResource(ctx, path)
	if err != nil {
		return resource, err
	}
	if resource.Kind != "audio" {
		return resource, fmt.Errorf("旁白不是音频: %w", model.ErrInvalidState)
	}
	resource.Name = name
	resource.AssetID = resource.ID
	if err = s.materializeResource(ctx, resource, projectID); err != nil {
		return resource, err
	}
	w.mu.Lock()
	w.document.Resources = append(w.document.Resources, resource)
	w.dirty = true
	w.mu.Unlock()
	s.rememberResourceEdit(nil, resource)
	if err = s.refreshProjectMedia(ctx); err != nil {
		return resource, err
	}
	s.publish("resource.changed", map[string]any{"project_id": projectID, "resource_id": resource.ID})
	return resource, nil
}
