package autoreview

import (
	"math"
	"path"
	"strings"
)

// EDLFacts are the parts of Input that come from an EDL.
type EDLFacts struct {
	Scenes             []Scene
	Lines              []Line
	EDLDurationSeconds float64
}

func number(v any) (float64, bool) {
	f, ok := v.(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func objects(v any) []map[string]any {
	items, _ := v.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// FromEDL reads scenes, narration lines (joined with their voice and subtitle
// cue) and the timeline length from a parsed EDL.
//
// A narration line "nar_X" is joined with the voice "vo_X" and the subtitle
// "sub_X", the naming the pipeline writes. A voice counts as audio only when
// its file is present in the package: exists is called with the voice's
// package-relative path and decides that (a test passes a stub; the service
// checks the file system). A path that is absolute or leaves the package never
// counts. Whatever does not parse is left out, which can only lower the score.
func FromEDL(edl map[string]any, exists func(rel string) bool) EDLFacts {
	var f EDLFacts
	for _, sc := range objects(edl["scenes"]) {
		s := Scene{}
		s.Label, _ = sc["label"].(string)
		s.Method, _ = sc["method"].(string)
		if c, ok := number(sc["confidence"]); ok {
			s.Confidence = &c
		}
		f.Scenes = append(f.Scenes, s)
	}
	voices, subs := map[string]map[string]any{}, map[string]map[string]any{}
	for _, v := range objects(edl["voice"]) {
		if id, _ := v["id"].(string); id != "" {
			voices[id] = v
		}
	}
	for _, v := range objects(edl["subtitle"]) {
		if id, _ := v["id"].(string); id != "" {
			subs[id] = v
		}
	}
	interval := func(m map[string]any) *Interval {
		s, ok1 := number(m["start"])
		e, ok2 := number(m["end"])
		if !ok1 || !ok2 {
			return nil
		}
		return &Interval{Start: s, End: e}
	}
	for _, n := range objects(edl["narration"]) {
		id, _ := n["id"].(string)
		l := Line{ID: id}
		l.Text, _ = n["text"].(string)
		l.Start, _ = number(n["start"])
		l.End, _ = number(n["end"])
		suffix := strings.TrimPrefix(id, "nar_")
		if v, ok := voices["vo_"+suffix]; ok && id != "" {
			src, _ := v["src"].(string)
			if src != "" && !path.IsAbs(src) && !strings.Contains(src, "\\") && !strings.Contains(src, "..") &&
				exists != nil && exists(src) {
				l.Audio = interval(v)
			}
		}
		if s, ok := subs["sub_"+suffix]; ok && id != "" {
			l.Subtitle = interval(s)
		}
		f.Lines = append(f.Lines, l)
	}
	for _, v := range objects(edl["video"]) {
		in, ok1 := number(v["in"])
		out, ok2 := number(v["out"])
		at, ok3 := number(v["timeline_in"])
		// A sum that overflows is not a duration; the clip is left out, which
		// can only make the timeline look shorter and the score lower.
		if end := at + (out - in); ok1 && ok2 && ok3 && out > in && !math.IsInf(end, 0) && !math.IsNaN(end) {
			f.EDLDurationSeconds = math.Max(f.EDLDurationSeconds, end)
		}
	}
	return f
}
