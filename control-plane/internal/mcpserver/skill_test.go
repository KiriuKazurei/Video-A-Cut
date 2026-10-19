package mcpserver

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSkillMCPDistributesInstructionsWithoutHumanTools(t *testing.T) {
	f := newLoopFixture(t, time.Minute, Agent{ID: "editor", Role: "narrator"})
	if err := f.svc.ConfigureSkillHarness(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, bad, raw := f.call("editor", "skills.get", map[string]string{"id": "video-use"})
	if bad || !strings.Contains(raw, "VAC adapter") || !strings.Contains(raw, "upstream/SKILL.md") {
		t.Fatalf("Skill handoff: %s", raw)
	}
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"skill_runs.finalize","arguments":{"run_id":"any","actor":"human:fake"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}`
	req, _ := http.NewRequest("POST", f.server.URL, strings.NewReader(body))
	for k, v := range map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "Mcp-Protocol-Version": "2026-07-28", "Mcp-Method": "tools/call", "Mcp-Name": "skill_runs.finalize", "Authorization": "Bearer " + f.tokens["editor"]} {
		req.Header.Set(k, v)
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 400 || !strings.Contains(string(rawBody), "unknown tool") {
		t.Fatalf("MCP exposed human approval: %s", rawBody)
	}
}
