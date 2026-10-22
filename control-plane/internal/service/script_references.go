package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/scriptsyntax"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

func (s *Service) resolveScriptDocument(w ScriptWorkbench) (scriptsyntax.Document, error) {
	doc := scriptsyntax.Parse(w.Script.Body)
	if !s.WorkspaceEnabled() {
		return doc, nil
	}
	ws := s.workspace
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.document == nil {
		return doc, model.ErrInvalidState
	}
	refs, err := project.BindReferences(ws.document, w.Script.Body, w.References, "", true)
	if err != nil {
		return doc, fmt.Errorf("%v: %w", err, model.ErrConflict)
	}
	cursor := 0
	for i := range doc.Paragraphs {
		p := &doc.Paragraphs[i]
		p.Video = ""
		for range p.References {
			if cursor >= len(refs) {
				return doc, model.ErrArgument
			}
			ref := refs[cursor]
			cursor++
			resolved, e := project.ResolveReference(ws.document, ref)
			if e != nil {
				return doc, fmt.Errorf("%v: %w", e, model.ErrConflict)
			}
			resolved.ID = ref.ID
			p.ResolvedReferences = append(p.ResolvedReferences, resolved)
			if ref.Kind == "video" {
				if p.VideoID != "" && p.VideoID != ref.ResourceID {
					return doc, fmt.Errorf("同段多个画面素材，请拆成段落: %w", model.ErrArgument)
				}
				p.VideoID = ref.ResourceID
			}
		}
		if p.VideoID == "" {
			for _, ref := range p.ResolvedReferences {
				if ref.MediaID != "" {
					for _, r := range ws.document.Resources {
						if r.ID == ref.MediaID && r.Kind == "video" {
							p.VideoID = r.ID
							break
						}
					}
				}
				if p.VideoID != "" {
					break
				}
			}
		}
	}
	return doc, nil
}

func (s *Service) bindHarnessReferences(plan *skill.Plan) error {
	if !s.WorkspaceEnabled() {
		return nil
	}
	ws := s.workspace
	ws.mu.Lock()
	refs, err := project.BindReferences(ws.document, plan.Body, plan.References, "", true)
	ws.mu.Unlock()
	if err != nil {
		return fmt.Errorf("%v: %w", err, model.ErrArgument)
	}
	plan.References = refs
	w := ScriptWorkbench{Script: model.Script{Body: plan.Body}, References: refs}
	doc, err := s.resolveScriptDocument(w)
	if err != nil {
		return err
	}
	for i, paragraph := range doc.Paragraphs {
		if i >= len(plan.Segments) {
			break
		}
		segment := plan.Segments[i]
		if paragraph.VideoID != "" && segment.MediaID != paragraph.VideoID {
			return fmt.Errorf("计划没有使用绑定的素材: %w", model.ErrArgument)
		}
		for _, ref := range paragraph.ResolvedReferences {
			if ref.AnchorTime != nil && ref.MediaID == segment.MediaID {
				at := project.Seconds(*ref.AnchorTime)
				if at < segment.Start || at >= segment.End {
					return fmt.Errorf("计划选段不包含引用帧: %w", model.ErrArgument)
				}
			}
		}
	}
	return nil
}

func (s *Service) finishSequenceEdit(ctx context.Context, run *skill.Run, args json.RawMessage) (map[string]any, error) {
	var req struct {
		Revision       uint64 `json:"sequence_revision"`
		ContentVersion string `json:"content_version"`
	}
	if err := decodeSkillArgs(args, &req); err != nil {
		return nil, err
	}
	view, err := s.Timeline(ctx, run.ProjectID, run.SequenceID)
	if err != nil {
		return nil, err
	}
	if req.Revision != view.Sequence.Revision || req.ContentVersion != view.ContentVersion || run.SequenceContentVersion != view.ContentVersion {
		return nil, model.ErrConflict
	}
	report, err := s.CreateSequencePreview(ctx, run.ProjectID, run.SequenceID, SequencePreviewRequest{Session: view.Session, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion})
	if err != nil {
		return nil, err
	}
	path, err := s.SequencePreviewPath(ctx, run.ProjectID, run.SequenceID, view.ContentVersion)
	if err != nil {
		return nil, err
	}
	artifact, err := s.registerSkillArtifact(run, path, "sequence-program.mp4", "mp4", 0)
	if err != nil {
		return nil, err
	}
	if artifact.SHA != report.SHA256 {
		return nil, model.ErrConflict
	}
	run.State = "succeeded"
	return map[string]any{"completed": true, "structured_edit": true, "sequence_revision": view.Sequence.Revision, "content_version": view.ContentVersion, "artifact": artifact, "human_acceptance": "not_checked"}, nil
}
