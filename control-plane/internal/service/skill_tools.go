package service

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/scriptsyntax"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// ExecuteSkillTool reserves a durable step before any billable or filesystem work.
// Duplicate requests return the original result; incomplete work is never replayed.
func (s *Service) ExecuteSkillTool(ctx context.Context, actor, id string, req skill.ToolRequest) (skill.Run, json.RawMessage, error) {
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return r, nil, err
	}
	internalModel := req.Name == "agent.model" && r.Mode == "builtin_api" && isHumanActor(actor)
	if req.IdempotencyKey == "" || len(req.IdempotencyKey) > 128 || (!internalModel && len(req.Arguments) > 128<<10) || (internalModel && len(req.Arguments) > 3<<20) || (!internalModel && !slices.Contains(skillTools, req.Name)) {
		return r, nil, model.ErrArgument
	}
	release, err := s.acquireScript("skill:" + id)
	if err != nil {
		return r, nil, err
	}
	defer release()
	if r.State == "cancelled" {
		return r, nil, model.ErrInvalidState
	}
	check := r
	if isHumanActor(actor) {
		check.LeaseUntil = nil
	}
	snap, err := s.checkSkillRun(ctx, check)
	if err != nil {
		return r, nil, err
	}
	scope := "skill-tool:" + id
	hash := skillHash(struct {
		Name      string
		Arguments json.RawMessage
	}{req.Name, req.Arguments})
	var step skill.Step
	old, stepID, replay := s.st.SkillRequest(ctx, scope, req.IdempotencyKey)
	if replay == nil {
		if old != hash {
			return r, nil, model.ErrConflict
		}
		steps, e := s.st.SkillSteps(ctx, id)
		if e != nil {
			return r, nil, e
		}
		for _, raw := range steps {
			if json.Unmarshal(raw, &step) == nil && step.ID == stepID {
				if step.State != "succeeded" {
					return r, nil, model.ErrConflict
				}
				return r, step.Result, nil
			}
		}
		return r, nil, model.ErrInvalidState
	}
	if !errors.Is(replay, model.ErrNotFound) {
		return r, nil, replay
	}
	if r.Version != req.ExpectedVersion {
		return r, nil, model.ErrConflict
	}
	if r.State != "queued" {
		return r, nil, model.ErrInvalidState
	}
	if !internalModel && !slices.Contains(snap.Package.Tools, req.Name) {
		return r, nil, model.ErrForbidden
	}
	if r.Steps-r.BudgetStepBase >= r.Budget.MaxSteps || r.ElapsedMilliseconds-r.BudgetElapsedBase >= int64(r.Budget.MaxSeconds)*1000 {
		return r, nil, fmt.Errorf("skill step budget exhausted: %w", model.ErrInvalidState)
	}
	step = skill.Step{ID: newID("step"), RunID: id, Name: req.Name, State: "running", RequestSHA: hash, CreatedAt: s.autoNow()}
	prior := r.Version
	r.Version++
	r.State = "running"
	r.Reason = ""
	phase := "working"
	switch req.Name {
	case "agent.model":
		phase = "generating"
	case "draw.check", "draw.preview", "draw.get_frame":
		phase = "checking"
	case "draw.render", "run.finish":
		phase = "rendering"
	}
	now := s.autoNow()
	r.Progress = &skill.RunProgress{Phase: phase, StartedAt: now, UpdatedAt: now}
	if r.AgentRole != "" {
		until := s.autoNow().Add(time.Duration(r.Budget.MaxSeconds) * time.Second)
		r.LeaseUntil = &until
	}
	r.Steps++
	r.UpdatedAt = s.autoNow()
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		if e := tx.SkillPut(ctx, "run", id, prior, r); e != nil {
			return e
		}
		if e := tx.PutSkillStep(ctx, step.ID, id, step); e != nil {
			return e
		}
		if e := s.journalTx(ctx, tx, actor, id, "tool.requested", map[string]any{"name": req.Name, "arguments": journalPayload(req.Arguments), "request_sha256": hash, "step_id": step.ID}); e != nil {
			return e
		}
		return tx.SkillRemember(ctx, scope, req.IdempotencyKey, hash, step.ID)
	})
	if err != nil {
		return r, nil, err
	}
	s.publish("workspace.skill.progress", map[string]any{"id": r.ID, "project_id": r.ProjectID})
	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(int64(r.Budget.MaxSeconds)*1000-(r.ElapsedMilliseconds-r.BudgetElapsedBase))*time.Millisecond)
	defer cancel()
	s.harnessMu.Lock()
	if s.harnessCancel == nil {
		s.harnessCancel = map[string]context.CancelFunc{}
	}
	s.harnessCancel[id] = cancel
	s.harnessMu.Unlock()
	defer func() { s.harnessMu.Lock(); delete(s.harnessCancel, id); s.harnessMu.Unlock() }()
	beforeTool := r
	result, toolErr := s.dispatchSkillTool(runCtx, &r, snap, req.Name, req.Arguments)
	r.ElapsedMilliseconds += time.Since(started).Milliseconds()
	if toolErr == nil {
		_, toolErr = s.checkSkillRun(runCtx, r)
	}
	if toolErr == nil {
		toolErr = runCtx.Err()
	}
	if toolErr != nil {
		elapsed := r.ElapsedMilliseconds
		framesSpent := r.FramesSpent
		r = beforeTool
		r.ElapsedMilliseconds = elapsed
		r.FramesSpent = framesSpent
		step.State = "failed"
		step.Result = result
		step.Error = "tool_failed"
		r.State = "waiting_human"
		r.Reason = "tool_failed"
		if strings.HasPrefix(req.Name, "draw.") && errors.Is(toolErr, model.ErrArgument) {
			r.State = "queued"
			r.Reason = "invalid_tool_arguments"
		}
		if errors.Is(toolErr, model.ErrConflict) {
			r.Reason = "context_changed"
		}
		if errors.Is(toolErr, model.ErrForbidden) {
			r.Reason = "authorization_revoked"
		}
		// Dependency/budget errors use fixed worker messages, never provider response bodies.
		if errors.Is(toolErr, errFrameBudget) {
			r.Reason = "frame_budget_exceeded"
			step.Error = r.Reason
		}
		if errors.Is(toolErr, errModelOutputTruncated) {
			r.Reason = "model_output_truncated"
			step.Error = r.Reason
		}
		if errors.Is(toolErr, errModelRequestTimeout) {
			r.Reason = "model_request_timeout"
			step.Error = r.Reason
		}
	} else {
		step.State = "succeeded"
		step.Result = result
		if r.State == "running" {
			r.State = "queued"
		}
	}
	if r.State == "queued" {
		now := s.autoNow()
		r.Progress = &skill.RunProgress{Phase: "working", StartedAt: now, UpdatedAt: now}
	}
	committed := r.Version
	if r.AgentRole != "" {
		until := s.autoNow().Add(2 * time.Minute)
		r.LeaseUntil = &until
	}
	r.Version++
	r.UpdatedAt = s.autoNow()
	// Persist failure after cancellation using a short independent context.
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveCancel()
	err = s.st.Transaction(saveCtx, func(tx *store.Store) error {
		if e := tx.SkillPut(saveCtx, "run", id, committed, r); e != nil {
			return e
		}
		if e := tx.PutSkillStep(saveCtx, step.ID, id, step); e != nil {
			return e
		}
		if e := s.journalTx(saveCtx, tx, actor, id, "tool."+step.State, map[string]any{"name": req.Name, "step_id": step.ID, "result_sha256": skillHash(string(result)), "reason": r.Reason}); e != nil {
			return e
		}
		return auditTx(saveCtx, tx, actor, "skill.tool."+step.State, id, req.Name)
	})
	if err != nil {
		return r, nil, err
	}
	s.publish("skill.run.changed", r)
	if toolErr == nil {
		s.importCompletedSequenceResult(ctx, r)
	}
	if toolErr != nil {
		return r, result, toolErr
	}
	return r, result, nil
}

var errFrameBudget = fmt.Errorf("frame budget exceeded: %w", model.ErrInvalidState)
var errModelOutputTruncated = fmt.Errorf("模型输出达到额度上限，请在服务商设置中调整最大输出 Token: %w", model.ErrInvalidState)
var errModelRequestTimeout = fmt.Errorf("模型请求超时，请检查连接或调整任务时限: %w", model.ErrInvalidState)

func decodeSkillArgs(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		if field, ok := strings.CutPrefix(err.Error(), "json: unknown field \""); ok && strings.HasSuffix(field, "\"") {
			field = strings.TrimSuffix(field, "\"")
			if len(field) > 0 && len(field) <= 64 && strings.IndexFunc(field, func(r rune) bool {
				return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
			}) < 0 {
				if field == "images" {
					return fmt.Errorf("unknown field %q: use composition.images or document.nodes for imported images: %w", field, model.ErrArgument)
				}
				return fmt.Errorf("unknown field %q: %w", field, model.ErrArgument)
			}
		}
		return fmt.Errorf("invalid tool JSON arguments: %w", model.ErrArgument)
	}
	return nil
}
func (s *Service) dispatchSkillTool(ctx context.Context, r *skill.Run, snap harnessSnapshot, name string, args json.RawMessage) (json.RawMessage, error) {
	respond := func(v any) (json.RawMessage, error) { return json.Marshal(v) }
	if strings.HasPrefix(name, "draw.") {
		return s.executeDrawTool(ctx, r, name, args)
	}
	switch name {
	case "timeline.read":
		var req struct {
			SequenceID string `json:"sequence_id"`
		}
		if e := decodeSkillArgs(args, &req); e != nil {
			return nil, e
		}
		if req.SequenceID == "" {
			req.SequenceID = r.SequenceID
		}
		v, e := s.Timeline(ctx, r.ProjectID, req.SequenceID)
		if e != nil {
			return nil, e
		}
		v.History = nil
		return respond(v)
	case "references.resolve":
		var req struct {
			References []project.Reference `json:"references"`
		}
		if e := decodeSkillArgs(args, &req); e != nil {
			return nil, e
		}
		v, e := s.ResolveProjectReferences(r.ProjectID, req.References)
		if e != nil {
			return nil, e
		}
		return respond(v)
	case "timeline.edit":
		var req struct {
			SequenceID string          `json:"sequence_id"`
			Command    project.Command `json:"command"`
		}
		if e := decodeSkillArgs(args, &req); e != nil {
			return nil, e
		}
		if req.SequenceID == "" {
			req.SequenceID = r.SequenceID
		}
		if req.SequenceID != r.SequenceID || r.SequenceID == "" {
			return nil, model.ErrForbidden
		}
		for _, op := range req.Command.Operations {
			if op.Type == "history.undo" || op.Type == "history.redo" {
				return nil, model.ErrForbidden
			}
		}
		v, e := s.TimelineCommand(ctx, "agent:"+r.ID, r.ProjectID, req.SequenceID, req.Command)
		if e != nil {
			return nil, e
		}
		r.StructuredEdit = true
		r.SequenceContentVersion = v.ContentVersion
		v.History = nil
		return respond(v)
	case "agent.model":
		var req struct {
			History []map[string]any `json:"history"`
		}
		if err := decodeSkillArgs(args, &req); err != nil {
			return nil, err
		}
		profile, _, err := s.GetProcessingProfile(ctx, r.ProfileID, r.ProfileRevision)
		if err != nil {
			return nil, err
		}
		drawings, _ := s.DrawCompositions(r.ProjectID)
		imageResources, _ := s.DrawImageResources(r.ProjectID)
		input := map[string]any{"image_resources": imageResources, "draw_compositions": drawings, "run": r, "skill": snap.Package, "media": snap.Media, "tools": snap.Package.Tools, "history": req.History, "provider": profile.Narration, "vision_provider": profile.Vision}
		if s.WorkspaceEnabled() && r.SequenceID != "" {
			view, e := s.Timeline(ctx, r.ProjectID, r.SequenceID)
			if e != nil {
				return nil, e
			}
			view.History = nil
			view.Sequence.HistoryCursor = 0
			refs, e := s.ReferenceDirectory(r.ProjectID, r.SequenceID)
			if e != nil {
				return nil, e
			}
			input["sequence"] = view
			input["references"] = refs
		}
		data, err := s.skillProcess(ctx, "agent", input)
		if err == nil {
			var decision struct {
				Wait string `json:"wait"`
			}
			if json.Unmarshal(data, &decision) == nil && decision.Wait != "" {
				r.State = "waiting_human"
				r.Reason = "agent_clarification"
			}
		}
		return data, err
	case "media.list":
		return respond(snap.Media)
	case "artifacts.list":
		return respond(r.Artifacts)
	case "summaries.get":
		return respond(map[string]any{"summary": r.Summary})
	case "summaries.submit":
		if err := validateContentSummary(args, r, snap); err != nil {
			return nil, err
		}
		r.Summary = append(json.RawMessage{}, args...)
		if err := s.writeSkillSummary(r); err != nil {
			return nil, err
		}
		return respond(map[string]bool{"saved": true})
	case "scripts.propose":
		var plan skill.Plan
		if err := decodeSkillArgs(args, &plan); err != nil {
			return nil, err
		}
		if err := s.bindHarnessReferences(&plan); err != nil {
			return nil, err
		}
		if err := validateHarnessPlan(plan, snap); err != nil {
			return nil, err
		}
		if err := enforceTargetedEdit(r, &plan); err != nil {
			return nil, err
		}
		var versionErr error
		plan.Version, versionErr = s.st.NextSkillPlanVersion(ctx, r.ID)
		if versionErr != nil {
			return nil, versionErr
		}
		plan.FinalizedBy = ""
		plan.FinalizedAt = nil
		// Agent proposals cannot create a voice configuration or external consent.
		for i := range plan.Segments {
			if r.EditRequest != nil {
				selected := false
				for _, window := range r.EditRequest.Windows {
					if window.SegmentIndex == i {
						selected = true
					}
				}
				if !selected {
					continue
				}
			}
			plan.Segments[i].TTSEnabled = false
			plan.Segments[i].Voice = nil
			plan.Segments[i].VoiceConsent = false
			plan.Segments[i].VoiceOffset = 0
		}
		r.Plan = &plan
		r.EditRequest = nil
		r.Inspection = nil
		r.State = "waiting_human"
		r.Reason = "plan_confirmation"
		return respond(plan)
	case "renders.create":
		if r.EditRequest != nil {
			return nil, model.ErrInvalidState
		}
		r.Inspection = nil
		if r.Plan == nil || !isHumanActor(r.Plan.FinalizedBy) || r.Plan.FinalizedAt == nil {
			return nil, model.ErrForbidden
		}
		if err := s.bindHarnessReferences(r.Plan); err != nil {
			return nil, err
		}
		if err := s.renderSkillPlan(ctx, r, snap); err != nil {
			return nil, err
		}
		return respond(r.Artifacts)
	case "renders.inspect":
		return s.inspectSkillRender(ctx, r)
	case "renders.review":
		var review skill.Inspection
		if err := decodeSkillArgs(args, &review); err != nil {
			return nil, err
		}
		if r.Inspection == nil || review.PlanVersion != r.Inspection.PlanVersion || !slices.Equal(review.FrameIDs, r.Inspection.FrameIDs) || (review.Verdict != "passed" && review.Verdict != "needs_changes") || len(review.Issues) > 20 {
			return nil, model.ErrArgument
		}
		if review.Verdict == "passed" && len(review.Issues) > 0 {
			return nil, model.ErrArgument
		}
		r.Inspection = &review
		return respond(review)
	case "media.get_frames":
		return s.extractSkillFrames(ctx, r, snap, args)
	case "media.transcribe":
		return s.transcribeSkillMedia(ctx, r, snap, args)
	case "artifacts.get":
		var a struct {
			ID string `json:"id"`
		}
		if err := decodeSkillArgs(args, &a); err != nil {
			return nil, err
		}
		path, art, err := s.skillArtifactPath(ctx, *r, a.ID)
		if err != nil {
			return nil, err
		}
		if art.Kind != "frame" && art.Kind != "transcript" {
			return nil, model.ErrArgument
		}
		limit := int64(256 << 10)
		if art.Kind == "frame" {
			limit = 16 << 20
		} // Full RGBA PNGs are larger than text transcripts.
		if art.Size > limit {
			return nil, model.ErrArgument
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return respond(map[string]any{"artifact": art, "base64": base64.StdEncoding.EncodeToString(raw)})
	case "run.finish":
		if r.StructuredEdit && r.Plan == nil {
			v, e := s.finishSequenceEdit(ctx, r, args)
			if e != nil {
				return nil, e
			}
			return respond(v)
		}
		if r.EditRequest != nil {
			return nil, model.ErrInvalidState
		}
		if r.Inspection == nil || r.Plan == nil || r.Inspection.PlanVersion != r.Plan.Version || r.Inspection.Verdict != "passed" {
			return nil, model.ErrInvalidState
		}
		if r.Plan == nil || r.Plan.FinalizedBy == "" {
			return nil, model.ErrForbidden
		}
		found := false
		for _, a := range r.Artifacts {
			if a.Kind == "mp4" && a.PlanVersion == r.Plan.Version {
				if _, _, err := s.skillArtifactPath(ctx, *r, a.ID); err != nil {
					return nil, err
				}
				found = true
			}
		}
		if !found {
			return nil, model.ErrInvalidState
		}
		r.State = "succeeded"
		return respond(map[string]bool{"completed": true})
	}
	return nil, model.ErrArgument
}
func validateHarnessPlan(p skill.Plan, snap harnessSnapshot) error {
	doc := scriptsyntax.Parse(p.Body)
	if len(doc.Diagnostics) > 0 || len(p.Segments) == 0 || len(p.Segments) > 20 || len(p.Segments) != len(doc.Paragraphs) {
		return model.ErrArgument
	}
	media := map[string]skill.Media{}
	for _, m := range snap.Media {
		media[m.ID] = m
	}
	for i, seg := range p.Segments {
		m, ok := media[seg.MediaID]
		d := seg.End - seg.Start
		if !ok || m.Kind == "audio" || !finiteScriptTime(seg.Start) || !finiteScriptTime(seg.End) || seg.Start < m.In || seg.End > m.Out || d < .1 || d > 120 || len(seg.Narration) > 16<<10 {
			return model.ErrArgument
		}
		if name := doc.Paragraphs[i].Video; len(p.References) == 0 && name != "" && name != m.Name {
			return model.ErrArgument
		}
	}
	return nil
}
func validateContentSummary(raw json.RawMessage, r *skill.Run, snap harnessSnapshot) error {
	var v struct {
		Model     string   `json:"model"`
		Narrative string   `json:"narrative"`
		Beats     []string `json:"beats"`
		Timeline  []struct {
			MediaID     string   `json:"media_id"`
			Start       float64  `json:"start"`
			End         float64  `json:"end"`
			Description string   `json:"description"`
			Confidence  string   `json:"confidence"`
			Frames      []string `json:"frame_ids"`
		} `json:"timeline"`
	}
	if len(raw) > 64<<10 || decodeSkillArgs(raw, &v) != nil || v.Model == "" || v.Narrative == "" || len(v.Timeline) == 0 {
		return model.ErrArgument
	}
	for _, row := range v.Timeline {
		valid := false
		for _, m := range snap.Media {
			if m.ID == row.MediaID && finiteScriptTime(row.Start) && finiteScriptTime(row.End) && row.Start >= m.In && row.End >= row.Start && row.End <= m.Out {
				valid = true
			}
		}
		if !valid || row.Description == "" || !slices.Contains([]string{"high", "medium", "low"}, row.Confidence) || len(row.Frames) == 0 {
			return model.ErrArgument
		}
		for _, id := range row.Frames {
			found := false
			for _, f := range r.Frames {
				if f.ID == id && f.MediaID == row.MediaID {
					found = true
				}
			}
			if !found {
				return model.ErrArgument
			}
		}
	}
	return nil
}
func (s *Service) skillProcess(ctx context.Context, operation string, payload any) (json.RawMessage, error) {
	return s.skillProcessWithPython(ctx, operation, payload, "")
}
func (s *Service) skillProcessWithPython(ctx context.Context, operation string, payload any, pythonOverride string) (json.RawMessage, error) {
	root, python, err := s.scriptRoot()
	if err != nil {
		return nil, err
	}
	if pythonOverride != "" {
		python = pythonOverride
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, python, "-B", "-m", "vac_worker.skill_runtime", operation)
	cmd.Dir = filepath.Join(root, "workers", "python")
	cmd.Env = toolEnvironment(payloadCredentialNames(payload, "provider", "vision_provider")...)
	configureMediaCommand(cmd)
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(raw)
	var out scriptOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	var progressDone chan struct{}
	var liveRun *skill.Run
	if input, ok := payload.(map[string]any); ok && operation == "agent" {
		liveRun, _ = input["run"].(*skill.Run)
	}
	if liveRun != nil && liveRun.Progress != nil && liveRun.Progress.Phase == "generating" {
		cmd.Stderr = nil
		pipe, e := cmd.StderrPipe()
		if e != nil {
			return nil, e
		}
		runID, projectID, initial := liveRun.ID, liveRun.ProjectID, *liveRun.Progress
		progressDone = make(chan struct{})
		go func() {
			defer close(progressDone)
			scanner := bufio.NewScanner(pipe)
			scanner.Buffer(make([]byte, 1024), 64<<10)
			for scanner.Scan() {
				line := scanner.Text()
				if !strings.HasPrefix(line, "VAC_AGENT_PROGRESS ") {
					continue
				}
				var update struct {
					OutputChars int `json:"output_chars"`
				}
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "VAC_AGENT_PROGRESS ")), &update) != nil || update.OutputChars < 0 || update.OutputChars > 4<<20 {
					continue
				}
				p := initial
				p.OutputChars = update.OutputChars
				p.UpdatedAt = s.autoNow()
				s.harnessMu.Lock()
				if s.harnessProgress == nil {
					s.harnessProgress = map[string]skill.RunProgress{}
				}
				s.harnessProgress[runID] = p
				s.harnessMu.Unlock()
				s.publish("workspace.skill.progress", map[string]any{"id": runID, "project_id": projectID})
			}
		}()
		defer func() { s.harnessMu.Lock(); delete(s.harnessProgress, runID); s.harnessMu.Unlock() }()
	}
	runErr := cmd.Run()
	if progressDone != nil {
		<-progressDone
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var status struct {
		OK   bool            `json:"ok"`
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(out.Bytes(), &status) != nil {
		return nil, fmt.Errorf("media worker unavailable: %w", model.ErrInvalidState)
	}
	if !status.OK || runErr != nil {
		if status.Code == "frame_budget_exceeded" {
			return status.Data, errFrameBudget
		}
		if status.Code == "model_output_truncated" {
			return nil, errModelOutputTruncated
		}
		if status.Code == "model_request_timeout" {
			return nil, errModelRequestTimeout
		}
		var diagnostic struct {
			ErrorType string `json:"error_type"`
		}
		_ = json.Unmarshal(status.Data, &diagnostic)
		switch diagnostic.ErrorType {
		case "TypeError", "KeyError", "ValueError", "ContentError", "RuntimeError", "FileNotFoundError", "OSError", "ImportError":
			return nil, fmt.Errorf("media worker failed (%s/%s): %w", status.Code, diagnostic.ErrorType, model.ErrInvalidState)
		}
		return nil, fmt.Errorf("media worker failed (%s): %w", status.Code, model.ErrInvalidState)
	}
	return status.Data, nil
}
func (s *Service) registerSkillArtifact(r *skill.Run, path, name, kind string, plan int) (skill.Artifact, error) {
	var a skill.Artifact
	dir, err := s.skillRunDir(*r)
	if err != nil {
		return a, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return a, model.ErrArgument
	}
	id := newID("art") + filepath.Ext(name)
	dest := filepath.Join(dir, "files", id)
	from, err := os.Open(path)
	if err != nil {
		return a, err
	}
	defer from.Close()
	to, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return a, err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(to, h), from)
	closeErr := to.Close()
	if err != nil {
		return a, err
	}
	if closeErr != nil {
		return a, closeErr
	}
	a = skill.Artifact{ID: id, Name: name, Kind: kind, Size: size, SHA: hex.EncodeToString(h.Sum(nil)), PlanVersion: plan}
	r.Artifacts = append(r.Artifacts, a)
	return a, nil
}
func (s *Service) skillArtifactPath(ctx context.Context, r skill.Run, id string) (string, skill.Artifact, error) {
	for _, a := range r.Artifacts {
		if a.ID != id {
			continue
		}
		if !filepath.IsLocal(id) || filepath.Base(id) != id {
			return "", a, model.ErrArgument
		}
		if a.PlanVersion > 0 && (r.Plan == nil || r.Plan.Version != a.PlanVersion || r.Plan.FinalizedBy == "") {
			return "", a, model.ErrConflict
		}
		dir, err := s.skillRunDir(r)
		if err != nil {
			return "", a, err
		}
		rel, _ := filepath.Rel(s.deliveryRoot, filepath.Join(dir, "files", id))
		path, info, err := s.deliveryPath(rel, false)
		if err != nil {
			return "", a, err
		}
		if info.Size() != a.Size {
			return "", a, model.ErrConflict
		}
		f, err := os.Open(path)
		if err != nil {
			return "", a, err
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", a, err
		}
		if hex.EncodeToString(h.Sum(nil)) != a.SHA {
			return "", a, model.ErrConflict
		}
		return path, a, nil
	}
	return "", skill.Artifact{}, model.ErrNotFound
}
func (s *Service) SkillArtifactPath(ctx context.Context, actor, id, key string) (string, skill.Artifact, error) {
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return "", skill.Artifact{}, err
	}
	if r.State == "cancelled" {
		return "", skill.Artifact{}, model.ErrInvalidState
	}
	check := r
	if isHumanActor(actor) {
		check.LeaseUntil = nil
	}
	if _, err = s.checkSkillRun(ctx, check); err != nil {
		return "", skill.Artifact{}, err
	}
	return s.skillArtifactPath(ctx, r, key)
}
func (s *Service) extractSkillFrames(ctx context.Context, r *skill.Run, snap harnessSnapshot, args json.RawMessage) (json.RawMessage, error) {
	var req struct {
		MediaID   string  `json:"media_id"`
		Every     int     `json:"every"`
		Interval  float64 `json:"interval_seconds"`
		Threshold float64 `json:"threshold"`
		Coverage  float64 `json:"coverage_seconds"`
	}
	if err := decodeSkillArgs(args, &req); err != nil {
		return nil, err
	}
	dir, err := s.skillRunDir(*r)
	if err != nil {
		return nil, err
	}
	output := filepath.Join(dir, newID("frames"))
	if err = os.Mkdir(output, 0700); err != nil {
		return nil, err
	}
	sources := []map[string]any{}
	for _, m := range snap.Media {
		if m.Kind == "audio" {
			continue
		}
		if req.MediaID != "" && req.MediaID != m.ID {
			continue
		}
		path, _, e := s.deliveryPath(filepath.FromSlash(snap.Paths[m.ID]), false)
		if e != nil {
			return nil, e
		}
		sources = append(sources, map[string]any{"id": m.ID, "path": path, "in": m.In, "out": m.Out})
	}
	if len(sources) == 0 {
		return nil, model.ErrArgument
	}
	data, err := s.skillProcess(ctx, "extract", map[string]any{"sources": sources, "output": output, "max_frames": r.Budget.MaxFrames - r.FramesSpent, "every": req.Every, "interval": req.Interval, "threshold": req.Threshold, "coverage": req.Coverage})
	if err != nil {
		return data, err
	}
	var result struct {
		Frames []struct {
			File     string  `json:"file"`
			MediaID  string  `json:"media_id"`
			PTS      float64 `json:"pts"`
			Score    float64 `json:"score"`
			Strategy string  `json:"strategy"`
		} `json:"frames"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Frames) > r.Budget.MaxFrames-r.FramesSpent {
		return nil, model.ErrInvalidState
	}
	r.FramesSpent += len(result.Frames)
	frames := []skill.Frame{}
	for _, row := range result.Frames {
		if !filepath.IsLocal(row.File) || filepath.Base(row.File) != row.File {
			return nil, model.ErrArgument
		}
		a, e := s.registerSkillArtifact(r, filepath.Join(output, row.File), row.File, "frame", 0)
		if e != nil {
			return nil, e
		}
		f := skill.Frame{ID: a.ID, MediaID: row.MediaID, PTS: row.PTS, Score: row.Score, Strategy: row.Strategy, SHA: a.SHA}
		frames = append(frames, f)
		r.Frames = append(r.Frames, f)
	}
	return json.Marshal(map[string]any{"frames": frames, "strategy": json.RawMessage(data)})
}
func (s *Service) transcribeSkillMedia(ctx context.Context, r *skill.Run, snap harnessSnapshot, args json.RawMessage) (json.RawMessage, error) {
	if !r.TranscriptionAuthorized {
		return nil, model.ErrForbidden
	}
	profile, _, err := s.GetProcessingProfile(ctx, r.ProfileID, r.ProfileRevision)
	if err != nil {
		return nil, err
	}
	if profile.Transcription == nil {
		return nil, fmt.Errorf("configure a transcription provider in the bound profile: %w", model.ErrInvalidState)
	}
	if !profile.Transcription.Local() && os.Getenv(profile.Transcription.TokenEnv) == "" {
		return nil, fmt.Errorf("missing transcription API key environment variable: %w", model.ErrInvalidState)
	}
	modelFingerprint, err := profile.Transcription.ModelFingerprint(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s: %w", err, model.ErrInvalidState)
	}
	var req struct {
		MediaID string `json:"media_id"`
	}
	if err := decodeSkillArgs(args, &req); err != nil {
		return nil, err
	}
	for _, m := range snap.Media {
		if m.ID != req.MediaID {
			continue
		}
		dir, err := s.skillRunDir(*r)
		if err != nil {
			return nil, err
		}
		// Immutable per-content cache. No repeat API call for the same source bytes.
		identity := []string{m.ID, m.SHA, r.ProfileSHA}
		if modelFingerprint != "" {
			identity = append(identity, modelFingerprint)
		}
		cache := filepath.Join(dir, "transcript-"+skillHash(identity)+".json")
		raw, err := os.ReadFile(cache)
		if os.IsNotExist(err) {
			path, _, e := s.deliveryPath(filepath.FromSlash(snap.Paths[m.ID]), false)
			if e != nil {
				return nil, e
			}
			raw, e = s.skillProcessWithPython(ctx, "transcribe", map[string]any{"path": path, "start": m.In, "end": m.Out, "output": cache, "provider": profile.Transcription, "model_fingerprint": modelFingerprint}, profile.Transcription.PythonExecutable)
			if e != nil {
				return nil, e
			}
			if modelFingerprint != "" {
				after, e := profile.Transcription.ModelFingerprint(ctx)
				if e != nil || after != modelFingerprint {
					_ = os.Remove(cache)
					return nil, fmt.Errorf("local ASR model changed during transcription: %w", model.ErrConflict)
				}
			}
		} else if err != nil {
			return nil, err
		}
		if len(raw) > 2<<20 {
			return nil, model.ErrArgument
		}
		a, e := s.registerSkillArtifact(r, cache, "transcript.json", "transcript", 0)
		if e != nil {
			return nil, e
		}
		return json.Marshal(map[string]any{"media_id": m.ID, "transcript": json.RawMessage(raw), "artifact": a})
	}
	return nil, model.ErrNotFound
}
func (s *Service) renderSkillPlan(ctx context.Context, r *skill.Run, snap harnessSnapshot) error {
	if err := validateHarnessPlan(*r.Plan, snap); err != nil {
		return err
	}
	dir, err := s.skillRunDir(*r)
	if err != nil {
		return err
	}
	out := filepath.Join(dir, newID("render"))
	if err = os.Mkdir(out, 0700); err != nil {
		return err
	}
	rows := []map[string]any{}
	for index, seg := range r.Plan.Segments {
		path, _, e := s.deliveryPath(filepath.FromSlash(snap.Paths[seg.MediaID]), false)
		if e != nil {
			return e
		}
		row := map[string]any{"path": path, "media_id": seg.MediaID, "source_ref": snap.Paths[seg.MediaID], "start": seg.Start, "end": seg.End, "narration": seg.Narration, "subtitles_enabled": scriptOption(seg.SubtitlesEnabled, r.Plan.Subtitles)}
		if seg.TTSEnabled && seg.Narration != "" {
			if seg.Voice == nil {
				return model.ErrArgument
			}
			voiceFile := filepath.Join(out, fmt.Sprintf("voice-%03d.wav", index+1))
			voice := ScriptSegment{Narration: seg.Narration, Start: seg.Start, End: seg.End, Voice: &ScriptVoice{Settings: *seg.Voice, OffsetSeconds: seg.VoiceOffset, AcceptExternal: seg.VoiceConsent}}
			if e := s.synthesizeScriptVoice(ctx, voice, voiceFile); e != nil {
				return e
			}
			row["voice_file"] = voiceFile
			row["voice_offset"] = seg.VoiceOffset
		}
		rows = append(rows, row)
	}
	_, err = s.scriptProcess(ctx, true, "render", map[string]any{"script": map[string]any{"script_id": r.ID, "version": r.Plan.Version, "visible_set_sha": r.VisibleSHA, "finalized_by": r.Plan.FinalizedBy, "finalized_at": r.Plan.FinalizedAt}, "segments": rows, "document": scriptsyntax.Parse(r.Plan.Body), "subtitles_enabled": r.Plan.Subtitles, "sources": []any{}, "output_dir": out})
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(s.deliveryRoot, out)
	delivery, err := s.verifyScriptDelivery(ctx, filepath.ToSlash(rel), r.ID, r.Plan.Version, r.VisibleSHA)
	if err != nil {
		return err
	}
	artifact, err := s.registerSkillArtifact(r, filepath.Join(out, "final.mp4"), "final.mp4", "mp4", r.Plan.Version)
	if err != nil {
		return err
	}
	if len(delivery.BasicQuality) != 0 && delivery.SHA256 == artifact.SHA {
		r.Artifacts[len(r.Artifacts)-1].BasicQuality = delivery.BasicQuality
	}
	zipPath := filepath.Join(dir, newID("delivery")+".zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	err = filepath.WalkDir(out, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return model.ErrForbidden
		}
		if d.IsDir() {
			return nil
		}
		name, e := filepath.Rel(out, path)
		if e != nil {
			return e
		}
		w, e := zw.Create(filepath.ToSlash(name))
		if e != nil {
			return e
		}
		src, e := os.Open(path)
		if e != nil {
			return e
		}
		defer src.Close()
		_, e = io.Copy(w, src)
		return e
	})
	closeZip := zw.Close()
	closeFile := f.Close()
	if err != nil {
		return err
	}
	if closeZip != nil {
		return closeZip
	}
	if closeFile != nil {
		return closeFile
	}
	_, err = s.registerSkillArtifact(r, zipPath, "delivery.zip", "premiere_package", r.Plan.Version)
	return err
}
