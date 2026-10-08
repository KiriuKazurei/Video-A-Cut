package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/api"
	dv "github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/deliveryvariant"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type styleTestProber struct{}

func (styleTestProber) ProbeMedia(context.Context, string) (preview.Media, error) {
	return preview.Media{Width: 1280, Height: 720}, nil
}
func TestSubtitleDraftCASAndSourceBinding(t *testing.T) {
	svc, root, _, run, rev := setupPreviewExportFixture(t)
	svc.SetMediaProber(styleTestProber{})
	addVariantSRT(t, root, rev, "1\n00:00:00,100 --> 00:00:00,900\n中文字幕\n\n")
	d, e := svc.GetSubtitleStyle(t.Context(), run.RunID)
	if e != nil {
		t.Fatal(e)
	}
	if d.DraftVersion != 0 || len(d.Cues) != 1 {
		t.Fatal(d)
	}
	d.Document.Style.Bold = true
	req := dv.SaveStyleRequest{RevisionID: rev.RevisionID, Document: d.Document}
	saved, e := svc.SaveSubtitleStyle(t.Context(), "human:test", run.RunID, req)
	if e != nil {
		t.Fatal(e)
	}
	if saved.DraftVersion != 1 || saved.StyleSHA256 == d.StyleSHA256 {
		t.Fatal(saved)
	}
	if _, e = svc.SaveSubtitleStyle(t.Context(), "human:test", run.RunID, req); !errors.Is(e, model.ErrConflict) {
		t.Fatal("stale save accepted", e)
	}
	loaded, e := svc.GetSubtitleStyle(t.Context(), run.RunID)
	if e != nil || !loaded.Document.Style.Bold || loaded.StyleSHA256 != saved.StyleSHA256 {
		t.Fatal(loaded, e)
	}
	req.ExpectedDraftVersion = 1
	req.Document.Style.FadeInMS = 801
	if _, e = svc.SaveSubtitleStyle(t.Context(), "human:test", run.RunID, req); !errors.Is(e, model.ErrArgument) {
		t.Fatal("invalid fade accepted", e)
	}
	req.Document.Style.FadeInMS = 0
	req.Document.SourceSRTSHA256 = "bad"
	if _, e = svc.SaveSubtitleStyle(t.Context(), "human:test", run.RunID, req); !errors.Is(e, model.ErrConflict) {
		t.Fatal("wrong source accepted", e)
	}
}

func TestSubtitleEditableVariantAndPreviewBinding(t *testing.T) {
	svc, root, _, run, rev := setupPreviewExportFixture(t)
	svc.SetMediaProber(styleTestProber{})
	ctx := t.Context()
	addVariantSRT(t, root, rev, "1\n00:00:00,100 --> 00:00:00,900\n中文字幕\n\n")
	d, e := svc.GetSubtitleStyle(ctx, run.RunID)
	if e != nil {
		t.Fatal(e)
	}
	d.Document.Style.PrimaryColor = "#00FF00"
	d.Document.Style.FadeInMS = 100
	d.Document.Style.FadeOutMS = 100
	saved, e := svc.SaveSubtitleStyle(ctx, "human:test", run.RunID, dv.SaveStyleRequest{RevisionID: rev.RevisionID, Document: d.Document})
	if e != nil {
		t.Fatal(e)
	}
	req := dv.StartRequest{SpecID: dv.EditableSpecID, RevisionID: rev.RevisionID, ExpectedVersion: run.Version, RequestID: "editable", DraftVersion: 1, StyleSHA256: saved.StyleSHA256}
	registerVariantCapability(t, svc, "old-exporter")
	if _, _, e = svc.StartDeliveryVariant(ctx, "human:test", run.RunID, req); !errors.Is(e, model.ErrInvalidState) {
		t.Fatal("old worker allowed v2", e)
	}
	cap := dv.Capability{ProtocolVersion: 1, Ready: true, Specs: []dv.SpecRef{{SpecID: dv.SpecID, SHA256: dv.SpecSHA}, {SpecID: dv.EditableSpecID, SHA256: dv.EditableSpecSHA}}, Fonts: []dv.FontFamily{{Family: "Microsoft YaHei", SHA256: hashBytes([]byte("font"))}}}
	if e = svc.ReportDeliveryVariantCapability(ctx, "new-exporter", "exporter", cap); e != nil {
		t.Fatal(e)
	}
	view, created, e := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, req)
	if e != nil || !created || view.StyleSHA256 != saved.StyleSHA256 {
		t.Fatal(view, created, e)
	}
	req.RequestID = "reuse"
	reused, created, e := svc.StartDeliveryVariant(ctx, "human:test", run.RunID, req)
	if e != nil || created || reused.VariantID != view.VariantID {
		t.Fatal(reused, created, e)
	}
	old, e := svc.ClaimForExecution(ctx, "old-exporter", "exporter", time.Now().Add(time.Minute), previewClaimOptions("old-exporter"))
	if e == nil && old.Task.TaskID != "" && old.Task.TaskID == view.TaskID {
		t.Fatal("old worker claimed v2")
	}
	if _, e = svc.ClaimForExecution(ctx, "new-exporter", "exporter", time.Now().Add(time.Minute), previewClaimOptions("new-exporter")); e != nil {
		t.Fatal(e)
	}
	output := renderVariantForTest(t, svc, root, view)
	if e = svc.SubmitDeliveryVariant(ctx, "new-exporter", view.TaskID, output); e != nil {
		t.Fatal("v2 Node/Go chain", e)
	}
	registerValidCapability(t, svc, "old-exporter")
	pReq := preview.StartRequest{RevisionID: rev.RevisionID, ExpectedVersion: run.Version, RequestID: "styled-preview", PresetID: preview.PresetID, SubtitleVariantID: view.VariantID}
	if _, _, e = svc.StartPreview(ctx, "human:test", run.RunID, pReq); !errors.Is(e, model.ErrInvalidState) {
		t.Fatal("old preview worker allowed ASS", e)
	}
	pcap := preview.Capability{ProtocolVersion: 1, ExecutionProtocol: service.PreviewExecutionProtocol, Ready: true, Presets: []preview.PresetRef{{PresetID: preview.PresetID, SHA256: preview.DefaultPresetSHA256()}}, SubtitleFormats: []string{"srt", "ass"}}
	if e = svc.ReportPreviewCapability(ctx, "new-exporter", "exporter", pcap); e != nil {
		t.Fatal(e)
	}
	p, created, e := svc.StartPreview(ctx, "human:test", run.RunID, pReq)
	if e != nil || !created || p.SubtitleVariantID != view.VariantID || p.StyleSHA256 != saved.StyleSHA256 {
		t.Fatal(p, created, e)
	}
	job, e := getTestStore(svc).GetPreviewJob(ctx, p.PreviewID)
	if e != nil {
		t.Fatal(e)
	}
	var frozen preview.Input
	_ = json.Unmarshal([]byte(job.InputJSON), &frozen)
	hash, e := preview.Fingerprint(frozen)
	if e != nil || frozen.SchemaVersion != 2 || hash != job.InputSHA256 || frozen.SubtitleVariant.ASS.SHA256 == "" {
		t.Fatal(frozen, e)
	}
	pReq.SubtitleVariantID = "different"
	if _, _, e = svc.StartPreview(ctx, "human:test", run.RunID, pReq); !errors.Is(e, model.ErrConflict) {
		t.Fatal("same request changed binding", e)
	}
	old, e = svc.ClaimForExecution(ctx, "old-exporter", "exporter", time.Now().Add(time.Minute), previewClaimOptions("old-exporter"))
	if e == nil && old.Task.TaskID != "" && old.Task.TaskID == p.TaskID {
		t.Fatal("old worker claimed ASS preview")
	}
	claimed, e := svc.ClaimForExecution(ctx, "new-exporter", "exporter", time.Now().Add(time.Minute), previewClaimOptions("new-exporter"))
	if e != nil || claimed.Task.TaskID == "" || claimed.Task.TaskID != p.TaskID {
		t.Fatal(claimed, e)
	}
}

func TestSubtitleStyleRESTAndFontCatalog(t *testing.T) {
	svc, root, _, run, rev := setupPreviewExportFixture(t)
	svc.SetMediaProber(styleTestProber{})
	addVariantSRT(t, root, rev, "1\n00:00:00,100 --> 00:00:00,900\n字幕\n\n")
	handler := api.New(svc).Handler()
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Host = "127.0.0.1"
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	path := "/api/workflows/" + run.RunID + "/subtitle-style"
	out := request(http.MethodGet, path, nil)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	var d dv.SubtitleStyleDraft
	if e := json.Unmarshal(out.Body.Bytes(), &d); e != nil {
		t.Fatal(e)
	}
	put := dv.SaveStyleRequest{RevisionID: rev.RevisionID, Document: d.Document}
	out = request(http.MethodPut, path, put)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out = request(http.MethodPut, path, put); out.Code != 409 {
		t.Fatal(out.Code, out.Body.String())
	}
	_ = svc.ReportDeliveryVariantCapability(t.Context(), "fonts-exporter", "exporter", dv.Capability{ProtocolVersion: 1, Ready: true, Fonts: []dv.FontFamily{{Family: "Microsoft YaHei", SHA256: hashBytes([]byte("font"))}}})
	out = request(http.MethodGet, "/api/subtitle-fonts", nil)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte("Microsoft YaHei")) {
		t.Fatal(out.Code, out.Body.String())
	}
}

func TestSubtitleDraftConcurrentSaveAndSourceChange(t *testing.T) {
	svc, root, _, run, rev := setupPreviewExportFixture(t)
	svc.SetMediaProber(styleTestProber{})
	addVariantSRT(t, root, rev, "1\n00:00:00,100 --> 00:00:00,900\n字幕\n\n")
	d, e := svc.GetSubtitleStyle(t.Context(), run.RunID)
	if e != nil {
		t.Fatal(e)
	}
	req := dv.SaveStyleRequest{RevisionID: rev.RevisionID, Document: d.Document}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, e := svc.SaveSubtitleStyle(t.Context(), "human:test", run.RunID, req)
			results <- e
		}()
	}
	close(start)
	passed, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			passed++
		} else if errors.Is(e, model.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if passed != 1 || conflicts != 1 {
		t.Fatal(passed, conflicts)
	}
	path := filepath.Join(root, filepath.FromSlash(rev.PackageRef), "subtitles.srt")
	if e = os.WriteFile(path, []byte("1\n00:00:00,100 --> 00:00:00,900\n修改源字幕\n\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.GetSubtitleStyle(t.Context(), run.RunID); !errors.Is(e, model.ErrConflict) {
		t.Fatal("changed SRT reused old draft", e)
	}
}
