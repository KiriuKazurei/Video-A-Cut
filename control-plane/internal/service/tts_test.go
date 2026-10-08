package service_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

func ttsSettings() tts.Settings {
	return tts.Settings{Engine: "stepfun", Endpoint: "https://api.stepfun.com/step_plan/v1", Model: "stepaudio-2.5-tts", TokenEnv: "VAC_TTS_TEST_MISSING", Voice: "vibrant-youth", Language: "zh", Speed: 1, Volume: 1}
}

func TestTTSTemplateCASBindingArchiveAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tts.db")
	st, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	svc := service.New(st)
	ctx := t.Context()
	req := tts.SaveRequest{TemplateID: "voice_test", Name: "测试声音", Settings: ttsSettings(), ReferenceText: "参考试听", IdempotencyKey: "one"}
	first, e := svc.SaveTTSTemplate(ctx, "human:test", req)
	if e != nil {
		t.Fatal(e)
	}
	repeat, e := svc.SaveTTSTemplate(ctx, "human:test", req)
	if e != nil || repeat.SHA256 != first.SHA256 {
		t.Fatal(repeat, e)
	}
	p := builtinProfile(1)
	p.TTSTemplate = &tts.Binding{TemplateID: first.TemplateID, Revision: 1}
	saved, e := svc.SaveProcessingProfile(ctx, "human:test", "bind", 0, p)
	if e != nil {
		t.Fatal(e)
	}
	if saved.TTSTemplate.Settings.Speed != 1 || saved.TTSTemplate.SHA256 != first.SHA256 || !saved.External() {
		t.Fatal("binding or consent missing", saved)
	}
	if e := svc.SetProcessingConsent(ctx, "human:test", saved.ProfileID, 1, true); e != nil {
		t.Fatal(e)
	}
	req.IdempotencyKey = "two"
	req.ExpectedRevision = 1
	req.Settings.Speed = 1.5
	second, e := svc.SaveTTSTemplate(ctx, "human:test", req)
	if e != nil || second.Revision != 2 {
		t.Fatal(second, e)
	}
	req.IdempotencyKey = "stale"
	if _, e = svc.SaveTTSTemplate(ctx, "human:test", req); !errors.Is(e, model.ErrConflict) {
		t.Fatal("stale save", e)
	}
	if e = svc.ArchiveTTSTemplate(ctx, "human:test", first.TemplateID, 1); !errors.Is(e, model.ErrConflict) {
		t.Fatal("stale archive", e)
	}
	if e = svc.ArchiveTTSTemplate(ctx, "human:test", first.TemplateID, 2); e != nil {
		t.Fatal(e)
	}
	if list, e := svc.ListTTSTemplates(ctx); e != nil || len(list) != 0 {
		t.Fatal(list, e)
	}
	if e = st.Close(); e != nil {
		t.Fatal(e)
	}
	st, e = store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	svc = service.New(st)
	historical, e := svc.GetTTSTemplate(ctx, first.TemplateID, 1)
	if e != nil || historical.SHA256 != first.SHA256 || !historical.Archived {
		t.Fatal(historical, e)
	}
	fixed, _, e := svc.GetProcessingProfile(ctx, p.ProfileID, 1)
	if e != nil || fixed.TTSTemplate.Revision != 1 || fixed.TTSTemplate.Settings.Speed != 1 {
		t.Fatal("bound profile drift", fixed, e)
	}
}

func TestTTSLegacyProfileCanonicalCompatibility(t *testing.T) {
	p := builtinProfile(1)
	canonical, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	var legacy map[string]any
	if e = json.Unmarshal(canonical, &legacy); e != nil {
		t.Fatal(e)
	}
	if _, ok := legacy["tts_template"]; ok {
		t.Fatal("optional field alters legacy canonical JSON")
	}
	// Pin the pre-TTS canonical field order as well as omission.
	if string(canonical) != "{\"schema_version\":1,\"profile_id\":\"local_regression\",\"revision\":1,\"name\":\"本地回归\",\"content_mode\":\"builtin\",\"vision\":{\"adapter\":\"builtin\"},\"narration\":{\"adapter\":\"builtin\"},\"sampling\":{\"max_frames\":12,\"max_bytes\":12582912,\"timeout_seconds\":60},\"tts_voice\":\"Microsoft Huihui Desktop\",\"export_target\":\"premiere\"}" {
		t.Fatal("legacy canonical JSON changed", string(canonical))
	}
}

func TestTTSPreviewRecoveryNeverResubmits(t *testing.T) {
	svc := newService(t)
	st := storeOf(t, svc)
	ctx := t.Context()
	p := tts.Preview{JobID: "tts_crashed", Status: "running", Settings: ttsSettings(), Text: "测试", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if e := st.PutTTSPreview(ctx, p); e != nil {
		t.Fatal(e)
	}
	if e := svc.ConfigureTTS("missing-python", filepath.Join(t.TempDir(), "missing-runtime")); e != nil {
		t.Fatal(e)
	}
	defer svc.CloseTTS()
	got, e := svc.GetTTSPreview(ctx, p.JobID)
	if e != nil || got.Status != "failed" || got.Message == "" {
		t.Fatal(got, e)
	}
}

func TestTTSBindingPreflightRequiresExactTemplateCapabilityAndConsent(t *testing.T) {
	svc, _, run := workflowSetup(t)
	ctx := t.Context()
	if _, e := svc.CancelWorkflow(ctx, "human:test", run.RunID, "TTS fixture", run.Version); e != nil {
		t.Fatal(e)
	}
	template, e := svc.SaveTTSTemplate(ctx, "human:test", tts.SaveRequest{TemplateID: "bound_cloud", Name: "云端", Settings: ttsSettings(), IdempotencyKey: "cloud"})
	if e != nil {
		t.Fatal(e)
	}
	p := builtinProfile(1)
	p.TTSTemplate = &tts.Binding{TemplateID: template.TemplateID, Revision: 1}
	p, e = svc.SaveProcessingProfile(ctx, "human:test", "cloud-profile", 0, p)
	if e != nil {
		t.Fatal(e)
	}
	sum, _ := p.Fingerprint()
	for _, role := range []string{"recognizer", "narrator", "exporter"} {
		if e := svc.ReportWorkerCapability(ctx, role, role, preparation.Capability{ProfileSHA256: sum, ToolsReady: true, CredentialsReady: true, TTSVoices: []string{p.TTSVoice}}); e != nil {
			t.Fatal(e)
		}
	}
	check := func(code string) string {
		r, e := svc.PreparedPreflight(ctx, "clip", p.ProfileID, 1)
		if e != nil {
			t.Fatal(e)
		}
		for _, c := range r.Checks {
			if c.Code == code {
				return c.Status
			}
		}
		t.Fatal("missing check", code)
		return ""
	}
	if check("worker_narrator") != "blocked" || check("external_authorization") != "blocked" {
		t.Fatal("old SAPI capability or missing consent allowed cloud TTS")
	}
	ready := true
	if e = svc.ReportWorkerCapability(ctx, "narrator", "narrator", preparation.Capability{ProfileSHA256: sum, ToolsReady: true, CredentialsReady: true, TTSReady: &ready, TTSTemplateSHA256: template.SHA256}); e != nil {
		t.Fatal(e)
	}
	if e = svc.SetProcessingConsent(ctx, "human:test", p.ProfileID, 1, true); e != nil {
		t.Fatal(e)
	}
	if check("worker_narrator") != "passed" || check("external_authorization") != "passed" {
		t.Fatal("exact binding capability or consent rejected")
	}
}

func TestTTSPreviewMissingCredentialAndIdempotency(t *testing.T) {
	python := os.Getenv("VAC_TTS_TEST_PYTHON")
	if python == "" {
		t.Skip("set VAC_TTS_TEST_PYTHON to run the real bridge")
	}
	t.Setenv("VAC_TTS_TEST_MISSING", "")
	svc := newService(t)
	if e := svc.ConfigureDeliveryRoot(t.TempDir()); e != nil {
		t.Fatal(e)
	}
	dir, e := filepath.Abs("../../../workers/python")
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.ConfigureTTS(python, dir); e != nil {
		t.Fatal(e)
	}
	defer svc.CloseTTS()
	req := tts.PreviewRequest{Text: "真实桥接但不外发", Settings: ttsSettings(), IdempotencyKey: "missing"}
	first, e := svc.StartTTSPreview(t.Context(), "human:test", req)
	if e != nil {
		t.Fatal(e)
	}
	same, e := svc.StartTTSPreview(t.Context(), "human:test", req)
	if e != nil || same.JobID != first.JobID {
		t.Fatal(same, e)
	}
	req.Text = "不同文本"
	if _, e = svc.StartTTSPreview(t.Context(), "human:test", req); !errors.Is(e, model.ErrConflict) {
		t.Fatal("idempotency changed", e)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		got, e := svc.GetTTSPreview(t.Context(), first.JobID)
		if e != nil {
			t.Fatal(e)
		}
		if got.Status == "failed" {
			if got.Message != "缺少 TTS 凭证，请在启动环境设置凭证变量" {
				t.Fatal(got)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("bridge did not terminate")
}
