package service_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestReadAssetEDLForRole(t *testing.T) {
	svc := newService(t)
	root := t.TempDir()
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(root, "src"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "src", "edl.json"), []byte(`{"timeline":{}}`), 0o644)
	ctx := context.Background()
	for _, a := range []model.Asset{
		{AssetID: "ok", Status: "ingested", AgentVisible: true, AllowedAgents: []string{"narrator"}, Artifacts: map[string]string{"edl": "src/edl.json"}},
		{AssetID: "locked", Status: "ingested", AgentVisible: true, Locked: true, AllowedAgents: []string{"narrator"}, Artifacts: map[string]string{"edl": "src/edl.json"}},
		{AssetID: "hidden", Status: "ingested", AllowedAgents: []string{"narrator"}, Artifacts: map[string]string{"edl": "src/edl.json"}},
		{AssetID: "escape", Status: "ingested", AgentVisible: true, AllowedAgents: []string{"narrator"}, Artifacts: map[string]string{"edl": "../outside.json"}},
		{AssetID: "noedl", Status: "ingested", AgentVisible: true, AllowedAgents: []string{"narrator"}},
	} {
		if err := svc.CreateAsset(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if raw, err := svc.ReadAssetEDLForRole(ctx, "narrator", "ok"); err != nil || string(raw) != `{"timeline":{}}` {
		t.Fatalf("visible: %q %v", raw, err)
	}
	for _, c := range []struct {
		role, id string
		want     error
	}{
		{"exporter", "ok", model.ErrNotFound},
		{"narrator", "locked", model.ErrNotFound},
		{"narrator", "hidden", model.ErrNotFound},
		{"narrator", "missing", model.ErrNotFound},
		{"narrator", "noedl", model.ErrNotFound},
		{"narrator", "escape", model.ErrArgument},
	} {
		if _, err := svc.ReadAssetEDLForRole(ctx, c.role, c.id); !errors.Is(err, c.want) {
			t.Errorf("%s/%s: %v want %v", c.role, c.id, err, c.want)
		}
	}
}
