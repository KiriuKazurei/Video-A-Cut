package service_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestUnifiedPreviewDraftIsolationAndOwnedLifetime(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	repo, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.ConfigureTTS("python", filepath.Join(repo, "workers", "python")); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(svc.CloseTTS)
	ctx := t.Context()
	view, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "preview.avp"), Name: "Preview"})
	if e != nil {
		t.Fatal(e)
	}
	pid := view.Document.Project.ID
	doc := project.DrawDocument{SchemaVersion: 1, Canvas: project.DrawCanvas{Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}}, DurationFrames: 30, Preset: "auto", Sources: []project.DrawSource{}, Nodes: []project.DrawNode{{ID: "title", Kind: "text", Text: "当前工程", Duration: 30}}}
	view, e = svc.SaveDrawComposition(ctx, pid, service.DrawSaveRequest{Session: view.ID, ExpectedRevision: view.Revision, IdempotencyKey: "create", Composition: project.DrawComposition{ID: "live", Name: "Live", Document: &doc}})
	if e != nil {
		t.Fatal(e)
	}
	c := view.Document.DrawCompositions[0]
	req := service.DrawPreviewRequest{Session: view.ID, ContentVersion: c.ContentVersion, RequestID: "frame-1", Frame: 15, Document: &doc}
	frame, e := svc.PreviewDrawComposition(ctx, pid, c.ID, req)
	if e != nil {
		t.Fatal(e)
	}
	if frame.RequestID != req.RequestID || frame.Frame != 15 || frame.Session != view.ID {
		t.Fatal("frame context lost")
	}
	raw, e := base64.StdEncoding.DecodeString(frame.PNG)
	if e != nil {
		t.Fatal(e)
	}
	img, e := png.Decode(bytes.NewReader(raw))
	if e != nil || img.Bounds().Dx() != 320 {
		t.Fatalf("actual image: %v", e)
	}
	firstPID := frame.WorkerPID
	doc.Nodes[0].Text = "草稿"
	req.RequestID = "frame-2"
	frame, e = svc.PreviewDrawComposition(ctx, pid, c.ID, req)
	if e != nil {
		t.Fatal(e)
	}
	if frame.WorkerPID != firstPID {
		t.Fatal("parameter edit recreated persistent worker")
	}
	if stored, _ := svc.DrawCompositions(pid); stored[0].Document.Nodes[0].Text != "当前工程" {
		t.Fatal("preview overwrote saved source")
	}
	for _, format := range []string{"webm-alpha", "prores4444"} {
		ext := ".webm"
		if format == "prores4444" {
			ext = ".mov"
		}
		target := filepath.Join(root, "drawing"+ext)
		renderReq := service.DrawRenderRequest{DrawActionRequest: service.DrawActionRequest{Session: view.ID, ContentVersion: c.ContentVersion}, Format: format}
		if _, e = svc.ExportDrawComposition(ctx, pid, c.ID, renderReq, target+".mp4"); !errors.Is(e, model.ErrArgument) {
			t.Fatalf("wrong output extension accepted: %v", e)
		}
		report, err := svc.ExportDrawComposition(ctx, pid, c.ID, renderReq, target)
		if err != nil {
			t.Fatal(err)
		}
		if report.Format != format || report.Alpha != "straight" || report.Frames != 30 {
			t.Fatalf("transparent delivery contract lost: %+v", report)
		}
		if info, err := os.Stat(target); err != nil || info.Size() != report.Size {
			t.Fatalf("export copy: %v", err)
		}
		if _, _, err := svc.DrawRenderPathFormat(pid, c.ID, c.ContentVersion, format); err != nil {
			t.Fatal(err)
		}
	}
	req.Session = "foreign"
	if _, e = svc.PreviewDrawComposition(ctx, pid, c.ID, req); !errors.Is(e, model.ErrConflict) {
		t.Fatalf("foreign session: %v", e)
	}
	if _, e = svc.ProjectFileCommand(ctx, "close", service.ProjectCommand{Decision: "discard", ExpectedSession: view.ID}); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.PreviewDrawComposition(ctx, pid, c.ID, req); e == nil {
		t.Fatal("closed project preview readable")
	}
}

func TestUnifiedPreviewNormalizesDraftSourceImageBinding(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	repo, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.ConfigureTTS("python", filepath.Join(repo, "workers", "python")); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(svc.CloseTTS)
	view, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "binding.avp"), Name: "Draft"})
	if e != nil {
		t.Fatal(e)
	}
	pid := view.Document.Project.ID
	path := filepath.Join(root, "green.png")
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{0, 255, 0, 255})
		}
	}
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = png.Encode(f, img); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	view, e = svc.ImportProjectResource(ctx, pid, path)
	if e != nil {
		t.Fatal(e)
	}
	res := view.Document.Resources[0]
	doc := project.DrawDocument{SchemaVersion: 1, Canvas: project.DrawCanvas{Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}}, DurationFrames: 30, Preset: "auto", Sources: []project.DrawSource{}, Nodes: []project.DrawNode{{ID: "title", Kind: "text", Text: "base", Duration: 30}}}
	view, e = svc.SaveDrawComposition(ctx, pid, service.DrawSaveRequest{Session: view.ID, ExpectedRevision: view.Revision, IdempotencyKey: "base", Composition: project.DrawComposition{ID: "draft", Name: "Draft", Document: &doc}})
	if e != nil {
		t.Fatal(e)
	}
	c := view.Document.DrawCompositions[0]
	doc.Sources = []project.DrawSource{{ID: "source", Engine: "remotion", Source: `import React from "react";import {Img,useCurrentFrame} from "remotion";export default function Scene({images}){const a=images.hero,f=useCurrentFrame(),p=Math.max(0,Math.min(1,(f-a.from_frame)/(a.to_frame-a.from_frame)));return <Img src={a.src} style={{position:'absolute',left:a.x+a.dx*p,top:a.y,width:a.width,height:a.height}}/>}`, ImageBindings: []project.DrawImageLayer{{ID: "hero", ResourceID: res.ID, Width: 80, Height: 80, DX: 120, FromFrame: 0, ToFrame: 29, Mode: "source"}}}}
	doc.Nodes = []project.DrawNode{{ID: "scene", Kind: "source", SourceID: "source", Duration: 30}}
	frame, e := svc.PreviewDrawComposition(ctx, pid, c.ID, service.DrawPreviewRequest{Session: view.ID, ContentVersion: c.ContentVersion, RequestID: "source-image", Frame: 0, Document: &doc})
	if e != nil {
		t.Fatalf("draft image binding: %v", e)
	}
	raw, e := base64.StdEncoding.DecodeString(frame.PNG)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := png.Decode(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	_, g, _, _ := decoded.At(40, 40).RGBA()
	if g < 50000 {
		t.Fatal("bound image pixels did not appear")
	}
	frame, e = svc.PreviewDrawComposition(ctx, pid, c.ID, service.DrawPreviewRequest{Session: view.ID, ContentVersion: c.ContentVersion, RequestID: "source-image-last", Frame: 29, Document: &doc})
	if e != nil {
		t.Fatal(e)
	}
	raw, e = base64.StdEncoding.DecodeString(frame.PNG)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e = png.Decode(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	_, g, _, _ = decoded.At(160, 40).RGBA()
	if g < 50000 {
		t.Fatal("source image motion parameters were lost")
	}
}

func TestDrawingSourceAttachNeedsNoMovieAndKeepsFrozenAndFollowingDependencies(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	view, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "source.avp"), Name: "Sources"})
	if e != nil {
		t.Fatal(e)
	}
	d := project.DrawDocument{SchemaVersion: 1, Canvas: project.DrawCanvas{Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}}, DurationFrames: 30, Preset: "auto", Sources: []project.DrawSource{}, Nodes: []project.DrawNode{{ID: "text", Kind: "text", Text: "Original", Duration: 30}}}
	view, e = svc.SaveDrawComposition(ctx, view.Document.Project.ID, service.DrawSaveRequest{Session: view.ID, ExpectedRevision: view.Revision, IdempotencyKey: "save", Composition: project.DrawComposition{ID: "source", Name: "Source", Document: &d}})
	if e != nil {
		t.Fatal(e)
	}
	pid := view.Document.Project.ID
	c := view.Document.DrawCompositions[0]
	seq := view.Document.Sequences[0]
	for _, mode := range []string{"snapshot", "follow"} {
		timeline, e := svc.ImportDrawComposition(ctx, "human:window", pid, c.ID, service.DrawImportRequest{Session: view.ID, ContentVersion: c.ContentVersion, SequenceID: seq.ID, ExpectedRevision: seq.Revision, IdempotencyKey: mode, SourceMode: mode})
		if e != nil {
			t.Fatal(e)
		}
		seq = timeline.Sequence
	}
	view, e = svc.ProjectWorkspace(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if len(view.Document.Resources) != 2 || view.Document.Resources[0].Draw == nil || view.Document.Resources[0].Source.OriginalPath != "" {
		t.Fatal("drawing source required a rendered disk file")
	}
	before, e := svc.Timeline(ctx, pid, seq.ID)
	if e != nil {
		t.Fatal(e)
	}
	// Select all tracks so both dependency modes contribute to the content hash.
	_ = before
	d.Nodes[0].Text = "Changed"
	c.Document = &d
	view, e = svc.SaveDrawComposition(ctx, pid, service.DrawSaveRequest{Session: view.ID, ExpectedRevision: view.Revision, IdempotencyKey: "change", Composition: c})
	if e != nil {
		t.Fatal(e)
	}
	if view.Document.Resources[0].Draw.Snapshot.Document.Nodes[0].Text != "Original" {
		t.Fatal("source edit rewrote frozen history")
	}
	after, e := svc.Timeline(ctx, pid, seq.ID)
	if e != nil {
		t.Fatal(e)
	}
	if after.ContentVersion == before.ContentVersion {
		t.Fatal("following source did not invalidate dependent sequence")
	}
	undo := service.DrawHistoryRequest{Session: view.ID, ExpectedRevision: view.Revision, CompositionRevision: view.Document.DrawCompositions[0].Revision, IdempotencyKey: "undo-scene", Action: "undo"}
	view, e = svc.MoveDrawingHistory(ctx, pid, c.ID, undo)
	if e != nil || view.Document.DrawCompositions[0].Document.Nodes[0].Text != "Original" {
		t.Fatalf("scene undo: %v", e)
	}
	revision := view.Revision
	replayed, e := svc.MoveDrawingHistory(ctx, pid, c.ID, undo)
	if e != nil || replayed.Revision != revision {
		t.Fatalf("history replay changed scene: %v", e)
	}
	redo := undo
	redo.Action = "redo"
	redo.IdempotencyKey = "redo-scene"
	if _, e = svc.MoveDrawingHistory(ctx, pid, c.ID, redo); !errors.Is(e, model.ErrConflict) {
		t.Fatalf("stale history accepted: %v", e)
	}
	redo.ExpectedRevision = view.Revision
	redo.CompositionRevision = view.Document.DrawCompositions[0].Revision
	view, e = svc.MoveDrawingHistory(ctx, pid, c.ID, redo)
	if e != nil || view.Document.DrawCompositions[0].Document.Nodes[0].Text != "Changed" {
		t.Fatalf("scene redo: %v", e)
	}
	if _, e = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{}); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: filepath.Join(root, "source.avp"), Decision: "discard", StopTasks: true}); e != nil {
		t.Fatal(e)
	}
	loaded, e := svc.ProjectResources(pid)
	if e != nil || len(loaded) != 2 || loaded[0].Missing {
		t.Fatalf("portable editable source did not reopen: %#v %v", loaded, e)
	}
}

func TestDrawImageBindings(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	view, e := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "images.avp"), Name: "Image bindings"})
	if e != nil {
		t.Fatal(e)
	}
	pid := view.Document.Project.ID
	path := filepath.Join(root, "image.png")
	write := func(c color.RGBA) {
		t.Helper()
		img := image.NewRGBA(image.Rect(0, 0, 32, 24))
		for y := 0; y < 24; y++ {
			for x := 0; x < 32; x++ {
				img.Set(x, y, c)
			}
		}
		f, e := os.Create(path)
		if e != nil {
			t.Fatal(e)
		}
		e = png.Encode(f, img)
		ce := f.Close()
		if e != nil || ce != nil {
			t.Fatalf("fixture %v %v", e, ce)
		}
	}
	write(color.RGBA{0, 255, 0, 255})
	view, e = svc.ImportProjectResource(ctx, pid, path)
	if e != nil {
		t.Fatal(e)
	}
	resource := view.Document.Resources[0]
	c := project.DrawComposition{ID: "bound", Name: "Image", Engine: "remotion", Source: "export default ()=>null", Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}, DurationFrames: 30, Images: []project.DrawImageLayer{{ID: "hero", ResourceID: resource.ID, ResourceSHA: "untrusted-client-value", X: 10, Y: 20, Width: 32, Height: 24, DX: 100, DY: 0, FromFrame: 0, ToFrame: 29}}}
	req := service.DrawSaveRequest{Session: view.ID, ExpectedRevision: view.Revision, IdempotencyKey: "images", Composition: c}
	saved, e := svc.SaveDrawComposition(ctx, pid, req)
	if e != nil {
		t.Fatal(e)
	}
	c = saved.Document.DrawCompositions[0]
	if c.Images[0].ResourceSHA != resource.Source.SHA256 {
		t.Fatal("client forged fingerprint")
	}
	catalog, e := svc.DrawImageResources(pid)
	if e != nil || len(catalog) != 1 || catalog[0].ID != resource.ID {
		t.Fatalf("catalog %v %v", catalog, e)
	}
	req.ExpectedRevision = saved.Revision
	req.IdempotencyKey = "foreign"
	req.Composition = c
	req.Composition.Images = append([]project.DrawImageLayer{}, c.Images...)
	req.Composition.Images[0].ResourceID = "foreign-image"
	if _, e = svc.SaveDrawComposition(ctx, pid, req); !errors.Is(e, model.ErrNotFound) {
		t.Fatalf("foreign image accepted %v", e)
	}
	removed := true
	state, e := svc.UpdateProjectResource(ctx, pid, resource.ID, service.ResourcePatch{Removed: &removed})
	if e != nil {
		t.Fatal(e)
	}
	if !state.Document.Resources[0].Referenced || !state.Document.Resources[0].Removed {
		t.Fatal("removed drawing image lost its placeholder")
	}
	if _, _, e = svc.DrawRenderPath(pid, c.ID, c.ContentVersion); !errors.Is(e, model.ErrConflict) {
		t.Fatalf("removed dependency accepted %v", e)
	}
	removed = false
	if _, e = svc.UpdateProjectResource(ctx, pid, resource.ID, service.ResourcePatch{Removed: &removed}); e != nil {
		t.Fatal(e)
	}
	write(color.RGBA{255, 0, 0, 255})
	if _, _, e = svc.DrawRenderPath(pid, c.ID, c.ContentVersion); !errors.Is(e, model.ErrConflict) {
		t.Fatalf("changed file accepted %v", e)
	}
	state, e = svc.UpdateProjectResource(ctx, pid, resource.ID, service.ResourcePatch{Path: path})
	if e != nil {
		t.Fatal(e)
	}
	req = service.DrawSaveRequest{Session: state.ID, ExpectedRevision: state.Revision, IdempotencyKey: "relinked", Composition: c}
	state, e = svc.SaveDrawComposition(ctx, pid, req)
	if e != nil {
		t.Fatal(e)
	}
	if state.Document.DrawCompositions[0].ContentVersion == c.ContentVersion {
		t.Fatal("relinked image reused stale render fingerprint")
	}
	if _, e = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{}); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.ProjectFileCommand(ctx, "close", service.ProjectCommand{}); e != nil {
		t.Fatal(e)
	}
	reopened, e := svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: filepath.Join(root, "images.avp")})
	if e != nil {
		t.Fatal(e)
	}
	layer := reopened.Document.DrawCompositions[0].Images[0]
	if layer.ResourceID != resource.ID || layer.X != 10 || layer.Width != 32 || layer.DX != 100 {
		t.Fatal("AVP lost image parameters")
	}
}
