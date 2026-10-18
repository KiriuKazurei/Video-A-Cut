package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/autoreview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// Auto-run mode, slice B1: authorization, revocation, the read model and the
// guarded scene confirmation. The driver that calls these on its own (the
// reconciler), narration approval, rerun and export arrive in later slices.

const (
	autoBriefMaxRunes = 2000
	autoHumanPrefix   = "human:"
)

// Default and upper bounds of the authorization scope. A request may narrow a
// default; it can never exceed a maximum.
const (
	defaultAutoMaxRounds         = 1
	maxAutoMaxRounds             = 2
	defaultAutoMaxTotalRounds    = 5
	maxAutoMaxTotalRounds        = 10
	defaultAutoMaxStageRetries   = 1
	defaultAutoMaxActions        = 200
	maxAutoMaxActions            = 1000
	defaultAutoMaxTTSChars       = 20000
	maxAutoMaxTTSChars           = 100000
	defaultAutoMaxWallSeconds    = 7200
	maxAutoMaxWallSeconds        = 86400
	defaultAutoDecisionTimeout   = 259200
	maxAutoDecisionTimeout       = 604800
	defaultAutoSceneConfidence   = 0.6
	autoThresholdSceneConfidence = "scene_confidence_min"
)

var autoThresholdKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

// AutoScopeRequest is the narrowing part of an authorization. Every field is
// optional; an omitted one takes the default.
type AutoScopeRequest struct {
	MaxRounds              *int               `json:"max_rounds"`
	MaxTotalRounds         *int               `json:"max_total_rounds"`
	MaxStageRetries        *int               `json:"max_stage_retries"`
	MaxActions             *int               `json:"max_actions"`
	MaxTTSChars            *int               `json:"max_tts_chars"`
	MaxWallSeconds         *int               `json:"max_wall_seconds"`
	DecisionTimeoutSeconds *int               `json:"decision_timeout_seconds"`
	Thresholds             map[string]float64 `json:"thresholds"`
	// RelaxThresholds is the explicit confirmation that thresholds looser than
	// the defaults are wanted. Without it they are refused.
	RelaxThresholds bool `json:"relax_thresholds"`
}

// AutoRunRequest is one authorization to run a prepared workflow unattended.
type AutoRunRequest struct {
	AssetID              string
	RevisionID           string
	ProfileID            string
	ProfileRevision      int
	ExpectedAssetVersion string
	Brief                string
	Scope                AutoScopeRequest
	AcceptExternalEgress bool
	NoticeSHA256         string
	// VisibleSetSHA is the fingerprint of the visible set the person just saw.
	// Omitting it is not an authorization of "whatever is visible".
	VisibleSetSHA  string
	IdempotencyKey string
}

// AutoView is the read model of GET /api/workflows/{run}/auto.
type AutoView struct {
	model.WorkflowAuto
	Rounds       []model.AutoRound    `json:"rounds"`
	LatestReview *model.AutoReview    `json:"latest_review"`
	Decisions    []model.AutoDecision `json:"decisions"`
	// Delivery says what the run has produced (package and 720p preview).
	Delivery AutoDelivery `json:"delivery"`
	// Decision is the open question while the run waits for the person.
	Decision *AutoDecisionPrompt `json:"decision"`
	// DeliveryHoldEnforced is always false in v1: delivery_hold is a label for
	// the UI ("待你确认"); delivery.zip and the preview files stay downloadable.
	DeliveryHoldEnforced bool `json:"delivery_hold_enforced"`
}

// AutoNoticeView is the fixed disclosure a human must see before authorizing.
type AutoNoticeView struct {
	ProfileID     string `json:"profile_id"`
	Revision      int    `json:"revision"`
	ProfileSHA256 string `json:"profile_sha256"`
	External      bool   `json:"external"`
	Notice        string `json:"notice"`
	NoticeSHA256  string `json:"notice_sha256"`
	// Visible-set lines are additive. The notice text and its hash stay a
	// function of the profile so existing disclosures keep their sha256.
	VisibleSetSHA string               `json:"visible_set_sha,omitempty"`
	VisibleAssets []model.VisibleAsset `json:"visible_assets,omitempty"`
	Grants        []AuthorizationGrant `json:"grants,omitempty"`
}

// AutoPolicyError reports that a run does not meet an automatic-approval
// criterion. It wraps model.ErrInvalidState; a later slice reads Code to turn
// it into a LOW assessment.
type AutoPolicyError struct{ Code string }

func (e *AutoPolicyError) Error() string {
	return "service: auto policy not met: " + e.Code
}
func (e *AutoPolicyError) Unwrap() error { return model.ErrInvalidState }

func isHumanActor(actor string) bool { return strings.HasPrefix(actor, autoHumanPrefix) }

func autoStateLive(state string) bool {
	return state == model.AutoStateActive || state == model.AutoStateAwaitingDecision
}

// validateBrief checks the user's requirement text. It is untrusted data, so
// the rules are about shape only: valid UTF-8, bounded length, no control
// characters (a newline is allowed). It is stored verbatim; there is no
// Unicode normalization because golang.org/x/text is not a dependency.
func validateBrief(brief string) (string, string, error) {
	brief = strings.TrimSpace(brief)
	if !utf8.ValidString(brief) || utf8.RuneCountInString(brief) > autoBriefMaxRunes {
		return "", "", fmt.Errorf("service: brief must be valid UTF-8 of at most %d characters: %w", autoBriefMaxRunes, model.ErrArgument)
	}
	for _, r := range brief {
		if unicode.IsControl(r) && r != '\n' {
			return "", "", fmt.Errorf("service: brief must not contain control characters: %w", model.ErrArgument)
		}
	}
	if brief == "" {
		return "", "", nil
	}
	sum := sha256.Sum256([]byte(brief))
	return brief, hex.EncodeToString(sum[:]), nil
}

func endpointLabel(endpoint, tokenEnv string) string {
	host := endpoint
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" {
		host = u.Host
	}
	if tokenEnv == "" {
		return host
	}
	return host + "（凭证变量 " + tokenEnv + "）"
}

// AutoNotice builds the disclosure text for a profile and its sha256. The text
// is deterministic in the profile, so the server can recompute it and compare
// with the hash the human confirmed.
func AutoNotice(p preparation.Profile, profileSHA string) (string, string) {
	var b strings.Builder
	external := p.External()
	if external {
		b.WriteString("自动模式将在无逐步确认的情况下完成剪辑、配音与字幕，并向以下外部端点发送数据：抽帧图像、场景标签、旁白文本和你的需求描述。\n")
	} else {
		b.WriteString("自动模式将在无逐步确认的情况下完成剪辑、配音与字幕。所有端点均为本机地址，不向外部发送数据。\n")
	}
	fmt.Fprintf(&b, "视觉识别：%s\n", endpointLabel(p.Vision.Endpoint, p.Vision.TokenEnv))
	fmt.Fprintf(&b, "解说生成：%s\n", endpointLabel(p.Narration.Endpoint, p.Narration.TokenEnv))
	if p.TTSTemplate != nil && p.TTSTemplate.Settings != nil && p.TTSTemplate.Settings.Engine == "stepfun" {
		fmt.Fprintf(&b, "语音合成：%s\n", endpointLabel(p.TTSTemplate.Settings.Endpoint, p.TTSTemplate.Settings.TokenEnv))
	}
	fmt.Fprintf(&b, "授权绑定预设 %s 第 %d 版（sha256 %s），新版本不继承，可随时撤销。AI 自评不等于人工验收。", p.ProfileID, p.Revision, profileSHA)
	text := b.String()
	sum := sha256.Sum256([]byte(text))
	return text, hex.EncodeToString(sum[:])
}

// AutoNoticeFor returns the disclosure the UI must show for one profile
// revision.
func (s *Service) AutoNoticeFor(ctx context.Context, profileID string, revision int) (AutoNoticeView, error) {
	if revision < 1 {
		return AutoNoticeView{}, model.ErrArgument
	}
	p, sum, err := s.GetProcessingProfile(ctx, profileID, revision)
	if err != nil {
		return AutoNoticeView{}, err
	}
	text, hash := AutoNotice(p, sum)
	view := AutoNoticeView{ProfileID: p.ProfileID, Revision: p.Revision, ProfileSHA256: sum, External: p.External(), Notice: text, NoticeSHA256: hash}
	set, err := loadVisibleSet(ctx, s.st)
	if err != nil {
		return AutoNoticeView{}, err
	}
	view.VisibleSetSHA = set.SHA
	view.VisibleAssets = set.Assets
	view.Grants = []AuthorizationGrant{
		{Code: "visible_set", Label: "当前 Agent 可见素材（逐项列在 visible_assets）"},
		{Code: "script_finalize", Label: "脚本定稿：仅限上面列出的可见素材"},
	}
	return view, nil
}

func intOr(v *int, def, lo, hi int, name string) (int, error) {
	n := def
	if v != nil {
		n = *v
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("service: %s must be between %d and %d: %w", name, lo, hi, model.ErrArgument)
	}
	return n, nil
}

func buildAutoScope(req AutoScopeRequest, external bool) (model.AutoScope, error) {
	var sc model.AutoScope
	var err error
	if sc.MaxRounds, err = intOr(req.MaxRounds, defaultAutoMaxRounds, 1, maxAutoMaxRounds, "max_rounds"); err != nil {
		return sc, err
	}
	if sc.MaxTotalRounds, err = intOr(req.MaxTotalRounds, defaultAutoMaxTotalRounds, sc.MaxRounds, maxAutoMaxTotalRounds, "max_total_rounds"); err != nil {
		return sc, err
	}
	if sc.MaxStageRetries, err = intOr(req.MaxStageRetries, defaultAutoMaxStageRetries, 0, 1, "max_stage_retries"); err != nil {
		return sc, err
	}
	if sc.MaxActions, err = intOr(req.MaxActions, defaultAutoMaxActions, 1, maxAutoMaxActions, "max_actions"); err != nil {
		return sc, err
	}
	if sc.MaxTTSChars, err = intOr(req.MaxTTSChars, defaultAutoMaxTTSChars, 1, maxAutoMaxTTSChars, "max_tts_chars"); err != nil {
		return sc, err
	}
	if sc.MaxWallSeconds, err = intOr(req.MaxWallSeconds, defaultAutoMaxWallSeconds, 60, maxAutoMaxWallSeconds, "max_wall_seconds"); err != nil {
		return sc, err
	}
	if sc.DecisionTimeoutSeconds, err = intOr(req.DecisionTimeoutSeconds, defaultAutoDecisionTimeout, 3600, maxAutoDecisionTimeout, "decision_timeout_seconds"); err != nil {
		return sc, err
	}
	if len(req.Thresholds) > 16 {
		return sc, fmt.Errorf("service: at most 16 thresholds: %w", model.ErrArgument)
	}
	sc.Thresholds = map[string]float64{}
	for k, v := range req.Thresholds {
		if !autoThresholdKey.MatchString(k) || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
			return sc, fmt.Errorf("service: invalid threshold %q: %w", k, model.ErrArgument)
		}
		sc.Thresholds[k] = v
	}
	// The scorer is the only reader of these numbers. Reject at authorization
	// what it would reject at scoring time (unknown key, out of range, an
	// inverted band or pace window) instead of discovering it after the export.
	if _, err := autoreview.ResolveThresholds(sc.Thresholds); err != nil {
		return sc, fmt.Errorf("service: %v: %w", err, model.ErrArgument)
	}
	loosened, err := autoreview.CheckOverrides(sc.Thresholds)
	if err != nil {
		return sc, fmt.Errorf("service: %v: %w", err, model.ErrArgument)
	}
	if len(loosened) > 0 && !req.RelaxThresholds {
		return sc, fmt.Errorf("service: thresholds %s are looser than the defaults; confirm with relax_thresholds: %w", strings.Join(loosened, ","), model.ErrArgument)
	}
	sc.RelaxedThresholds = len(loosened) > 0
	sc.ExternalEgress = external
	sc.AutoSceneConfirm, sc.AutoNarrationApprove, sc.AutoPreviewExport = true, true, true
	return sc, nil
}

// autoStart carries an authorization into startWorkflow's own transaction so
// the run, its profile binding, the optional consent and the overlay row are
// committed or rolled back together.
type autoStart struct {
	brief        string
	briefSHA     string
	scope        model.AutoScope
	noticeSHA    string
	grantConsent bool
	profile      preparation.Profile
	profileSHA   string
	requestHash  string
	visibleSHA   string
	visibleIDs   []string
}

// StartAutoRun authorizes and starts an unattended run on a prepared profile.
// The actor must be a human: an agent can never grant itself authority.
func (s *Service) StartAutoRun(ctx context.Context, actor string, in AutoRunRequest) (model.WorkflowRun, AutoView, error) {
	if !isHumanActor(actor) {
		return model.WorkflowRun{}, AutoView{}, fmt.Errorf("service: auto run authorization requires a human actor: %w", model.ErrForbidden)
	}
	if in.ProfileRevision < 1 || in.IdempotencyKey == "" || in.ExpectedAssetVersion == "" {
		return model.WorkflowRun{}, AutoView{}, model.ErrArgument
	}
	brief, briefSHA, err := validateBrief(in.Brief)
	if err != nil {
		return model.WorkflowRun{}, AutoView{}, err
	}
	p, sha, err := s.GetProcessingProfile(ctx, in.ProfileID, in.ProfileRevision)
	if err != nil {
		return model.WorkflowRun{}, AutoView{}, err
	}
	if p.ContentMode != "configured" {
		return model.WorkflowRun{}, AutoView{}, fmt.Errorf("service: auto mode requires a configured profile; builtin is regression only: %w", model.ErrArgument)
	}
	external := p.External()
	// The consent step exists only for a profile that sends data off the
	// machine. An all-loopback profile has nothing to consent to, and
	// SetProcessingConsent would answer ErrArgument for it.
	if external && !in.AcceptExternalEgress {
		return model.WorkflowRun{}, AutoView{}, fmt.Errorf("service: accept_external_egress is required for a profile with external endpoints: %w", model.ErrArgument)
	}
	_, wantNotice := AutoNotice(p, sha)
	if in.NoticeSHA256 != wantNotice {
		return model.WorkflowRun{}, AutoView{}, fmt.Errorf("service: notice_sha256 does not match the current disclosure: %w", model.ErrArgument)
	}
	scope, err := buildAutoScope(in.Scope, external)
	if err != nil {
		return model.WorkflowRun{}, AutoView{}, err
	}
	// The global authorization is the human grant of this visible set and of
	// script finalization inside it. A missing or stale fingerprint is not a grant.
	vis, err := loadVisibleSet(ctx, s.st)
	if err != nil {
		return model.WorkflowRun{}, AutoView{}, err
	}
	if in.VisibleSetSHA == "" {
		return model.WorkflowRun{}, AutoView{}, fmt.Errorf("service: visible_set_sha is required: %w", model.ErrArgument)
	}
	if in.VisibleSetSHA != vis.SHA {
		return model.WorkflowRun{}, AutoView{}, fmt.Errorf("service: visible_set_sha does not match the current visible set: %w", model.ErrConflict)
	}
	if !containsString(vis.IDs, in.AssetID) {
		return model.WorkflowRun{}, AutoView{}, fmt.Errorf("service: asset is not in the agent-visible set: %w", model.ErrForbidden)
	}
	scope.VisibleSetSHA = vis.SHA
	scope.VisibleAssetIDs = append([]string(nil), vis.IDs...)
	scope.ScriptFinalize = true
	// Host tools are checked outside the write transaction, as in
	// StartPreparedWorkflow; governance, consent and worker capability are
	// rechecked inside it.
	checks, err := preparation.NewLocalProbe().Probe(ctx, p)
	if err != nil {
		return model.WorkflowRun{}, AutoView{}, err
	}
	for _, c := range checks {
		if c.Status != "passed" {
			return model.WorkflowRun{}, AutoView{}, model.ErrInvalidState
		}
	}
	scopeJSON, _ := json.Marshal(scope)
	scopeSum := sha256.Sum256(scopeJSON)
	prepared := preparation.PreparedStart{
		AssetID: in.AssetID, RevisionID: in.RevisionID, ProfileID: in.ProfileID, ProfileRevision: in.ProfileRevision,
		ProfileSHA256: sha, ExpectedAssetVersion: in.ExpectedAssetVersion, IdempotencyKey: in.IdempotencyKey,
	}
	auto := &autoStart{
		brief: brief, briefSHA: briefSHA, scope: scope, noticeSHA: wantNotice, grantConsent: external,
		profile: p, profileSHA: sha,
		requestHash: briefSHA + "|" + hex.EncodeToString(scopeSum[:]) + "|" + wantNotice + "|" + vis.SHA,
		visibleSHA:  vis.SHA, visibleIDs: vis.IDs,
	}
	run, err := s.startWorkflow(ctx, actor, in.AssetID, in.RevisionID, in.IdempotencyKey, p.ContentMode, &prepared, auto)
	if err != nil {
		return model.WorkflowRun{}, AutoView{}, err
	}
	view, err := s.GetAuto(ctx, run.RunID)
	if err != nil {
		return model.WorkflowRun{}, AutoView{}, err
	}
	return run, view, nil
}

// insertAutoOverlay writes the workflow_auto row and its authorize decision.
// It runs inside startWorkflow's transaction.
func (s *Service) insertAutoOverlay(ctx context.Context, tx *store.Store, actor string, run model.WorkflowRun, a *autoStart, grantedByRun bool) error {
	now := run.CreatedAt
	overlay := model.WorkflowAuto{
		RunID: run.RunID, AutoVersion: 1, State: model.AutoStateActive,
		Brief: a.brief, BriefSHA256: a.briefSHA, AuthorizedBy: actor, AuthorizedAt: now,
		ConsentGrantedByRun: grantedByRun, NoticeSHA256: a.noticeSHA, Scope: a.scope,
		ProfileID: a.profile.ProfileID, ProfileRevision: a.profile.Revision, ProfileSHA256: a.profileSHA,
		MaxRounds: a.scope.MaxRounds, MaxTotalRounds: a.scope.MaxTotalRounds,
		CreatedAt: now, UpdatedAt: now, DeadlineAt: now.Add(time.Duration(a.scope.MaxWallSeconds) * time.Second),
	}
	if err := tx.InsertWorkflowAuto(ctx, overlay); err != nil {
		return err
	}
	basis, _ := json.Marshal(map[string]any{"profile_id": overlay.ProfileID, "profile_revision": overlay.ProfileRevision,
		"notice_sha256": overlay.NoticeSHA256, "brief_sha256": overlay.BriefSHA256, "external_egress": a.scope.ExternalEgress,
		"thresholds": a.scope.Thresholds, "relaxed_thresholds": a.scope.RelaxedThresholds,
		"visible_set_sha": a.scope.VisibleSetSHA, "visible_asset_ids": a.scope.VisibleAssetIDs,
		"script_finalize": a.scope.ScriptFinalize})
	if err := tx.InsertAutoDecision(ctx, model.AutoDecision{RunID: run.RunID, Kind: model.AutoDecisionAuthorize, Actor: actor, Basis: basis, CreatedAt: now}); err != nil {
		return err
	}
	return auditTx(ctx, tx, actor, "auto.authorize", run.RunID, "profile="+overlay.ProfileID+" notice_sha256="+overlay.NoticeSHA256)
}

// GetAuto returns the auto-run view of a run, or ErrNotFound for a run that
// is not an auto run.
func (s *Service) GetAuto(ctx context.Context, runID string) (AutoView, error) {
	v, err := autoView(ctx, s.st, runID)
	if err != nil {
		return AutoView{}, err
	}
	if err := s.fillAutoView(ctx, &v); err != nil {
		return AutoView{}, err
	}
	return v, nil
}

func autoView(ctx context.Context, st *store.Store, runID string) (AutoView, error) {
	a, err := st.GetWorkflowAuto(ctx, runID)
	if err != nil {
		return AutoView{}, err
	}
	rounds, err := st.ListAutoRounds(ctx, runID)
	if err != nil {
		return AutoView{}, err
	}
	reviews, err := st.ListAutoReviews(ctx, runID)
	if err != nil {
		return AutoView{}, err
	}
	decisions, err := st.ListAutoDecisions(ctx, runID)
	if err != nil {
		return AutoView{}, err
	}
	v := AutoView{WorkflowAuto: a, Rounds: rounds, Decisions: decisions}
	if len(reviews) > 0 {
		last := reviews[len(reviews)-1]
		v.LatestReview = &last
	}
	return v, nil
}

// ListAutoReviews returns every stored self-assessment of an auto run.
func (s *Service) ListAutoReviews(ctx context.Context, runID string) ([]model.AutoReview, error) {
	if _, err := s.st.GetWorkflowAuto(ctx, runID); err != nil {
		return nil, err
	}
	return s.st.ListAutoReviews(ctx, runID)
}

// publishAutoPtr announces an overlay change a hook returned; nil means the
// hook changed nothing (not an auto run, or no longer live).
func (s *Service) publishAutoPtr(a *model.WorkflowAuto) {
	if a != nil {
		s.publishAuto(*a)
	}
}

func (s *Service) publishAuto(a model.WorkflowAuto) {
	s.publish("auto.changed", map[string]any{"run_id": a.RunID, "state": a.State, "round": a.Round, "auto_version": a.AutoVersion})
}

// consentSnapshot records the profile's external-consent state so a later
// reader can tell what the revoke left behind.
func consentSnapshot(ctx context.Context, tx *store.Store, a model.WorkflowAuto) (string, bool, error) {
	granted, err := tx.ProfileConsent(ctx, a.ProfileID, a.ProfileRevision, a.ProfileSHA256)
	if err != nil {
		return "", false, err
	}
	body, _ := json.Marshal(map[string]any{
		"granted": granted, "profile_id": a.ProfileID, "revision": a.ProfileRevision,
		"sha": a.ProfileSHA256, "granted_by_this_run": a.ConsentGrantedByRun,
	})
	return string(body), granted, nil
}

// RevokeAuto withdraws an authorization. Nothing automatic happens on the run
// afterwards; a worker task already queued or running is not killed, and a
// review gate that is open stays open for a human. alsoRevokeConsent defaults
// to false because the profile consent is per profile version and shared with
// manual runs; the consent state at revoke time is always recorded.
func (s *Service) RevokeAuto(ctx context.Context, actor, runID, reason string, expectedAutoVersion int, alsoRevokeConsent bool) (AutoView, error) {
	if !isHumanActor(actor) {
		return AutoView{}, fmt.Errorf("service: revoking an auto run requires a human actor: %w", model.ErrForbidden)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 500 || expectedAutoVersion < 1 {
		return AutoView{}, model.ErrArgument
	}
	var saved model.WorkflowAuto
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		a, err := tx.GetWorkflowAuto(ctx, runID)
		if err != nil {
			return err
		}
		if a.AutoVersion != expectedAutoVersion {
			return fmt.Errorf("service: auto version conflict: %w", model.ErrConflict)
		}
		if a.State == model.AutoStateRevoked || a.State == model.AutoStateCancelled {
			return fmt.Errorf("service: auto run is already %s: %w", a.State, model.ErrInvalidState)
		}
		snapshot, granted, err := consentSnapshot(ctx, tx, a)
		if err != nil {
			return err
		}
		if alsoRevokeConsent && granted {
			if err := tx.SetProfileConsent(ctx, a.ProfileID, a.ProfileRevision, a.ProfileSHA256, actor, false); err != nil {
				return err
			}
			if err := auditTx(ctx, tx, actor, "profile.consent", a.ProfileID, fmt.Sprintf("revision=%d granted=false via=auto.revoke", a.ProfileRevision)); err != nil {
				return err
			}
		}
		now := s.autoNow()
		a.State, a.RevokedBy, a.RevokedAt, a.RevokeReason, a.ConsentAtRevoke = model.AutoStateRevoked, actor, &now, reason, snapshot
		a.AutoVersion++
		a.UpdatedAt = now
		if err := tx.UpdateWorkflowAuto(ctx, a); err != nil {
			return err
		}
		basis, _ := json.Marshal(map[string]any{"reason": reason, "also_revoke_consent": alsoRevokeConsent})
		if err := tx.InsertAutoDecision(ctx, model.AutoDecision{RunID: runID, Round: a.Round, Kind: model.AutoDecisionRevoke, Actor: actor, Basis: basis, CreatedAt: now}); err != nil {
			return err
		}
		if err := auditTx(ctx, tx, actor, "auto.revoke", runID, reason); err != nil {
			return err
		}
		saved = a
		return nil
	})
	if err != nil {
		return AutoView{}, err
	}
	s.publishAuto(saved)
	if alsoRevokeConsent {
		s.publish("profile.changed", map[string]any{"profile_id": saved.ProfileID})
	}
	return s.GetAuto(ctx, runID)
}

// DecideAuto records the user's answer when an auto run is waiting for one.
// export completes the run and lifts the delivery hold; discard stops it.
// revise needs the rerun machinery of a later slice and is refused after its
// arguments and budget are validated.
func (s *Service) DecideAuto(ctx context.Context, actor, runID, decision, feedback string, expectedAutoVersion int, idemKey string) (model.WorkflowRun, AutoView, error) {
	if !isHumanActor(actor) {
		return model.WorkflowRun{}, AutoView{}, fmt.Errorf("service: an auto decision requires a human actor: %w", model.ErrForbidden)
	}
	if expectedAutoVersion < 1 || idemKey == "" || strings.ContainsAny(idemKey, "/\\") {
		return model.WorkflowRun{}, AutoView{}, model.ErrArgument
	}
	switch decision {
	case "export", "discard", "revise", "confirm_visible_set":
	default:
		return model.WorkflowRun{}, AutoView{}, fmt.Errorf("service: decision must be export, revise, discard or confirm_visible_set: %w", model.ErrArgument)
	}
	feedback = strings.TrimSpace(feedback)
	if decision == "revise" {
		if feedback == "" {
			return model.WorkflowRun{}, AutoView{}, fmt.Errorf("service: revise needs feedback: %w", model.ErrArgument)
		}
		if _, _, err := validateBrief(feedback); err != nil {
			return model.WorkflowRun{}, AutoView{}, err
		}
	}
	scope := "auto.decision:" + runID
	hash := decision + "|" + feedback + "|" + fmt.Sprint(expectedAutoVersion)
	var saved model.WorkflowAuto
	var revised model.WorkflowRun
	replayed := false
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		if got, _, e := tx.GetIdempotency(ctx, scope, idemKey); e == nil {
			if got != hash {
				return fmt.Errorf("service: idempotency key reused with different content: %w", model.ErrConflict)
			}
			replayed = true
			return nil
		} else if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		a, err := tx.GetWorkflowAuto(ctx, runID)
		if err != nil {
			return err
		}
		if a.AutoVersion != expectedAutoVersion {
			return fmt.Errorf("service: auto version conflict: %w", model.ErrConflict)
		}
		if a.State != model.AutoStateAwaitingDecision {
			return fmt.Errorf("service: auto run is %s, not waiting for a decision: %w", a.State, model.ErrInvalidState)
		}
		now := s.autoNow()
		kind := model.AutoDecisionUserExport
		switch decision {
		case "export":
			// Releasing the hold means there is something to release: the
			// package and the 720p preview of the current export. A run that
			// waits for another reason (the narration was refused before any
			// export) can be revised or discarded, not exported.
			d, derr := s.autoDeliveryAt(ctx, tx, runID)
			if derr != nil {
				return derr
			}
			if !d.Delivered {
				return fmt.Errorf("service: there is no delivered export to release; revise or discard: %w", model.ErrInvalidState)
			}
			a.State, a.DeliveryHold, a.HaltReason = model.AutoStateCompleted, false, ""
		case "discard":
			kind = model.AutoDecisionUserDiscard
			a.State, a.HaltReason = model.AutoStateHalted, model.AutoHaltUserDiscarded
		case "confirm_visible_set":
			if a.HaltReason != model.AutoWaitVisibleSet {
				return fmt.Errorf("service: confirm_visible_set is only for a changed visible set: %w", model.ErrInvalidState)
			}
			set, serr := loadVisibleSet(ctx, tx)
			if serr != nil {
				return serr
			}
			run, rerr := tx.GetWorkflow(ctx, runID)
			if rerr != nil {
				return rerr
			}
			if !containsString(set.IDs, run.AssetID) {
				return fmt.Errorf("service: asset is not in the agent-visible set: %w", model.ErrForbidden)
			}
			if err := s.writeVisibleConfirmation(ctx, tx, actor, set.SHA, true); err != nil {
				return err
			}
			a.Scope.VisibleSetSHA = set.SHA
			a.Scope.VisibleAssetIDs = append([]string(nil), set.IDs...)
			a.Scope.ScriptFinalize = true
			a.State, a.HaltReason, a.DeliveryHold = model.AutoStateActive, "", false
			kind = model.AutoDecisionVisibleSet
		case "revise":
			if a.TotalRounds >= a.MaxTotalRounds || a.ActionsUsed >= a.Scope.MaxActions || a.TTSCharsUsed >= a.Scope.MaxTTSChars {
				return fmt.Errorf("service: no budget left for another round: %w", model.ErrInvalidState)
			}
			// A user's revise costs a round (total_rounds) but not an automatic
			// one (round), and no action. The wall clock restarts: the pause
			// that began at the wait ends here.
			var rerr error
			revised, rerr = s.applyAutoRerun(ctx, tx, &a, rerunSpec{manual: true, feedback: autoFeedbackJSON(nil, feedback), outcome: "revise"}, now)
			if errors.Is(rerr, errRerunAssetBusy) {
				return fmt.Errorf("service: another run holds the asset: %w", model.ErrConflict)
			}
			if rerr != nil {
				return rerr
			}
			kind = model.AutoDecisionUserRevise
			// The export that was held is invalidated by the rerun: there is
			// nothing left to confirm until the new pass is scored.
			a.DeliveryHold = false
		}
		a.PausedAt, a.DecisionDeadlineAt = nil, nil
		a.AutoVersion++
		a.UpdatedAt = now
		if err := tx.UpdateWorkflowAuto(ctx, a); err != nil {
			return err
		}
		basis, _ := json.Marshal(map[string]any{"decision": decision, "feedback_chars": utf8.RuneCountInString(feedback)})
		if err := tx.InsertAutoDecision(ctx, model.AutoDecision{RunID: runID, Round: a.Round, Kind: kind, Actor: actor, Basis: basis, CreatedAt: now}); err != nil {
			return err
		}
		if err := auditTx(ctx, tx, actor, "auto.decision", runID, decision); err != nil {
			return err
		}
		if err := tx.PutIdempotency(ctx, scope, idemKey, hash, "{}"); err != nil {
			return err
		}
		saved = a
		return nil
	})
	if err != nil {
		return model.WorkflowRun{}, AutoView{}, err
	}
	if !replayed {
		if revised.RunID != "" {
			s.publish("workflow.changed", revised)
		}
		s.publishAuto(saved)
	}
	run, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return model.WorkflowRun{}, AutoView{}, err
	}
	view, err := s.GetAuto(ctx, runID)
	return run, view, err
}

// autoRunInput builds the part of a worker's task input that belongs to an
// auto run, or nil for a run that is not one. Retry feedback is structured
// reason codes only, never text a model or a worker produced.
func autoRunInput(ctx context.Context, st *store.Store, runID string) (*AutoInput, error) {
	a, err := st.GetWorkflowAuto(ctx, runID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	in := &AutoInput{Brief: a.Brief, BriefSHA256: a.BriefSHA256, Round: a.TotalRounds}
	rounds, err := st.ListAutoRounds(ctx, runID)
	if err != nil {
		return nil, err
	}
	for _, r := range rounds {
		if r.Round != a.TotalRounds || r.Feedback == "" {
			continue
		}
		var fb struct {
			Codes []model.AutoFeedbackItem `json:"codes"`
			User  string                   `json:"user"`
		}
		if err := json.Unmarshal([]byte(r.Feedback), &fb); err != nil {
			return nil, fmt.Errorf("service: auto round feedback of %s is malformed: %w", runID, model.ErrInvalidState)
		}
		for _, item := range fb.Codes {
			if model.AutoFeedbackCodes[item.Code] {
				in.Feedback = append(in.Feedback, item)
			}
		}
		in.UserNote = fb.User
	}
	in.FeedbackText = autoreview.RenderFeedbackAll(in.Feedback)
	return in, nil
}

// AutoInput is the auto-run part of a worker task input.
type AutoInput struct {
	Brief       string                   `json:"brief"`
	BriefSHA256 string                   `json:"brief_sha256"`
	Round       int                      `json:"round"`
	Feedback    []model.AutoFeedbackItem `json:"feedback,omitempty"`
	UserNote    string                   `json:"user_note,omitempty"`
	// FeedbackText is Feedback rendered through the fixed templates of the
	// autoreview package. It is produced here from the closed codes, never
	// stored and never taken from a model.
	FeedbackText []string `json:"feedback_text,omitempty"`
}

// ensureNoLiveAuto is Rule A: refuse to start another run, or to reopen the
// asset for one, while an auto run on the asset is active or waiting for the
// user. It reads through st, which may be the plain store (an early,
// side-effect-free check) or a transaction (the atomic one).
func ensureNoLiveAuto(ctx context.Context, st *store.Store, assetID string) error {
	runID, live, err := st.ActiveAutoForAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if live {
		return fmt.Errorf("service: asset %s has auto run %s in progress: %w", assetID, runID, model.ErrConflict)
	}
	return nil
}

func sceneNumber(scene map[string]any, key string) (float64, bool) {
	v, ok := number(scene[key])
	return v, ok
}

// autoSceneCriteria applies the §3 rules to one scene and returns a failure
// code, or "" when the scene may be confirmed automatically.
func autoSceneCriteria(scene map[string]any, minConfidence float64, labels map[string]bool, ranks map[int]bool) string {
	if len(stringList(scene["evidence_frames"])) == 0 {
		return "scene_criteria_failed"
	}
	conf, ok := sceneNumber(scene, "confidence")
	if !ok || conf < minConfidence {
		return "low_confidence"
	}
	if scene["method"] == "metadata_only" {
		return "scene_criteria_failed"
	}
	label, _ := scene["label"].(string)
	if strings.TrimSpace(label) == "" || strings.ContainsAny(label, "\r\n") || utf8.RuneCountInString(label) > 60 || labels[label] {
		return "scene_criteria_failed"
	}
	labels[label] = true
	if rank, ok := sceneNumber(scene, "sequence_rank"); ok {
		r := int(rank)
		if float64(r) != rank || r < 0 || ranks[r] {
			return "scene_criteria_failed"
		}
		ranks[r] = true
	}
	return ""
}

// AutoConfirmScene confirms every pending scene of an auto run in one
// transaction, on behalf of the auto-reviewer. It is a new closure rather than
// a hook on ConfirmScene because transactions cannot nest and ConfirmScene
// writes one scene per transaction. Scenes a human already decided are skipped
// and count as satisfied; if any remaining scene fails the criteria nothing is
// written and an *AutoPolicyError is returned.
func (s *Service) AutoConfirmScene(ctx context.Context, runID string, version, autoVersion int, revisionID string) (model.WorkflowRun, error) {
	a0, err := s.st.GetWorkflowAuto(ctx, runID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.WorkflowRun{}, fmt.Errorf("service: run %s is not an auto run: %w", runID, model.ErrForbidden)
		}
		return model.WorkflowRun{}, err
	}
	// A ledger row exists only if the action committed, and rows are never
	// deleted, so a replay of a committed confirmation is a success whatever
	// the run has done since (moved on, finished, been revoked). It is decided
	// before any state, scope, budget or version check, all of which describe
	// a world the first execution already changed.
	replayKey := fmt.Sprintf("auto:%s:%d:%s:%s", runID, a0.Round, model.AutoActionSceneConfirm, revisionID)
	if done, err := s.st.AutoActionExists(ctx, runID, replayKey); err != nil {
		return model.WorkflowRun{}, err
	} else if done {
		return s.st.GetWorkflow(ctx, runID)
	}
	// Evidence files are hashed here, before the write lock is taken. The
	// revision is immutable, and lockRun below proves it is still current.
	pre, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return model.WorkflowRun{}, err
	}
	if pre.CurrentRevisionID != revisionID {
		return model.WorkflowRun{}, fmt.Errorf("service: revision is not current: %w", model.ErrConflict)
	}
	preRev, err := s.st.GetRevision(ctx, revisionID)
	if err != nil {
		return model.WorkflowRun{}, err
	}
	preEDL, err := s.readRevision(preRev)
	if err != nil {
		return model.WorkflowRun{}, err
	}
	if _, err := s.evidenceFiles(preRev.PackageRef, preEDL); err != nil {
		return model.WorkflowRun{}, err
	}

	var saved model.WorkflowRun
	var savedAuto model.WorkflowAuto
	if s.beforeAutoTx != nil {
		s.beforeAutoTx()
	}
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		a, err := tx.GetWorkflowAuto(ctx, runID)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				return fmt.Errorf("service: run %s is not an auto run: %w", runID, model.ErrForbidden)
			}
			return err
		}
		key := fmt.Sprintf("auto:%s:%d:%s:%s", runID, a.Round, model.AutoActionSceneConfirm, revisionID)
		if done, err := tx.AutoActionExists(ctx, runID, key); err != nil {
			return err
		} else if done {
			// Another call committed the same action after the early check.
			cur, err := tx.GetWorkflow(ctx, runID)
			if err != nil {
				return err
			}
			saved = cur
			return nil
		}
		run, rev, edl, err := s.lockRun(ctx, tx, runID, version, revisionID)
		if err != nil {
			return err
		}
		now := s.autoNow()
		switch {
		case a.State != model.AutoStateActive:
			return fmt.Errorf("service: auto run is %s: %w", a.State, model.ErrForbidden)
		case !a.Scope.AutoSceneConfirm:
			return fmt.Errorf("service: scene confirmation is outside the authorized scope: %w", model.ErrForbidden)
		case now.After(a.DeadlineAt):
			return fmt.Errorf("service: auto run is past its deadline: %w", model.ErrForbidden)
		case a.ActionsUsed >= a.Scope.MaxActions:
			return errAutoBudgetSpent
		case a.AutoVersion != autoVersion:
			return fmt.Errorf("service: auto version conflict: %w", model.ErrConflict)
		}
		if run.Stage != "scene_review" || run.Status != model.WorkflowAwaitingReview {
			return fmt.Errorf("workflow is not at scene review: %w", model.ErrInvalidState)
		}
		if run.ContentMode != "configured" {
			return &AutoPolicyError{Code: "scene_criteria_failed"}
		}
		if err := s.ensureIdle(ctx, tx, run); err != nil {
			return err
		}
		reviews, err := tx.ListSceneReviews(ctx, run.RunID, rev.RevisionID)
		if err != nil {
			return err
		}
		existing := map[string]model.SceneReview{}
		for _, r := range reviews {
			existing[r.SceneID] = r
		}
		minConf := defaultAutoSceneConfidence
		if v, ok := a.Scope.Thresholds[autoThresholdSceneConfidence]; ok {
			minConf = v
		}
		scenes := asMapSlice(edl["scenes"])
		if len(scenes) == 0 {
			return &AutoPolicyError{Code: "scene_criteria_failed"}
		}
		var pending []map[string]any
		var skipped []string
		labels, ranks := map[string]bool{}, map[int]bool{}
		for _, scene := range scenes {
			id, _ := scene["scene_id"].(string)
			if id == "" {
				return &AutoPolicyError{Code: "scene_criteria_failed"}
			}
			if prior, ok := existing[id]; ok && isHumanActor(prior.Actor) {
				if prior.Decision != "confirmed" {
					// A human rejection is a takeover. The hook that halts the run
					// arrives with B3; until then refuse to write around it.
					return fmt.Errorf("service: scene %s was rejected by a human: %w", id, model.ErrConflict)
				}
				skipped = append(skipped, id)
				continue
			}
			if code := autoSceneCriteria(scene, minConf, labels, ranks); code != "" {
				return &AutoPolicyError{Code: code}
			}
			pending = append(pending, scene)
		}
		var written []string
		for _, scene := range pending {
			id, _ := scene["scene_id"].(string)
			frames := stringList(scene["evidence_frames"])
			applied, err := tx.UpsertSceneReviewAuto(ctx, model.SceneReview{
				RunID: run.RunID, RevisionID: rev.RevisionID, SceneID: id, Decision: "confirmed",
				Note: "auto-review", EvidenceSHA256: frames[0], Actor: model.AutoActor, CreatedAt: now,
			})
			if err != nil {
				return err
			}
			if !applied {
				// A human decided it between the read above and this write.
				// BEGIN IMMEDIATE makes that impossible; if it ever happens the
				// scene is satisfied, not overwritten.
				skipped = append(skipped, id)
				continue
			}
			written = append(written, id)
			basis, _ := json.Marshal(map[string]any{"confidence": scene["confidence"], "evidence_sha256": frames[0]})
			if err := tx.InsertAutoDecision(ctx, model.AutoDecision{RunID: run.RunID, Round: a.Round, Kind: model.AutoDecisionSceneConfirm, Target: id, Actor: model.AutoActor, Basis: basis, CreatedAt: now}); err != nil {
				return err
			}
		}
		for _, id := range skipped {
			if err := tx.InsertAutoDecision(ctx, model.AutoDecision{RunID: run.RunID, Round: a.Round, Kind: model.AutoDecisionSkipHuman, Target: id, Actor: existing[id].Actor, CreatedAt: now}); err != nil {
				return err
			}
		}
		if _, err := tx.InsertAutoAction(ctx, run.RunID, key, model.AutoActionSceneConfirm, revisionID, now); err != nil {
			return err
		}
		if err := s.openSortIfReady(ctx, tx, &run, edl); err != nil {
			return err
		}
		run.Version++
		run.UpdatedAt = now
		if err := tx.UpdateWorkflow(ctx, run); err != nil {
			return err
		}
		a.ActionsUsed++
		a.AutoVersion++
		a.UpdatedAt = now
		if err := tx.UpdateWorkflowAuto(ctx, a); err != nil {
			return err
		}
		if err := auditTx(ctx, tx, model.AutoActor, "auto.scene_confirm", runID, fmt.Sprintf("revision=%s confirmed=%d skipped_human=%d", revisionID, len(written), len(skipped))); err != nil {
			return err
		}
		saved, savedAuto = run, a
		return nil
	})
	if err != nil {
		return model.WorkflowRun{}, err
	}
	if savedAuto.RunID != "" { // empty on a replay: nothing changed, nothing to announce
		s.publish("review.changed", saved)
		s.publishAuto(savedAuto)
	}
	return saved, nil
}

// autoTakeover halts a live auto run because a person acted on it. It reads
// and writes the database only, so it can run inside the closure of the human
// action that caused it. An auto run that is not live is left alone, and so is
// anything done by the auto-reviewer itself. The updated overlay is returned
// when it changed, so the caller can announce it after the transaction.
func autoTakeover(ctx context.Context, tx *store.Store, runID, actor, reason, via string) (*model.WorkflowAuto, error) {
	return autoOverride(ctx, tx, runID, actor, model.AutoStateHalted, reason, model.AutoDecisionTakeover, via, "auto.takeover", false)
}

// autoHumanAcceptanceTx closes a live overlay because a person recorded
// acceptance on its run: the run is in human hands from here, so the overlay
// becomes halted(human_completed/human_rejected), the delivery hold is released (acceptance is
// the person's own decision) and both clocks stop. It frees the asset for the
// next run (Rule A looks only at live overlays). It is not a takeover: nothing
// the person did is undone. A run that is not auto, or an overlay that is not
// live, is left alone.
func autoHumanAcceptanceTx(ctx context.Context, tx *store.Store, runID, actor, via, result string) (*model.WorkflowAuto, error) {
	reason, audit := model.AutoHaltHumanCompleted, "auto.human_completed"
	if result == model.AcceptanceFailed {
		reason, audit = model.AutoHaltHumanRejected, "auto.human_rejected"
	}
	return autoOverride(ctx, tx, runID, actor, model.AutoStateHalted, reason, model.AutoDecisionHumanAcceptance, via, audit, true)
}

// autoCancelTx records that a person cancelled the workflow of a live auto run.
func autoCancelTx(ctx context.Context, tx *store.Store, runID, actor, reason string) (*model.WorkflowAuto, error) {
	return autoOverride(ctx, tx, runID, actor, model.AutoStateCancelled, "", model.AutoDecisionCancelled, "cancel", "auto.cancel", false)
}

func autoOverride(ctx context.Context, tx *store.Store, runID, actor, state, reason, kind, via, auditAction string, releaseHold bool) (*model.WorkflowAuto, error) {
	if actor == model.AutoActor {
		return nil, nil
	}
	a, err := tx.GetWorkflowAuto(ctx, runID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil // not an auto run: nothing to do
	}
	if err != nil {
		return nil, err
	}
	if !autoStateLive(a.State) {
		return nil, nil
	}
	now := time.Now().UTC()
	a.State, a.HaltReason = state, reason
	if releaseHold {
		a.DeliveryHold = false
	}
	a.PausedAt, a.DecisionDeadlineAt = nil, nil
	a.AutoVersion++
	a.UpdatedAt = now
	if err := tx.UpdateWorkflowAuto(ctx, a); err != nil {
		return nil, err
	}
	basis, _ := json.Marshal(map[string]any{"via": via, "reason": reason})
	if err := tx.InsertAutoDecision(ctx, model.AutoDecision{RunID: runID, Round: a.Round, Kind: kind, Actor: actor, Basis: basis, CreatedAt: now}); err != nil {
		return nil, err
	}
	if err := auditTx(ctx, tx, actor, auditAction, runID, via+" "+reason); err != nil {
		return nil, err
	}
	return &a, nil
}
