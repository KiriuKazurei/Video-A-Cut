package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// maxAgentEDLBytes bounds what an agent can pull through MCP in one call.
const maxAgentEDLBytes = 1 << 20

// ReadAssetEDLForRole returns the raw edl.json of an asset visible to role.
//
// This is the only content an agent can read through the control plane. The
// visibility rule is the same one ListVisibleAssets applies, checked on the
// freshly read row, and the file goes through deliveryPath so the recorded
// root-relative path cannot escape the delivery root even if the row was
// tampered with. Unknown and invisible assets both answer ErrNotFound so a
// caller cannot probe which ids exist.
func (s *Service) ReadAssetEDLForRole(ctx context.Context, role, assetID string) ([]byte, error) {
	a, err := s.st.GetAsset(ctx, assetID)
	if err != nil || !assetVisibleForRole(a, role) {
		return nil, fmt.Errorf("service: read edl %s: %w", assetID, model.ErrNotFound)
	}
	rel := a.Artifacts["edl"]
	if rel == "" {
		return nil, fmt.Errorf("service: read edl %s: asset has no edl: %w", assetID, model.ErrNotFound)
	}
	path, info, err := s.deliveryPath(filepath.FromSlash(rel), false)
	if err != nil {
		return nil, fmt.Errorf("service: read edl %s: %w", assetID, err)
	}
	if info.Size() > maxAgentEDLBytes {
		return nil, fmt.Errorf("service: read edl %s: file exceeds %d bytes: %w", assetID, maxAgentEDLBytes, model.ErrArgument)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("service: read edl %s: %w", assetID, err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxAgentEDLBytes+1))
	if err != nil {
		return nil, fmt.Errorf("service: read edl %s: %w", assetID, err)
	}
	if len(raw) > maxAgentEDLBytes {
		return nil, fmt.Errorf("service: read edl %s: file grew past limit: %w", assetID, model.ErrArgument)
	}
	return raw, nil
}
