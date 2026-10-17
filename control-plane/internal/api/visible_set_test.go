package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func TestVisibleSetAPIBlocksScriptUntilConfirmed(t *testing.T) {
	srv := newSeededServer(t,
		model.Asset{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true, AllowedAgents: []string{"narrator"}},
		model.Asset{AssetID: "other", Status: model.AssetStatusIngested, AgentVisible: false, AllowedAgents: []string{"narrator"}},
	)

	rec := postJSON(t, srv, "/api/assets/clip/script-entry", `{}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("script entry before confirm = %d %s, want 403", rec.Code, rec.Body.String())
	}
	listed := get(t, srv, "/api/assets/clip/scripts")
	if listed.Code != http.StatusOK || listed.Body.String() != "[]\n" && listed.Body.String() != "[]" {
		body := listed.Body.String()
		var rows []model.Script
		if err := json.Unmarshal(listed.Body.Bytes(), &rows); err != nil || len(rows) != 0 {
			t.Fatalf("scripts after refused entry = %d %s", listed.Code, body)
		}
	}

	view := get(t, srv, "/api/visible-set")
	if view.Code != http.StatusOK {
		t.Fatalf("visible-set = %d %s", view.Code, view.Body.String())
	}
	var set struct {
		SHA256    string   `json:"sha256"`
		Confirmed bool     `json:"confirmed"`
		AssetIDs  []string `json:"asset_ids"`
		Assets    []struct {
			AssetID      string `json:"asset_id"`
			Ingested     bool   `json:"ingested"`
			AgentVisible bool   `json:"agent_visible"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(view.Body.Bytes(), &set); err != nil {
		t.Fatal(err)
	}
	if set.Confirmed || len(set.AssetIDs) != 1 || set.AssetIDs[0] != "clip" || len(set.Assets) != 1 || !set.Assets[0].Ingested || !set.Assets[0].AgentVisible {
		t.Fatalf("set = %+v", set)
	}
	assets := get(t, srv, "/api/assets")
	if assets.Code != http.StatusOK {
		t.Fatalf("assets = %d %s", assets.Code, assets.Body.String())
	}
	var library []model.Asset
	if err := json.Unmarshal(assets.Body.Bytes(), &library); err != nil || len(library) != 2 {
		t.Fatalf("library = %s %v", assets.Body.String(), err)
	}
	for _, a := range library {
		if a.Status == "" {
			t.Fatalf("asset %s has no ingested status", a.AssetID)
		}
		if a.AssetID == "other" && a.AgentVisible {
			t.Fatal("other is ingested but must stay out of the visible set")
		}
	}

	stale := postJSON(t, srv, "/api/visible-set/confirm", `{"visible_set_sha":"`+set.SHA256[:32]+`"}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale confirm = %d %s", stale.Code, stale.Body.String())
	}
	ok := postJSON(t, srv, "/api/visible-set/confirm", `{"visible_set_sha":"`+set.SHA256+`"}`)
	if ok.Code != http.StatusOK {
		t.Fatalf("confirm = %d %s", ok.Code, ok.Body.String())
	}
	entered := postJSON(t, srv, "/api/assets/clip/script-entry", `{}`)
	if entered.Code != http.StatusOK {
		t.Fatalf("script entry = %d %s", entered.Code, entered.Body.String())
	}
	var script model.Script
	if err := json.Unmarshal(entered.Body.Bytes(), &script); err != nil || script.Status != model.ScriptDraft {
		t.Fatalf("script = %s %v", entered.Body.String(), err)
	}
	final := postJSON(t, srv, "/api/scripts/"+script.ScriptID+"/finalize", `{}`)
	if final.Code != http.StatusOK {
		t.Fatalf("finalize = %d %s", final.Code, final.Body.String())
	}

	batch := postJSON(t, srv, "/api/assets/visibility", `{"asset_ids":["other"],"agent_visible":true}`)
	if batch.Code != http.StatusOK {
		t.Fatalf("batch = %d %s", batch.Code, batch.Body.String())
	}
	after := get(t, srv, "/api/assets/clip/scripts")
	var rows []model.Script
	if err := json.Unmarshal(after.Body.Bytes(), &rows); err != nil || len(rows) != 1 || rows[0].Status != model.ScriptNeedsRefinalize || rows[0].ScriptID != script.ScriptID {
		t.Fatalf("scripts after visibility change = %s %v", after.Body.String(), err)
	}
	if rec := postJSON(t, srv, "/api/assets/clip/script-entry", `{}`); rec.Code != http.StatusForbidden {
		t.Fatalf("entry after the set changed = %d %s, want 403", rec.Code, rec.Body.String())
	}
}
