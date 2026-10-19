package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

type SaveScriptVoiceRequest struct {
	ExpectedVersion int          `json:"expected_version"`
	VisibleSetSHA   string       `json:"visible_set_sha"`
	SegmentIndex    int          `json:"segment_index"`
	Narration       string       `json:"narration"`
	Settings        tts.Settings `json:"settings"`
	OffsetSeconds   float64      `json:"offset_seconds"`
	Enabled         bool         `json:"enabled"`
	AcceptExternal  bool         `json:"accept_external"`
}

// Voice edits are script revisions, never mutable attachments to an old final.
func (s *Service) SaveScriptVoice(ctx context.Context, actor, id string, req SaveScriptVoiceRequest) (ScriptWorkbench, error) {
	if err := req.Settings.Validate(); err != nil {
		return ScriptWorkbench{}, fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	if !tts.ValidText(req.Narration, 1000, req.Enabled) {
		return ScriptWorkbench{}, model.ErrArgument
	}
	return s.scriptWrite(ctx, actor, id, req.ExpectedVersion, req.VisibleSetSHA, "voice", func(w *ScriptWorkbench) error {
		if req.SegmentIndex < 0 || req.SegmentIndex >= len(w.Segments) {
			return model.ErrArgument
		}
		seg := &w.Segments[req.SegmentIndex]
		seg.Narration = req.Narration
		seg.TTSEnabled = &req.Enabled
		seg.Voice = &ScriptVoice{Settings: req.Settings, OffsetSeconds: req.OffsetSeconds, AcceptExternal: req.AcceptExternal}
		if w.Script.Status == model.ScriptFinalized || w.Script.Status == model.ScriptNeedsRefinalize {
			w.Script.Status, w.Script.StaleReason = model.ScriptNeedsRefinalize, "script_changed"
		} else {
			w.Script.Status, w.Script.StaleReason = model.ScriptDraft, ""
		}
		return nil
	})
}

func (s *Service) synthesizeScriptVoice(ctx context.Context, segment ScriptSegment, output string) error {
	if segment.Voice == nil || strings.TrimSpace(segment.Narration) == "" {
		return fmt.Errorf("请在 TTS 专页保存本段配音设置和旁白: %w", model.ErrInvalidState)
	}
	voice := segment.Voice
	if err := voice.Settings.Validate(); err != nil {
		return fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	if !tts.ValidText(segment.Narration, 1000, true) {
		return fmt.Errorf("配音每段为 1～1000 字符: %w", model.ErrArgument)
	}
	if voice.Settings.External() && !voice.AcceptExternal {
		return fmt.Errorf("请在 TTS 专页明确授权本段旁白的外发: %w", model.ErrForbidden)
	}
	r := s.ttsRuntime
	if r == nil || !r.ready {
		return fmt.Errorf("TTS 运行环境未配置: %w", model.ErrInvalidState)
	}
	jobCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return model.ErrInvalidState
	}
	r.wg.Add(1)
	r.mu.Unlock()
	defer r.wg.Done()
	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	case <-jobCtx.Done():
		return jobCtx.Err()
	}
	result, err := s.runTTSProcess(jobCtx, map[string]any{"text": segment.Narration, "settings": voice.Settings, "output": output})
	if err != nil {
		return fmt.Errorf("%s: %w", ttsErrorMessage(err), model.ErrInvalidState)
	}
	_, info, err := s.previewPackageFile(filepath.Dir(output), filepath.Base(output))
	if err != nil {
		return err
	}
	if result.Size < 44 || result.Size > 32*1024*1024 || info.Size() != result.Size || !finiteScriptTime(result.Duration) || result.Duration <= 0 {
		return model.ErrInvalidState
	}
	// A slow voice must be edited or given more room, never silently truncated.
	if result.Duration+voice.OffsetSeconds > segment.End-segment.Start+1.0/30 {
		return fmt.Errorf("配音时长超出当前片段，请调整文稿、语速、起点或视频窗口: %w", model.ErrInvalidState)
	}
	return nil
}
