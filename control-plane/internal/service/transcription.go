package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"time"
)

// Probe only inspects local files and imports dependencies; it never loads a model or calls a provider.
func (s *Service) CheckLocalTranscription(ctx context.Context, provider preparation.TranscriptionProvider) (preparation.TranscriptionDiagnostic, error) {
	result := preparation.TranscriptionDiagnostic{Code: "local_asr_unavailable"}
	if !provider.Local() || provider.Validate() != nil {
		return result, model.ErrArgument
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, python, err := s.scriptRoot()
	if err != nil {
		result.Message = err.Error()
		return result, nil
	}
	if provider.PythonExecutable != "" {
		python = provider.PythonExecutable
	}
	result.PythonExecutable = python
	fingerprint, err := provider.ModelFingerprint(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		result.Code = "local_asr_model_missing"
		result.Message = err.Error()
		return result, nil
	}
	raw, err := s.skillProcessWithPython(ctx, "probe-transcription", map[string]any{"provider": provider}, python)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		result.Message = fmt.Sprintf("本地转录环境不可用：%s。请确认所选 Python 已安装 requirements-asr.txt；不会下载模型或回退云服务。", err)
		return result, nil
	}
	var reply struct {
		Version string `json:"dependency_version"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return result, err
	}
	result.OK, result.Code = true, "local_asr_ready"
	result.ModelFingerprint, result.DependencyVersion = fingerprint, reply.Version
	result.Message = "本地模型文件和转录依赖可用，尚未执行语音识别。"
	return result, nil
}
