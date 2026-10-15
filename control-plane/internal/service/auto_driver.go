package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/autoreview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// errRerunAssetBusy means another run holds the asset, so a rerun cannot
// start. The transaction that met it rolls back whole; the caller then records
// awaiting_user_decision(asset_busy) in a transaction of its own.
var errRerunAssetBusy = errors.New("service: another run holds the asset")

// autoStepsPerTick bounds how many steps one run takes in a single tick, so one
// busy run cannot hold the loop.
const autoStepsPerTick = 16

// autoGuard is the check every auto action makes inside its own transaction
// against the overlay it has just read: still active, in scope, inside the
// clock and the action budget, and the caller's view of the overlay is current.
func autoGuard(a model.WorkflowAuto, inScope bool, autoVersion int, now time.Time) error {
	switch {
	case a.State != model.AutoStateActive:
		return fmt.Errorf("service: auto run is %s: %w", a.State, model.ErrForbidden)
	case !inScope:
		return fmt.Errorf("service: the action is outside the authorized scope: %w", model.ErrForbidden)
	case now.After(a.DeadlineAt):
		return fmt.Errorf("service: auto run is past its deadline: %w", model.ErrForbidden)
	case a.ActionsUsed >= a.Scope.MaxActions:
		return fmt.Errorf("service: auto action budget is spent: %w", model.ErrForbidden)
	case a.AutoVersion != autoVersion:
		return fmt.Errorf("service: auto version conflict: %w", model.ErrConflict)
	}
	return nil
}

func autoCanRerun(a model.WorkflowAuto) bool {
	return a.Round < a.MaxRounds && a.TotalRounds < a.MaxTotalRounds &&
		a.ActionsUsed < a.Scope.MaxActions && a.TTSCharsUsed < a.Scope.MaxTTSChars
}

func autoDecisionTimeout(a model.WorkflowAuto) time.Duration {
	sec := a.Scope.DecisionTimeoutSeconds
	if sec <= 0 {
		sec = defaultAutoDecisionTimeout
	}
	return time.Duration(sec) * time.Second
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// ---------------------------------------------------------------- preview ---

// AutoStartPreview starts the preview of the current export for an auto run.
// It is the only way the auto-reviewer starts a preview. The ledger row, the
// overlay update and the new job commit together; asking again for the same
// round returns the same job and writes nothing. The cost is one action.
//
// This is the boundary with B4: B3 calls it from the driver; B4 owns the
// end-to-end exercise of the whole chain with real workers.
func (s *Service) AutoStartPreview(ctx context.Context, runID string, version, autoVersion int, revisionID string) (preview.View, bool, error) {
	a0, err := s.st.GetWorkflowAuto(ctx, runID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return preview.View{}, false, fmt.Errorf("service: run %s is not an auto run: %w", runID, model.ErrForbidden)
		}
		return preview.View{}, false, err
	}
	// total_rounds, not round: a manual revise changes only total_rounds, and
	// its preview must not collide with the previous round's request.
	reqID := fmt.Sprintf("auto:%s:%d:preview", runID, a0.TotalRounds)
	req := preview.StartRequest{RevisionID: revisionID, ExpectedVersion: version, PresetID: preview.PresetID, RequestID: reqID}
	var saved model.WorkflowAuto
	view, created, err := s.startPreview(ctx, model.AutoActor, runID, req, func(ctx context.Context, tx *store.Store) error {
		a, err := tx.GetWorkflowAuto(ctx, runID)
		if err != nil {
			return err
		}
		now := s.autoNow()
		if err := autoGuard(a, a.Scope.AutoPreviewExport, autoVersion, now); err != nil {
			return err
		}
		if applied, err := tx.InsertAutoAction(ctx, runID, reqID, model.AutoActionPreview, revisionID, now); err != nil {
			return err
		} else if !applied {
			return fmt.Errorf("service: preview action already recorded: %w", model.ErrConflict)
		}
		a.ActionsUsed++
		a.AutoVersion++
		a.UpdatedAt = now
		if err := tx.UpdateWorkflowAuto(ctx, a); err != nil {
			return err
		}
		if err := auditTx(ctx, tx, model.AutoActor, "auto.preview", runID, "revision="+revisionID); err != nil {
			return err
		}
		saved = a
		return nil
	})
	if err != nil {
		return preview.View{}, false, err
	}
	if created {
		s.publishAuto(saved)
	}
	return view, created, nil
}

// -------------------------------------------------------------- narration ---

var (
	autoBannedPattern = regexp.MustCompile(`(?i)https?://|www\.|[a-z0-9-]+\.(com|cn|net|org|tv|cc|top|xyz)\b|@|\d{7,}`)
	autoBannedWords   = []string{"微信", "加群", "私信", "扫码", "二维码", "外挂", "代练", "充值", "优惠码", "wechat", "telegram", "qq群"}
)

// autoNarrationBanned reports whether a line carries a URL, an address, a long
// number (a phone or an account) or a word on the fixed list.
func autoNarrationBanned(text string) bool {
	if autoBannedPattern.MatchString(text) {
		return true
	}
	lower := strings.ToLower(text)
	for _, w := range autoBannedWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// autoNarrationCode returns the feedback code of the first reason the lines
// cannot be approved, or "" when every line passes. Structure (window inside
// the clip, source scene exists) is validateWorkflowEdit's job and is run first
// by the caller.
func autoNarrationCode(edl map[string]any, confirmed map[string]bool, skip map[string]bool, paceMax float64) string {
	lines := asMapSlice(edl["narration"])
	if len(lines) == 0 {
		return autoreview.CodeTTSIncomplete
	}
	labels := map[string]string{}
	for _, sc := range asMapSlice(edl["scenes"]) {
		id, _ := sc["scene_id"].(string)
		label, _ := sc["label"].(string)
		labels[id] = label
	}
	type window struct{ start, end float64 }
	var wins []window
	code := ""
	for _, line := range lines {
		id, _ := line["id"].(string)
		start, _ := number(line["start"])
		end, _ := number(line["end"])
		wins = append(wins, window{start, end})
		if skip[id] {
			continue
		}
		text, _ := line["text"].(string)
		scene, _ := line["source_scene_id"].(string)
		label, _ := line["source_scene_label"].(string)
		switch {
		case strings.TrimSpace(text) == "" || strings.ContainsAny(text, "\r\n"):
			code = firstCode(code, autoreview.CodeItemBelowFloor)
		case float64(utf8.RuneCountInString(text))/(end-start) > paceMax:
			code = firstCode(code, autoreview.CodePacingOutOfRange)
		case label == "" || labels[scene] != label || !confirmed[scene]:
			code = firstCode(code, autoreview.CodeItemBelowFloor)
		case autoNarrationBanned(text):
			code = firstCode(code, autoreview.CodeItemBelowFloor)
		}
	}
	sort.Slice(wins, func(i, j int) bool { return wins[i].start < wins[j].start })
	for i := 1; i < len(wins); i++ {
		if wins[i].start < wins[i-1].end-1e-9 {
			code = firstCode(code, autoreview.CodeItemBelowFloor)
		}
	}
	return code
}

func firstCode(have, next string) string {
	if have != "" {
		return have
	}
	return next
}

// sceneConfirmations returns which scenes of the run are confirmed along the
// revision lineage of rev (the narrate revision, its sort parent, its
// recognize parent): the nearest decision for a scene wins.
func sceneConfirmations(ctx context.Context, tx *store.Store, runID string, rev model.Revision) (map[string]bool, error) {
	out := map[string]bool{}
	decided := map[string]bool{}
	cur := rev
	for i := 0; i < 8; i++ {
		rows, err := tx.ListSceneReviews(ctx, runID, cur.RevisionID)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if !decided[r.SceneID] {
				decided[r.SceneID] = true
				out[r.SceneID] = r.Decision == "confirmed"
			}
		}
		if cur.ParentRevisionID == "" {
			break
		}
		if cur, err = tx.GetRevision(ctx, cur.ParentRevisionID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AutoApproveNarration approves every pending narration line of the current
// revision in one transaction, then opens the TTS stage like the last human
// approval would. Lines a person already approved with the same text are
// satisfied and skipped; a person's approval of different text halts the run
// (human_decision_conflict). If any pending line fails the fixed criteria, none
// is approved and an AutoPolicyError names the reason, which the driver turns
// into a rerun with feedback.
func (s *Service) AutoApproveNarration(ctx context.Context, runID string, version, autoVersion int, revisionID string) (model.WorkflowRun, error) {
	a0, err := s.st.GetWorkflowAuto(ctx, runID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.WorkflowRun{}, fmt.Errorf("service: run %s is not an auto run: %w", runID, model.ErrForbidden)
		}
		return model.WorkflowRun{}, err
	}
	// As for scene confirmation, a committed action is a success on replay,
	// decided before any check that describes the world the action changed.
	replayKey := fmt.Sprintf("auto:%s:%d:%s:%s", runID, a0.TotalRounds, model.AutoActionApprove, revisionID)
	if done, err := s.st.AutoActionExists(ctx, runID, replayKey); err != nil {
		return model.WorkflowRun{}, err
	} else if done {
		return s.st.GetWorkflow(ctx, runID)
	}
	var saved model.WorkflowRun
	var savedAuto model.WorkflowAuto
	clashed := false
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		a, err := tx.GetWorkflowAuto(ctx, runID)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				return fmt.Errorf("service: run %s is not an auto run: %w", runID, model.ErrForbidden)
			}
			return err
		}
		key := fmt.Sprintf("auto:%s:%d:%s:%s", runID, a.TotalRounds, model.AutoActionApprove, revisionID)
		if done, err := tx.AutoActionExists(ctx, runID, key); err != nil {
			return err
		} else if done {
			cur, err := tx.GetWorkflow(ctx, runID)
			saved = cur
			return err
		}
		run, rev, edl, err := s.lockRun(ctx, tx, runID, version, revisionID)
		if err != nil {
			return err
		}
		now := s.autoNow()
		if err := autoGuard(a, a.Scope.AutoNarrationApprove, autoVersion, now); err != nil {
			return err
		}
		if run.Stage != "draft_review" || run.Status != model.WorkflowAwaitingReview {
			return fmt.Errorf("workflow is not at draft review: %w", model.ErrInvalidState)
		}
		if run.ContentMode != "configured" {
			return &AutoPolicyError{Code: autoreview.CodeItemBelowFloor}
		}
		if err := s.ensureIdle(ctx, tx, run); err != nil {
			return err
		}
		lines := asMapSlice(edl["narration"])
		if len(lines) == 0 {
			return &AutoPolicyError{Code: autoreview.CodeTTSIncomplete}
		}
		if err := validateWorkflowEdit(edl); err != nil {
			return &AutoPolicyError{Code: autoreview.CodeItemBelowFloor}
		}
		th, err := autoreview.ResolveThresholds(a.Scope.Thresholds)
		if err != nil {
			return fmt.Errorf("service: stored auto thresholds are invalid: %w", model.ErrInvalidState)
		}
		rows, err := tx.ListNarrationApprovalRows(ctx, run.RunID, rev.RevisionID)
		if err != nil {
			return err
		}
		type pendingLine struct{ id, hash string }
		var pending []pendingLine
		skip := map[string]bool{}
		var skipped []string
		chars := 0
		for _, line := range lines {
			id, _ := line["id"].(string)
			start, _ := number(line["start"])
			end, _ := number(line["end"])
			text, _ := line["text"].(string)
			chars += utf8.RuneCountInString(text)
			hash := NarrationDraftHash(text, start, end, narrationSource(line))
			if row, ok := rows[id]; ok && row.Actor != model.AutoActor {
				if row.Hash == hash {
					skip[id] = true
					skipped = append(skipped, id)
					continue
				}
				// A person approved other text for this line: their decision and
				// ours disagree, so the person wins and the run stops.
				basis, _ := json.Marshal(map[string]any{"narration_id": id, "human_hash": row.Hash, "draft_hash": hash})
				if err := s.applyAutoTransition(ctx, tx, &a, autoTransition{state: model.AutoStateHalted, reason: model.AutoHaltHumanDecisionClash,
					kind: model.AutoDecisionHalt, target: id, basis: basis, audit: "auto.halt"}, now); err != nil {
					return err
				}
				savedAuto, clashed = a, true
				return nil
			}
			pending = append(pending, pendingLine{id, hash})
		}
		if a.TTSCharsUsed+chars > a.Scope.MaxTTSChars {
			return &AutoPolicyError{Code: model.AutoHaltMaxTTSChars}
		}
		confirmed, err := sceneConfirmations(ctx, tx, run.RunID, rev)
		if err != nil {
			return err
		}
		if code := autoNarrationCode(edl, confirmed, skip, th.PaceMax); code != "" {
			return &AutoPolicyError{Code: code}
		}
		written := 0
		for _, p := range pending {
			applied, err := tx.InsertNarrationApprovalAuto(ctx, run.RunID, rev.RevisionID, p.id, p.hash, now)
			if err != nil {
				return err
			}
			if !applied {
				skipped = append(skipped, p.id) // a person's row appeared: satisfied, not overwritten
				continue
			}
			written++
			basis, _ := json.Marshal(map[string]any{"draft_hash": p.hash})
			if err := tx.InsertAutoDecision(ctx, model.AutoDecision{RunID: runID, Round: a.Round, Kind: model.AutoDecisionNarration, Target: p.id, Actor: model.AutoActor, Basis: basis, CreatedAt: now}); err != nil {
				return err
			}
		}
		for _, id := range skipped {
			if err := tx.InsertAutoDecision(ctx, model.AutoDecision{RunID: runID, Round: a.Round, Kind: model.AutoDecisionSkipHuman, Target: id, Actor: rows[id].Actor, CreatedAt: now}); err != nil {
				return err
			}
		}
		if _, err := tx.InsertAutoAction(ctx, runID, key, model.AutoActionApprove, revisionID, now); err != nil {
			return err
		}
		if err := s.openTTSIfReady(ctx, tx, &run, edl); err != nil {
			return err
		}
		run.Version++
		run.UpdatedAt = now
		if err := tx.UpdateWorkflow(ctx, run); err != nil {
			return err
		}
		a.ActionsUsed++
		a.TTSCharsUsed += chars
		a.AutoVersion++
		a.UpdatedAt = now
		if err := tx.UpdateWorkflowAuto(ctx, a); err != nil {
			return err
		}
		if err := auditTx(ctx, tx, model.AutoActor, "auto.narration_approve", runID,
			fmt.Sprintf("revision=%s approved=%d skipped_human=%d chars=%d", revisionID, written, len(skipped), chars)); err != nil {
			return err
		}
		saved, savedAuto = run, a
		return nil
	})
	if err != nil {
		return model.WorkflowRun{}, err
	}
	if clashed {
		s.publishAuto(savedAuto)
		return model.WorkflowRun{}, fmt.Errorf("service: a person approved different text for a narration line: %w", model.ErrConflict)
	}
	if savedAuto.RunID != "" {
		s.publish("review.changed", saved)
		s.publishAuto(savedAuto)
	}
	return saved, nil
}

// ------------------------------------------------------------ transitions ---

type autoTransition struct {
	state, reason string
	hold          *bool
	wait          bool // entering awaiting_user_decision: pause the wall clock
	kind, target  string
	basis         any
	audit         string
}

func boolPtr(b bool) *bool { return &b }

// applyAutoTransition moves the overlay to a new state inside tx and records
// the decision and audit row. It writes the overlay, so the caller must not.
func (s *Service) applyAutoTransition(ctx context.Context, tx *store.Store, a *model.WorkflowAuto, t autoTransition, now time.Time) error {
	a.State, a.HaltReason = t.state, t.reason
	if t.hold != nil {
		a.DeliveryHold = *t.hold
	}
	if t.wait {
		paused, deadline := now, now.Add(autoDecisionTimeout(*a))
		a.PausedAt, a.DecisionDeadlineAt = &paused, &deadline
	} else {
		a.PausedAt, a.DecisionDeadlineAt = nil, nil
	}
	a.AutoVersion++
	a.UpdatedAt = now
	if err := tx.UpdateWorkflowAuto(ctx, *a); err != nil {
		return err
	}
	basis, _ := json.Marshal(t.basis)
	if t.basis == nil {
		basis = []byte("{}")
	}
	if err := tx.InsertAutoDecision(ctx, model.AutoDecision{RunID: a.RunID, Round: a.Round, Kind: t.kind, Target: t.target, Actor: model.AutoActor, Basis: basis, CreatedAt: now}); err != nil {
		return err
	}
	return auditTx(ctx, tx, model.AutoActor, t.audit, a.RunID, t.state+" "+t.reason)
}

// autoHalt stops a live auto run for a reason the driver found (clock, budget,
// a failed stage). expectedVersion makes it a no-op for a run somebody else
// has changed since the driver looked.
func (s *Service) autoHalt(ctx context.Context, runID string, expectedVersion int, reason string) error {
	var saved model.WorkflowAuto
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		a, err := tx.GetWorkflowAuto(ctx, runID)
		if err != nil {
			return err
		}
		if !autoStateLive(a.State) || a.AutoVersion != expectedVersion {
			return fmt.Errorf("service: auto run changed: %w", model.ErrConflict)
		}
		if err := s.applyAutoTransition(ctx, tx, &a, autoTransition{state: model.AutoStateHalted, reason: reason,
			kind: model.AutoDecisionHalt, basis: map[string]any{"reason": reason}, audit: "auto.halt"}, s.autoNow()); err != nil {
			return err
		}
		saved = a
		return nil
	})
	if err != nil {
		return err
	}
	s.publishAuto(saved)
	return nil
}

// ------------------------------------------------------------------ rerun ---

type rerunSpec struct {
	manual   bool   // a user's revise: counts total_rounds only, not round or actions
	key      string // ledger key (auto reruns)
	target   string
	feedback string // JSON for workflow_auto_rounds.feedback
	outcome  string // stamped on the round that ends
}

// applyAutoRerun sends the run back to recognize on its base revision. It
// changes the run, the asset and the rounds table inside tx and updates a in
// memory; writing the overlay and the decision is the caller's.
//
// It returns errRerunAssetBusy (Rule B, or Rule C when the unique index of one
// live run per asset refuses the update) and ErrInvalidState when a cap is
// reached. Both leave nothing behind once the caller rolls back.
func (s *Service) applyAutoRerun(ctx context.Context, tx *store.Store, a *model.WorkflowAuto, spec rerunSpec, now time.Time) (model.WorkflowRun, error) {
	run, err := tx.GetWorkflow(ctx, a.RunID)
	if err != nil {
		return model.WorkflowRun{}, err
	}
	if run.Status != model.WorkflowAwaitingReview && run.Status != model.WorkflowReadyAcceptance {
		return model.WorkflowRun{}, fmt.Errorf("service: workflow is %s, not waiting: %w", run.Status, model.ErrInvalidState)
	}
	if a.TotalRounds >= a.MaxTotalRounds || a.ActionsUsed >= a.Scope.MaxActions || a.TTSCharsUsed >= a.Scope.MaxTTSChars ||
		(!spec.manual && a.Round >= a.MaxRounds) {
		return model.WorkflowRun{}, fmt.Errorf("service: no budget left for another round: %w", model.ErrInvalidState)
	}
	if err := s.ensureIdle(ctx, tx, run); err != nil {
		return model.WorkflowRun{}, err
	}
	if other, err := tx.ActiveWorkflow(ctx, run.AssetID); err == nil && other.RunID != run.RunID {
		return model.WorkflowRun{}, errRerunAssetBusy
	} else if err != nil && !errors.Is(err, model.ErrNotFound) {
		return model.WorkflowRun{}, err
	}
	asset, err := tx.GetAsset(ctx, run.AssetID)
	if err != nil {
		return model.WorkflowRun{}, err
	}
	if asset.Status == model.AssetStatusExported {
		asset.Status = model.AssetStatusIngested
		if err := tx.UpdateAsset(ctx, asset); err != nil {
			return model.WorkflowRun{}, err
		}
	}
	if err := s.invalidateFrom(ctx, tx, &run, model.TaskTypeRecognize); err != nil {
		return model.WorkflowRun{}, err
	}
	if err := tx.CloseAutoRound(ctx, a.RunID, a.TotalRounds, spec.outcome, now); err != nil {
		return model.WorkflowRun{}, err
	}
	round := a.TotalRounds + 1
	if err := tx.InsertAutoRound(ctx, model.AutoRound{RunID: a.RunID, Round: round, BaseRevisionID: run.BaseRevisionID, Feedback: spec.feedback, StartedAt: now}); err != nil {
		return model.WorkflowRun{}, err
	}
	// NoteWorkflowSuccess accepts a stage only if its input is the current
	// revision, so the rerun starts again from the base revision.
	run.CurrentRevisionID = run.BaseRevisionID
	run.Status, run.Stage = model.WorkflowRunning, model.TaskTypeRecognize
	run.BlockedReason, run.ErrorCode = "", ""
	if err := s.queueStage(ctx, tx, run, model.TaskTypeRecognize, run.BaseRevisionID, ""); err != nil {
		return model.WorkflowRun{}, err
	}
	run.Version++
	run.UpdatedAt = now
	if err := tx.UpdateWorkflow(ctx, run); err != nil {
		if isUniqueViolation(err) {
			return model.WorkflowRun{}, errRerunAssetBusy
		}
		return model.WorkflowRun{}, err
	}
	a.TotalRounds = round
	if !spec.manual {
		a.Round++
		a.ActionsUsed++
		if applied, err := tx.InsertAutoAction(ctx, a.RunID, spec.key, model.AutoActionRerun, spec.target, now); err != nil {
			return model.WorkflowRun{}, err
		} else if !applied {
			return model.WorkflowRun{}, fmt.Errorf("service: rerun already recorded: %w", model.ErrConflict)
		}
	} else {
		a.DeadlineAt = now.Add(time.Duration(a.Scope.MaxWallSeconds) * time.Second)
	}
	a.State, a.HaltReason = model.AutoStateActive, ""
	a.PausedAt, a.DecisionDeadlineAt = nil, nil
	return run, nil
}

func autoFeedbackJSON(items []model.AutoFeedbackItem, user string) string {
	if items == nil {
		items = []model.AutoFeedbackItem{}
	}
	texts := autoreview.RenderFeedbackAll(items)
	if texts == nil {
		texts = []string{}
	}
	body, _ := json.Marshal(map[string]any{"codes": items, "texts": texts, "user": user})
	return string(body)
}

// --------------------------------------------------------------- routing ---

// AutoRouteReview sends a scored run where its band says. HIGH completes the
// auto run; MEDIUM waits for the user with the delivery held; LOW starts a
// bounded rerun when the reasons are improvable and the caps allow it, waits
// for the user when the caps are spent, and halts when a rerun cannot help.
// Everything, a rerun included, commits in one transaction guarded by the
// auto version, so a stale or repeated call changes nothing.
func (s *Service) AutoRouteReview(ctx context.Context, runID, reviewID string, autoVersion int) (model.WorkflowAuto, error) {
	out, run, err := s.routeReview(ctx, runID, reviewID, autoVersion, false)
	if errors.Is(err, errRerunAssetBusy) {
		out, run, err = s.routeReview(ctx, runID, reviewID, autoVersion, true)
	}
	if err != nil {
		return model.WorkflowAuto{}, err
	}
	if run != nil {
		s.publish("workflow.changed", *run)
	}
	s.publishAuto(out)
	return out, nil
}

func (s *Service) routeReview(ctx context.Context, runID, reviewID string, autoVersion int, assetBusy bool) (model.WorkflowAuto, *model.WorkflowRun, error) {
	var out model.WorkflowAuto
	var rerun *model.WorkflowRun
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		a, err := tx.GetWorkflowAuto(ctx, runID)
		if err != nil {
			return err
		}
		rv, err := tx.GetAutoReview(ctx, runID, reviewID)
		if err != nil {
			return err
		}
		run, err := tx.GetWorkflow(ctx, runID)
		if err != nil {
			return err
		}
		now := s.autoNow()
		switch {
		case a.State != model.AutoStateActive:
			return fmt.Errorf("service: auto run is %s: %w", a.State, model.ErrForbidden)
		case now.After(a.DeadlineAt):
			return fmt.Errorf("service: auto run is past its deadline: %w", model.ErrForbidden)
		case a.AutoVersion != autoVersion || rv.Round != a.Round:
			return fmt.Errorf("service: auto version conflict: %w", model.ErrConflict)
		case run.Status != model.WorkflowReadyAcceptance:
			return fmt.Errorf("service: workflow is %s, not at acceptance: %w", run.Status, model.ErrInvalidState)
		}
		current, err := s.previewRevisionID(ctx, tx, runID)
		if err != nil {
			return err
		}
		if current != rv.RevisionID {
			return fmt.Errorf("service: the review is not of the current export: %w", model.ErrConflict)
		}
		basis := map[string]any{"review_id": rv.ReviewID, "band": rv.Band, "score": rv.Score, "scorer_version": rv.ScorerVersion}
		route := func(t autoTransition) error {
			t.kind, t.target, t.basis, t.audit = model.AutoDecisionRoute, rv.Band, basis, "auto.route"
			return s.applyAutoTransition(ctx, tx, &a, t, now)
		}
		switch rv.Band {
		case autoreview.BandHigh:
			if err := route(autoTransition{state: model.AutoStateCompleted, hold: boolPtr(false)}); err != nil {
				return err
			}
		case autoreview.BandMedium:
			if err := route(autoTransition{state: model.AutoStateAwaitingDecision, reason: model.AutoWaitMedium, hold: boolPtr(true), wait: true}); err != nil {
				return err
			}
		case autoreview.BandLow:
			var inputs struct {
				Improvable bool `json:"improvable"`
			}
			_ = json.Unmarshal(rv.Inputs, &inputs)
			var reasons []autoreview.Reason
			_ = json.Unmarshal(rv.Reasons, &reasons)
			switch {
			case !inputs.Improvable:
				if err := route(autoTransition{state: model.AutoStateHalted, reason: model.AutoHaltNotImprovable}); err != nil {
					return err
				}
			case !autoCanRerun(a):
				if err := route(autoTransition{state: model.AutoStateAwaitingDecision, reason: model.AutoWaitExhausted, hold: boolPtr(true), wait: true}); err != nil {
					return err
				}
			case assetBusy:
				if err := route(autoTransition{state: model.AutoStateAwaitingDecision, reason: model.AutoWaitAssetBusy, hold: boolPtr(true), wait: true}); err != nil {
					return err
				}
			default:
				items := autoreview.FeedbackItems(reasons)
				r, err := s.startAutoRerun(ctx, tx, &a, run.RunID, fmt.Sprintf("auto:%s:%d:rerun:%s", runID, a.TotalRounds, rv.ReviewID), rv.ReviewID, items, autoreview.BandLow, now)
				if err != nil {
					return err
				}
				rerun = &r
			}
		default:
			return fmt.Errorf("service: unknown review band %q: %w", rv.Band, model.ErrInvalidState)
		}
		out = a
		return nil
	})
	return out, rerun, err
}

// startAutoRerun runs the rerun, writes the overlay and the decision.
func (s *Service) startAutoRerun(ctx context.Context, tx *store.Store, a *model.WorkflowAuto, runID, key, target string, items []model.AutoFeedbackItem, why string, now time.Time) (model.WorkflowRun, error) {
	run, err := s.applyAutoRerun(ctx, tx, a, rerunSpec{key: key, target: target, feedback: autoFeedbackJSON(items, ""), outcome: "rerun:" + why}, now)
	if err != nil {
		return model.WorkflowRun{}, err
	}
	a.AutoVersion++
	a.UpdatedAt = now
	if err := tx.UpdateWorkflowAuto(ctx, *a); err != nil {
		return model.WorkflowRun{}, err
	}
	basis, _ := json.Marshal(map[string]any{"reason": why, "feedback": items, "round": a.Round, "total_rounds": a.TotalRounds})
	if err := tx.InsertAutoDecision(ctx, model.AutoDecision{RunID: runID, Round: a.Round, Kind: model.AutoDecisionRerun, Target: target, Actor: model.AutoActor, Basis: basis, CreatedAt: now}); err != nil {
		return model.WorkflowRun{}, err
	}
	if err := auditTx(ctx, tx, model.AutoActor, "auto.rerun", runID, fmt.Sprintf("round=%d total=%d reason=%s", a.Round, a.TotalRounds, why)); err != nil {
		return model.WorkflowRun{}, err
	}
	return run, nil
}

// AutoRerunForPolicy handles a stage the fixed criteria refused (scenes or
// narration): the same bounded rerun a LOW score gets, or a wait for the user
// when the caps are spent.
func (s *Service) AutoRerunForPolicy(ctx context.Context, runID string, autoVersion int, codes []string) error {
	err := s.rerunForPolicy(ctx, runID, autoVersion, codes, false)
	if errors.Is(err, errRerunAssetBusy) {
		err = s.rerunForPolicy(ctx, runID, autoVersion, codes, true)
	}
	return err
}

func (s *Service) rerunForPolicy(ctx context.Context, runID string, autoVersion int, codes []string, assetBusy bool) error {
	var out model.WorkflowAuto
	var rerun *model.WorkflowRun
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		a, err := tx.GetWorkflowAuto(ctx, runID)
		if err != nil {
			return err
		}
		now := s.autoNow()
		if err := autoGuard(a, true, autoVersion, now); err != nil {
			return err
		}
		run, err := tx.GetWorkflow(ctx, runID)
		if err != nil {
			return err
		}
		var items []model.AutoFeedbackItem
		for _, c := range codes {
			if model.AutoFeedbackCodes[c] {
				items = append(items, model.AutoFeedbackItem{Code: c})
			}
		}
		wait := func(reason string) error {
			return s.applyAutoTransition(ctx, tx, &a, autoTransition{state: model.AutoStateAwaitingDecision, reason: reason, hold: boolPtr(true), wait: true,
				kind: model.AutoDecisionRoute, target: "policy", basis: map[string]any{"codes": codes}, audit: "auto.route"}, now)
		}
		switch {
		case !autoCanRerun(a):
			err = wait(model.AutoWaitExhausted)
		case assetBusy:
			err = wait(model.AutoWaitAssetBusy)
		default:
			var r model.WorkflowRun
			r, err = s.startAutoRerun(ctx, tx, &a, runID, fmt.Sprintf("auto:%s:%d:rerun:policy:%s", runID, a.TotalRounds, run.CurrentRevisionID), run.CurrentRevisionID, items, "policy", now)
			rerun = &r
		}
		if err != nil {
			return err
		}
		out = a
		return nil
	})
	if err != nil {
		return err
	}
	if rerun != nil {
		s.publish("workflow.changed", *rerun)
	}
	s.publishAuto(out)
	return nil
}

// ------------------------------------------------------------------ retry ---

// AutoRetryStage retries a failed stage once for the auto run, inside the
// per-stage retry cap. The ledger row, the overlay update and the new task
// commit together.
func (s *Service) AutoRetryStage(ctx context.Context, runID string, version, autoVersion int, stage string) (model.WorkflowRun, error) {
	a0, err := s.st.GetWorkflowAuto(ctx, runID)
	if err != nil {
		return model.WorkflowRun{}, err
	}
	n, err := s.st.CountAutoRetries(ctx, runID, stage)
	if err != nil {
		return model.WorkflowRun{}, err
	}
	key := fmt.Sprintf("auto:%s:%d:retry:%s:%d", runID, a0.TotalRounds, stage, n+1)
	var saved model.WorkflowAuto
	run, err := s.retryWorkflow(ctx, model.AutoActor, runID, stage, key, version, func(ctx context.Context, tx *store.Store) error {
		a, err := tx.GetWorkflowAuto(ctx, runID)
		if err != nil {
			return err
		}
		now := s.autoNow()
		if err := autoGuard(a, true, autoVersion, now); err != nil {
			return err
		}
		used, err := tx.CountAutoRetries(ctx, runID, stage)
		if err != nil {
			return err
		}
		if used >= a.Scope.MaxStageRetries {
			return fmt.Errorf("service: stage %s has no retries left: %w", stage, model.ErrForbidden)
		}
		if applied, err := tx.InsertAutoAction(ctx, runID, key, model.AutoActionRetry, stage, now); err != nil {
			return err
		} else if !applied {
			return fmt.Errorf("service: retry already recorded: %w", model.ErrConflict)
		}
		a.ActionsUsed++
		a.AutoVersion++
		a.UpdatedAt = now
		if err := tx.UpdateWorkflowAuto(ctx, a); err != nil {
			return err
		}
		if err := auditTx(ctx, tx, model.AutoActor, "auto.retry", runID, "stage="+stage); err != nil {
			return err
		}
		saved = a
		return nil
	})
	if err != nil {
		return model.WorkflowRun{}, err
	}
	if saved.RunID != "" {
		s.publishAuto(saved)
	}
	return run, nil
}

// ----------------------------------------------------------------- driver ---

// AutoTick advances every live auto run as far as it can go without waiting
// for a worker or a person. It is deterministic: all time comes from the
// service clock, all state from the database, so a restart repeats nothing.
// One failing run does not stop the others; their errors are joined.
func (s *Service) AutoTick(ctx context.Context) error {
	ids, err := s.st.ListLiveAutoRunIDs(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		for i := 0; i < autoStepsPerTick; i++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			progressed, err := s.advanceAuto(ctx, id)
			if err != nil {
				errs = append(errs, fmt.Errorf("auto run %s: %w", id, err))
				break
			}
			if !progressed {
				break
			}
		}
	}
	return errors.Join(errs...)
}

// advanceAuto takes at most one step on one run and reports whether it did.
// Losing a race (a person acted, another tick ran) is not an error: the run is
// looked at again next tick.
func (s *Service) advanceAuto(ctx context.Context, runID string) (bool, error) {
	progressed, err := s.stepAuto(ctx, runID)
	if errors.Is(err, model.ErrConflict) {
		return false, nil
	}
	return progressed, err
}

func (s *Service) stepAuto(ctx context.Context, runID string) (bool, error) {
	a, err := s.st.GetWorkflowAuto(ctx, runID)
	if err != nil {
		return false, err
	}
	now := s.autoNow()
	switch a.State {
	case model.AutoStateAwaitingDecision:
		if a.DecisionDeadlineAt != nil && now.After(*a.DecisionDeadlineAt) {
			return true, s.autoHalt(ctx, runID, a.AutoVersion, model.AutoHaltDecisionTimeout)
		}
		return false, nil
	case model.AutoStateActive:
	default:
		return false, nil
	}
	// The wall clock runs only while the run is active; waiting for the user
	// pauses it (see applyAutoTransition and the revise reset).
	if now.After(a.DeadlineAt) {
		return true, s.autoHalt(ctx, runID, a.AutoVersion, model.AutoHaltDeadline)
	}
	run, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return false, err
	}
	switch {
	case run.Status == model.WorkflowCancelled:
		return true, s.autoHalt(ctx, runID, a.AutoVersion, model.AutoHaltHumanTakeover)
	case run.Status == model.WorkflowFailed:
		return s.stepFailed(ctx, a, run)
	case run.Status == model.WorkflowAwaitingReview && run.Stage == "scene_review":
		if !a.Scope.AutoSceneConfirm {
			return false, nil
		}
		_, err := s.AutoConfirmScene(ctx, runID, run.Version, a.AutoVersion, run.CurrentRevisionID)
		return s.afterPolicyStep(ctx, a, err)
	case run.Status == model.WorkflowAwaitingReview && run.Stage == "draft_review":
		if !a.Scope.AutoNarrationApprove {
			return false, nil
		}
		_, err := s.AutoApproveNarration(ctx, runID, run.Version, a.AutoVersion, run.CurrentRevisionID)
		return s.afterPolicyStep(ctx, a, err)
	case run.Status == model.WorkflowReadyAcceptance:
		return s.stepAcceptance(ctx, a, run)
	}
	return false, nil // created or running: a worker owns the next step
}

// afterPolicyStep turns the result of an auto approval into driver progress: a
// refusal by the fixed criteria becomes a bounded rerun with feedback, a spent
// TTS budget halts, anything else is the caller's error.
func (s *Service) afterPolicyStep(ctx context.Context, a model.WorkflowAuto, err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	var pe *AutoPolicyError
	if !errors.As(err, &pe) {
		return false, err
	}
	switch {
	case pe.Code == model.AutoHaltMaxTTSChars:
		return true, s.autoHalt(ctx, a.RunID, a.AutoVersion, model.AutoHaltMaxTTSChars)
	case model.AutoFeedbackCodes[pe.Code]:
		return true, s.AutoRerunForPolicy(ctx, a.RunID, a.AutoVersion, []string{pe.Code})
	}
	return true, s.autoHalt(ctx, a.RunID, a.AutoVersion, model.AutoHaltNarrationRefused)
}

func (s *Service) stepFailed(ctx context.Context, a model.WorkflowAuto, run model.WorkflowRun) (bool, error) {
	n, err := s.st.CountAutoRetries(ctx, a.RunID, run.Stage)
	if err != nil {
		return false, err
	}
	if n >= a.Scope.MaxStageRetries || a.ActionsUsed >= a.Scope.MaxActions {
		return true, s.autoHalt(ctx, a.RunID, a.AutoVersion, model.AutoHaltStageFailed)
	}
	_, err = s.AutoRetryStage(ctx, a.RunID, run.Version, a.AutoVersion, run.Stage)
	return err == nil, err
}

// stepAcceptance is the end of the pipeline: preview the export, wait for the
// preview, score it, route. Each call does one of those.
func (s *Service) stepAcceptance(ctx context.Context, a model.WorkflowAuto, run model.WorkflowRun) (bool, error) {
	if !a.Scope.AutoPreviewExport {
		return false, nil
	}
	exportRev, err := s.getLatestExportRevisionID(ctx, run.RunID)
	if err != nil {
		return false, err
	}
	if exportRev == "" {
		return false, nil
	}
	jobs, err := s.st.ListPreviewJobs(ctx, run.RunID)
	if err != nil {
		return false, err
	}
	var job *model.PreviewJob
	for i := range jobs { // newest first
		if !jobs[i].Invalidated && jobs[i].RevisionID == exportRev {
			job = &jobs[i]
			break
		}
	}
	if job == nil {
		_, _, err := s.AutoStartPreview(ctx, run.RunID, run.Version, a.AutoVersion, exportRev)
		if errors.Is(err, model.ErrInvalidState) {
			// No worker can render the preview, or the asset is not exported.
			return true, s.autoHalt(ctx, run.RunID, a.AutoVersion, model.AutoHaltPreviewUnavailable)
		}
		return err == nil, err
	}
	view, err := s.projectPreviewView(ctx, *job, exportRev)
	if err != nil {
		return false, err
	}
	switch view.Status {
	case model.TaskStatusQueued, model.TaskStatusClaimed, model.TaskStatusRunning:
		return false, nil
	case model.TaskStatusSucceeded, model.TaskStatusFailed:
	default:
		return true, s.autoHalt(ctx, run.RunID, a.AutoVersion, model.AutoHaltPreviewUnavailable)
	}
	review, err := s.ScoreAutoRun(ctx, run.RunID, job.PreviewID, a.AutoVersion)
	if err != nil {
		return false, err
	}
	// ScoreAutoRun bumps the version unless it replayed a stored review.
	cur, err := s.st.GetWorkflowAuto(ctx, run.RunID)
	if err != nil {
		return false, err
	}
	_, err = s.AutoRouteReview(ctx, run.RunID, review.ReviewID, cur.AutoVersion)
	return err == nil, err
}
