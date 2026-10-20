package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func avpWorkspaceFixture(t *testing.T) (*service.Service, string) {
	t.Helper()
	svc := newService(t)
	root := t.TempDir()
	delivery := filepath.Join(root, "delivery")
	if err := os.MkdirAll(delivery, 0700); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfigureDeliveryRoot(delivery); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfigureProjectWorkspace(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseProjectWorkspace)
	return svc, root
}

func TestProjectSwitchCancelsAndWaitsForOldRequests(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "first.avp"), Name: "First"})
	if err != nil {
		t.Fatal(err)
	}
	request, release, err := svc.BeginProjectRequest(ctx, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "next.avp"), Name: "Next", StopTasks: true})
		result <- err
	}()
	select {
	case <-request.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("old request was not cancelled")
	}
	if svc.CurrentProjectID() != view.Document.Project.ID {
		t.Fatal("projection changed before old request exited")
	}
	release()
	select {
	case err = <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("switch did not finish after old request exited")
	}
}

func TestProjectSwitchWaitsForWorkerIdleAcknowledgement(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "worker.avp"), Name: "Worker"})
	if err != nil {
		t.Fatal(err)
	}
	doc := project.AvsDocument{Format: "avs", SchemaVersion: 1, ID: "source", Title: "Worker", Body: "draft", Segments: json.RawMessage(`[]`), Bindings: []project.Binding{}}
	path := filepath.Join(root, "source.avs")
	if err = project.WriteAtomic(path, doc); err != nil {
		t.Fatal(err)
	}
	view, err = svc.ImportScriptFile(ctx, view.Document.Project.ID, service.ScriptFileImport{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.CreateTask(ctx, model.Task{TaskID: "worker-task", AssetID: view.Document.Resources[0].AssetID, AgentRole: "narrator", Type: model.TaskTypeNarrate}); err != nil {
		t.Fatal(err)
	}
	lease := time.Now().Add(time.Minute)
	if err = svc.Heartbeat(ctx, "worker", "narrator", "", lease); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ClaimTask(ctx, "worker", "narrator", lease); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "after-worker.avp"), Name: "After", Decision: "discard", StopTasks: true})
		result <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		task, e := svc.GetTask(ctx, "worker-task")
		if e != nil {
			t.Fatal(e)
		}
		if task.Status == model.TaskStatusCancelled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker cancellation not requested")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if svc.CurrentProjectID() != view.Document.Project.ID {
		t.Fatal("switched before worker cleanup acknowledged idle")
	}
	if err = svc.Heartbeat(ctx, "worker", "narrator", "", lease); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker acknowledgement did not release switch")
	}
}

func TestProjectFailedAtomicSaveKeepsFileAndDirtyState(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows read-only replacement behavior")
	}
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	path := filepath.Join(root, "readonly.avp")
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: path, Name: "Atomic"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "note.txt")
	if err = os.WriteFile(source, []byte("unsaved"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ImportProjectResource(ctx, view.Document.Project.ID, source); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	if _, err = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{}); err == nil {
		t.Fatal("read-only formal file was overwritten")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed save damaged formal file", err)
	}
	view, err = svc.ProjectWorkspace(ctx)
	if err != nil || !view.Dirty || len(view.Document.Resources) != 1 {
		t.Fatal("failed save discarded working edits", err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	recent := filepath.Join(root, "workspace", "recent.json")
	if err = os.Chmod(recent, 0400); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(recent, 0600)
	view, err = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{})
	if err != nil || view.Dirty {
		t.Fatal("recent list failure must not report successful formal save as failed", err)
	}
	if _, err = svc.ProjectFileCommand(ctx, "close", service.ProjectCommand{}); err != nil {
		t.Fatal(err)
	}
	view, err = svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: path})
	if err != nil || view.Document == nil || len(view.Document.Resources) != 1 {
		t.Fatal("recent list failure rolled back valid project open", err)
	}
}

func TestProjectClosedWorkspaceLogsNeedNoCredentials(t *testing.T) {
	svc, _ := avpWorkspaceFixture(t)
	rows, err := svc.AgentOperations(context.Background(), "human:window", "", 0, 100)
	if err != nil || len(rows) != 0 {
		t.Fatal("home journal access", err)
	}
}

func TestProjectFileRoundTripIsolationAndSaveAs(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	path := filepath.Join(root, "中文工程.avp")
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: path, Name: "中文工程"})
	if err != nil {
		t.Fatal(err)
	}
	first := view.Document.Project.ID
	source := filepath.Join(root, "旁白.md")
	if err = os.WriteFile(source, []byte("# 文稿\n保留原文件"), 0600); err != nil {
		t.Fatal(err)
	}
	view, err = svc.ImportProjectResource(ctx, first, source)
	if err != nil {
		t.Fatal(err)
	}
	r := view.Document.Resources[0]
	notes := "片头使用"
	view, err = svc.UpdateProjectResource(ctx, first, r.ID, service.ResourcePatch{Notes: &notes})
	if err != nil {
		t.Fatal(err)
	}
	if !view.Dirty {
		t.Fatal("resource edits did not dirty document")
	}
	if _, err = svc.ProjectFileCommand(ctx, "close", service.ProjectCommand{}); !errors.Is(err, model.ErrConflict) {
		t.Fatal("unsaved edits were discarded", err)
	}
	view, err = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{})
	if err != nil || view.Dirty {
		t.Fatal("save", err, view.Dirty)
	}
	secondPath := filepath.Join(root, "另存.avp")
	view, err = svc.ProjectFileCommand(ctx, "save-as", service.ProjectCommand{Path: secondPath})
	if err != nil {
		t.Fatal(err)
	}
	second := view.Document.Project.ID
	if second == first {
		t.Fatal("Save As reused project identity")
	}
	if _, err = svc.GetProject(ctx, first); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("old project remained directly accessible", err)
	}
	otherNotes := "独立修改"
	if _, err = svc.UpdateProjectResource(ctx, second, r.ID, service.ResourcePatch{Notes: &otherNotes}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: path, Decision: "discard"}); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ProjectResources(first)
	if err != nil || len(list) != 1 || list[0].Notes != notes {
		t.Fatalf("file round trip: %+v %v", list, err)
	}
	if _, err = svc.ProjectResources(second); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("other project leaked", err)
	}
	if raw, err := os.ReadFile(source); err != nil || string(raw) != "# 文稿\n保留原文件" {
		t.Fatal("import modified original")
	}
	// Opening into a completely different database/cache must reconstruct the
	// project from its file, including script/runtime records, not an old pointer.
	fresh, _ := avpWorkspaceFixture(t)
	view, err = fresh.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: path})
	if err != nil || view.Document.Resources[0].Notes != notes {
		t.Fatal("fresh database open", err)
	}
	if _, err = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{ExpectedSession: "obsolete"}); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale UI generation accepted", err)
	}
}

func TestProjectRecoveryMissingMediaAndInvalidOpen(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	path := filepath.Join(root, "recover.avp")
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: path, Name: "Recovery"})
	if err != nil {
		t.Fatal(err)
	}
	id := view.Document.Project.ID
	source := filepath.Join(root, "source.txt")
	_ = os.WriteFile(source, []byte("original"), 0600)
	view, err = svc.ImportProjectResource(ctx, id, source)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.WriteProjectRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	view, err = svc.ProjectWorkspace(ctx)
	if err != nil || len(view.Recoveries) != 1 {
		t.Fatal("recovery absent", err, view.Recoveries)
	}
	recovery := view.Recoveries[0]
	view, err = svc.ProjectFileCommand(ctx, "recover", service.ProjectCommand{Path: path, RecoveryPath: recovery.RecoveryPath, Decision: "discard"})
	if err != nil || !view.Dirty || len(view.Document.Resources) != 1 {
		t.Fatal("recovery restore", err)
	}
	if _, err = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{}); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(root, "broken.avp")
	_ = os.WriteFile(broken, []byte(`{"format":"avp","schema_version":99}`), 0600)
	if _, err = svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: broken}); err == nil {
		t.Fatal("unsupported format accepted")
	}
	if svc.CurrentProjectID() != id {
		t.Fatal("bad open replaced current document")
	}
	if err = os.Rename(source, source+".moved"); err != nil {
		t.Fatal(err)
	}
	view, err = svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: path})
	if err != nil || !view.Document.Resources[0].Missing {
		t.Fatal("missing resource was lost", err)
	}
	resourceID := view.Document.Resources[0].ID
	view, err = svc.UpdateProjectResource(ctx, id, resourceID, service.ResourcePatch{Path: source + ".moved"})
	if err == nil {
		t.Fatal("unsupported relink extension accepted")
	}
	replacement := filepath.Join(root, "replacement.txt")
	_ = os.WriteFile(replacement, []byte("replacement"), 0600)
	view, err = svc.UpdateProjectResource(ctx, id, resourceID, service.ResourcePatch{Path: replacement})
	if err != nil || view.Document.Resources[0].Missing {
		t.Fatal("relink", err)
	}
}

func TestProjectScriptFileImportAndFreshCache(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	path := filepath.Join(root, "script.avp")
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: path, Name: "Script"})
	if err != nil {
		t.Fatal(err)
	}
	id := view.Document.Project.ID
	script := project.AvsDocument{Format: "avs", SchemaVersion: 1, ID: "original-script", Title: "脚本", Body: "开场介绍", Segments: json.RawMessage(`[{"index":0,"narration":"你好","media_id":"old-video","start":0,"end":1}]`), Bindings: []project.Binding{{ID: "old-video", Name: "原视频.mp4", Kind: "video"}}}
	avs := filepath.Join(root, "source.avs")
	if err = project.WriteAtomic(avs, script); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ImportScriptFile(ctx, id, service.ScriptFileImport{Path: avs}); !errors.Is(err, model.ErrArgument) {
		t.Fatal("binding prompt bypassed", err)
	}
	view, err = svc.ImportScriptFile(ctx, id, service.ScriptFileImport{Path: avs, ScriptOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	r := view.Document.Resources[0]
	w, err := svc.GetScriptWorkbench(ctx, r.ScriptID)
	if err != nil || w.Segments[0].MediaID != "" {
		t.Fatal("script-only import retained foreign media", err)
	}
	set, err := svc.VisibleSet(ctx)
	if err != nil || !set.Confirmed {
		t.Fatal("manual visibility confirmation remains", err)
	}
	w, err = svc.SaveScriptWorkbench(ctx, "human:window", r.ScriptID, service.SaveScriptRequest{ExpectedVersion: w.Version, VisibleSetSHA: set.SHA256, Title: "修改后的标题", Body: "{@原视频.mp4}尚未关联也可以编辑", Segments: w.Segments})
	if err != nil {
		t.Fatal("unbound draft cannot be edited", err)
	}
	if _, err = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{}); err != nil {
		t.Fatal(err)
	}
	exported := filepath.Join(root, "copy.avs")
	if err = svc.ExportScriptFile(ctx, id, r.ScriptID, exported); err != nil {
		t.Fatal(err)
	}
	copy, err := svc.InspectScriptFile(exported)
	if err != nil || copy.Title != "修改后的标题" || len(copy.Bindings) != 1 || !copy.Bindings[0].Unresolved {
		t.Fatal("AVS round trip", err, copy)
	}
	fresh, _ := avpWorkspaceFixture(t)
	view, err = fresh.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	w, err = fresh.GetScriptWorkbench(ctx, view.Document.Resources[0].ScriptID)
	if err != nil || w.Script.Title != "修改后的标题" {
		t.Fatal("fresh cache lost script", err)
	}
	original, _ := svc.InspectScriptFile(avs)
	if original.Title != "脚本" {
		t.Fatal("editing project overwrote original AVS")
	}
	// Resolve a retained descriptor using a current resource. Segment positions
	// survive script-only export/import even though live media IDs were cleared.
	fixture, err := filepath.Abs("../../../workers/node/timeline-cli/fixtures/media/clip_01.mp4")
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.ImportProjectResource(ctx, id, fixture)
	if err != nil {
		t.Fatal(err)
	}
	media := view.Document.Resources[len(view.Document.Resources)-1]
	view, err = svc.BindProjectScript(ctx, id, r.ScriptID, service.ScriptFileImport{Bindings: map[string]string{"old-video": media.ID}})
	if err != nil {
		t.Fatal("resolve pending binding", err)
	}
	w, err = svc.GetScriptWorkbench(ctx, r.ScriptID)
	if err != nil || w.Segments[0].MediaID != media.ID || !strings.Contains(w.Script.Body, "{@clip_01.mp4}") {
		t.Fatal("binding did not update segment and syntax", err, w)
	}
	if err = svc.ExportScriptFile(ctx, id, r.ScriptID, exported); err != nil {
		t.Fatal(err)
	}
	copy, err = svc.InspectScriptFile(exported)
	if err != nil || len(copy.Bindings) != 1 || copy.Bindings[0].Unresolved {
		t.Fatal("resolved AVS export", err, copy)
	}
	// Reimport an exported AVS with an explicit local replacement rather than
	// trusting its old resource IDs or paths in a different project document.
	local := filepath.Join(root, "replacement.mp4")
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(local, raw, 0600); err != nil {
		t.Fatal(err)
	}
	view, err = svc.ImportScriptFile(ctx, id, service.ScriptFileImport{Path: exported, Files: map[string]string{copy.Bindings[0].ID: local}})
	if err != nil {
		t.Fatal("AVS exported then reimported with local replacement", err)
	}
	imported := view.Document.Resources[len(view.Document.Resources)-1]
	w, err = svc.GetScriptWorkbench(ctx, imported.ScriptID)
	if err != nil || imported.ScriptID == r.ScriptID || w.Segments[0].MediaID == media.ID || !strings.Contains(w.Script.Body, "{@replacement.mp4}") {
		t.Fatal("AVS reimport retained old identity or binding", err, w)
	}
	// Syntax-only drafts have explicit bindings before generation creates any
	// segments. Saving/exporting must not drop that selected association.
	script.Body, script.Segments = "{@原视频.mp4}尚未生成", json.RawMessage(`[]`)
	if err = project.WriteAtomic(avs, script); err != nil {
		t.Fatal(err)
	}
	view, err = svc.ImportScriptFile(ctx, id, service.ScriptFileImport{Path: avs, Bindings: map[string]string{"old-video": media.ID}})
	if err != nil {
		t.Fatal(err)
	}
	draftID := view.Document.Resources[len(view.Document.Resources)-1].ScriptID
	if err = svc.ExportScriptFile(ctx, id, draftID, exported); err != nil {
		t.Fatal(err)
	}
	copy, err = svc.InspectScriptFile(exported)
	if err != nil || len(copy.Bindings) != 1 || copy.Bindings[0].Unresolved || copy.Bindings[0].ResourceID != media.ID {
		t.Fatal("syntax-only draft lost its selected association", err, copy)
	}
	if _, err = svc.UpdateProjectResource(ctx, id, media.ID, service.ResourcePatch{Path: local}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectFileCommand(ctx, "undo", service.ProjectCommand{}); err != nil {
		t.Fatal(err)
	}
	asset, err := svc.GetAsset(ctx, media.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	edl, err := os.ReadFile(filepath.Join(root, "delivery", filepath.FromSlash(asset.Artifacts["edl"])))
	var restored struct {
		Video []struct {
			Source string `json:"src"`
		} `json:"video"`
	}
	if err != nil || json.Unmarshal(edl, &restored) != nil || len(restored.Video) != 1 || restored.Video[0].Source != fixture {
		t.Fatal("undo relink did not restore worker EDL source", err, string(edl))
	}
	removed := true
	view, err = svc.UpdateProjectResource(ctx, id, media.ID, service.ResourcePatch{Removed: &removed})
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range view.Document.Resources {
		if resource.ID == media.ID && (!resource.Removed || !resource.Missing || !resource.Referenced) {
			t.Fatal("referenced media lost its relinkable missing placeholder", resource)
		}
	}
	view, err = svc.UpdateProjectResource(ctx, id, media.ID, service.ResourcePatch{Path: fixture, Removed: new(bool)})
	if err != nil {
		t.Fatal(err)
	}
	w, err = svc.GetScriptWorkbench(ctx, r.ScriptID)
	if err != nil || w.Segments[0].MediaID != media.ID {
		t.Fatal("relink changed the script's stable binding ID", err)
	}
}
