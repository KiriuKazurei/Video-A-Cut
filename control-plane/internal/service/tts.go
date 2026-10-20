package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

func templateHash(t tts.Template) string {
	t.SHA256 = ""
	t.Archived = false
	t.CreatedAt = time.Time{}
	return tts.Hash(t)
}
func (s *Service) GetTTSTemplate(ctx context.Context, id string, revision int) (tts.Template, error) {
	if !tts.Identifier.MatchString(id) || revision < 0 {
		return tts.Template{}, model.ErrArgument
	}
	t, e := s.st.GetTTSTemplate(ctx, id, revision)
	if e != nil {
		return t, e
	}
	if t.TemplateID != id || t.Revision < 1 || (revision > 0 && revision != t.Revision) || t.Settings.Validate() != nil || templateHash(t) != t.SHA256 {
		return t, model.ErrInvalidState
	}
	return t, nil
}
func (s *Service) ListTTSTemplates(ctx context.Context) ([]tts.Template, error) {
	list, e := s.st.ListTTSTemplates(ctx)
	if e != nil {
		return nil, e
	}
	for _, t := range list {
		if !tts.Identifier.MatchString(t.TemplateID) || t.Revision < 1 || t.Settings.Validate() != nil || templateHash(t) != t.SHA256 {
			return nil, model.ErrInvalidState
		}
	}
	return list, nil
}
func (s *Service) SaveTTSTemplate(ctx context.Context, actor string, req tts.SaveRequest) (tts.Template, error) {
	if actor == "" || !tts.Identifier.MatchString(req.TemplateID) || req.ExpectedRevision < 0 || !tts.ValidText(req.Name, 100, true) || !tts.ValidText(req.ReferenceText, 1000, false) || !tts.ValidText(req.IdempotencyKey, 128, true) {
		return tts.Template{}, model.ErrArgument
	}
	if e := req.Settings.Validate(); e != nil {
		return tts.Template{}, fmt.Errorf("%v: %w", e, model.ErrArgument)
	}
	t := tts.Template{TemplateID: req.TemplateID, Revision: req.ExpectedRevision + 1, Name: req.Name, Settings: req.Settings, ReferenceText: req.ReferenceText, CreatedAt: time.Now().UTC()}
	t.SHA256 = templateHash(t)
	scope := "tts.template.save:" + t.TemplateID
	e := s.st.Transaction(ctx, func(tx *store.Store) error {
		digest, body, e := tx.GetIdempotency(ctx, scope, req.IdempotencyKey)
		if e == nil {
			if digest != t.SHA256 {
				return model.ErrConflict
			}
			return json.Unmarshal([]byte(body), &t)
		}
		if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		if req.ExpectedRevision == 0 {
			if _, e := tx.GetTTSTemplate(ctx, t.TemplateID, 0); e == nil {
				return model.ErrConflict
			} else if !errors.Is(e, model.ErrNotFound) {
				return e
			}
		}
		if e = tx.SaveTTSTemplate(ctx, t, req.ExpectedRevision); e != nil {
			return e
		}
		bodyBytes, _ := json.Marshal(t)
		if e = tx.PutIdempotency(ctx, scope, req.IdempotencyKey, t.SHA256, string(bodyBytes)); e != nil {
			return e
		}
		return tx.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "tts.template.save", Target: t.TemplateID, Detail: fmt.Sprintf("revision=%d sha256=%s", t.Revision, t.SHA256)})
	})
	if e == nil {
		s.publish("tts.template.changed", map[string]any{"template_id": t.TemplateID})
	}
	return t, e
}
func (s *Service) ArchiveTTSTemplate(ctx context.Context, actor, id string, expected int) error {
	if actor == "" || !tts.Identifier.MatchString(id) || expected < 1 {
		return model.ErrArgument
	}
	e := s.st.Transaction(ctx, func(tx *store.Store) error {
		if e := tx.ArchiveTTSTemplate(ctx, id, expected); e != nil {
			return e
		}
		return tx.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "tts.template.archive", Target: id})
	})
	if e == nil {
		s.publish("tts.template.changed", map[string]any{"template_id": id})
	}
	return e
}
func (s *Service) resolveTTSBinding(ctx context.Context, b *tts.Binding) (*tts.Binding, error) {
	if b == nil {
		return nil, nil
	}
	if b.Revision < 1 {
		return nil, model.ErrArgument
	}
	t, e := s.GetTTSTemplate(ctx, b.TemplateID, b.Revision)
	if e != nil {
		return nil, e
	}
	if b.SHA256 != "" && b.SHA256 != t.SHA256 {
		return nil, model.ErrConflict
	}
	return &tts.Binding{TemplateID: t.TemplateID, Revision: t.Revision, SHA256: t.SHA256, Settings: &t.Settings}, nil
}

func (s *Service) StartTTSPreview(ctx context.Context, actor string, req tts.PreviewRequest) (tts.Preview, error) {
	if s.WorkspaceEnabled() && req.Settings.External() && !s.ProviderPolicy().ExternalTTS { return tts.Preview{}, fmt.Errorf("请在服务商设置中启用云端配音: %w", model.ErrForbidden) }
	if actor == "" || !tts.ValidText(req.Text, 1000, true) || !tts.ValidText(req.IdempotencyKey, 128, true) {
		return tts.Preview{}, model.ErrArgument
	}
	if e := req.Settings.Validate(); e != nil {
		return tts.Preview{}, fmt.Errorf("%v: %w", e, model.ErrArgument)
	}
	r := s.ttsRuntime
	if r == nil || !r.ready {
		return tts.Preview{}, fmt.Errorf("TTS Python 运行目录未配置: %w", model.ErrInvalidState)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return tts.Preview{}, model.ErrInvalidState
	}
	digest := tts.Hash(struct {
		Text     string       `json:"text"`
		Settings tts.Settings `json:"settings"`
	}{req.Text, req.Settings})
	p := tts.Preview{JobID: newID("tts"), Text: req.Text, Settings: req.Settings, InputSHA256: digest, Status: "queued", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	created := false
	e := s.st.Transaction(ctx, func(tx *store.Store) error {
		hash, body, e := tx.GetIdempotency(ctx, "tts.preview", req.IdempotencyKey)
		if e == nil {
			if hash != digest {
				return model.ErrConflict
			}
			return json.Unmarshal([]byte(body), &p)
		}
		if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		if len(r.cancel) >= 8 {
			return model.ErrResourceBusy
		}
		if e = tx.PutTTSPreview(ctx, p); e != nil {
			return e
		}
		bodyBytes, _ := json.Marshal(p)
		if e = tx.PutIdempotency(ctx, "tts.preview", req.IdempotencyKey, digest, string(bodyBytes)); e != nil {
			return e
		}
		created = true
		return tx.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "tts.preview.create", Target: p.JobID, Detail: digest})
	})
	if e != nil {
		return p, e
	}
	if created {
		taskCtx, cancel := context.WithTimeout(r.ctx, 120*time.Second)
		r.cancel[p.JobID] = cancel
		r.wg.Add(1)
		go s.executeTTSPreview(taskCtx, p)
		s.publish("tts.preview.changed", map[string]any{"job_id": p.JobID})
	} else {
		p, e = s.st.GetTTSPreview(ctx, p.JobID)
	}
	return p, e
}
func (s *Service) GetTTSPreview(ctx context.Context, id string) (tts.Preview, error) {
	if !tts.Identifier.MatchString(id) {
		return tts.Preview{}, model.ErrArgument
	}
	p, e := s.st.GetTTSPreview(ctx, id)
	if e == nil && time.Since(p.CreatedAt) > 24*time.Hour {
		p.Expired = true
	}
	return p, e
}
func (s *Service) ListTTSPreviews(ctx context.Context) ([]tts.Preview, error) {
	jobs, e := s.st.ListTTSPreviews(ctx)
	if len(jobs) > 50 {
		jobs = jobs[:50]
	}
	for i := range jobs {
		if time.Since(jobs[i].CreatedAt) > 24*time.Hour {
			jobs[i].Expired = true
		}
	}
	return jobs, e
}
func (s *Service) CancelTTSPreview(ctx context.Context, actor, id string) (tts.Preview, error) {
	r := s.ttsRuntime
	if r == nil || !tts.Identifier.MatchString(id) || actor == "" {
		return tts.Preview{}, model.ErrArgument
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, e := s.st.GetTTSPreview(ctx, id)
	if e != nil {
		return p, e
	}
	if p.Status == "cancelled" {
		return p, nil
	}
	if p.Status != "queued" && p.Status != "running" {
		return p, model.ErrInvalidState
	}
	p.Status = "cancelled"
	p.Message = "用户取消试听"
	p.UpdatedAt = time.Now().UTC()
	e = s.st.Transaction(ctx, func(tx *store.Store) error {
		if e := tx.PutTTSPreview(ctx, p); e != nil {
			return e
		}
		return tx.WriteAudit(ctx, model.AuditLog{Actor: actor, Action: "tts.preview.cancel", Target: id})
	})
	if e == nil {
		if cancel := r.cancel[id]; cancel != nil {
			cancel()
		}
		s.publish("tts.preview.changed", map[string]any{"job_id": id})
	}
	return p, e
}

func ttsErrorMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "试听生成超过 120 秒期限，请手动重试"
	}
	if errors.Is(err, context.Canceled) {
		return "试听已中断"
	}
	return "TTS 运行环境或音频处理失败，请检查 Python、FFmpeg 与声音配置"
}
func ttsTokenEnv(name string) string {
	if name == "" {
		return "VAC_TTS_TOKEN"
	}
	return strings.TrimSpace(name)
}
