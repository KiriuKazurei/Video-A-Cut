package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
)

// PreflightWorkflow is read-only: no revision copy, reopening, audit write,
// process launch, credential read or model request happens during preparation.
func (s *Service) PreflightWorkflow(ctx context.Context, assetID string, profile preparation.Profile) (preparation.Report, error) {
	if err := ctx.Err(); err != nil {
		return preparation.Report{}, err
	}
	report, err := (preparation.Engine{Probe: preparation.NewLocalProbe()}).Check(ctx, profile)
	if err != nil {
		return preparation.Report{}, fmt.Errorf("invalid processing profile: %v: %w", err, model.ErrArgument)
	}
	asset, err := s.st.GetAsset(ctx, assetID)
	if err != nil {
		return preparation.Report{}, err
	}
	add := func(code string, passed bool, message string) {
		status := "blocked"
		if passed {
			status = "passed"
		}
		report.Checks = append(report.Checks, preparation.Check{Code: code, Status: status, Message: message})
	}
	add("asset_governance", asset.AgentVisible && !asset.Locked, "资产需对 Agent 可见且未锁定")
	for _, role := range []string{"recognizer", "narrator", "exporter"} {
		add("role_"+role, containsString(asset.AllowedAgents, role), "需要资产允许角色 "+role)
	}
	_, activeErr := s.st.ActiveWorkflow(ctx, assetID)
	if activeErr != nil && !errors.Is(activeErr, model.ErrNotFound) {
		return preparation.Report{}, activeErr
	}
	add("active_workflow", errors.Is(activeErr, model.ErrNotFound), "同一资产不得存在另一个活动流程")
	_, _, packageErr := s.deliveryPath(filepath.FromSlash(asset.Artifacts["edl"]), false)
	add("controlled_edl", packageErr == nil, "EDL 需为交付根目录内可读取的受控文件；完整媒体检查待接入")
	return report, nil
}
