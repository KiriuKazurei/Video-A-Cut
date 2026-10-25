package project

import (
	"encoding/json"
	"testing"
)

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
