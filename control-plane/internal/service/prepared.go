package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"strings"
)

func (s *Service) ListProcessingProfiles(ctx context.Context, limit, offset int) ([]preparation.Profile, error) {
	rows, e := s.st.ListCurrentProfiles(ctx, limit, offset)
	if e != nil {
		return nil, e
	}
	out := []preparation.Profile{}
	for _, row := range rows {
		p, e := verifiedProfile(row.ProfileID, row.CanonicalJSON, row.SHA256)
		if e != nil {
			return nil, e
		}
		if p.Revision != row.Revision || p.SchemaVersion != row.SchemaVersion {
			return nil, model.ErrInvalidState
		}
		out = append(out, p)
	}
	return out, nil
}
func (s *Service) SetProcessingConsent(ctx context.Context, actor, id string, revision int, granted bool) error {
	if actor == "" || revision < 1 {
		return model.ErrArgument
	}
	p, sum, e := s.GetProcessingProfile(ctx, id, revision)
	if e != nil {
		return e
	}
	if !p.Vision.External() && !p.Narration.External() {
		return model.ErrArgument
	}
	e = s.st.Transaction(ctx, func(tx *store.Store) error {
		if e := tx.SetProfileConsent(ctx, id, revision, sum, actor, granted); e != nil {
			return e
		}
		return tx.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "profile.consent", Target: id, Detail: fmt.Sprintf("revision=%d granted=%t", revision, granted)})
	})
	if e == nil {
		s.publish("profile.changed", map[string]any{"profile_id": id})
	}
	return e
}
func (s *Service) ReportWorkerCapability(ctx context.Context, id, role string, c preparation.Capability) error {
	if len(c.ProfileSHA256) != 64 || !isHex(c.ProfileSHA256) || len(c.TTSVoices) > 128 || !containsString([]string{"recognizer", "narrator", "exporter"}, role) {
		return model.ErrArgument
	}
	for _, v := range c.TTSVoices {
		if !printableLimited(v, 1, 200) {
			return model.ErrArgument
		}
	}
	body, e := json.Marshal(c)
	if e != nil {
		return e
	}
	e = s.st.PutCapability(ctx, id, role, c.ProfileSHA256, string(body))
	if e == nil {
		s.publish("capability.changed", map[string]any{"role": role})
	}
	return e
}
func (s *Service) preparedChecks(ctx context.Context, st *store.Store, asset model.Asset, p preparation.Profile, sum string) ([]preparation.Check, string, error) {
	checks := []preparation.Check{}
	add := func(code string, passed bool, message string) {
		state := "blocked"
		if passed {
			state = "passed"
		}
		checks = append(checks, preparation.Check{Code: code, Status: state, Message: message})
	}
	add("asset_governance", asset.AgentVisible && !asset.Locked, "资产须可见且未锁定")
	for _, role := range []string{"recognizer", "narrator", "exporter"} {
		add("role_"+role, containsString(asset.AllowedAgents, role), "资产须允许 "+role)
	}
	_, e := st.ActiveWorkflow(ctx, asset.AssetID)
	if e != nil && !errors.Is(e, model.ErrNotFound) {
		return nil, "", e
	}
	add("active_workflow", errors.Is(e, model.ErrNotFound), "同一资产只允许一个活动流程")
	add("input_ready", asset.InputKind != model.InputKindRawRecording, "原始录屏须先完成导入准备")
	if asset.IngestRunID != "" {
		run, e := st.GetIngestRun(ctx, asset.IngestRunID)
		if e != nil {
			return nil, "", e
		}
		add("ingest_ready", run.State == "ready", "导入包须为已就绪的准备结果")
		add("ingest_profile", run.ProfileID == p.ProfileID && run.ProfileRevision == p.Revision && run.ProfileSHA256 == sum, "须使用准备时绑定的同版预设；换预设需重新准备")
	}
	if _, e := st.ActiveIngestRun(ctx, asset.AssetID); e == nil {
		add("active_ingest", false, "导入流程仍在进行")
	} else if !errors.Is(e, model.ErrNotFound) {
		return nil, "", e
	}
	_, edlSHA, edlErr := s.readEDLRel(asset.Artifacts["edl"])
	add("controlled_edl", edlErr == nil, "校验受控 EDL 输入")
	b, _ := json.Marshal(struct {
		Asset  model.Asset
		EDLSHA string
	}{asset, edlSHA})
	digest := sha256.Sum256(b)
	version := hex.EncodeToString(digest[:])
	caps, e := st.Capabilities(ctx, sum)
	if e != nil {
		return nil, "", e
	}
	for _, role := range []string{"recognizer", "narrator", "exporter"} {
		ready := false
		for _, raw := range caps[role] {
			var c preparation.Capability
			if json.Unmarshal([]byte(raw), &c) == nil && c.ProfileSHA256 == sum && c.ToolsReady && c.CredentialsReady && (role != "narrator" || containsString(c.TTSVoices, p.TTSVoice)) {
				ready = true
			}
		}
		add("worker_"+role, ready, "需要匹配预设、凭证及声音的新鲜 Worker 能力")
	}
	consent := true
	if p.Vision.External() || p.Narration.External() {
		consent, e = st.ProfileConsent(ctx, p.ProfileID, p.Revision, sum)
		if e != nil {
			return nil, "", e
		}
	}
	add("external_authorization", consent, "外部端点需该版本明确许可")
	return checks, version, nil
}
func (s *Service) PreparedPreflight(ctx context.Context, assetID, id string, revision int) (preparation.Report, error) {
	p, sum, e := s.GetProcessingProfile(ctx, id, revision)
	if e != nil {
		return preparation.Report{}, e
	}
	asset, e := s.st.GetAsset(ctx, assetID)
	if e != nil {
		return preparation.Report{}, e
	}
	r := preparation.Report{SchemaVersion: 1, ProfileID: p.ProfileID, ProfileRevision: p.Revision, ProfileSHA256: sum, Status: "blocked", Checks: []preparation.Check{}}
	local, e := preparation.NewLocalProbe().Probe(ctx, p)
	if e != nil {
		return r, e
	}
	r.Checks = append(r.Checks, local...)
	checks, version, e := s.preparedChecks(ctx, s.st, asset, p, sum)
	if e != nil {
		return r, e
	}
	r.Checks = append(r.Checks, checks...)
	r.ExpectedAssetVersion = version
	r.CanStart = true
	for _, c := range r.Checks {
		if c.Status != "passed" {
			r.CanStart = false
		}
	}
	if r.CanStart {
		r.Status = "ready"
	}
	return r, nil
}
func (s *Service) StartPreparedWorkflow(ctx context.Context, actor string, in preparation.PreparedStart) (model.WorkflowRun, error) {
	if in.ProfileRevision < 1 || in.IdempotencyKey == "" || in.ExpectedAssetVersion == "" {
		return model.WorkflowRun{}, model.ErrArgument
	}
	p, sha, e := s.GetProcessingProfile(ctx, in.ProfileID, in.ProfileRevision)
	if e != nil {
		return model.WorkflowRun{}, e
	}
	in.ProfileSHA256 = sha
	// Host tools are checked outside the write transaction. Governance, file
	// digest, consent and expiring Worker capabilities are rechecked inside it.
	checks, e := preparation.NewLocalProbe().Probe(ctx, p)
	if e != nil {
		return model.WorkflowRun{}, e
	}
	for _, c := range checks {
		if c.Status != "passed" {
			return model.WorkflowRun{}, model.ErrInvalidState
		}
	}
	return s.startWorkflow(ctx, actor, in.AssetID, in.RevisionID, in.IdempotencyKey, p.ContentMode, &in)
}
func (s *Service) boundProfile(ctx context.Context, st *store.Store, run string) (*preparation.Profile, string, error) {
	b, e := st.ProfileBinding(ctx, run)
	if errors.Is(e, model.ErrNotFound) {
		return nil, "", nil
	}
	if e != nil {
		return nil, "", e
	}
	p, e := verifiedProfile(b.ProfileID, b.JSON, b.SHA256)
	if e != nil {
		return nil, "", e
	}
	if p.Revision != b.Revision {
		return nil, "", model.ErrInvalidState
	}
	if p.Vision.External() || p.Narration.External() {
		ok, e := st.ProfileConsent(ctx, p.ProfileID, p.Revision, b.SHA256)
		if e != nil {
			return nil, "", e
		}
		if !ok {
			return nil, "", model.ErrForbidden
		}
	}
	return &p, b.SHA256, nil
}

func (s *Service) WorkflowProfile(ctx context.Context, run string) (*preparation.Profile, string, error) {
	return s.boundProfile(ctx, s.st, strings.TrimSpace(run))
}

func (s *Service) WorkflowProfileInfo(ctx context.Context, run string) (*store.ProfileBinding, error) {
	b, e := s.st.ProfileBinding(ctx, run)
	if errors.Is(e, model.ErrNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return &b, nil
}
