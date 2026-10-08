package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
)

// DefaultPreviewCapabilityExpiry is how long an agent's reported preview capability remains valid.
const DefaultPreviewCapabilityExpiry = 30 * time.Second

// ReportPreviewCapability records the preview capability for an exporter agent.
func (s *Service) ReportPreviewCapability(ctx context.Context, agentID, role string, cap preview.Capability) error {
	if agentID == "" {
		return fmt.Errorf("service: agent_id is required: %w", model.ErrArgument)
	}
	if role != "exporter" {
		return fmt.Errorf("service: only exporter agents may report preview capability: %w", model.ErrForbidden)
	}
	raw, err := json.Marshal(cap)
	if err != nil {
		return fmt.Errorf("service: encode preview capability: %w", err)
	}
	expiresAt := time.Now().UTC().Add(DefaultPreviewCapabilityExpiry)
	ready := cap.Ready && cap.ProtocolVersion == preview.ProtocolVersion && cap.ExecutionProtocol == PreviewExecutionProtocol
	if err := s.st.PutPreviewCapability(ctx, agentID, cap.ProtocolVersion, ready, string(raw), expiresAt); err != nil {
		return err
	}
	s.publish("capability.changed", map[string]any{"role": role, "agent_id": agentID})
	return nil
}

// CheckPreviewCapability checks if there is any active worker reporting ready preview capability
// for the given preset.
func (s *Service) CheckPreviewCapability(ctx context.Context, presetID string) (bool, error) {
	if presetID == "" {
		presetID = preview.PresetID
	}
	caps, err := s.st.ListActivePreviewCapabilities(ctx)
	if err != nil {
		return false, err
	}
	wantedSHA := ""
	if presetID == preview.PresetID {
		wantedSHA = preview.DefaultPresetSHA256()
	}
	for _, c := range caps {
		if !c.Ready || c.ProtocolVersion != preview.ProtocolVersion {
			continue
		}
		var parsed preview.Capability
		if err := json.Unmarshal([]byte(c.CapabilityJSON), &parsed); err != nil {
			continue
		}
		if !parsed.Ready || parsed.ProtocolVersion != preview.ProtocolVersion || parsed.ExecutionProtocol != PreviewExecutionProtocol {
			continue
		}
		for _, p := range parsed.Presets {
			if p.PresetID == presetID {
				if wantedSHA == "" || p.SHA256 == wantedSHA {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
