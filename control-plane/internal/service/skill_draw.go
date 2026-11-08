package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/shotcraft"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

func drawPackage(id string) bool {
	return id == "vac-draw-hyperframes" || id == "vac-draw-remotion" || id == "vac-draw-pipeline"
}
func drawEngineAllowed(skillID, engine string) bool {
	return skillID != "vac-draw-hyperframes" && skillID != "vac-draw-remotion" ||
		skillID == "vac-draw-hyperframes" && engine == "hyperframes" || skillID == "vac-draw-remotion" && engine == "remotion"
}

func drawCompositionAllowed(skillID string, c project.DrawComposition) bool {
	if c.Document == nil {
		return drawEngineAllowed(skillID, c.Engine)
	}
	for _, source := range c.Document.Sources {
		if !drawEngineAllowed(skillID, source.Engine) {
			return false
		}
	}
	return true
}

var unboundDrawImage = regexp.MustCompile(`Image alias ([A-Za-z0-9_-]{1,64}) is not bound to an imported resource`)

func (s *Service) executeDrawTool(ctx context.Context, r *skill.Run, name string, args json.RawMessage) (result json.RawMessage, err error) {
	defer func() {
		if err != nil {
			if match := unboundDrawImage.FindStringSubmatch(err.Error()); len(match) == 2 {
				err = fmt.Errorf("image alias %q is not bound to an imported resource: %w", match[1], model.ErrArgument)
			}
		}
	}()
	respond := func(value any) (json.RawMessage, error) { return json.Marshal(value) }
	if !s.WorkspaceEnabled() {
		return nil, model.ErrInvalidState
	}
	if name == "draw.list" {
		list, e := s.DrawCompositions(r.ProjectID)
		if e != nil {
			return nil, e
		}
		return respond(list)
	}
	if name == "draw.capabilities" {
		v, e := s.DrawCapabilities(r.ProjectID)
		if e != nil {
			return nil, e
		}
		return respond(v)
	}
	if name == "draw.shot" {
		return shotTool(args)
	}
	if name == "draw.save" {
		var req struct {
			DrawSaveRequest
			ShotCard string `json:"shot_card,omitempty"`
		}
		if e := decodeSkillArgs(args, &req); e != nil {
			return nil, e
		}
		if req.ShotCard != "" {
			if e := useShotTemplate(&req.Composition, req.ShotCard); e != nil {
				return nil, e
			}
		}
		if !drawCompositionAllowed(r.SkillID, req.Composition) {
			return nil, fmt.Errorf("composition sources do not match this Skill's engine: %w", model.ErrArgument)
		}
		v, e := s.SaveDrawComposition(ctx, r.ProjectID, req.DrawSaveRequest)
		if e != nil {
			return nil, e
		}
		return respond(v)
	}
	var action struct {
		TargetTrackID    string `json:"target_track_id,omitempty"`
		At               int64  `json:"at,omitempty"`
		Format           string `json:"format,omitempty"`
		SourceMode       string `json:"source_mode,omitempty"`
		RequestID        string `json:"request_id,omitempty"`
		CompositionID    string `json:"composition_id"`
		Session          string `json:"session"`
		ContentVersion   string `json:"content_version"`
		SequenceID       string `json:"sequence_id,omitempty"`
		ExpectedRevision uint64 `json:"expected_revision,omitempty"`
		IdempotencyKey   string `json:"idempotency_key,omitempty"`
		Frame            int64  `json:"frame,omitempty"`
	}
	if e := decodeSkillArgs(args, &action); e != nil {
		return nil, e
	}
	c, e := s.drawComposition(r.ProjectID, action.CompositionID)
	if e != nil {
		return nil, e
	}
	if !drawCompositionAllowed(r.SkillID, c) {
		return nil, fmt.Errorf("composition sources do not match this Skill's engine: %w", model.ErrArgument)
	}
	req := DrawActionRequest{action.Session, action.ContentVersion}
	switch name {
	case "draw.check":
		return s.CheckDrawComposition(ctx, r.ProjectID, c.ID, req)
	case "draw.render":
		report, e := s.RenderDrawCompositionFormat(ctx, r.ProjectID, c.ID, DrawRenderRequest{DrawActionRequest: req, Format: action.Format})
		if e != nil {
			return nil, e
		}
		path, _, e := s.DrawRenderPathFormat(r.ProjectID, c.ID, c.ContentVersion, action.Format)
		if e != nil {
			return nil, e
		}
		_, extension, _ := drawFormat(action.Format)
		artifact, e := s.registerSkillArtifact(r, path, c.Name+extension, strings.TrimPrefix(extension, "."), 0)
		if e != nil {
			return nil, e
		}
		return respond(map[string]any{"render": report, "artifact": artifact})
	case "draw.import":
		if action.SequenceID == "" {
			action.SequenceID = r.SequenceID
		}
		if action.SequenceID != r.SequenceID {
			return nil, model.ErrForbidden
		}
		v, e := s.ImportDrawComposition(ctx, "agent:"+r.ID, r.ProjectID, c.ID, DrawImportRequest{Session: action.Session, ContentVersion: action.ContentVersion, ExpectedRevision: action.ExpectedRevision, IdempotencyKey: action.IdempotencyKey, SequenceID: action.SequenceID, SourceMode: action.SourceMode, TargetTrackID: action.TargetTrackID, At: action.At})
		if e != nil {
			return nil, e
		}
		r.StructuredEdit = true
		r.SequenceContentVersion = v.ContentVersion
		v.History = nil
		return respond(v)
	case "draw.get_frame", "draw.preview":
		if _, e = s.drawContext(r.ProjectID, c.ID, req); e != nil {
			return nil, e
		}
		if action.Frame < 0 || action.Frame >= c.DurationFrames {
			return nil, model.ErrArgument
		}
		if r.FramesSpent >= r.Budget.MaxFrames {
			return nil, errFrameBudget
		}
		dir, e := s.harnessDirectory(filepath.Join("projects", r.ProjectID, "skill-runs", r.ID, "draw-frames"))
		if e != nil {
			return nil, e
		}
		id := "draw_" + skillHash([]any{c.ContentVersion, action.Frame})[:20]
		path := filepath.Join(dir, id+".png")
		if name == "draw.preview" || c.Document != nil {
			requestID := action.RequestID
			if requestID == "" {
				requestID = newID("frame")
			}
			preview, e := s.PreviewDrawComposition(ctx, r.ProjectID, c.ID, DrawPreviewRequest{Session: action.Session, ContentVersion: action.ContentVersion, RequestID: requestID, Frame: action.Frame})
			if e != nil {
				return nil, e
			}
			raw, e := base64.StdEncoding.DecodeString(preview.PNG)
			if e != nil {
				return nil, e
			}
			if e = os.WriteFile(path, raw, 0600); e != nil {
				return nil, e
			}
		} else {
			movie, _, e := s.DrawRenderPath(r.ProjectID, c.ID, c.ContentVersion)
			if e != nil {
				return nil, e
			}
			cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-y", "-ss", strconv.FormatFloat(c.FPS.Seconds(action.Frame), 'f', 9, 64), "-protocol_whitelist", "file,pipe", "-i", movie, "-frames:v", "1", "-vf", "scale='min(1280,iw)':-2", "-threads", "1", path)
			configureMediaCommand(cmd)
			if e = cmd.Run(); e != nil {
				return nil, fmt.Errorf("draw frame extraction failed: %w", e)
			}
		}
		artifact, e := s.registerSkillArtifact(r, path, id+".png", "frame", 0)
		if e != nil {
			return nil, e
		}
		r.FramesSpent++
		frame := skill.Frame{ID: artifact.ID, MediaID: c.ID, PTS: c.FPS.Seconds(action.Frame), Strategy: "draw-frame", SHA: artifact.SHA}
		r.Frames = append(r.Frames, frame)
		if info, e := os.Stat(path); e != nil || info.Size() == 0 {
			return nil, model.ErrInvalidState
		}
		return respond(map[string]any{"frame": frame, "artifact": artifact})
	}
	return nil, fmt.Errorf("unknown draw tool %s: %w", strings.TrimPrefix(name, "draw."), model.ErrArgument)
}

func shotTool(args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Action     string `json:"action"`
		ID         string `json:"id,omitempty"`
		Query      string `json:"query,omitempty"`
		Offset     int    `json:"offset,omitempty"`
		Dependency string `json:"dependency,omitempty"`
	}
	if err := decodeSkillArgs(args, &req); err != nil {
		return nil, err
	}
	switch req.Action {
	case "list":
		cards := shotcraft.List(req.Query)
		if req.Offset < 0 || req.Offset > len(cards) {
			return nil, model.ErrArgument
		}
		return json.Marshal(map[string]any{"revision": shotcraft.Revision, "total": len(cards), "cards": cards[req.Offset:min(req.Offset+12, len(cards))]})
	case "read":
		card, style, ok := shotcraft.Lookup(req.ID)
		if !ok {
			return nil, fmt.Errorf("unknown shot card or variant %q: %w", req.ID, model.ErrArgument)
		}
		if req.Dependency != "" {
			if !strings.HasPrefix(req.Dependency, "demos/_fixtures/") && !strings.HasPrefix(req.Dependency, "demos/_textures/") && !strings.HasPrefix(req.Dependency, "assets/lib/") {
				return nil, model.ErrArgument
			}
			body, err := shotcraft.Read(req.Dependency)
			if err != nil {
				return nil, model.ErrArgument
			}
			return json.Marshal(map[string]string{"path": req.Dependency, "source": body})
		}
		recipe, err := shotcraft.Read(card.Recipe)
		if err != nil {
			return nil, err
		}
		tpl := shotcraft.Compiled(card, style)
		result := map[string]any{
			"card": card, "style": style, "recipe": recipe, "revision": shotcraft.Revision,
			"license": "Apache-2.0", "author": "Vincentwei1021", "engine": "remotion", "width": 1920, "height": 1080,
			"fps": project.Rate{Num: 30, Den: 1}, "duration_frames": tpl.DurationFrames, "source": tpl.Source,
			"image_aliases": tpl.ImageAliases,
		}
		if tpl.Source == "" {
			body, err := shotcraft.Read(style.Source)
			if err != nil {
				return nil, err
			}
			result["reference_source"] = body
			result["needs_adapter"] = tpl.NeedsAdapter
		}
		return json.Marshal(result)
	default:
		return nil, model.ErrArgument
	}
}

func useShotTemplate(c *project.DrawComposition, id string) error {
	card, variant, ok := shotcraft.Lookup(id)
	if !ok {
		return fmt.Errorf("unknown shot card or variant %q: %w", id, model.ErrArgument)
	}
	tpl := shotcraft.Compiled(card, variant)
	if tpl.Source == "" {
		return fmt.Errorf("shot card %q needs adaptation: %s: %w", id, tpl.NeedsAdapter, model.ErrArgument)
	}
	for _, alias := range tpl.ImageAliases {
		bound := false
		for _, binding := range c.Images {
			bound = bound || binding.ID == alias && binding.Mode == "source"
		}
		if !bound {
			return fmt.Errorf("shot card %q needs current-project image alias %q in source mode: %w", id, alias, model.ErrArgument)
		}
	}
	if c.Document != nil || c.Source != "" || (c.Engine != "" && c.Engine != "remotion") ||
		c.Width != 0 || c.Height != 0 || c.FPS != (project.Rate{}) || c.DurationFrames != 0 {
		return fmt.Errorf("shot_card supplies its own source, engine and timebase: %w", model.ErrArgument)
	}
	if c.Name == "" {
		c.Name = variant.ID
	}
	c.Engine, c.Source, c.Width, c.Height = "remotion", tpl.Source, 1920, 1080
	c.FPS = project.Rate{Num: 30, Den: 1}
	c.DurationFrames = int64(tpl.DurationFrames)
	return nil
}
