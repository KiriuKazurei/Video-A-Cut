package service

import (
	"strings"
	"testing"
)

func TestToolSandboxDoesNotInheritSecretsOrLoaderOverrides(t *testing.T) {
	t.Setenv("VAC_HUMAN_ACCESS_TOKEN", "human-only")
	t.Setenv("UNRELATED_API_KEY", "private")
	t.Setenv("PYTHONPATH", "untrusted-loader")
	t.Setenv("NODE_OPTIONS", "--require bad.js")
	t.Setenv("SELECTED_MODEL_TOKEN", "selected")
	env := strings.Join(toolEnvironment("SELECTED_MODEL_TOKEN", "VAC_HUMAN_ACCESS_TOKEN"), "\n")
	for _, forbidden := range []string{"human-only", "private", "untrusted-loader", "bad.js"} {
		if strings.Contains(env, forbidden) {
			t.Fatalf("leaked %s", forbidden)
		}
	}
	if !strings.Contains(env, "SELECTED_MODEL_TOKEN=selected") {
		t.Fatal("selected provider unavailable")
	}
	names := payloadCredentialNames(map[string]any{"provider": map[string]any{"token_env": "SELECTED_MODEL_TOKEN"}, "run": map[string]any{"token_env": "UNRELATED_API_KEY"}}, "provider")
	if len(names) != 1 || names[0] != "SELECTED_MODEL_TOKEN" {
		t.Fatal(names)
	}
}
