package service

import (
	"slices"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

// Bind current editable dependencies on save. Frozen source snapshots retain
// their own RAW recipes, so a later global image adjustment cannot rewrite them.
func bindDrawingAssets(doc *project.AvpDocument, c *project.DrawComposition) error {
	bindings := []project.DrawAssetBinding{}
	seen := map[string]bool{}
	for _, im := range c.ImageBindings() {
		i := slices.IndexFunc(doc.Resources, func(r project.Resource) bool { return r.ID == im.ResourceID && r.Kind == "image" })
		if i < 0 {
			return model.ErrNotFound
		}
		r := doc.Resources[i]
		sha := project.ResourceContentVersion(r)
		for j := range c.Images {
			if c.Images[j].ResourceID == r.ID {
				c.Images[j].ResourceSHA = sha
			}
		}
		if c.Document != nil {
			for j := range c.Document.Nodes {
				if c.Document.Nodes[j].Kind == "image" && c.Document.Nodes[j].ResourceID == r.ID {
					c.Document.Nodes[j].ResourceSHA = sha
				}
			}
			if !seen[r.ID] {
				b := project.DrawAssetBinding{ResourceID: r.ID, SHA: sha, OriginalSHA: r.Source.SHA256}
				if project.IsCameraRaw(r.Source.OriginalPath) {
					recipe := project.DefaultRawDevelop()
					if r.RawDevelop != nil {
						recipe = *r.RawDevelop
					}
					b.RawDevelop = &recipe
				}
				bindings = append(bindings, b)
				seen[r.ID] = true
			}
		}
	}
	if c.Document != nil {
		c.Document.AssetBindings = bindings
	}
	return nil
}

// Dependency refresh is not an authored source edit: keep the editor revision
// while changing the pixel version and the project resource transaction.
func refreshDrawingDependency(doc *project.AvpDocument, r project.Resource) {
	if r.Kind != "image" {
		return
	}
	for i := range doc.DrawCompositions {
		c := &doc.DrawCompositions[i]
		if !slices.ContainsFunc(c.ImageBindings(), func(im project.DrawImageLayer) bool { return im.ResourceID == r.ID }) {
			continue
		}
		if bindDrawingAssets(doc, c) == nil {
			c.ContentVersion = c.ContentHash()
		}
	}
}
