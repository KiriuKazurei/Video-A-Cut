package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestSequenceFrameHTTP(t *testing.T) {
	srv := newServer(t)
	ctx := t.Context()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "delivery"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := srv.svc.ConfigureDeliveryRoot(filepath.Join(root, "delivery")); err != nil {
		t.Fatal(err)
	}
	if err := srv.svc.ConfigureProjectWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.svc.CloseProjectWorkspace)
	wd, _ := os.Getwd()
	if err := srv.svc.ConfigureTTS("python", filepath.Join(wd, "..", "..", "..", "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.svc.CloseTTS)
	session, err := srv.svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "frame.avp"), Name: "Frame"})
	if err != nil {
		t.Fatal(err)
	}
	pid := session.Document.Project.ID
	// Reuse the maintained media fixture rather than generating another video.
	session, err = srv.svc.ImportProjectResource(ctx, pid, filepath.Join(wd, "..", "..", "..", "workers", "node", "timeline-cli", "fixtures", "media", "clip_01.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	seq := session.Document.Sequences[0]
	view, err := srv.svc.TimelineCommand(ctx, "human:window", pid, seq.ID, project.Command{Session: session.ID, ExpectedRevision: seq.Revision, IdempotencyKey: "frame", Operations: []project.Operation{{Type: "clip.insert", TrackID: seq.Tracks[0].ID, ResourceID: session.Document.Resources[0].ID}}})
	if err != nil {
		t.Fatal(err)
	}
	req := service.SequenceFrameRequest{Session: session.ID, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion, Frame: 1}
	call := func(value service.SequenceFrameRequest) *httptest.ResponseRecorder {
		body, _ := json.Marshal(value)
		r := localRequest("POST", "/api/projects/"+pid+"/sequences/"+seq.ID+"/preview/frame", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-VAC-Project-Session", session.ID)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	w := call(req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result service.SequenceFramePreview
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(result.Image, "data:image/png;base64,"))
	if err != nil || len(raw) < 8 || string(raw[:8]) != "\x89PNG\r\n\x1a\n" || result.Frame != 1 || result.ContentVersion != view.ContentVersion {
		t.Fatal("invalid PNG response", err)
	}
	req.Frame = -1
	if w = call(req); w.Code != 400 {
		t.Fatal("invalid frame", w.Code, w.Body.String())
	}
	req.Frame = 1
	req.ContentVersion = "stale"
	if w = call(req); w.Code != 409 {
		t.Fatal("stale frame", w.Code, w.Body.String())
	}
	closeFrame := func(sessionID string) *httptest.ResponseRecorder {
		r := localRequest("DELETE", "/api/projects/"+pid+"/sequences/"+seq.ID+"/preview/frame?session="+sessionID, nil)
		r.Header.Set("X-VAC-Project-Session", session.ID)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	if w = closeFrame("stale"); w.Code != 409 {
		t.Fatal("foreign preview release", w.Code, w.Body.String())
	}
	if w = closeFrame(session.ID); w.Code != 200 {
		t.Fatal("preview release", w.Code, w.Body.String())
	}
}

func TestProjectHTTPRangeAndIsolation(t *testing.T) {
	srv := newServer(t)
	root := t.TempDir()
	delivery := filepath.Join(root, "delivery")
	if err := os.Mkdir(delivery, 0700); err != nil {
		t.Fatal(err)
	}
	if err := srv.svc.ConfigureDeliveryRoot(delivery); err != nil {
		t.Fatal(err)
	}
	if err := srv.svc.ConfigureProjectWorkspace(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.svc.CloseProjectWorkspace)
	call := func(method, path, body, session string) *httptest.ResponseRecorder {
		req := localRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("X-VAC-Project-Session", session)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	for _, path := range []string{"/api/audit", "/api/agent-operations"} {
		if rec := call("GET", path, "", ""); rec.Code != 200 {
			t.Fatal("home logs require a project or credentials", path, rec.Code, rec.Body.String())
		}
	}
	newFile := func(name string) project.Session {
		data, _ := json.Marshal(service.ProjectCommand{Path: filepath.Join(root, name+".avp"), Name: name, Decision: "discard"})
		rec := call("POST", "/api/workspace/new", string(data), "")
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body.String())
		}
		var view project.Session
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		return view
	}
	first := newFile("A")
	source := filepath.Join(root, "字幕.srt")
	if err := os.WriteFile(source, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"path": source})
	rec := call("POST", "/api/projects/"+first.Document.Project.ID+"/resources", string(body), first.ID)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var imported project.Session
	_ = json.Unmarshal(rec.Body.Bytes(), &imported)
	path := "/api/projects/" + first.Document.Project.ID + "/resources/" + imported.Document.Resources[0].ID + "/content"
	req := localRequest("GET", path, nil)
	req.Header.Set("Range", "bytes=2-5")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 206 || rec.Body.String() != "2345" {
		t.Fatal("source Range", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("preview created human cookie")
	}
	rec = call("POST", "/api/projects/"+first.Document.Project.ID+"/resources", `{"kind":"script"}`, first.ID)
	if rec.Code != 200 {
		t.Fatalf("new script HTTP request failed: %d %s", rec.Code, rec.Body.String())
	}
	var scripted project.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &scripted); err != nil {
		t.Fatal(err)
	}
	created := scripted.Document.Resources[len(scripted.Document.Resources)-1]
	if created.Kind != "script" || created.ScriptID == "" || len(scripted.Document.Scripts) != 1 {
		t.Fatal("HTTP creation did not create a readable project script")
	}
	second := newFile("B")
	if second.ID == first.ID {
		t.Fatal("session did not rotate")
	}
	if rec = call("GET", path, "", ""); rec.Code != 404 {
		t.Fatal("old content leaked", rec.Code)
	}
	if rec = call("GET", "/api/assets", "", first.ID); rec.Code != 409 {
		t.Fatal("obsolete request reached new projection", rec.Code)
	}
	rec = call("GET", "/api/projects", "", second.ID)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), first.Document.Project.ID) || !strings.Contains(rec.Body.String(), second.Document.Project.ID) {
		t.Fatal("project discovery leaked", rec.Code, rec.Body.String())
	}
	if rec = call("POST", "/api/visible-set/confirm", `{}`, second.ID); rec.Code != 410 {
		t.Fatal("visible set confirmation flow remains", rec.Code)
	}
}
