package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
)

// A frozen source remains editable in the AVP and can be decoded without a
// cached movie. Follow is explicit; completed/historical outputs stay frozen.
type DrawSourceReference struct {
	CompositionID string          `json:"composition_id"`
	Mode          string          `json:"mode"`
	Snapshot      DrawComposition `json:"snapshot"`
}

// DrawDocument is portable editing state, independent of compiled frame caches.
type DrawDocument struct {
	AssetBindings  []DrawAssetBinding `json:"asset_bindings,omitempty"`
	SchemaVersion  int                `json:"schema_version"`
	Canvas         DrawCanvas         `json:"canvas"`
	DurationFrames int64              `json:"duration_frames"`
	Preset         string             `json:"preset"`
	Sources        []DrawSource       `json:"sources"`
	Nodes          []DrawNode         `json:"nodes"`
	Variables      map[string]any     `json:"variables,omitempty"`
	Theme          map[string]any     `json:"theme,omitempty"`
	Seed           uint32             `json:"seed,omitempty"`
	Goal           string             `json:"goal,omitempty"`
	Explanation    string             `json:"explanation,omitempty"`
}
type DrawAssetBinding struct {
	ResourceID  string      `json:"resource_id"`
	SHA         string      `json:"sha256"`
	OriginalSHA string      `json:"original_sha256"`
	RawDevelop  *RawDevelop `json:"raw_develop,omitempty"`
}
type DrawCanvas struct {
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	FPS        Rate   `json:"fps"`
	Background string `json:"background,omitempty"`
	ColorSpace string `json:"color_space,omitempty"`
	Alpha      string `json:"alpha,omitempty"`
}
type DrawSource struct {
	Canvas         *DrawCanvas      `json:"canvas,omitempty"`
	DurationFrames int64            `json:"duration_frames,omitempty"`
	ImageBindings  []DrawImageLayer `json:"image_bindings,omitempty"`
	ID             string           `json:"id"`
	Name           string           `json:"name,omitempty"`
	Engine         string           `json:"engine"`
	Source         string           `json:"source"`
	Reason         string           `json:"reason,omitempty"`
	Capabilities   []string         `json:"capabilities,omitempty"`
}
type DrawTransform struct {
	X        float64  `json:"x"`
	Y        float64  `json:"y"`
	Width    float64  `json:"width,omitempty"`
	Height   float64  `json:"height,omitempty"`
	Rotation float64  `json:"rotation,omitempty"`
	ScaleX   *float64 `json:"scale_x,omitempty"`
	ScaleY   *float64 `json:"scale_y,omitempty"`
}
type DrawKeyframe struct {
	Frame   int64    `json:"frame"`
	X       float64  `json:"x"`
	Y       float64  `json:"y"`
	Opacity *float64 `json:"opacity,omitempty"`
}
type DrawCrop struct {
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
}
type DrawNode struct {
	Shape        string         `json:"shape,omitempty"`
	ID           string         `json:"id"`
	Name         string         `json:"name,omitempty"`
	Kind         string         `json:"kind"`
	SourceID     string         `json:"source_id,omitempty"`
	ParentID     string         `json:"parent_id,omitempty"`
	ResourceID   string         `json:"resource_id,omitempty"`
	ResourceSHA  string         `json:"resource_sha256,omitempty"`
	From         int64          `json:"from"`
	Duration     int64          `json:"duration"`
	SourceOffset int64          `json:"source_offset,omitempty"`
	Speed        *Rate          `json:"speed,omitempty"`
	Transform    DrawTransform  `json:"transform"`
	Opacity      *float64       `json:"opacity,omitempty"`
	Z            int            `json:"z,omitempty"`
	Hidden       bool           `json:"hidden,omitempty"`
	Fit          string         `json:"fit,omitempty"`
	Crop         *DrawCrop      `json:"crop,omitempty"`
	Keyframes    []DrawKeyframe `json:"keyframes,omitempty"`
	Text         string         `json:"text,omitempty"`
	Color        string         `json:"color,omitempty"`
	FontSize     float64        `json:"font_size,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty"`
}

// Unknown layer effects must fail explicitly instead of being dropped on save.
func (n *DrawNode) UnmarshalJSON(raw []byte) error {
	type plain DrawNode
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*plain)(n))
}

type DrawPlanNode struct {
	NodeID       string   `json:"node_id"`
	Engine       string   `json:"engine"`
	Reason       string   `json:"reason"`
	Dependencies []string `json:"dependencies"`
	Capabilities []string `json:"capabilities"`
}
type DrawRenderPlan struct {
	SchemaVersion    int               `json:"schema_version"`
	PolicyVersion    string            `json:"policy_version"`
	Nodes            []DrawPlanNode    `json:"nodes"`
	Engines          []string          `json:"engines"`
	Alpha            string            `json:"alpha"`
	ColorSpace       string            `json:"color_space"`
	RuntimeVersions  map[string]string `json:"runtime_versions"`
	FrameEvaluations int64             `json:"frame_evaluations"`
	FallbackPolicy   string            `json:"fallback_policy"`
}

func (d DrawDocument) Validate() error {
	if d.SchemaVersion != 1 || len(d.Nodes) > 128 || len(d.Sources) > 32 || d.DurationFrames < 1 || d.DurationFrames > 18000 || d.Canvas.Width < 16 || d.Canvas.Width > 3840 || d.Canvas.Height < 16 || d.Canvas.Height > 2160 || d.Canvas.Width%2 != 0 || d.Canvas.Height%2 != 0 || d.Canvas.FPS.Num < 1 || d.Canvas.FPS.Den < 1 || d.Canvas.FPS.Num > 1e9 || d.Canvas.FPS.Den > 1e9 || d.Canvas.FPS.Value() < 1 || d.Canvas.FPS.Value() > 120 || d.Canvas.FPS.Seconds(d.DurationFrames) > 300 {
		return fmt.Errorf("invalid drawing document bounds")
	}
	if d.Preset != "" && !slices.Contains([]string{"auto", "interface", "data", "media"}, d.Preset) {
		return fmt.Errorf("unknown drawing purpose preset")
	}
	if d.Canvas.Alpha != "" && d.Canvas.Alpha != "straight" || d.Canvas.ColorSpace != "" && d.Canvas.ColorSpace != "srgb" {
		return fmt.Errorf("unsupported alpha or color space")
	}
	sources := map[string]DrawSource{}
	for _, s := range d.Sources {
		if !validID(s.ID) || sources[s.ID].ID != "" || (s.Engine != "hyperframes" && s.Engine != "remotion") || len(s.Source) == 0 || len(s.Source) > 96<<10 {
			return fmt.Errorf("invalid drawing source %q", s.ID)
		}
		sources[s.ID] = s
		allowed := []string{"rgba", "transform", "image", "text", "mask", "dom", "gsap"}
		if s.Engine == "remotion" {
			allowed = []string{"rgba", "transform", "image", "text", "mask", "react", "svg"}
		}
		for _, capability := range s.Capabilities {
			if !slices.Contains(allowed, capability) {
				return fmt.Errorf("source %s: unsupported capability %s", s.ID, capability)
			}
		}
		if s.Canvas != nil {
			frames := s.DurationFrames
			if frames == 0 {
				frames = d.DurationFrames
			}
			if e := (DrawDocument{SchemaVersion: 1, Canvas: *s.Canvas, DurationFrames: frames}).Validate(); e != nil {
				return fmt.Errorf("source %s: %w", s.ID, e)
			}
		}
	}
	nodes := map[string]DrawNode{}
	for _, n := range d.Nodes {
		if !validID(n.ID) || nodes[n.ID].ID != "" || !slices.Contains([]string{"source", "image", "text", "group", "mask", "shape"}, n.Kind) || n.From < 0 || n.Duration < 1 || n.From+n.Duration > 18000 || n.ParentID == "" && n.From+n.Duration > d.DurationFrames || n.SourceOffset < 0 || n.SourceOffset > 18000 {
			return fmt.Errorf("invalid drawing node %q", n.ID)
		}
		if n.Shape != "" && n.Shape != "rect" && n.Shape != "ellipse" {
			return fmt.Errorf("unsupported drawing shape")
		}
		if n.Kind == "source" && sources[n.SourceID].ID == "" || n.Kind == "image" && !validID(n.ResourceID) {
			return fmt.Errorf("missing node source %q", n.ID)
		}
		if n.Speed != nil && (n.Speed.Num < 1 || n.Speed.Den < 1 || n.Speed.Num > 1e9 || n.Speed.Den > 1e9 || n.Speed.Value() > 100) {
			return fmt.Errorf("invalid node speed")
		}
		if n.Kind == "source" {
			s := sources[n.SourceID]
			fps := d.Canvas.FPS
			if s.Canvas != nil {
				fps = s.Canvas.FPS
			}
			duration := s.DurationFrames
			if duration == 0 {
				duration = d.DurationFrames
			}
			offset, e := drawingFrameOffset(n.Duration-1, n.Speed, fps, d.Canvas.FPS)
			if e != nil {
				return e
			}
			last := n.SourceOffset + offset
			if duration < 1 || last >= duration {
				return fmt.Errorf("node %s exceeds its source frame range", n.ID)
			}
		}
		values := []float64{n.Transform.X, n.Transform.Y, n.Transform.Width, n.Transform.Height, n.Transform.Rotation, n.FontSize}
		for _, v := range []*float64{n.Opacity, n.Transform.ScaleX, n.Transform.ScaleY} {
			if v != nil {
				values = append(values, *v)
			}
		}
		if n.Crop != nil {
			values = append(values, n.Crop.Top, n.Crop.Right, n.Crop.Bottom, n.Crop.Left)
		}
		for _, v := range values {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 100000 {
				return fmt.Errorf("invalid node transform")
			}
		}
		if n.Transform.Width < 0 || n.Transform.Height < 0 || n.Opacity != nil && (*n.Opacity < 0 || *n.Opacity > 1) {
			return fmt.Errorf("invalid node dimensions/opacity")
		}
		last := int64(-1)
		for _, k := range n.Keyframes {
			if k.Frame <= last || k.Frame < 0 || k.Frame >= n.Duration || math.IsNaN(k.X+k.Y) || math.IsInf(k.X+k.Y, 0) || k.Opacity != nil && (*k.Opacity < 0 || *k.Opacity > 1) {
				return fmt.Errorf("invalid ordered node keyframes")
			}
			last = k.Frame
		}
		if n.Fit != "" && !slices.Contains([]string{"contain", "cover", "fill"}, n.Fit) {
			return fmt.Errorf("invalid node image fit")
		}
		nodes[n.ID] = n
	}
	for _, n := range d.Nodes {
		seen := map[string]bool{n.ID: true}
		for parent := n.ParentID; parent != ""; parent = nodes[parent].ParentID {
			if seen[parent] {
				return fmt.Errorf("drawing graph contains a cycle")
			}
			seen[parent] = true
			if nodes[parent].Kind != "group" && nodes[parent].Kind != "mask" {
				return fmt.Errorf("missing drawing group")
			}
		}
	}
	raw, marshalErr := json.Marshal(d)
	if marshalErr != nil {
		return fmt.Errorf("invalid drawing values: %w", marshalErr)
	}
	if len(raw) > 4<<20 {
		return fmt.Errorf("drawing document too large")
	}
	return nil
}

// Plan validates Agent-authored choices; it never silently rewrites source into
// another engine. Common layers avoid starting an unnecessary second engine.
func (d DrawDocument) Plan() (DrawRenderPlan, error) {
	p := DrawRenderPlan{SchemaVersion: 1, PolicyVersion: "vac-unified-1", Nodes: []DrawPlanNode{}, Engines: []string{}, Alpha: "straight", ColorSpace: "srgb", RuntimeVersions: map[string]string{"hyperframes": "0.8.193", "remotion": "4.0.538", "adapter": "vac-unified-1"}, FrameEvaluations: d.DurationFrames * int64(len(d.Nodes)), FallbackPolicy: "explicit-error-no-unverified-fallback"}
	if e := d.Validate(); e != nil {
		return p, e
	}
	for _, n := range d.Nodes {
		engine, reason, deps := "common", "公共图层直接合成", []string{}
		capabilities := []string{"rgba", "transform", n.Kind}
		if n.ParentID != "" {
			deps = append(deps, n.ParentID)
		}
		if n.Kind == "source" {
			for _, s := range d.Sources {
				if s.ID == n.SourceID {
					engine = s.Engine
					capabilities = append([]string{"rgba", "transform"}, s.Capabilities...)
					reason = s.Reason
					if reason == "" {
						if engine == "hyperframes" {
							reason = "保留 HTML/CSS/GSAP 的原生时间线"
						} else {
							reason = "复用 React 组件及逐帧表达"
						}
					}
					deps = append(deps, s.ID)
				}
			}
		}
		if n.Kind == "image" {
			deps = append(deps, n.ResourceID)
		}
		if engine != "common" && !slices.Contains(p.Engines, engine) {
			p.Engines = append(p.Engines, engine)
		}
		p.Nodes = append(p.Nodes, DrawPlanNode{NodeID: n.ID, Engine: engine, Reason: reason, Dependencies: deps, Capabilities: capabilities})
	}
	slices.Sort(p.Engines)
	return p, nil
}

// Validate imported compatibility bindings before converting them to nodes.
// Resource hashes are server-owned and will be replaced during project binding.
func ValidateDrawImageBindings(images []DrawImageLayer) error {
	checked := append([]DrawImageLayer{}, images...)
	for i := range checked {
		checked[i].ResourceSHA = ""
	}
	probe := DrawComposition{ID: "binding_check", Name: "binding", Engine: "remotion", Source: "export default ()=>null", Width: 16, Height: 16, FPS: Rate{Num: 30, Den: 1}, DurationFrames: 1, Images: checked}
	return probe.Validate()
}

func (c *DrawComposition) NormalizeDocument() {
	if c.Document == nil {
		return
	}
	// Older callers place source images beside the document or on a source.
	// Convert those bindings to canonical image nodes before discarding metadata.
	images := append([]DrawImageLayer{}, c.Images...)
	for _, source := range c.Document.Sources {
		images = append(images, source.ImageBindings...)
	}
	for _, im := range images {
		found := slices.IndexFunc(c.Document.Nodes, func(n DrawNode) bool { return n.ID == im.ID })
		if found >= 0 {
			if n := c.Document.Nodes[found]; n.Kind == "image" && n.ResourceID == im.ResourceID {
				continue
			}
			// Retain a conflicting alias as a duplicate, so validation rejects it.
		}
		n := DrawNode{ID: im.ID, Kind: "image", ResourceID: im.ResourceID, ResourceSHA: im.ResourceSHA, Duration: c.Document.DurationFrames, Transform: DrawTransform{X: im.X, Y: im.Y, Width: im.Width, Height: im.Height}, Fit: im.Fit, Opacity: im.Opacity, Hidden: im.Mode == "source", Z: 100 + len(c.Document.Nodes)}
		if im.ToFrame > im.FromFrame && im.FromFrame < n.Duration-1 && (im.DX != 0 || im.DY != 0) {
			last := min(im.ToFrame, n.Duration-1)
			progress := float64(last-im.FromFrame) / float64(im.ToFrame-im.FromFrame)
			n.Keyframes = []DrawKeyframe{{Frame: im.FromFrame, X: im.X, Y: im.Y}, {Frame: last, X: im.X + im.DX*progress, Y: im.Y + im.DY*progress}}
		}
		c.Document.Nodes = append(c.Document.Nodes, n)
	}
	// Converted aliases are owned by the canonical nodes. Keeping the old
	// lists would resurrect an image after a user removed its node.
	for i := range c.Document.Sources {
		c.Document.Sources[i].ImageBindings = nil
	}
	if plan, e := c.Document.Plan(); e == nil {
		c.Plan = &plan
	}
	c.Engine = "unified"
	c.Source = ""
	c.Width = c.Document.Canvas.Width
	c.Height = c.Document.Canvas.Height
	c.FPS = c.Document.Canvas.FPS
	c.DurationFrames = c.Document.DurationFrames
	c.Images = nil
}

// The adapter does not mutate legacy source/hash or write the opened file.
func (c DrawComposition) UnifiedDocument() DrawDocument {
	if c.Document != nil {
		return *c.Document
	}
	d := DrawDocument{SchemaVersion: 1, Canvas: DrawCanvas{Width: c.Width, Height: c.Height, FPS: c.FPS}, DurationFrames: c.DurationFrames, Preset: "auto", Sources: []DrawSource{{ID: "legacy-source", Engine: c.Engine, Source: c.Source, ImageBindings: c.Images}}, Nodes: []DrawNode{{ID: "legacy-scene", Kind: "source", SourceID: "legacy-source", Duration: c.DurationFrames}}}
	for _, im := range c.Images {
		n := DrawNode{ID: im.ID, Kind: "image", ResourceID: im.ResourceID, ResourceSHA: im.ResourceSHA, Duration: c.DurationFrames, Transform: DrawTransform{X: im.X, Y: im.Y, Width: im.Width, Height: im.Height}, Opacity: im.Opacity, Fit: im.Fit, Z: 100 + len(d.Nodes)}
		n.Hidden = im.Mode == "source"
		if im.ToFrame > im.FromFrame {
			n.Keyframes = []DrawKeyframe{{Frame: im.FromFrame, X: im.X, Y: im.Y}, {Frame: im.ToFrame, X: im.X + im.DX, Y: im.Y + im.DY}}
		}
		d.Nodes = append(d.Nodes, n)
	}
	return d
}

func (c DrawComposition) ImageBindings() []DrawImageLayer {
	if c.Document == nil {
		return c.Images
	}
	out := []DrawImageLayer{}
	for _, n := range c.Document.Nodes {
		if n.Kind == "image" {
			out = append(out, DrawImageLayer{ID: n.ID, ResourceID: n.ResourceID, ResourceSHA: n.ResourceSHA})
		}
	}
	return out
}
