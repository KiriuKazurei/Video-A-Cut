package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

type scriptOutput struct{ bytes.Buffer }

// Do not spend twice on simultaneous generate/export clicks for one script.
func (s *Service) acquireScript(id string) (func(), error) {
	s.scriptMu.Lock()
	defer s.scriptMu.Unlock()
	if s.scriptBusy[id] {
		return nil, model.ErrConflict
	}
	if s.scriptBusy == nil {
		s.scriptBusy = map[string]bool{}
	}
	s.scriptBusy[id] = true
	return func() { s.scriptMu.Lock(); delete(s.scriptBusy, id); s.scriptMu.Unlock() }, nil
}

func (b *scriptOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4<<20 {
		return 0, fmt.Errorf("script output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func (s *Service) scriptRoot() (string, string, error) {
	if s.ttsRuntime != nil && s.ttsRuntime.dir != "" {
		root := filepath.Clean(filepath.Join(s.ttsRuntime.dir, "..", ".."))
		if _, err := os.Stat(filepath.Join(root, "workers", "python", "vac_worker", "script_workbench.py")); err == nil {
			return root, s.ttsRuntime.python, nil
		}
	}
	wd, _ := os.Getwd()
	for _, root := range []string{wd, filepath.Join(wd, "..")} {
		if _, err := os.Stat(filepath.Join(root, "workers", "python", "vac_worker", "script_workbench.py")); err == nil {
			return root, "python", nil
		}
	}
	return "", "", fmt.Errorf("脚本运行环境未配置，请配置 Python worker 目录: %w", model.ErrInvalidState)
}

func (s *Service) scriptProcess(ctx context.Context, node bool, operation string, payload any) ([]byte, error) {
	root, python, err := s.scriptRoot()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	var cmd *exec.Cmd
	if node {
		cmd = exec.CommandContext(ctx, "node", filepath.Join(root, "workers", "node", "timeline-cli", "src", "script-workbench.mjs"), operation)
		cmd.Dir = filepath.Join(root, "workers", "node", "timeline-cli")
	} else {
		cmd = exec.CommandContext(ctx, python, "-B", "-m", "vac_worker.script_workbench")
		cmd.Dir = filepath.Join(root, "workers", "python")
	}
	hideTTSCommand(cmd)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONDONTWRITEBYTECODE=1")
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	cmd.Stdin = bytes.NewReader(raw)
	var output scriptOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	runErr := cmd.Run()
	var status struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if json.Unmarshal(output.Bytes(), &status) != nil {
		return nil, fmt.Errorf("脚本 worker 不可用或响应无效: %w", model.ErrInvalidState)
	}
	if !status.OK || runErr != nil {
		if status.Message == "" {
			status.Message = "脚本操作失败，请检查运行环境"
		}
		return nil, fmt.Errorf("%s: %w", status.Message, model.ErrInvalidState)
	}
	return output.Bytes(), nil
}

func (s *Service) ScriptFonts(ctx context.Context) ([]string, error) {
	raw, err := s.scriptProcess(ctx, true, "fonts", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Fonts []string `json:"fonts"`
	}
	err = json.Unmarshal(raw, &result)
	return result.Fonts, err
}

type GenerateScriptRequest struct {
	ExpectedVersion  int    `json:"expected_version"`
	VisibleSetSHA    string `json:"visible_set_sha"`
	ProfileID        string `json:"profile_id"`
	ProfileRevision  int    `json:"profile_revision"`
	AllowWebResearch bool   `json:"allow_web_research"`
}

func (s *Service) checkScriptContext(ctx context.Context, id string, version int, sha string, media []ScriptMedia, generating bool) error {
	return s.st.Transaction(ctx, func(tx *store.Store) error {
		w, err := workbenchAt(ctx, tx, id)
		if err != nil {
			return err
		}
		if w.Version != version || w.Script.VisibleSetSHA != sha || w.Script.Status == model.ScriptExpired {
			return model.ErrConflict
		}
		view, err := s.visibleSetAt(ctx, tx)
		if err != nil {
			return err
		}
		if !view.Confirmed || view.SHA256 != sha {
			return model.ErrConflict
		}
		for _, m := range media {
			a, err := tx.GetAsset(ctx, m.AssetID)
			if err != nil {
				return err
			}
			if a.Locked || !a.AgentVisible {
				return model.ErrForbidden
			}
			if generating && (!containsString(a.AllowedAgents, "recognizer") || !containsString(a.AllowedAgents, "narrator")) {
				return fmt.Errorf("可见素材需允许视觉和解说角色: %w", model.ErrForbidden)
			}
		}
		return s.requireVisibleConfirmation(ctx, tx, w.Script.AssetID)
	})
}

func (s *Service) GenerateScriptWorkbench(ctx context.Context, actor, id string, req GenerateScriptRequest) (ScriptWorkbench, error) {
	if err := requireHumanConfirm(actor, "generate script"); err != nil {
		return ScriptWorkbench{}, err
	}
	release, err := s.acquireScript(id)
	if err != nil {
		return ScriptWorkbench{}, err
	}
	defer release()
	w, err := s.GetScriptWorkbench(ctx, id)
	if err != nil {
		return w, err
	}
	if w.Version != req.ExpectedVersion || w.Script.VisibleSetSHA != req.VisibleSetSHA {
		return w, model.ErrConflict
	}
	if err = s.requireVisibleConfirmation(ctx, s.st, w.Script.AssetID); err != nil {
		return w, err
	}
	media, err := s.ScriptMediaCatalogue(ctx)
	if err != nil {
		return w, err
	}
	if err = validateScriptPlan(ScriptWorkbench{Document: w.Document}, media, false); err != nil {
		return w, err
	}
	if err = s.checkScriptContext(ctx, id, req.ExpectedVersion, req.VisibleSetSHA, media, true); err != nil {
		return w, err
	}
	profile, sha, err := s.GetProcessingProfile(ctx, req.ProfileID, req.ProfileRevision)
	if err != nil {
		return w, err
	}
	if profile.ContentMode != "configured" {
		return w, fmt.Errorf("请选择已配置模型的预设: %w", model.ErrArgument)
	}
	if profile.External() {
		ok, e := s.st.ProfileConsent(ctx, profile.ProfileID, profile.Revision, sha)
		if e != nil {
			return w, e
		}
		if !ok {
			return w, fmt.Errorf("该预设版本尚未授权外发: %w", model.ErrForbidden)
		}
	}
	privateMedia := []map[string]any{}
	for _, m := range media {
		privateMedia = append(privateMedia, map[string]any{"id": m.ID, "asset_id": m.AssetID, "name": m.Name, "path": m.Path, "in": m.In, "out": m.Out})
	}
	raw, err := s.scriptProcess(ctx, false, "generate", map[string]any{"profile": profile, "document": w.Document, "media": privateMedia, "allow_web_research": req.AllowWebResearch})
	if err != nil {
		return w, err
	}
	var result struct {
		Segments  []ScriptSegment `json:"segments"`
		Sources   []ScriptSource  `json:"sources"`
		Questions []string        `json:"questions"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return w, err
	}
	if len(result.Questions) > 10 {
		return w, model.ErrArgument
	}
	for _, q := range result.Questions {
		if len(q) > 2000 {
			return w, model.ErrArgument
		}
	}
	// Permission can be revoked while the provider is answering. A late answer
	// is discarded instead of overwriting a newer draft or granting consent.
	if profile.External() {
		ok, e := s.st.ProfileConsent(ctx, profile.ProfileID, profile.Revision, sha)
		if e != nil {
			return w, e
		}
		if !ok {
			return w, model.ErrForbidden
		}
	}
	return s.scriptWrite(ctx, actor, id, req.ExpectedVersion, req.VisibleSetSHA, "generate", func(w *ScriptWorkbench) error {
		w.ProfileID = profile.ProfileID
		w.ProfileRevision = profile.Revision
		w.AllowWebResearch = req.AllowWebResearch
		w.Segments = result.Segments
		w.Sources = result.Sources
		w.Questions = result.Questions
		w.Rejected = false
		if w.Script.Status == model.ScriptFinalized || w.Script.Status == model.ScriptNeedsRefinalize {
			w.Script.Status = model.ScriptNeedsRefinalize
			w.Script.StaleReason = "script_changed"
		} else {
			w.Script.Status = model.ScriptDraft
		}
		return nil
	}, func(tx *store.Store) error {
		if !profile.External() {
			return nil
		}
		ok, err := tx.ProfileConsent(ctx, profile.ProfileID, profile.Revision, sha)
		if err != nil {
			return err
		}
		if !ok {
			return model.ErrForbidden
		}
		return nil
	})
}

func (s *Service) ExportScriptWorkbench(ctx context.Context, actor, id string, version int, sha string) (store.ScriptDelivery, error) {
	if err := requireHumanConfirm(actor, "export script"); err != nil {
		return store.ScriptDelivery{}, err
	}
	release, err := s.acquireScript(id)
	if err != nil {
		return store.ScriptDelivery{}, err
	}
	defer release()
	w, err := s.GetScriptWorkbench(ctx, id)
	if err != nil {
		return store.ScriptDelivery{}, err
	}
	if w.Version != version || w.Script.Status != model.ScriptFinalized || w.Script.VisibleSetSHA != sha {
		return store.ScriptDelivery{}, model.ErrConflict
	}
	if err = s.requireVisibleConfirmation(ctx, s.st, w.Script.AssetID); err != nil {
		return store.ScriptDelivery{}, err
	}
	if w.TTSEnabled {
		return store.ScriptDelivery{}, fmt.Errorf("当前交付包含画面、原视频声音和字幕，暂不合成配音；请先关闭配音选项再交付: %w", model.ErrInvalidState)
	}
	media, err := s.ScriptMediaCatalogue(ctx)
	if err != nil {
		return store.ScriptDelivery{}, err
	}
	if err = validateScriptPlan(w, media, true); err != nil {
		return store.ScriptDelivery{}, err
	}
	if err = s.checkScriptContext(ctx, id, version, sha, media, false); err != nil {
		return store.ScriptDelivery{}, err
	}
	byID := map[string]ScriptMedia{}
	for _, m := range media {
		byID[m.ID] = m
	}
	segments := []map[string]any{}
	for _, seg := range w.Segments {
		segments = append(segments, map[string]any{"path": byID[seg.MediaID].Path, "media_id": seg.MediaID, "source_ref": byID[seg.MediaID].Source, "start": seg.Start, "end": seg.End, "narration": seg.Narration, "source_urls": seg.SourceURLs})
	}
	rel := filepath.ToSlash(filepath.Join("script-deliveries", id, newID("export")))
	out := filepath.Join(s.deliveryRoot, filepath.FromSlash(rel))
	if err = os.MkdirAll(out, 0700); err != nil {
		return store.ScriptDelivery{}, err
	}
	if _, _, err = s.deliveryPath(filepath.FromSlash(rel), true); err != nil {
		return store.ScriptDelivery{}, err
	}
	_, err = s.scriptProcess(ctx, true, "render", map[string]any{"script": map[string]any{"script_id": id, "version": version, "visible_set_sha": sha, "finalized_by": w.Script.FinalizedBy, "finalized_at": w.Script.FinalizedAt}, "segments": segments, "document": w.Document, "subtitles_enabled": w.SubtitlesEnabled, "sources": w.Sources, "output_dir": out})
	if err != nil {
		return store.ScriptDelivery{}, err
	}
	d := store.ScriptDelivery{Version: version, VisibleSetSHA: sha, PackageRef: rel, CreatedAt: time.Now().UTC()}
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		cur, e := workbenchAt(ctx, tx, id)
		if e != nil {
			return e
		}
		if cur.Version != version || cur.Script.Status != model.ScriptFinalized || cur.Script.VisibleSetSHA != sha {
			return model.ErrConflict
		}
		if e = s.requireVisibleConfirmation(ctx, tx, cur.Script.AssetID); e != nil {
			return e
		}
		if e = tx.PutScriptDelivery(ctx, id, d); e != nil {
			return e
		}
		return auditTx(ctx, tx, actor, "script.export", id, fmt.Sprint(version))
	})
	return d, err
}

func (s *Service) ScriptDeliveryPath(ctx context.Context, id, name string) (string, error) {
	w, err := s.GetScriptWorkbench(ctx, id)
	if err != nil {
		return "", err
	}
	if w.Script.Status != model.ScriptFinalized {
		return "", model.ErrInvalidState
	}
	if err = s.requireVisibleConfirmation(ctx, s.st, w.Script.AssetID); err != nil {
		return "", err
	}
	view, err := s.VisibleSet(ctx)
	if err != nil {
		return "", err
	}
	if view.SHA256 != w.Script.VisibleSetSHA {
		return "", model.ErrConflict
	}
	d, err := s.st.GetScriptDelivery(ctx, id, w.Version)
	if err != nil {
		return "", err
	}
	if d.VisibleSetSHA != view.SHA256 {
		return "", model.ErrConflict
	}
	if name != "" && name != "final.mp4" {
		return "", model.ErrArgument
	}
	rel := filepath.Join(filepath.FromSlash(d.PackageRef), name)
	p, _, err := s.deliveryPath(rel, name == "")
	return p, err
}
