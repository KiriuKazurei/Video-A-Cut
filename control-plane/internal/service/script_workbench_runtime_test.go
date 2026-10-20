package service_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

// Real Go -> Python/FFmpeg -> local HTTP model -> Go -> Node/FFmpeg delivery.
// The provider is a loopback stub: this proves the bridge, not model quality.
func TestScriptWorkbenchRealWorkerRoundTrip(t *testing.T) { testScriptWorkbenchRoundTrip(t, false) }

func TestProjectScriptWorkbenchRealWorkerRoundTrip(t *testing.T) {
	testScriptWorkbenchRoundTrip(t, true)
}

func testScriptWorkbenchRoundTrip(t *testing.T, native bool) {
	if testing.Short() {
		t.Skip("real Python/Node/FFmpeg round trip")
	}
	svc, sc, media := scriptFixture(t)
	ctx := t.Context()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=blue:s=320x180:r=30:d=1", "-c:v", "libx264", "-preset", "ultrafast", "-threads", "1", "-pix_fmt", "yuv420p", media[0].Path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture video: %s %v", out, err)
	}
	if native {
		if err := svc.ConfigureProjectWorkspace(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(svc.CloseProjectWorkspace)
		view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(t.TempDir(), "script.avp"), Name: "Script"})
		if err != nil {
			t.Fatal(err)
		}
		view, err = svc.ImportProjectResource(ctx, view.Document.Project.ID, media[0].Path)
		if err != nil {
			t.Fatal(err)
		}
		sc, err = svc.EnterScript(ctx, "human:window", view.Document.Resources[0].AssetID)
		if err != nil {
			t.Fatal(err)
		}
		media, err = svc.ScriptMediaCatalogue(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			t.Error(err)
		}
		if r.Header.Get("Authorization") != "Bearer local-test-token" {
			t.Error("provider credential missing")
		}
		requests.Add(1)
		var result any
		if bytes.Contains(raw, []byte("Describe only the supplied")) {
			if !bytes.Contains(raw, []byte("image_url")) {
				t.Error("vision was not sent a real sampled frame")
			}
			result = map[string]any{"media": []any{map[string]any{"id": media[0].ID, "description": "蓝色演示画面"}}}
		} else {
			if !bytes.Contains(raw, []byte("visible_media")) {
				t.Error("narrator did not receive inspected media")
			}
			result = map[string]any{"segments": []any{map[string]any{"index": 0, "narration": "抽卡通常消耗资源，随机获得角色或道具。", "media_id": media[0].ID, "start": 0, "end": 1, "source_urls": []string{}}}, "questions": []string{}}
		}
		text, _ := json.Marshal(result)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(text)}}}})
	}))
	defer provider.Close()
	p := autoProfile(false)
	p.Vision.Endpoint = provider.URL + "/v1"
	p.Narration.Endpoint = p.Vision.Endpoint
	p.Vision.APIFormat = "openai"
	p.Narration.APIFormat = "openai"
	p.Vision.TokenEnv = "VAC_SCRIPT_ROUNDTRIP_TOKEN"
	p.Narration.TokenEnv = p.Vision.TokenEnv
	t.Setenv(p.Vision.TokenEnv, "local-test-token")
	if _, err := svc.SaveProcessingProfile(ctx, "human:test", "profile", 0, p); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	python, err := exec.LookPath("python")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfigureTTS(python, filepath.Join(wd, "..", "..", "..", "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseTTS)
	req := service.SaveScriptRequest{VisibleSetSHA: sc.VisibleSetSHA, Body: "{@a.mp4}{\"抽卡说明\",字号:32px,}介绍抽卡的通用原理"}
	w, err := svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req)
	if err != nil {
		t.Fatal(err)
	}
	w, err = svc.GenerateScriptWorkbench(ctx, "human:test", sc.ScriptID, service.GenerateScriptRequest{ExpectedVersion: w.Version, VisibleSetSHA: sc.VisibleSetSHA, ProfileID: p.ProfileID, ProfileRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if w.Script.Status != model.ScriptDraft || len(w.Segments) != 1 || w.Segments[0].Narration == w.Document.Paragraphs[0].Instruction || requests.Load() != 2 {
		t.Fatalf("generated draft: %+v calls=%d", w, requests.Load())
	}
	w, err = svc.FinalizeScriptWorkbench(ctx, "human:test", sc.ScriptID, w.Version, sc.VisibleSetSHA)
	if err != nil {
		t.Fatal(err)
	}
	d, err := svc.ExportScriptWorkbench(ctx, "human:test", sc.ScriptID, w.Version, sc.VisibleSetSHA)
	if err != nil {
		t.Fatal(err)
	}
	if d.Version != w.Version {
		t.Fatalf("delivery version %d != %d", d.Version, w.Version)
	}
	path, err := svc.ScriptDeliveryPath(ctx, sc.ScriptID, "final.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() < 1000 {
		t.Fatalf("real output missing: %v", err)
	}
	reload, err := svc.GetScriptWorkbench(ctx, sc.ScriptID)
	if err != nil || reload.Delivery == nil || reload.Delivery.Version != w.Version {
		t.Fatalf("delivery missing after reload: %+v %v", reload.Delivery, err)
	}
	if native {
		copy := filepath.Join(t.TempDir(), "unrelated.mp4")
		raw, e := os.ReadFile(media[0].Path)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(copy, raw, 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = svc.ImportProjectResource(ctx, svc.CurrentProjectID(), copy); e != nil {
			t.Fatal(e)
		}
		if _, e = svc.ScriptDeliveryPath(ctx, sc.ScriptID, "final.mp4"); e != nil {
			t.Fatal("unrelated import invalidated delivery", e)
		}
		renamed := req
		renamed.ExpectedVersion = w.Version
		renamed.Title = "Only display title"
		renamed.Segments = w.Segments
		metadata, e := svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, renamed)
		if e != nil || metadata.Version != w.Version {
			t.Fatal("rename created a content revision", e)
		}
		if _, e = svc.ScriptDeliveryPath(ctx, sc.ScriptID, "final.mp4"); e != nil {
			t.Fatal("rename invalidated delivery", e)
		}
	}
	req.Body += "，补充新的说明"
	req.ExpectedVersion = w.Version
	req.Segments = w.Segments
	if _, err = svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ScriptDeliveryPath(ctx, sc.ScriptID, "final.mp4"); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("edited delivery still accessible: %v", err)
	}
}
