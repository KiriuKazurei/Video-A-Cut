package service_test

import (
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
