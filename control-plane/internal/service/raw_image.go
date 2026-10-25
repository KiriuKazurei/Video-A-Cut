package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

func (s *Service) rawImageProcess(ctx context.Context, operation, path, output string, recipe *project.RawDevelop) (map[string]any, error) {
	root, python, err := s.scriptRoot()
	if err != nil {
		return nil, err
	}
	ctx, release := s.trackProjectExecution(ctx)
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	payload, _ := json.Marshal(map[string]any{"operation": operation, "path": path, "output": output, "settings": recipe})
	cmd := exec.CommandContext(ctx, python, "-B", "-m", "vac_worker.raw_image")
	cmd.Dir = filepath.Join(root, "workers", "python")
	cmd.Env = toolEnvironment()
	configureMediaCommand(cmd)
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(payload)
	var out scriptOutput
	cmd.Stdout = &out
	cmd.Stderr = &scriptOutput{}
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var result struct {
		OK        bool           `json:"ok"`
		Data      map[string]any `json:"data"`
		ErrorType string         `json:"error_type"`
	}
	if json.Unmarshal(out.Bytes(), &result) != nil || !result.OK || runErr != nil {
		return nil, fmt.Errorf("RAW 解码失败（%s）；请确认本地 worker 已安装 requirements-media.txt，且 LibRaw 支持该相机原片；不会使用内嵌缩略图替代原片: %w", result.ErrorType, model.ErrArgument)
	}
	return result.Data, nil
}
func (s *Service) imageRenderPath(ctx context.Context, projectID string, r project.Resource) (string, error) {
	if project.IsCameraRaw(r.Source.OriginalPath) {
		return s.CreateResourcePreview(ctx, projectID, r.ID)
	}
	return s.ProjectResourcePath(projectID, r.ID, false)
}
