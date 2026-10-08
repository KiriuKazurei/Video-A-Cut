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
	"sync"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

type ttsRuntime struct {
	mu            sync.Mutex
	wg            sync.WaitGroup
	python, dir   string
	ready, closed bool
	ctx           context.Context
	stop          context.CancelFunc
	cancel        map[string]context.CancelFunc
	gate          chan struct{}
}
type TTSCapabilities struct {
	RuntimeReady     bool        `json:"runtime_ready"`
	MediaReady       bool        `json:"media_ready"`
	PitchAvailable   bool        `json:"pitch_available"`
	CredentialsReady bool        `json:"credentials_ready"`
	SAPIVoices       []string    `json:"sapi_voices"`
	StepVoices       []tts.Voice `json:"stepfun_voices"`
	Message          string      `json:"message,omitempty"`
}
type ttsProcessResult struct {
	OK         bool     `json:"ok"`
	Message    string   `json:"message"`
	Duration   float64  `json:"duration_seconds"`
	Size       int64    `json:"size_bytes"`
	SAPIVoices []string `json:"sapi_voices"`
	Pitch      bool     `json:"pitch_available"`
	MediaReady bool     `json:"media_ready"`
}

func (s *Service) ConfigureTTS(python, dir string) error {
	if python == "" {
		python = "python"
	}
	if dir == "" {
		wd, _ := os.Getwd()
		for _, candidate := range []string{filepath.Join(wd, "workers", "python"), filepath.Join(wd, "..", "workers", "python")} {
			if info, e := os.Stat(filepath.Join(candidate, "vac_worker", "tts_console.py")); e == nil && !info.IsDir() {
				dir = candidate
				break
			}
		}
	}
	root, cancel := context.WithCancel(context.Background())
	r := &ttsRuntime{python: python, dir: dir, ctx: root, stop: cancel, cancel: map[string]context.CancelFunc{}, gate: make(chan struct{}, 1)}
	if dir != "" {
		abs, e := filepath.Abs(dir)
		if e != nil {
			return e
		}
		r.dir = abs
		info, e := os.Stat(filepath.Join(abs, "vac_worker", "tts_console.py"))
		r.ready = e == nil && !info.IsDir() && s.deliveryRoot != ""
	}
	// Recovery does not re-submit a possibly paid cloud request.
	jobs, e := s.st.ListTTSPreviews(context.Background())
	if e != nil {
		cancel()
		return e
	}
	for _, p := range jobs {
		if p.Status == "queued" || p.Status == "running" {
			p.Status = "failed"
			p.Message = "服务重启中断试听，请手动重试"
			p.UpdatedAt = time.Now().UTC()
			if e = s.st.PutTTSPreview(context.Background(), p); e != nil {
				cancel()
				return e
			}
		}
	}
	s.ttsRuntime = r
	if r.ready {
		path := filepath.Join(s.deliveryRoot, ".tts-previews")
		if e = os.MkdirAll(path, 0700); e != nil {
			return e
		}
		if _, e = s.previewPackageDir(".tts-previews"); e != nil {
			return e
		}
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-r.ctx.Done():
					return
				case <-ticker.C:
					r.mu.Lock()
					_ = s.trimTTSCache()
					r.mu.Unlock()
				}
			}
		}()
		return s.trimTTSCache()
	}
	return nil
}
func (s *Service) CloseTTS() {
	if r := s.ttsRuntime; r != nil {
		r.mu.Lock()
		r.closed = true
		r.stop()
		for _, cancel := range r.cancel {
			cancel()
		}
		r.mu.Unlock()
		r.wg.Wait()
	}
}

// Bound the bridge's JSON output. Never return process stderr to the browser.
type boundedTTSOutput struct{ bytes.Buffer }

func (b *boundedTTSOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, fmt.Errorf("TTS bridge output too large")
	}
	return b.Buffer.Write(p)
}
func (s *Service) runTTSProcess(ctx context.Context, payload any) (ttsProcessResult, error) {
	r := s.ttsRuntime
	b, e := json.Marshal(payload)
	if e != nil {
		return ttsProcessResult{}, e
	}
	cmd := exec.CommandContext(ctx, r.python, "-m", "vac_worker.tts_console")
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONDONTWRITEBYTECODE=1")
	hideTTSCommand(cmd)
	cmd.Stdin = bytes.NewReader(b)
	var out boundedTTSOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	runErr := cmd.Run()
	var result ttsProcessResult
	if e = json.Unmarshal(out.Bytes(), &result); e != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("TTS bridge unavailable")
	}
	if !result.OK {
		if result.Message == "" {
			return result, fmt.Errorf("TTS bridge failed")
		}
		return result, fmt.Errorf("%s", result.Message)
	}
	return result, runErr
}
func (s *Service) TTSCapabilities(ctx context.Context, tokenEnv string) (TTSCapabilities, error) {
	// Validate the variable name through the same config rules; never return its value.
	name := ttsTokenEnv(tokenEnv)
	spec := tts.Settings{Engine: "stepfun", Endpoint: "https://api.stepfun.com/step_plan/v1", Model: "stepaudio-2.5-tts", TokenEnv: name, Voice: "vibrant-youth", Language: "zh", Speed: 1, Volume: 1}
	if spec.Validate() != nil {
		return TTSCapabilities{}, model.ErrArgument
	}
	out := TTSCapabilities{SAPIVoices: []string{}, StepVoices: tts.StepVoices, CredentialsReady: os.Getenv(name) != ""}
	r := s.ttsRuntime
	if r == nil || !r.ready {
		out.Message = "需配置 TTS Python 运行目录与交付根目录"
		return out, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result, e := s.runTTSProcess(probeCtx, map[string]any{"operation": "capabilities"})
	if e != nil {
		out.Message = ttsErrorMessage(e)
		return out, nil
	}
	out.RuntimeReady = true
	out.MediaReady = result.MediaReady
	out.PitchAvailable = result.Pitch
	if result.SAPIVoices != nil {
		out.SAPIVoices = result.SAPIVoices
	}
	return out, nil
}
func (s *Service) executeTTSPreview(ctx context.Context, p tts.Preview) {
	r := s.ttsRuntime
	defer r.wg.Done()
	defer func() {
		r.mu.Lock()
		if cancel := r.cancel[p.JobID]; cancel != nil {
			cancel()
		}
		delete(r.cancel, p.JobID)
		r.mu.Unlock()
	}()
	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	case <-ctx.Done():
		s.finishTTSPreview(p, ttsProcessResult{}, ctx.Err())
		return
	}
	r.mu.Lock()
	current, e := s.st.GetTTSPreview(context.Background(), p.JobID)
	if e == nil && current.Status == "queued" && ctx.Err() == nil {
		p.Status = "running"
		p.UpdatedAt = time.Now().UTC()
		e = s.st.PutTTSPreview(context.Background(), p)
	} else {
		r.mu.Unlock()
		s.finishTTSPreview(p, ttsProcessResult{}, context.Canceled)
		return
	}
	r.mu.Unlock()
	if e != nil {
		return
	}
	s.publish("tts.preview.changed", map[string]any{"job_id": p.JobID})
	root, e := s.previewPackageDir(".tts-previews")
	if e != nil {
		s.finishTTSPreview(p, ttsProcessResult{}, e)
		return
	}
	dir := filepath.Join(root, p.JobID)
	if e = os.Mkdir(dir, 0700); e != nil {
		s.finishTTSPreview(p, ttsProcessResult{}, e)
		return
	}
	if _, e = s.previewPackageDir(filepath.ToSlash(filepath.Join(".tts-previews", p.JobID))); e != nil {
		s.finishTTSPreview(p, ttsProcessResult{}, e)
		return
	}
	result, e := s.runTTSProcess(ctx, map[string]any{"text": p.Text, "settings": p.Settings, "output": filepath.Join(dir, "audio.wav")})
	if e == nil {
		_, info, fileErr := s.previewPackageFile(dir, "audio.wav")
		if fileErr != nil || info.Size() != result.Size || result.Size < 44 || result.Size > 32*1024*1024 || result.Duration <= 0 || result.Duration > 600 {
			e = fmt.Errorf("invalid TTS audio result")
		}
	}
	s.finishTTSPreview(p, result, e)
}
func (s *Service) finishTTSPreview(p tts.Preview, result ttsProcessResult, processErr error) {
	r := s.ttsRuntime
	r.mu.Lock()
	defer r.mu.Unlock()
	current, e := s.st.GetTTSPreview(context.Background(), p.JobID)
	if e != nil {
		return
	}
	if current.Status != "cancelled" {
		current.Status = "succeeded"
		current.DurationSeconds = result.Duration
		current.SizeBytes = result.Size
		if processErr != nil {
			current.Status = "failed"
			current.Message = ttsErrorMessage(processErr)
			if result.Message != "" {
				current.Message = result.Message
			}
		}
		current.UpdatedAt = time.Now().UTC()
		if e = s.st.PutTTSPreview(context.Background(), current); e != nil {
			return
		}
	}
	if current.Status != "succeeded" {
		_ = s.removeTTSFiles(current.JobID)
	}
	_ = s.trimTTSCache()
	s.publish("tts.preview.changed", map[string]any{"job_id": p.JobID})
}
func (s *Service) removeTTSFiles(id string) error {
	if !tts.Identifier.MatchString(id) {
		return model.ErrArgument
	}
	dir, e := s.previewPackageDir(".tts-previews/" + id)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	e = filepath.WalkDir(dir, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return model.ErrForbidden
		}
		return nil
	})
	if e != nil {
		return e
	}
	return os.RemoveAll(dir)
}
func (s *Service) trimTTSCache() error {
	jobs, e := s.st.ListTTSPreviews(context.Background())
	if e != nil {
		return e
	}
	var total int64
	// Jobs arrive newest first, retaining recent results within the cache budget.
	for _, p := range jobs {
		if p.Status == "queued" || p.Status == "running" {
			continue
		}
		remove := p.Expired || time.Since(p.CreatedAt) > 24*time.Hour || p.Status != "succeeded"
		if !remove {
			total += p.SizeBytes
			if total > 100*1024*1024 {
				remove = true
			}
		}
		if remove {
			if e = s.removeTTSFiles(p.JobID); e != nil {
				return e
			}
			if !p.Expired {
				p.Expired = true
				if e = s.st.PutTTSPreview(context.Background(), p); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func (s *Service) TTSPreviewAudio(ctx context.Context, id string) (string, error) {
	p, e := s.GetTTSPreview(ctx, id)
	if e != nil {
		return "", e
	}
	if p.Status != "succeeded" || p.Expired {
		return "", model.ErrInvalidState
	}
	dir, e := s.previewPackageDir(".tts-previews/" + id)
	if e != nil {
		return "", model.ErrNotFound
	}
	path, _, e := s.previewPackageFile(dir, "audio.wav")
	return path, e
}
