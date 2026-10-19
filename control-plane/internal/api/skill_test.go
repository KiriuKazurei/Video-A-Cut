package api

import (
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"net/http"
	"testing"
)

func TestSkillHTTPProjectAndBuiltinResources(t *testing.T) {
	srv := newSeededServer(t)
	if err := srv.svc.ConfigureDeliveryRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := srv.svc.ConfigureSkillHarness(t.Context()); err != nil {
		t.Fatal(err)
	}
	rec := postJSON(t, srv, "/api/projects", `{"name":"new","flow":"visual_heavy","expected_version":0,"assets":[],"visible":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var p skill.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	view := get(t, srv, "/api/projects/"+p.ID+"/visible-set")
	if view.Code != 200 {
		t.Fatal(view.Body.String())
	}
	rec = postJSON(t, srv, "/api/skill-runs", `{"project_id":"`+p.ID+`","skill_id":"vac-visual-summary","execution_mode":"external_mcp","visible_set_sha":"wrong","goal":"summarize","idempotency_key":"one"}`)
	if rec.Code != 403 {
		t.Fatalf("unconfirmed run: %d %s", rec.Code, rec.Body.String())
	}
	res := get(t, srv, "/api/skills/video-use/content")
	var pack skill.Package
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &pack) != nil || pack.Resources["upstream/LICENSE"] == "" || pack.Resources["upstream/SKILL.md"] == "" {
		t.Fatalf("missing builtin provenance %s", res.Body.String())
	}
}
