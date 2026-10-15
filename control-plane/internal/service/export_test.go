package service

import (
	"context"

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
