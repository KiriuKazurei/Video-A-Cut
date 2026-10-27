package project

import (
	"encoding/json"
	"testing"
)

func TestUnifiedImageBindingsAreNotSilentlyDiscarded(t *testing.T) {
	for _, placement := range []string{"composition", "source"} {
		t.Run(placement, func(t *testing.T) {
			d := DrawDocument{SchemaVersion: 1, Canvas: DrawCanvas{Width: 320, Height: 180, FPS: Rate{30, 1}}, DurationFrames: 30, Sources: []DrawSource{{ID: "fish", Engine: "remotion", Source: "export default ()=>null"}}, Nodes: []DrawNode{{ID: "scene", Kind: "source", SourceID: "fish", Duration: 30}}}
			im := DrawImageLayer{ID: "water", ResourceID: "photo", Width: 320, Height: 180, Mode: "source"}
			c := DrawComposition{ID: "draw", Name: "Fish", Document: &d}
			if placement == "composition" {
				c.Images = []DrawImageLayer{im}
			} else {
				c.Document.Sources[0].ImageBindings = []DrawImageLayer{im}
			}
			c.NormalizeDocument()
			if len(c.ImageBindings()) != 1 {
				t.Fatal("image binding was silently lost")
			}
			node := c.Document.Nodes[1]
			if node.ID != "water" || node.ResourceID != "photo" || !node.Hidden {
				t.Fatalf("wrong source image: %+v", node)
			}
			if e := c.Validate(); e != nil {
				t.Fatal(e)
			}
			c.NormalizeDocument()
			if len(c.Document.Nodes) != 2 {
				t.Fatal("normalization duplicated image")
			}
			c.Document.Nodes = c.Document.Nodes[:1]
			c.NormalizeDocument()
			if len(c.Document.Nodes) != 1 {
				t.Fatal("removed image reappeared")
			}
		})
	}
}

func TestDrawingNestedFrameContract(t *testing.T) {
	d := DrawDocument{Canvas: DrawCanvas{FPS: Rate{30, 1}}, Sources: []DrawSource{{ID: "s", Canvas: &DrawCanvas{FPS: Rate{24, 1}}}}, Nodes: []DrawNode{{ID: "g", Kind: "group", From: 2, Duration: 6, SourceOffset: 1, Speed: &Rate{2, 1}}, {ID: "m", Kind: "mask", ParentID: "g", From: 3, Duration: 6, Speed: &Rate{1, 2}}, {ID: "v", Kind: "source", ParentID: "m", SourceID: "s", Duration: 6, SourceOffset: 2, Speed: &Rate{3, 2}}}}
	frame, e := d.InstanceFrame("v", 4)
	if e != nil || !frame.Active || frame.ParentFrame != 1 || frame.Frame != 3 || frame.FPS != (Rate{24, 1}) {
		t.Fatalf("nested clock: %#v %v", frame, e)
	}
	for _, at := range []int64{1, 7} {
		frame, e = d.InstanceFrame("v", at)
		if e != nil || frame.Active {
			t.Fatalf("half-open range: %#v %v", frame, e)
		}
	}
}

func TestUnifiedDrawingDocumentGraphAndPortablePlan(t *testing.T) {
	d := DrawDocument{SchemaVersion: 1, Canvas: DrawCanvas{Width: 320, Height: 180, FPS: Rate{Num: 30, Den: 1}}, DurationFrames: 30, Preset: "auto",
		Sources: []DrawSource{{ID: "html", Engine: "hyperframes", Source: "<!doctype html>"}, {ID: "react", Engine: "remotion", Source: "export default ()=>null"}},
		Nodes:   []DrawNode{{ID: "group", Kind: "group", Duration: 30}, {ID: "web", Kind: "source", SourceID: "html", ParentID: "group", Duration: 30}, {ID: "card", Kind: "source", SourceID: "react", Duration: 30}},
	}
	c := DrawComposition{ID: "unified", Name: "组合", Document: &d}
	c.NormalizeDocument()
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	plan, e := d.Plan()
	if e != nil || len(plan.Nodes) != 3 || plan.Nodes[1].Engine != "hyperframes" || plan.Nodes[2].Engine != "remotion" {
		t.Fatalf("plan: %#v, %v", plan, e)
	}
	if plan.RuntimeVersions["hyperframes"] != "0.8.193" || plan.FrameEvaluations != 90 || plan.FallbackPolicy != "explicit-error-no-unverified-fallback" {
		t.Fatal("plan lost fixed runtime/budget/failure policy")
	}
	d.Sources[0].Capabilities = []string{"cross-document-backdrop-filter"}
	if d.Validate() == nil {
		t.Fatal("unsupported source capability silently accepted")
	}
	d.Sources[0].Capabilities = nil
	c.Revision = 1
	c.ContentVersion = c.ContentHash()
	avp := AvpDocument{Format: "avp", SchemaVersion: 3, Project: Identity{ID: "p", Name: "test"}, DrawCompositions: []DrawComposition{c}}
	path := t.TempDir() + "/unified.avp"
	if e := WriteAtomic(path, avp); e != nil {
		t.Fatal(e)
	}
	var loaded AvpDocument
	if e := Read(path, "avp", &loaded); e != nil {
		t.Fatal(e)
	}
	if e := loaded.Validate(); e != nil {
		t.Fatal(e)
	}
	if loaded.DrawCompositions[0].Document.Sources[1].Source != d.Sources[1].Source {
		t.Fatal("editable sources lost")
	}
	renamed := c
	renamed.Name = "new"
	if renamed.ContentHash() != c.ContentVersion {
		t.Fatal("rename invalidated pixels")
	}
	d.Nodes[0].ParentID = "web"
	if d.Validate() == nil {
		t.Fatal("cycle accepted")
	}
	d.Nodes[0].ParentID = ""
	d.Nodes[1].SourceID = "missing"
	if d.Validate() == nil {
		t.Fatal("missing source accepted")
	}
	legacy := DrawComposition{ID: "old", Name: "Old", Engine: "remotion", Source: "export default ()=>null", Width: 320, Height: 180, FPS: Rate{30, 1}, DurationFrames: 30}
	legacyDoc := legacy.UnifiedDocument()
	if len(legacyDoc.Nodes) != 1 || legacyDoc.Sources[0].Source != legacy.Source || legacy.Document != nil {
		t.Fatal("legacy mapping mutated original")
	}
}

func TestDrawCompositionSpecAndPortableSource(t *testing.T) {
	c := DrawComposition{ID: "draw-1", Name: "Title", Engine: "remotion", Source: "export default ()=>null", Width: 1920, Height: 1080, FPS: Rate{Num: 30, Den: 1}, DurationFrames: 90, Revision: 2, ContentVersion: ""}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*DrawComposition){func(v *DrawComposition) { v.Width = 1919 }, func(v *DrawComposition) { v.Source = "" }, func(v *DrawComposition) { v.Engine = "shell" }, func(v *DrawComposition) { v.FPS.Den = 0 }, func(v *DrawComposition) { v.DurationFrames = 18001 }} {
		v := c
		change(&v)
		if v.Validate() == nil {
			t.Fatalf("invalid specification accepted: %#v", v)
		}
	}
	d := AvpDocument{Format: "avp", SchemaVersion: 2, Project: Identity{ID: "project-draw", Name: "Draw"}}
	c.ContentVersion = c.ContentHash()
	d.DrawCompositions = []DrawComposition{c}
	raw, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	var copy AvpDocument
	if e = json.Unmarshal(raw, &copy); e != nil {
		t.Fatal(e)
	}
	if e = copy.Validate(); e != nil {
		t.Fatal(e)
	}
	if len(copy.DrawCompositions) != 1 || copy.DrawCompositions[0].Source != c.Source || copy.DrawCompositions[0].Revision != 2 {
		t.Fatal("draw source lost on AVP round trip")
	}
	copy.DrawCompositions[0].Source += " // changed outside VAC"
	if copy.Validate() == nil {
		t.Fatal("changed source reused old content version")
	}
	copy.DrawCompositions[0] = c
	copy.DrawCompositions = append(copy.DrawCompositions, c)
	if copy.Validate() == nil {
		t.Fatal("duplicate draw ids accepted")
	}
}

func TestDrawImageLayerValidation(t *testing.T) {
	c := DrawComposition{ID: "drawing", Name: "Images", Engine: "remotion", Source: "export default ()=>null", Width: 320, Height: 180, FPS: Rate{Num: 30, Den: 1}, DurationFrames: 30, Images: []DrawImageLayer{{ID: "first", ResourceID: "image", ResourceSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", X: 10, Y: 20, Width: 80, Height: 40, DX: 100, DY: 0, FromFrame: 0, ToFrame: 29}}}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	c.Revision = 1
	c.ContentVersion = c.ContentHash()
	doc := AvpDocument{Format: "avp", SchemaVersion: 2, Project: Identity{ID: "p", Name: "Images"}, DrawCompositions: []DrawComposition{c}, Resources: []Resource{{ID: "image", Kind: "image", Name: "Picture"}}}
	if e := doc.Validate(); e != nil {
		t.Fatal(e)
	}
	doc.Resources = nil
	if doc.Validate() == nil {
		t.Fatal("foreign image reference accepted")
	}
	bad := c
	bad.Images = append([]DrawImageLayer{}, c.Images...)
	bad.Images[0].Width = -1
	if bad.Validate() == nil {
		t.Fatal("negative image size accepted")
	}
	bad.Images[0] = c.Images[0]
	bad.Images = append(bad.Images, c.Images[0])
	if bad.Validate() == nil {
		t.Fatal("duplicate image instance accepted")
	}
	moved := c
	moved.Images = append([]DrawImageLayer{}, c.Images...)
	moved.Images[0].DX++
	if moved.ContentHash() == c.ContentVersion {
		t.Fatal("image motion did not invalidate render")
	}
}

func TestCameraRawRecipesAreValidatedAndAffectOnlyContentDependencies(t *testing.T) {
	recipe := DefaultRawDevelop()
	if err := recipe.Validate(); err != nil {
		t.Fatal(err)
	}
	if !IsCameraRaw("Sony.ARW") || !IsCameraRaw("Photo.DNG") || IsCameraRaw("Other.NEF") {
		t.Fatal("RAW scope must match DNG/ARW")
	}
	r := Resource{Source: Source{OriginalPath: "Sony.ARW", SHA256: "original"}, RawDevelop: &recipe}
	first := ResourceContentVersion(r)
	r.Name = "Rename"
	r.Notes = "Notes"
	if first != ResourceContentVersion(r) {
		t.Fatal("cosmetic edit invalidated content")
	}
	recipe.Exposure = 1
	if first == ResourceContentVersion(r) {
		t.Fatal("recipe failed to invalidate image content")
	}
	recipe.Exposure = 4
	if recipe.Validate() == nil {
		t.Fatal("invalid exposure accepted")
	}
	r.Source.OriginalPath = "original.png"
	if ResourceContentVersion(r) != "original" {
		t.Fatal("ordinary image compatibility changed")
	}
}
