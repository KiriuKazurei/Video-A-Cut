package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
)

type subtitleCue struct {
	Start, End float64
	Text       string
}

var subtitleTimes = regexp.MustCompile(`^\s*((?:\d+:)?\d{1,2}:\d{2}[,.]\d{1,3})\s+-->\s+((?:\d+:)?\d{1,2}:\d{2}[,.]\d{1,3})`)
var assOverride = regexp.MustCompile(`\{[^}]*\}`)

func subtitleTime(value string) (float64, error) {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(value), ",", "."), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, model.ErrArgument
	}
	var result float64
	for i, p := range parts {
		v, e := strconv.ParseFloat(p, 64)
		if e != nil || v < 0 || !finiteScriptTime(v) || (i > 0 && v >= 60) {
			return 0, model.ErrArgument
		}
		result = result*60 + v
	}
	return result, nil
}
func parseSubtitleCues(text, extension string) ([]subtitleCue, error) {
	text = strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\ufeff")
	out := []subtitleCue{}
	add := func(a, b, body string) error {
		start, e := subtitleTime(a)
		if e != nil {
			return e
		}
		end, e := subtitleTime(b)
		if e != nil || end <= start {
			return model.ErrArgument
		}
		body = strings.TrimSpace(body)
		if body == "" {
			return nil
		}
		out = append(out, subtitleCue{start, end, body})
		if len(out) > 5000 {
			return fmt.Errorf("字幕最多 5000 条: %w", model.ErrArgument)
		}
		return nil
	}
	if strings.EqualFold(extension, ".ass") {
		fields := []string{"Layer", "Start", "End", "Style", "Name", "MarginL", "MarginR", "MarginV", "Effect", "Text"}
		events := false
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") {
				events = strings.EqualFold(line, "[Events]")
				continue
			}
			if !events {
				continue
			}
			if strings.HasPrefix(line, "Format:") {
				fields = strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "Format:")), ",")
				continue
			}
			if !strings.HasPrefix(line, "Dialogue:") {
				continue
			}
			parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(line, "Dialogue:")), ",", len(fields))
			if len(parts) != len(fields) {
				return nil, model.ErrArgument
			}
			values := map[string]string{}
			for i, k := range fields {
				values[strings.ToLower(strings.TrimSpace(k))] = parts[i]
			}
			body := strings.ReplaceAll(assOverride.ReplaceAllString(values["text"], ""), `\N`, "\n")
			body = strings.ReplaceAll(body, `\n`, "\n")
			if err := add(values["start"], values["end"], body); err != nil {
				return nil, err
			}
		}
	} else {
		lines := strings.Split(text, "\n")
		for i := 0; i < len(lines); i++ {
			match := subtitleTimes.FindStringSubmatch(lines[i])
			if match == nil {
				continue
			}
			body := []string{}
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != ""; i++ {
				body = append(body, lines[i])
			}
			if err := add(match[1], match[2], strings.Join(body, "\n")); err != nil {
				return nil, err
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("字幕没有有效的 SRT / ASS / VTT 时码: %w", model.ErrArgument)
	}
	return out, nil
}
func (s *Service) expandSubtitleInsert(ctx context.Context, d *project.AvpDocument, seq *project.Sequence, op project.Operation) ([]project.Operation, error) {
	if op.Type != "clip.insert" || op.Text != "" {
		return []project.Operation{op}, nil
	}
	var resource *project.Resource
	for i := range d.Resources {
		if d.Resources[i].ID == op.ResourceID {
			resource = &d.Resources[i]
			break
		}
	}
	if resource == nil || resource.Kind != "subtitle" {
		return []project.Operation{op}, nil
	}
	if resource.Missing || resource.Removed || resource.Changed {
		return nil, model.ErrConflict
	}
	info, err := os.Stat(resource.Source.OriginalPath)
	if err != nil || info.Size() > 8<<20 {
		return nil, model.ErrArgument
	}
	raw, err := os.ReadFile(resource.Source.OriginalPath)
	if err != nil {
		return nil, err
	}
	cues, err := parseSubtitleCues(string(raw), filepath.Ext(resource.Source.OriginalPath))
	if err != nil {
		return nil, err
	}
	var target project.Track
	if op.TrackID == "" {
		for _, t := range seq.Tracks {
			if t.Kind == "subtitle" && !t.Locked && t.GroupID == "" {
				op.TrackID = t.ID
				break
			}
		}
		if op.TrackID == "" {
			t := project.Track{ID: project.TimelineID("trk"), Name: "字幕", Kind: "subtitle", Height: 64, Clips: []project.Clip{}}
			seq.Tracks = append(seq.Tracks, t)
			op.TrackID = t.ID
		}
	}
	for _, t := range seq.Tracks {
		if t.ID == op.TrackID {
			target = t
			break
		}
	}
	if target.ID == "" || target.Kind != "subtitle" || target.Locked {
		return nil, model.ErrArgument
	}
	result := []project.Operation{}
	if op.Mode == "insert" {
		var last float64
		for _, cue := range cues {
			last = max(last, cue.End)
		}
		duration := max(int64(1), seq.FPS.Frames(last))
		result = append(result, project.Operation{Type: "gap.insert", TrackID: target.ID, At: op.At, Duration: &duration})
	}
	// Concurrent cues stay concurrent, each on a separate subtitle lane. Reusing
	// overwrite on one lane would silently discard the earlier cue.
	lanes := []string{target.ID}
	ends := []int64{op.At}
	for _, cue := range cues {
		c := op
		c.At = op.At + seq.FPS.Frames(cue.Start)
		duration := max(int64(1), seq.FPS.Frames(cue.End)-seq.FPS.Frames(cue.Start))
		lane := -1
		for i, end := range ends {
			if end <= c.At {
				lane = i
				break
			}
		}
		if lane < 0 {
			t := project.Track{ID: project.TimelineID("trk"), Kind: "subtitle", Name: "字幕叠加", Height: 64, GroupID: target.GroupID, Clips: []project.Clip{}}
			seq.Tracks = append(seq.Tracks, t)
			lanes = append(lanes, t.ID)
			ends = append(ends, op.At)
			lane = len(lanes) - 1
		}
		c.TrackID = lanes[lane]
		ends[lane] = c.At + duration
		c.Duration = &duration
		c.Text = cue.Text
		c.Mode = "overwrite"
		result = append(result, c)
	}
	return result, nil
}
