package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDrawBuiltinsAreExecutableLocalStrategies(t *testing.T) {
	byID := map[string]Package{}
	for _, p := range Builtins() {
		byID[p.ID] = p
	}
	video := byID["video-use"]
	if video.Version != "0.1.0-vac.7" || video.Runtime != "harness" || !slices.Contains(video.Tools, "draw.render") {
		t.Fatal("video-use cannot execute drawing")
	}
	for _, id := range []string{"vac-draw-hyperframes", "vac-draw-remotion", "vac-draw-pipeline"} {
		p := byID[id]
		if p.Runtime != "harness" || !p.Enabled || !p.Builtin || p.Version != "0.3.0" {
			t.Fatalf("inactive drawing Skill: %#v", p)
		}
		for _, tool := range []string{"draw.list", "draw.save", "draw.check", "draw.render", "draw.get_frame", "draw.import", "timeline.edit", "run.finish"} {
			if !slices.Contains(p.Tools, tool) {
				t.Fatalf("%s missing %s", id, tool)
			}
		}
		if !strings.Contains(p.Content, "strategy=draw") || p.Resources["vac/tool-contracts.md"] == "" {
			t.Fatal("missing strategy or readable contracts")
		}
		body, e := os.ReadFile(filepath.Join("..", "..", "..", "skills", id, "SKILL.md"))
		if e != nil {
			t.Fatal(e)
		}
		if !strings.HasPrefix(p.Content, strings.TrimRight(strings.ReplaceAll(string(body), "\r\n", "\n"), "\r\n")) {
			t.Fatalf("%s builtin documentation drift", id)
		}
		var meta Package
		raw, e := os.ReadFile(filepath.Join("..", "..", "..", "skills", id, "skill.json"))
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(raw, &meta); e != nil {
			t.Fatal(e)
		}
		if meta.ID != p.ID || meta.Version != p.Version || meta.Runtime != p.Runtime || !slices.Equal(meta.Tools, p.Tools) {
			t.Fatalf("%s metadata mismatch", id)
		}
		assertNoShell(t, id, p.Content)
	}
	assertNoShell(t, "video-use", video.Content)
}
func assertNoShell(t *testing.T, id, content string) {
	t.Helper()
	for _, marker := range []string{"npx ", "exec.Command", "child_process", "os/exec", "bash -c", "/bin/sh"} {
		if strings.Contains(content, marker) {
			t.Fatalf("%s exposes shell marker %q", id, marker)
		}
	}
}
