package preparation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTranscriptionIsOptionalVersionedProvider(t *testing.T) {
	p := fixture()
	before, err := p.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	p.Transcription = &TranscriptionProvider{APIFormat: "elevenlabs", Endpoint: "https://api.elevenlabs.io/v1", Model: "scribe_v1", TokenEnv: "VAC_ASR_TOKEN"}
	after, err := p.Fingerprint()
	if err != nil || after == before || !p.External() {
		t.Fatalf("transcription not bound to profile fingerprint: %v", err)
	}
	p.Transcription.Endpoint = "https://api.elevenlabs.io/v1?key=secret"
	if p.Validate() == nil {
		t.Fatal("credential in endpoint was accepted")
	}
}

func TestLocalTranscriptionIsOfflineAndBindsModelContent(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"model.bin", "config.json", "tokenizer.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	provider := TranscriptionProvider{APIFormat: "faster_whisper", ModelPath: dir, Device: "cpu", ComputeType: "int8"}
	p := fixture()
	p.Transcription = &provider
	before, err := p.Fingerprint()
	if err != nil || p.External() {
		t.Fatalf("local profile requires cloud permission: %v", err)
	}
	modelBefore, err := provider.ModelFingerprint(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "model.bin"), []byte("changed model"), 0600); err != nil {
		t.Fatal(err)
	}
	modelAfter, err := provider.ModelFingerprint(t.Context())
	if err != nil || modelBefore == modelAfter {
		t.Fatal("model contents did not invalidate cache identity", err)
	}
	provider.Language = "zh"
	after, err := p.Fingerprint()
	if err != nil || before == after {
		t.Fatal("language not versioned", err)
	}
	for name, mutate := range map[string]func(*TranscriptionProvider){
		"model_identifier": func(v *TranscriptionProvider) { v.ModelPath = "base" },
		"cloud_endpoint":   func(v *TranscriptionProvider) { v.Endpoint = "https://example.invalid" },
		"cloud_key":        func(v *TranscriptionProvider) { v.TokenEnv = "SECRET" },
		"relative_python":  func(v *TranscriptionProvider) { v.PythonExecutable = "python" },
		"cpu_float16":      func(v *TranscriptionProvider) { v.ComputeType = "float16" },
		"too_many_threads": func(v *TranscriptionProvider) { v.CPUThreads = 100 },
	} {
		t.Run(name, func(t *testing.T) {
			v := provider
			mutate(&v)
			if v.Validate() == nil {
				t.Fatal("invalid local configuration accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := provider.ModelFingerprint(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspection: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "tokenizer.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.ModelFingerprint(t.Context()); err == nil {
		t.Fatal("incomplete model accepted")
	}
}
