package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestScriptWorkbenchAPIManualAndRevisionConflict(t *testing.T) {
	srv := newSeededServer(t, model.Asset{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true})
	view := get(t, srv, "/api/visible-set")
	var set struct {
		SHA string `json:"sha256"`
	}
	if err := json.Unmarshal(view.Body.Bytes(), &set); err != nil {
		t.Fatal(err)
	}
	postJSON(t, srv, "/api/visible-set/confirm", `{"visible_set_sha":"`+set.SHA+`"}`)
	entry := postJSON(t, srv, "/api/assets/clip/script-entry", `{}`)
	var sc model.Script
	if err := json.Unmarshal(entry.Body.Bytes(), &sc); err != nil {
		t.Fatal(err)
	}
	parse := postJSON(t, srv, "/api/script-workbench/parse", `{"body":"{黑屏}介绍机制"}`)
	if parse.Code != 200 || !bytes.Contains(parse.Body.Bytes(), []byte(`"effect":"黑屏"`)) {
		t.Fatalf("parse: %d %s", parse.Code, parse.Body)
	}
	path := "/api/scripts/" + sc.ScriptID + "/workbench"
	payload := []byte(`{"expected_version":0,"visible_set_sha":"` + set.SHA + `","body":"介绍抽卡机制","title":"手写稿"}`)
	for i, want := range []int{http.StatusOK, http.StatusConflict} {
		rec := httptest.NewRecorder()
		req := localRequest("PUT", path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("save%d: %d %s", i, rec.Code, rec.Body)
		}
	}
	revisions := get(t, srv, "/api/scripts/"+sc.ScriptID+"/revisions")
	if revisions.Code != 200 || !bytes.Contains(revisions.Body.Bytes(), []byte(`"action":"save"`)) {
		t.Fatalf("revisions: %s", revisions.Body)
	}
	finalized := postJSON(t, srv, path+"/finalize", `{"expected_version":1,"visible_set_sha":"`+set.SHA+`"}`)
	if finalized.Code < 400 {
		t.Fatalf("partial draft finalized: %s", finalized.Body)
	}
}

func TestScriptWorkbenchSaveReachesAnOpenSSEStream(t *testing.T) {
	srv := newBusServer(t)
	ctx := t.Context()
	view, err := srv.svc.VisibleSet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = srv.svc.ConfirmVisibleSet(ctx, "human:test", view.SHA256, false); err != nil {
		t.Fatal(err)
	}
	sc, err := srv.svc.EnterScript(ctx, "human:test", sseAsset.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	stream, _ := connectSSE(t, srv)
	defer stream.stop()
	if _, err = srv.svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, service.SaveScriptRequest{VisibleSetSHA: view.SHA256, Body: "介绍抽卡机制"}); err != nil {
		t.Fatal(err)
	}
	frame := stream.waitFrameName(t, "script.changed")
	var payload map[string]string
	if err = json.Unmarshal([]byte(frame.data), &payload); err != nil || payload["script_id"] != sc.ScriptID || payload["asset_id"] != sseAsset.AssetID {
		t.Fatalf("script event=%s err=%v", frame.data, err)
	}
}
