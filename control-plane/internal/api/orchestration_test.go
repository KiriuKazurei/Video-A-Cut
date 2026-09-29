package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestCreateTaskWithDependsOn(t *testing.T) {
	srv := newSeededServer(t, model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested})
	if rec := post(t, srv, "/api/tasks", createBody("t_1")); rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}
	body := `{"task_id":"t_2","asset_id":"a_1","type":"export","agent_role":"exporter","depends_on":["t_1"]}`
	rec := post(t, srv, "/api/tasks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with deps: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeTask(t, rec)
	if len(got.DependsOn) != 1 || got.DependsOn[0] != "t_1" || got.Attempts != 0 {
		t.Fatalf("task = %+v", got)
	}
	for name, tc := range map[string]struct {
		body string
		code int
	}{
		"missing dep": {`{"task_id":"t_3","asset_id":"a_1","type":"export","agent_role":"exporter","depends_on":["nope"]}`, http.StatusNotFound},
		"self dep":    {`{"task_id":"t_4","asset_id":"a_1","type":"export","agent_role":"exporter","depends_on":["t_4"]}`, http.StatusBadRequest},
		"attempts":    {`{"task_id":"t_5","asset_id":"a_1","type":"export","agent_role":"exporter","attempts":9}`, http.StatusBadRequest},
	} {
		if rec := post(t, srv, "/api/tasks", tc.body); rec.Code != tc.code {
			t.Errorf("%s: %d %s", name, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}
