package service

import (
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"testing"
)

func TestNewIngestCanPrepareAnExportedAssetWithoutUnfreezingContentTasks(t *testing.T) {
	asset := model.Asset{Status: model.AssetStatusExported, InputKind: model.InputKindEDLPackage}
	for _, kind := range []string{ingest.TaskMediaProbe, ingest.TaskSegment, ingest.TaskMediaPrepare} {
		if !typeReady(model.Task{Type: kind, AgentRole: ingest.RoleIngester}, asset) {
			t.Fatalf("new %s was frozen by old export", kind)
		}
	}
	for _, kind := range []string{model.TaskTypeExport, "recognize", "narrate"} {
		if typeReady(model.Task{Type: kind, AgentRole: "exporter"}, asset) {
			t.Fatalf("old content task %s was unfrozen", kind)
		}
	}
	if typeReady(model.Task{Type: ingest.TaskMediaProbe, AgentRole: "recognizer"}, asset) {
		t.Fatal("wrong role bypassed freeze")
	}
}
