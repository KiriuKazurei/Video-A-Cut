package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

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
	for _,path:=range []string{"/api/audit","/api/agent-operations"}{if rec:=call("GET",path,"","");rec.Code!=200{t.Fatal("home logs require a project or credentials",path,rec.Code,rec.Body.String())}}
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
