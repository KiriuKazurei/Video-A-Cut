package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSkillHumanJournalRequiresSeparateSession(t *testing.T) {
	t.Setenv("VAC_HUMAN_ACCESS_TOKEN", "vac-human-test-credential-00000000000001")
	srv := newSeededServer(t)
	for _, path := range []string{"/api/agent-operations", "/api/agent-operations/verify", "/api/skill-runs/run/steps", "/api/skill-runs/run/checkpoints"} {
		r := localRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer external-agent-credential")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		if rec.Code != 403 {
			t.Fatalf("Agent accessed %s: %d", path, rec.Code)
		}
	}
	unlock := postJSON(t, srv, "/api/human-session", `{"token":"vac-human-test-credential-00000000000001"}`)
	if unlock.Code != 200 || len(unlock.Result().Cookies()) != 1 || !unlock.Result().Cookies()[0].HttpOnly {
		t.Fatalf("session %d %s", unlock.Code, unlock.Body.String())
	}
	req := localRequest("GET", "/api/agent-operations", nil)
	req.AddCookie(unlock.Result().Cookies()[0])
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	note := localRequest("POST", "/api/agent-operations/notes", strings.NewReader(`{"run_id":"run","note":"human annotation"}`))
	note.Header.Set("Content-Type", "application/json")
	note.AddCookie(unlock.Result().Cookies()[0])
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, note)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	req = localRequest("DELETE", "/api/agent-operations", nil)
	req.AddCookie(unlock.Result().Cookies()[0])
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 405 {
		t.Fatal("journal deletion route exists", rec.Code)
	}
	t.Setenv("VAC_HUMAN_ACCESS_TOKEN", "")
	req = localRequest("GET", "/api/agent-operations", nil)
	req.AddCookie(unlock.Result().Cookies()[0])
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("old session survived human credential revocation", rec.Code)
	}
}

func TestSkillMediaTicketIsReadOnlyPathBoundAndRevocable(t *testing.T) {
	t.Setenv("VAC_HUMAN_ACCESS_TOKEN", "vac-human-test-credential-00000000000001")
	path := "/api/skill-runs/run/artifacts/video.mp4"
	ticket, e := issueHumanMediaTicket(path)
	if e != nil {
		t.Fatal(e)
	}
	handler := protectHumanSkill(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-3" {
			t.Error("Range lost")
		}
		w.WriteHeader(206)
	}))
	for _, candidate := range []struct {
		method, path string
		status       int
	}{{"GET", ticket, 206}, {"POST", ticket, 403}, {"GET", strings.Replace(ticket, path, "/api/agent-operations", 1), 403}, {"GET", strings.Replace(ticket, path, "/api/skill-runs/run2/artifacts/video.mp4", 1), 403}} {
		req := localRequest(candidate.method, candidate.path, nil)
		req.Header.Set("Range", "bytes=0-3")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != candidate.status {
			t.Fatalf("scoped media %s %s %d", candidate.method, candidate.path, rec.Code)
		}
	}
	t.Setenv("VAC_HUMAN_ACCESS_TOKEN", "changed-human-credential-00000000000001")
	req := localRequest("GET", ticket, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("ticket survived human credential rotation")
	}
}
