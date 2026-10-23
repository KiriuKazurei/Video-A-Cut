package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDrawBuiltinsAreInstructionStubs(t *testing.T) {
	byID := map[string]Package{}
	for _, p := range Builtins() {
		byID[p.ID] = p
	}
	video := byID["video-use"]
	if video.Version != "0.1.0-vac.5" || video.Runtime != "harness" {
		t.Fatalf("video-use promotion: %#v", video.Version)
	}
	if !strings.Contains(video.Content, "绘制") || !strings.Contains(video.Content, "0.1.0-vac.5") || !strings.Contains(video.Content, "方案接线中") {
		t.Fatal("video-use adapter is not the route-2 entry")
	}
	for _, id := range []string{"vac-draw-hyperframes", "vac-draw-remotion", "vac-draw-pipeline"} {
		p, ok := byID[id]
		if !ok {
			t.Fatalf("missing builtin %s", id)
		}
		if p.Runtime != "instructions_only" || len(p.Tools) != 0 || len(p.Permissions) != 0 || !p.Builtin || !p.Enabled {
			t.Fatalf("%s is not an empty instruction stub: runtime=%s tools=%v", id, p.Runtime, p.Tools)
		}
		if !strings.Contains(p.Content, "方案接线中") || !strings.Contains(p.Content, "import_into_editing") || !strings.Contains(p.Content, "composite_preview") || !strings.Contains(p.Content, "merge_render") {
			t.Fatalf("%s does not encode the three handoffs", id)
		}
		assertNoShell(t, id, p.Content)
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "skills", id, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimRight(string(raw), "\n") != strings.TrimRight(p.Content, "\n") {
			t.Fatalf("%s SKILL.md drifted from the builtin", id)
		}
		var meta Package
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "skills", id, "skill.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(body, &meta); err != nil {
			t.Fatal(err)
		}
		if meta.ID != p.ID || meta.Version != p.Version || meta.Runtime != p.Runtime || len(meta.Tools) != 0 {
			t.Fatalf("%s skill.json does not match the builtin", id)
		}
	}
	assertNoShell(t, "video-use", video.Content)
}

func assertNoShell(t *testing.T, id, content string) {
	t.Helper()
	for _, marker := range []string{"npx ", "exec.Command", "child_process", "os/exec", "bash -c", "/bin/sh"} {
		if strings.Contains(content, marker) {
			t.Fatalf("%s exposes a shell marker %q", id, marker)
		}
	}
}
