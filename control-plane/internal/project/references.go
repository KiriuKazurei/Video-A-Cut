package project

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"
)

// ReferenceDirectory deliberately returns duplicate names. An occurrence is
// resolved by identity, never by the first matching label in this directory.
func ReferenceDirectory(d *AvpDocument, sequenceID string) []ReferenceCandidate {
	out := []ReferenceCandidate{}
	for _, r := range d.Resources {
		if r.Removed || r.Missing {
			continue
		}
		out = append(out, ReferenceCandidate{Reference: Reference{Name: r.Name, Kind: r.Kind, TargetID: r.ID, ResourceID: r.ID}, Owner: r.Name})
		for _, s := range d.Sequences {
			if sequenceID != "" && s.ID != sequenceID {
				continue
			}
			for _, t := range s.Tracks {
				for _, c := range t.Clips {
					if c.ResourceID != r.ID {
						continue
					}
					// Each source marker occurrence belongs to a specific clip instance.
					for _, m := range r.Markers {
						if m.SourceTime == nil {
							continue
						}
						seconds := Seconds(*m.SourceTime)
						if seconds < Seconds(c.SourceIn) || seconds >= Seconds(c.SourceOut) {
							continue
						}
						frame := c.At + s.FPS.Frames((seconds-Seconds(c.SourceIn))/c.Speed)
						out = append(out, ReferenceCandidate{Reference: Reference{Name: m.Notes, Kind: "source_marker", TargetID: m.ID, ResourceID: r.ID, SequenceID: s.ID, ClipID: c.ID}, Owner: s.Name + " / " + t.Name + " / " + c.Name, Frame: &frame, SourceTime: m.SourceTime, FPS: s.FPS})
					}
				}
			}
		}
		// The source itself is also a valid anchor before it is used in a sequence.
		for _, m := range r.Markers {
			if m.SourceTime != nil {
				out = append(out, ReferenceCandidate{Reference: Reference{Name: m.Notes, Kind: "source_marker", TargetID: m.ID, ResourceID: r.ID}, Owner: r.Name, SourceTime: m.SourceTime})
			}
		}
	}
	for _, s := range d.Sequences {
		if sequenceID != "" && s.ID != sequenceID {
			continue
		}
		for _, m := range s.Markers {
			frame := m.Frame
			out = append(out, ReferenceCandidate{Reference: Reference{Name: m.Notes, Kind: "sequence_marker", TargetID: m.ID, SequenceID: s.ID}, Owner: s.Name, Frame: &frame, FPS: s.FPS})
		}
	}
	return enrichReferenceCandidates(d, out)
}

func SameReference(a, b Reference) bool {
	return a.Kind == b.Kind && a.TargetID == b.TargetID && a.ResourceID == b.ResourceID && a.SequenceID == b.SequenceID && a.ClipID == b.ClipID
}

func ResolveReference(d *AvpDocument, ref Reference) (ReferenceCandidate, error) {
	if ref.Unresolved {
		return ReferenceCandidate{}, fmt.Errorf("引用 %s 待关联", ref.Name)
	}
	for _, candidate := range ReferenceDirectory(d, ref.SequenceID) {
		if SameReference(ref, candidate.Reference) {
			return candidate, nil
		}
	}
	return ReferenceCandidate{}, fmt.Errorf("引用 %s 已失效：对象被删除、缺失或标记已在片段范围外", ref.Name)
}

type ReferenceToken struct {
	Name  string `json:"name"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

func ReferenceTokens(body string) []ReferenceToken {
	units := utf16.Encode([]rune(body))
	out := []ReferenceToken{}
	for i := 0; i+2 < len(units); i++ {
		if units[i] == '\\' {
			i++
			continue
		}
		if units[i] == '{' && units[i+1] != '@' {
			quoted, escaped := false, false
			j := i + 1
			for ; j < len(units); j++ {
				ch := units[j]
				if quoted {
					if escaped {
						escaped = false
					} else if ch == '\\' {
						escaped = true
					} else if ch == '"' {
						quoted = false
					}
				} else if ch == '"' {
					quoted = true
				} else if ch == '}' {
					break
				}
			}
			i = j
			continue
		}
		if units[i] != '{' || units[i+1] != '@' {
			continue
		}
		end := i + 2
		for end < len(units) && units[end] != '}' && units[end] != '{' && units[end] != '\n' && units[end] != '\r' {
			end++
		}
		if end < len(units) && units[end] == '}' && end > i+2 {
			out = append(out, ReferenceToken{string(utf16.Decode(units[i+2 : end])), i, end + 1})
			i = end
		}
	}
	return out
}

func enrichReferenceCandidates(d *AvpDocument, out []ReferenceCandidate) []ReferenceCandidate {
	for i := range out {
		c := &out[i]
		if c.Kind == "source_marker" {
			c.MediaID = c.ResourceID
			c.AnchorTime = c.SourceTime
		}
		if c.Kind == "sequence_marker" && c.Frame != nil {
			for _, s := range d.Sequences {
				if s.ID != c.SequenceID {
					continue
				}
				tracks := ActiveTracks(s)
				for j := len(tracks) - 1; j >= 0; j-- {
					t := tracks[j]
					if t.Kind != "video" {
						continue
					}
					for _, clip := range t.Clips {
						if clip.At <= *c.Frame && clip.At+clip.Duration > *c.Frame {
							c.MediaID = clip.ResourceID
							time := MediaTime(Seconds(clip.SourceIn) + s.FPS.Seconds(*c.Frame-clip.At)*clip.Speed)
							c.AnchorTime = &time
							break
						}
					}
					if c.MediaID != "" {
						break
					}
				}
			}
		}
	}
	return out
}

func ActiveTracks(s Sequence) []Track {
	tracks := []Track{}
	solo := map[string]bool{}
	for _, t := range s.Tracks {
		if s.ScopeGroup == "" || (s.ScopeGroup == "original" && t.GroupID == "") || s.ScopeGroup == t.GroupID {
			tracks = append(tracks, t)
			if t.Solo {
				solo[t.Kind] = true
			}
		}
	}
	return slices.DeleteFunc(tracks, func(t Track) bool { return t.Hidden || (t.Kind == "audio" && t.Muted) || (solo[t.Kind] && !t.Solo) })
}

func SemanticBody(body string, refs []Reference) string {
	units := utf16.Encode([]rune(body))
	ordered := append([]Reference{}, refs...)
	slices.SortFunc(ordered, func(a, b Reference) int { return b.Start - a.Start })
	for _, ref := range ordered {
		if ref.Start < 0 || ref.End > len(units) || ref.Start >= ref.End || ref.Unresolved {
			continue
		}
		identity, _ := json.Marshal([]string{ref.Kind, ref.TargetID, ref.ResourceID, ref.SequenceID, ref.ClipID})
		token := utf16.Encode([]rune("{@" + string(identity) + "}"))
		units = append(append(append([]uint16{}, units[:ref.Start]...), token...), units[ref.End:]...)
	}
	return string(utf16.Decode(units))
}

// BindReferences validates explicit selections and fills only unambiguous
// legacy names. Ambiguous handwritten references remain an actionable error.
func BindReferences(d *AvpDocument, body string, bindings []Reference, sequenceID string, requireResolved bool) ([]Reference, error) {
	tokens := ReferenceTokens(body)
	out := []Reference{}
	seen := map[string]bool{}
	for _, token := range tokens {
		matches := []Reference{}
		for _, b := range bindings {
			if b.Start == token.Start && b.End == token.End && b.Name == token.Name {
				matches = append(matches, b)
			}
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("同一文本引用包含重复绑定")
		}
		var ref Reference
		if len(matches) == 1 {
			ref = matches[0]
		} else {
			candidates := []ReferenceCandidate{}
			for _, c := range ReferenceDirectory(d, sequenceID) {
				if c.Name == token.Name {
					candidates = append(candidates, c)
				}
			}
			if len(candidates) == 1 {
				ref = candidates[0].Reference
			} else {
				ref = Reference{Name: token.Name, Unresolved: true}
			}
			ref.ID = TimelineID("ref")
			ref.Start, ref.End = token.Start, token.End
		}
		if ref.ID == "" {
			ref.ID = TimelineID("ref")
		}
		if seen[ref.ID] {
			return nil, fmt.Errorf("重复的引用 ID")
		}
		seen[ref.ID] = true
		_, err := ResolveReference(d, ref)
		if err != nil {
			ref.Unresolved = true
			if requireResolved {
				return nil, fmt.Errorf("%s：请选择具体的素材或标记", token.Name)
			}
		}
		out = append(out, ref)
	}
	return out, nil
}

// Rename keeps UTF-16 occurrence offsets in step with the actual editor text.
// Descending replacements avoid changing the meaning of an earlier offset.
func RenameMarkerReferences(d *AvpDocument, id, name string) {
	renameReferences(d, func(r Reference) bool {
		return r.TargetID == id && (r.Kind == "source_marker" || r.Kind == "sequence_marker")
	}, name)
}
func RenameResourceReferences(d *AvpDocument, id, name string) {
	renameReferences(d, func(r Reference) bool {
		return (r.TargetID == id || r.ResourceID == id) && (r.Kind == "video" || r.Kind == "audio")
	}, name)
}
func renameReferences(d *AvpDocument, match func(Reference) bool, name string) {
	for i := range d.Scripts {
		script := &d.Scripts[i]
		units := utf16.Encode([]rune(script.Body))
		order := []int{}
		for j, r := range script.References {
			if match(r) {
				order = append(order, j)
			}
		}
		slices.SortFunc(order, func(a, b int) int { return script.References[b].Start - script.References[a].Start })
		for _, j := range order {
			ref := &script.References[j]
			if ref.Start < 0 || ref.End > len(units) || ref.End <= ref.Start || string(utf16.Decode(units[ref.Start:ref.End])) != "{@"+ref.Name+"}" {
				ref.Unresolved = true
				continue
			}
			replacement := utf16.Encode([]rune("{@" + name + "}"))
			delta := len(replacement) - (ref.End - ref.Start)
			end := ref.End
			units = append(append(append([]uint16{}, units[:ref.Start]...), replacement...), units[ref.End:]...)
			ref.Name = name
			ref.End += delta
			for k := range script.References {
				if k != j && script.References[k].Start >= end {
					script.References[k].Start += delta
					script.References[k].End += delta
				}
			}
		}
		script.Body = string(utf16.Decode(units))
	}
}

func ValidateReferences(body string, refs []Reference) error {
	tokens := ReferenceTokens(body)
	seen := map[string]bool{}
	for _, r := range refs {
		if !validID(r.ID) || seen[r.ID] || !slices.ContainsFunc(tokens, func(t ReferenceToken) bool { return t.Start == r.Start && t.End == r.End && t.Name == r.Name }) {
			return fmt.Errorf("引用绑定与正文不一致")
		}
		seen[r.ID] = true
		if !r.Unresolved && (!validID(r.TargetID) || !strings.Contains("|video|audio|image|subtitle|document|script|source_marker|sequence_marker|", "|"+r.Kind+"|")) {
			return fmt.Errorf("引用类型或对象无效")
		}
	}
	return nil
}
