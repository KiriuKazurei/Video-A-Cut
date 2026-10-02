package preparation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func fixture() Profile {
	return Profile{SchemaVersion: 1, ProfileID: "local_regression", Revision: 1, Name: "本地回归", ContentMode: "builtin", Vision: Provider{Adapter: "builtin"}, Narration: Provider{Adapter: "builtin"}, Sampling: Sampling{MaxFrames: 12, MaxBytes: 12 * 1024 * 1024, TimeoutSeconds: 60}, TTSVoice: "Microsoft Huihui Desktop", ExportTarget: "premiere"}
}

func TestProfileFingerprintAndCredentialBoundary(t *testing.T) {
	p := fixture()
	a, err := p.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	p.Sampling.MaxFrames++
	b, err := p.Fingerprint()
	if err != nil || a == b {
		t.Fatal("configuration changes must alter fingerprint")
	}
	p.ContentMode = "configured"
	p.Vision = Provider{Adapter: "vision", Endpoint: "http://127.0.0.1:1234/vision", Model: "local-model", TokenEnv: "VAC_VISION_TOKEN"}
	p.Narration = Provider{Adapter: "narration", Endpoint: "http://localhost:1234/narration", Model: "local-model", TokenEnv: "VAC_NARRATION_TOKEN"}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.Vision.Endpoint = "https://user:secret-marker@example.com/vision"
	if err := p.Validate(); err == nil || strings.Contains(err.Error(), "secret-marker") {
		t.Fatal("credential URL must be refused without echoing its contents")
	}
	p.Vision.Endpoint = "https://example.com/vision?token=secret-marker"
	if err := p.Validate(); err == nil || strings.Contains(err.Error(), "secret-marker") {
		t.Fatal("query credentials must be refused")
	}
	p.Vision.Endpoint = "http://example.com/vision"
	if err := p.Validate(); err == nil {
		t.Fatal("remote HTTP was accepted")
	}
}

type fakeProbe struct{ called bool }

func (p *fakeProbe) Probe(context.Context, Profile) ([]Check, error) {
	p.called = true
	checks := []Check{}
	for _, code := range requiredRuntimeChecks {
		checks = append(checks, Check{Code: code, Status: "passed", Message: "fixture"})
	}
	return checks, nil
}

func TestPreflightCannotStartEvenWithHealthyTools(t *testing.T) {
	probe := &fakeProbe{}
	r, err := (Engine{Probe: probe}).Check(t.Context(), fixture())
	if err != nil || !probe.called || r.CanStart || r.Status != "blocked" {
		t.Fatalf("unexpected report: %+v, %v", r, err)
	}
	if r.Checks[len(r.Checks)-1].Code != "prepared_launch" {
		t.Fatal("missing explicit launch barrier")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	probe.called = false
	if _, err := (Engine{Probe: probe}).Check(ctx, fixture()); !errors.Is(err, context.Canceled) || probe.called {
		t.Fatal("cancelled preflight must not probe")
	}
}

func TestExternalSettingsAreNotAuthorization(t *testing.T) {
	p := fixture()
	p.ContentMode = "configured"
	p.Vision = Provider{Adapter: "vision", Endpoint: "https://example.com/vision", Model: "model", TokenEnv: "VAC_VISION_TOKEN"}
	p.Narration = Provider{Adapter: "narration", Endpoint: "https://example.com/narration", Model: "model", TokenEnv: "VAC_NARRATION_TOKEN"}
	r, err := (Engine{}).Check(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range r.Checks {
		if c.Code == "external_authorization" && c.Status == "blocked" {
			found = true
		}
	}
	if !found || r.CanStart {
		t.Fatal("profile must not grant outbound permission")
	}
}
