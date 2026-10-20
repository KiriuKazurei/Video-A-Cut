package api

import (
 "net/http/httptest"
 "strings"
 "testing"
)

func TestWindowActionsDoNotRequireHumanCredentials(t *testing.T) {
 srv := newSeededServer(t)
 for _, credential := range []string{"", "obsolete-human-token"} {
  t.Setenv("VAC_HUMAN_ACCESS_TOKEN", credential)
  for _, path := range []string{"/api/agent-operations", "/api/agent-operations/verify", "/api/audit"} {
   req := localRequest("GET",path,nil)
   rec := httptest.NewRecorder()
   srv.Handler().ServeHTTP(rec,req)
   if rec.Code != 200 { t.Fatalf("window action %s requires credentials: %d %s",path,rec.Code,rec.Body.String()) }
   if len(rec.Result().Cookies()) != 0 { t.Fatal("window action created an identity cookie") }
  }
 }
 note := localRequest("POST","/api/agent-operations/notes",strings.NewReader(`{"run_id":"run","note":"window annotation"}`))
 note.Header.Set("Content-Type","application/json")
 rec := httptest.NewRecorder(); srv.Handler().ServeHTTP(rec,note)
 if rec.Code != 200 { t.Fatal(rec.Code,rec.Body.String()) }
 req := localRequest("DELETE","/api/agent-operations",nil)
 rec = httptest.NewRecorder(); srv.Handler().ServeHTTP(rec,req)
 if rec.Code != 405 { t.Fatal("append-only journal deletion route exists",rec.Code) }
 req = localRequest("POST","/api/human-session",strings.NewReader(`{}`))
 req.Header.Set("Content-Type","application/json")
 rec = httptest.NewRecorder(); srv.Handler().ServeHTTP(rec,req)
 if rec.Code != 404 { t.Fatal("obsolete unlock route remains",rec.Code) }
}

func TestPreviewURLHasNoHumanSessionTicket(t *testing.T) {
 path := "/api/skill-runs/run/artifacts/video.mp4"
 url,err := issueHumanMediaTicket(path)
 if err != nil || url != path { t.Fatalf("preview still uses identity ticket: %q %v",url,err) }
}
