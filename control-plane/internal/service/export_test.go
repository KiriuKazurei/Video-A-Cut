package service

import (
	"context"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// ReopenAssetFromStart exposes the internal call startWorkflow makes, so a
// test can prove the guard inside ReopenAsset itself without going through the
// earlier pre-check.
func (s *Service) ReopenAssetFromStart(ctx context.Context, actor, assetID string) (model.Asset, error) {
	return s.reopenAsset(ctx, actor, assetID, true)
}

// SetAfterEarlyAutoCheck installs a hook that runs between startWorkflow's
// early Rule A check and its transaction, to reproduce the race the
// in-transaction check closes.
func (s *Service) SetAfterEarlyAutoCheck(f func()) { s.afterEarlyAutoCheck = f }

// SetAfterScoreCollect installs a hook that runs between ScoreAutoRun's file
// reads and its transaction, to reproduce a preview that goes stale in that gap.
func (s *Service) SetAfterScoreCollect(f func()) { s.afterScoreCollect = f }

// SetBeforePreviewTx installs a hook that runs between startPreview's
// pre-transaction checks and its transaction, to create the job another start
// would have created in that gap.
func (s *Service) SetBeforePreviewTx(f func()) { s.beforePreviewTx = f }

// SetReviewEncoder replaces the JSON encoder ScoreAutoRun stores a review with.
func (s *Service) SetReviewEncoder(f func(any) ([]byte, error)) { s.encodeJSON = f }

// ErrAutoBudgetSpent is the sentinel the driver turns into halted(budget).
var ErrAutoBudgetSpent = errAutoBudgetSpent

// AutoGuard exposes the check every automatic action makes in its transaction.
func AutoGuard(a model.WorkflowAuto, inScope bool, autoVersion int, now time.Time) error {
	return autoGuard(a, inScope, autoVersion, now)
}

// SetBeforeAutoTx installs a hook that runs between an auto approval's early
// replay check and its transaction.
func (s *Service) SetBeforeAutoTx(f func()) { s.beforeAutoTx = f }
