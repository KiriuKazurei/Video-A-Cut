package service_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestTimelineAtomicCASReplayHistoryAndPortableSave(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "timeline.avp"), Name: "Timeline"})
	if err != nil {
		t.Fatal(err)
	}
	id, seq := view.Document.Project.ID, view.Document.Sequences[0]
	req := project.Command{Session: view.ID, ExpectedRevision: seq.Revision, IdempotencyKey: "add", Operations: []project.Operation{{Type: "track.add", Kind: "audio"}}}
	first, err := svc.TimelineCommand(ctx, "human:window", id, seq.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.TimelineCommand(ctx, "human:window", id, seq.ID, req)
	if err != nil || replay.Sequence.Revision != first.Sequence.Revision {
		t.Fatalf("replay %v", err)
	}
	req.IdempotencyKey = "stale"
	if _, err = svc.TimelineCommand(ctx, "human:window", id, seq.ID, req); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale revision accepted %v", err)
	}
	req.ExpectedRevision = first.Sequence.Revision
	req.IdempotencyKey = "atomic"
	req.Operations = append(req.Operations, project.Operation{Type: "clip.move", ClipIDs: []string{"outside"}})
	if _, err = svc.TimelineCommand(ctx, "human:window", id, seq.ID, req); err == nil {
		t.Fatal("invalid batch accepted")
	}
	current, _ := svc.Timeline(ctx, id, seq.ID)
	if len(current.Sequence.Tracks) != 7 || current.Sequence.Revision != first.Sequence.Revision {
		t.Fatal("partial mutation")
	}
	req.IdempotencyKey = "undo"
	req.Operations = []project.Operation{{Type: "history.undo"}}
	current, err = svc.TimelineCommand(ctx, "human:window", id, seq.ID, req)
	if err != nil || len(current.Sequence.Tracks) != 6 {
		t.Fatalf("undo %v", err)
	}
	req.ExpectedRevision = current.Sequence.Revision
	req.IdempotencyKey = "redo"
	req.Operations[0].Type = "history.redo"
	current, err = svc.TimelineCommand(ctx, "human:window", id, seq.ID, req)
	if err != nil || len(current.Sequence.Tracks) != 7 {
		t.Fatalf("redo %v", err)
	}
	view, err = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{ExpectedSession: view.ID})
	if err != nil {
		t.Fatal(err)
	}
	savedPath := view.Path
	if _, err = svc.ProjectFileCommand(ctx, "close", service.ProjectCommand{ExpectedSession: view.ID}); err != nil {
		t.Fatal(err)
	}
	view, err = svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: savedPath})
	if err != nil {
		t.Fatal(err)
	}
	if view.Document.SchemaVersion != project.SchemaVersion || len(view.Document.Sequences[0].Tracks) != 7 {
		t.Fatal("saved timeline lost")
	}
	req.Session = view.ID
	req.ExpectedRevision = current.Sequence.Revision
	req.IdempotencyKey = "foreign"
	if _, err = svc.TimelineCommand(ctx, "human:window", "other", seq.ID, req); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("cross-project mutation %v", err)
	}
}

func TestTimelineMarkerRenamePreservesDraftAndDuplicateBindings(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "markers.avp"), Name: "Markers"})
	if err != nil {
		t.Fatal(err)
	}
	id, seq := view.Document.Project.ID, view.Document.Sequences[0]
	commands := func(ops ...project.Operation) service.TimelineView {
		t.Helper()
		v, e := svc.Timeline(ctx, id, seq.ID)
		if e != nil {
			t.Fatal(e)
		}
		v, e = svc.TimelineCommand(ctx, "human:window", id, seq.ID, project.Command{Session: view.ID, ExpectedRevision: v.Sequence.Revision, IdempotencyKey: project.TimelineID("test"), Operations: ops})
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	added := commands(project.Operation{Type: "marker.add", Marker: &project.Marker{Kind: "point", Notes: "重名", Frame: 10}}, project.Operation{Type: "marker.add", Marker: &project.Marker{Kind: "point", Notes: "重名", Frame: 20}})
	a, b := added.Sequence.Markers[0].ID, added.Sequence.Markers[1].ID
	directory, err := svc.ReferenceDirectory(id, seq.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := "😀 {@重名} 正文重名 {@重名}"
	tokens := project.ReferenceTokens(body)
	refs := []project.Reference{}
	for i, target := range []string{a, b} {
		for _, candidate := range directory {
			if candidate.TargetID == target {
				r := candidate.Reference
				r.ID = project.TimelineID("ref")
				r.Start = tokens[i].Start
				r.End = tokens[i].End
				refs = append(refs, r)
			}
		}
	}
	external := filepath.Join(root, "original.avs")
	sc := project.AvsDocument{Format: "avs", SchemaVersion: 2, ID: "external", Title: "Draft", Body: body, References: refs, Segments: json.RawMessage(`[]`)}
	if err = project.WriteAtomic(external, sc); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(external)
	view, err = svc.ImportScriptFile(ctx, id, service.ScriptFileImport{Path: external})
	if err != nil {
		t.Fatal(err)
	}
	scriptID := view.Document.Scripts[0].ID
	w, err := svc.GetScriptWorkbench(ctx, scriptID)
	if err != nil {
		t.Fatal(err)
	}
	version := w.Version
	before := commands(project.Operation{Type: "marker.update", MarkerID: a, Marker: &project.Marker{Kind: "point", Notes: "新备注", Frame: 10}})
	w, err = svc.GetScriptWorkbench(ctx, scriptID)
	if err != nil || w.Script.Body != "😀 {@新备注} 正文重名 {@重名}" || w.Version != version {
		t.Fatalf("selective rename: %v %+v", err, w)
	}
	set, _ := svc.VisibleSet(ctx)
	w, err = svc.SaveScriptWorkbench(ctx, "human:window", scriptID, service.SaveScriptRequest{ExpectedVersion: w.Version, VisibleSetSHA: set.SHA256, Title: w.Script.Title, Body: w.Script.Body + " 人工新增", References: w.References, Segments: w.Segments})
	if err != nil {
		t.Fatal(err)
	}
	undo := commands(project.Operation{Type: "history.undo"})
	w, err = svc.GetScriptWorkbench(ctx, scriptID)
	if err != nil || w.Script.Body != body+" 人工新增" {
		t.Fatalf("undo overwrote newer draft: %v %+v", err, w)
	}
	if undo.ContentVersion != before.ContentVersion {
		t.Fatal("marker label invalidated program cache")
	}
	bytes, _ := os.ReadFile(external)
	if string(bytes) != string(original) {
		t.Fatal("project edit overwrote external AVS")
	}
	current, _ := svc.ProjectWorkspace(ctx)
	copy, e := svc.ManageSequence(ctx, id, service.SequenceRequest{Session: current.ID, ExpectedRevision: current.Revision, IdempotencyKey: "duplicate", Action: "duplicate", SequenceID: seq.ID})
	if e != nil {
		t.Fatal(e)
	}
	if copy.Document.Sequences[0].Markers[0].ID != a || copy.Document.Sequences[1].Markers[0].ID == a {
		t.Fatal("duplication changed original marker identity")
	}
}
