package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
)

// CodeClip belongs to an occurrence, not a shared drawing resource. Source
// replacement creates new immutable resources; undo retains the old bindings.
type CodeClip struct {
	Description          string `json:"description"`
	GeneratedDescription string `json:"generated_description"`
	Revision             uint64 `json:"revision"`
	DescriptionState     string `json:"description_state,omitempty"`
	DescriptionError     string `json:"description_error,omitempty"`
	RunID                string `json:"run_id,omitempty"`
	ContentVersion       string `json:"content_version,omitempty"`
}

type CodeSegment struct {
	Composition DrawComposition `json:"composition"`
	Description string          `json:"description"`
}

// A generated job captures occurrence IDs. A new occurrence keeps its editable
// content, but must never display or wait for a job targeting the original.
func newClipIdentity(c *Clip) {
	c.ID = TimelineID("clip")
	if c.Code == nil {
		return
	}
	c.Code = clone(c.Code)
	c.Code.RunID, c.Code.DescriptionError, c.Code.ContentVersion = "", "", ""
	c.Code.DescriptionState = "ready"
	if c.Code.Description == "" {
		c.Code.DescriptionState = "pending"
	}
}

func CodeClipVersion(c Clip) string {
	c = clone(c)
	c.At = 0
	if c.Code != nil {
		c.Code.ContentVersion = ""
		c.Code.RunID = ""
		c.Code.DescriptionState = ""
		c.Code.DescriptionError = ""
	}
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func CodeTargetVersion(d *AvpDocument, c Clip) string {
	source := ""
	for _, r := range d.Resources {
		if r.ID != c.ResourceID {
			continue
		}
		source = ResourceContentVersion(r)
		if r.Draw != nil && r.Draw.Mode == "follow" {
			for _, composition := range d.DrawCompositions {
				if composition.ID == r.Draw.CompositionID {
					source = composition.ContentVersion
				}
			}
		}
	}
	raw, _ := json.Marshal([]string{CodeClipVersion(c), source})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Sequence) replaceCode(d *AvpDocument, op Operation, origin Origin) error {
	if len(op.ClipIDs) != 1 || len(op.CodeSegments) == 0 || len(op.CodeSegments) > 32 {
		return fmt.Errorf("代码替换需要一个目标和1到32段画面")
	}
	ti, ci := -1, -1
	for i, t := range s.Tracks {
		for j, c := range t.Clips {
			if c.ID == op.ClipIDs[0] {
				ti, ci = i, j
			}
		}
	}
	if ti < 0 {
		return fmt.Errorf("代码条已删除")
	}
	t := s.Tracks[ti]
	old := t.Clips[ci]
	if t.Kind != "code" || t.Locked || op.CodeVersion == "" || op.CodeVersion != CodeTargetVersion(d, old) {
		return fmt.Errorf("目标代码条已修改或锁定")
	}
	clips := []Clip{}
	resources := []Resource{}
	at := old.At
	for _, segment := range op.CodeSegments {
		c := clone(segment.Composition)
		c.NormalizeDocument()
		if c.Document != nil {
			for i := range c.Document.AudioBindings {
				a := &c.Document.AudioBindings[i]
				r, err := resource(d, a.ResourceID)
				if err != nil {
					return err
				}
				if r.Missing || r.Removed || r.Changed || (a.ResourceSHA != "" && a.ResourceSHA != r.Source.SHA256) {
					return fmt.Errorf("代码声音资源已改变")
				}
				a.ResourceSHA = r.Source.SHA256
			}
		}
		if err := c.Validate(); err != nil {
			return err
		}
		c.Revision = 1
		c.ContentVersion = c.ContentHash()
		if segment.Description == "" {
			return fmt.Errorf("生成画面必须附带描述")
		}
		r := Resource{ID: TimelineID("res"), Name: c.Name, Kind: "video", Source: Source{SHA256: c.ContentVersion}, Out: c.FPS.Seconds(c.DurationFrames), Metadata: map[string]any{"duration": c.FPS.Seconds(c.DurationFrames), "streams": []map[string]any{{"index": 0, "codec_type": "video"}}}, Draw: &DrawSourceReference{CompositionID: c.ID, Mode: "snapshot", Snapshot: c}}
		resources = append(resources, r)
		clip := Clip{ID: TimelineID("clip"), ResourceID: r.ID, Name: c.Name, At: at, Duration: s.FPS.Frames(r.Out), SourceIn: MediaTime(0), SourceOut: MediaTime(r.Out), Speed: 1, LinkID: old.LinkID, Origin: origin, Effects: Effects{Scale: 1}, Code: &CodeClip{Description: segment.Description, GeneratedDescription: segment.Description, Revision: 1, DescriptionState: "ready"}}
		if len(clips) == 0 {
			clip.ID = old.ID
		} else {
			clip.LinkID = ""
		}
		if clip.Duration < 1 {
			return fmt.Errorf("生成画面时长无效")
		}
		at += clip.Duration
		clips = append(clips, clip)
	}
	for i := range clips {
		part := sliceClip(old, clips[i].At, clips[i].At+clips[i].Duration, s.FPS)
		clips[i].Effects = part.Effects
	}
	end := old.At + old.Duration
	delta := at - end
	// Apply on a private copy: ripple failure never leaves a partially replaced target.
	next := clone(*s)
	next.Tracks[ti].Clips = slices.Delete(next.Tracks[ti].Clips, ci, ci+1)
	if delta > 0 {
		if err := insertGap(&next, end, delta, t.GroupID); err != nil {
			return err
		}
	}
	if delta < 0 {
		for _, track := range next.Tracks {
			if track.GroupID != t.GroupID {
				continue
			}
			for _, c := range track.Clips {
				if c.At < end && c.At+c.Duration > at {
					return fmt.Errorf("缩短范围包含未选择内容，无法波纹")
				}
			}
		}
		if err := ripple(&next, end, delta, t.GroupID); err != nil {
			return err
		}
	}
	next.Tracks[ti].Clips = append(next.Tracks[ti].Clips, clips...)
	for _, other := range next.Tracks[ti].Clips {
		for _, c := range clips {
			if other.ID != c.ID && other.At < c.At+c.Duration && other.At+other.Duration > c.At {
				return fmt.Errorf("替换范围与其他代码条重叠")
			}
		}
	}
	*s = next
	d.Resources = append(d.Resources, resources...)
	return nil
}

func ResourceTrackKind(r Resource) string {
	if r.Draw != nil {
		return "code"
	}
	if r.Kind == "image" {
		return "video"
	}
	return r.Kind
}

func migrateCodeTracks(state *SequenceState, resources []Resource) bool {
	changed := false
	tracks := []Track{}
	for _, original := range state.Tracks {
		if original.Kind == "code" {
			for j := range original.Clips {
				if original.Clips[j].Code == nil {
					original.Clips[j].Code = &CodeClip{Revision: 1, DescriptionState: "pending"}
					changed = true
				}
			}
		}
		if original.Kind != "video" {
			tracks = append(tracks, original)
			continue
		}
		var runs []Track
		for _, clip := range original.Clips {
			kind := "video"
			if i := slices.IndexFunc(resources, func(r Resource) bool { return r.ID == clip.ResourceID }); i >= 0 && resources[i].Draw != nil {
				kind = "code"
			}
			if len(runs) == 0 || runs[len(runs)-1].Kind != kind {
				t := original
				t.Clips = []Clip{}
				t.Kind = kind
				if len(runs) > 0 {
					t.ID = fmt.Sprintf("%s_migrated_%d", original.ID, len(runs))
				}
				if kind == "code" {
					t.Name = original.Name + " · 代码"
					changed = true
				}
				runs = append(runs, t)
			}
			if kind == "code" && clip.Code == nil {
				clip.Code = &CodeClip{Revision: 1, DescriptionState: "pending"}
			}
			runs[len(runs)-1].Clips = append(runs[len(runs)-1].Clips, clip)
		}
		if len(runs) > 1 {
			for i := range runs {
				runs[i].VisualGroup = original.ID
			}
		}
		if len(runs) == 0 {
			tracks = append(tracks, original)
		} else {
			tracks = append(tracks, runs...)
		}
	}
	state.Tracks = tracks
	return changed
}

func (s *Sequence) codeScript(op Operation) error {
	if len(op.ClipIDs) != 1 || op.Description == nil || len(*op.Description) > 64<<10 {
		return fmt.Errorf("代码描述需要一个目标片段")
	}
	for i := range s.Tracks {
		t := &s.Tracks[i]
		for j := range t.Clips {
			c := &t.Clips[j]
			if c.ID != op.ClipIDs[0] {
				continue
			}
			if t.Kind != "code" || t.Locked {
				return fmt.Errorf("代码条不存在或已锁定")
			}
			if c.Code == nil {
				c.Code = &CodeClip{}
			}
			if op.CodeRevision != c.Code.Revision {
				return fmt.Errorf("代码描述已更新，请刷新后重试")
			}
			fx, body, err := ParseCodeDescription(*op.Description, *c, s.FPS)
			if err != nil {
				return err
			}
			if op.Mode == "describe" && c.Code.Description != "" {
				return fmt.Errorf("用户已填写描述")
			}
			if op.Mode != "describe" {
				c.Effects = fx
			}
			c.Code.Description = body
			c.Code.Revision++
			c.Code.DescriptionState = "ready"
			if op.Mode == "describe" {
				c.Code.GeneratedDescription = body
			}
			return nil
		}
	}
	return fmt.Errorf("代码条已删除")
}
