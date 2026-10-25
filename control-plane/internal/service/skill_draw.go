package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

func drawPackage(id string) bool {
	return id == "vac-draw-hyperframes" || id == "vac-draw-remotion" || id == "vac-draw-pipeline"
}
func drawEngineAllowed(skillID, engine string) bool {
	return skillID != "vac-draw-hyperframes" && skillID != "vac-draw-remotion" ||
		skillID == "vac-draw-hyperframes" && engine == "hyperframes" || skillID == "vac-draw-remotion" && engine == "remotion"
}
func (s *Service) executeDrawTool(ctx context.Context, r *skill.Run, name string, args json.RawMessage) (json.RawMessage, error) {
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
	if name == "draw.save" {
		var req DrawSaveRequest
		if e := decodeSkillArgs(args, &req); e != nil {
			return nil, e
		}
		if !drawEngineAllowed(r.SkillID, req.Composition.Engine) {
			return nil, model.ErrForbidden
		}
		v, e := s.SaveDrawComposition(ctx, r.ProjectID, req)
		if e != nil {
			return nil, e
		}
		return respond(v)
	}
	var action struct {
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
	if !drawEngineAllowed(r.SkillID, c.Engine) {
		return nil, model.ErrForbidden
	}
	req := DrawActionRequest{action.Session, action.ContentVersion}
	switch name {
	case "draw.check":
		return s.CheckDrawComposition(ctx, r.ProjectID, c.ID, req)
	case "draw.render":
		report, e := s.RenderDrawComposition(ctx, r.ProjectID, c.ID, req)
		if e != nil {
			return nil, e
		}
		path, _, e := s.DrawRenderPath(r.ProjectID, c.ID, c.ContentVersion)
		if e != nil {
			return nil, e
		}
		artifact, e := s.registerSkillArtifact(r, path, c.Name+".mp4", "mp4", 0)
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
		v, e := s.ImportDrawComposition(ctx, "agent:"+r.ID, r.ProjectID, c.ID, DrawImportRequest{Session: action.Session, ContentVersion: action.ContentVersion, ExpectedRevision: action.ExpectedRevision, IdempotencyKey: action.IdempotencyKey, SequenceID: action.SequenceID})
		if e != nil {
			return nil, e
		}
		r.StructuredEdit = true
		r.SequenceContentVersion = v.ContentVersion
		v.History = nil
		return respond(v)
	case "draw.get_frame":
		if _, e = s.drawContext(r.ProjectID, c.ID, req); e != nil {
			return nil, e
		}
		if action.Frame < 0 || action.Frame >= c.DurationFrames {
			return nil, model.ErrArgument
		}
		if r.FramesSpent >= r.Budget.MaxFrames {
			return nil, errFrameBudget
		}
		movie, _, e := s.DrawRenderPath(r.ProjectID, c.ID, c.ContentVersion)
		if e != nil {
			return nil, e
		}
		dir, e := s.harnessDirectory(filepath.Join("projects", r.ProjectID, "skill-runs", r.ID, "draw-frames"))
		if e != nil {
			return nil, e
		}
		id := "draw_" + skillHash([]any{c.ContentVersion, action.Frame})[:20]
		path := filepath.Join(dir, id+".png")
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-nostdin", "-y", "-ss", strconv.FormatFloat(c.FPS.Seconds(action.Frame), 'f', 9, 64), "-protocol_whitelist", "file,pipe", "-i", movie, "-frames:v", "1", "-vf", "scale='min(1280,iw)':-2", "-threads", "1", path)
		configureMediaCommand(cmd)
		if e = cmd.Run(); e != nil {
			return nil, fmt.Errorf("draw frame extraction failed: %w", e)
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
