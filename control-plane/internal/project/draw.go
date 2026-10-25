package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// DrawComposition stores editable source, never a command or a provider credential.
type DrawImageLayer struct {
	ID          string   `json:"id"`
	ResourceID  string   `json:"resource_id"`
	ResourceSHA string   `json:"resource_sha256,omitempty"`
	X           float64  `json:"x"`
	Y           float64  `json:"y"`
	Width       float64  `json:"width"`
	Height      float64  `json:"height"`
	DX          float64  `json:"dx"`
	DY          float64  `json:"dy"`
	FromFrame   int64    `json:"from_frame"`
	ToFrame     int64    `json:"to_frame"`
	Fit         string   `json:"fit,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	Opacity     *float64 `json:"opacity,omitempty"`
}
type DrawComposition struct {
	Images         []DrawImageLayer `json:"images,omitempty"`
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Engine         string           `json:"engine"`
	Source         string           `json:"source"`
	Width          int              `json:"width"`
	Height         int              `json:"height"`
	FPS            Rate             `json:"fps"`
	DurationFrames int64            `json:"duration_frames"`
	Revision       uint64           `json:"revision"`
	ContentVersion string           `json:"content_version"`
}

func (c DrawComposition) Validate() error {
	if !validID(c.ID) || strings.TrimSpace(c.Name) == "" || len(c.Name) > 256 || len(c.Source) == 0 || len(c.Source) > 96<<10 ||
		(c.Engine != "hyperframes" && c.Engine != "remotion") || c.Width < 16 || c.Width > 3840 || c.Height < 16 || c.Height > 2160 ||
		c.Width%2 != 0 || c.Height%2 != 0 || c.FPS.Num < 1 || c.FPS.Den < 1 || (c.FPS.Value() < 1 || c.FPS.Value() > 120) ||
		c.DurationFrames < 1 || c.DurationFrames > 18000 || c.FPS.Seconds(c.DurationFrames) > 300 {
		return fmt.Errorf("invalid draw composition: use HTML or React source, even dimensions, 1-120 fps, and at most 300 seconds")
	}
	if len(c.Images) > 64 {
		return fmt.Errorf("too many draw image layers")
	}
	seen := map[string]bool{}
	resources := map[string]bool{}
	for _, image := range c.Images {
		if !validID(image.ID) || len(image.ID) > 128 || seen[image.ID] || !validID(image.ResourceID) {
			return fmt.Errorf("invalid or duplicate draw image identity")
		}
		seen[image.ID] = true
		resources[image.ResourceID] = true
		if len(resources) > 32 {
			return fmt.Errorf("too many original draw images")
		}
		for _, value := range []float64{image.X, image.Y, image.DX, image.DY, image.Width, image.Height} {
			if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 100000 {
				return fmt.Errorf("invalid image transform")
			}
		}
		if image.Width <= 0 || image.Height <= 0 || image.Width > 32768 || image.Height > 32768 || image.FromFrame < 0 || image.ToFrame < image.FromFrame || image.ToFrame > 18000 {
			return fmt.Errorf("invalid image size or animation frames")
		}
		if image.Fit != "" && image.Fit != "contain" && image.Fit != "cover" && image.Fit != "fill" {
			return fmt.Errorf("invalid image fit")
		}
		if image.Mode != "" && image.Mode != "layer" && image.Mode != "source" {
			return fmt.Errorf("invalid image mode")
		}
		if image.Opacity != nil && (math.IsNaN(*image.Opacity) || math.IsInf(*image.Opacity, 0) || *image.Opacity < 0 || *image.Opacity > 1) {
			return fmt.Errorf("invalid image opacity")
		}
		if image.ResourceSHA != "" {
			raw, e := hex.DecodeString(image.ResourceSHA)
			if e != nil || len(raw) != 32 {
				return fmt.Errorf("invalid image resource fingerprint")
			}
		}
	}
	return nil
}

// ContentHash excludes labels and revision bookkeeping, so renaming does not
// invalidate pixels while externally edited AVP source cannot reuse stale output.
func (c DrawComposition) ContentHash() string {
	c.Name = ""
	c.Revision = 0
	c.ContentVersion = ""
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
