package api

import (
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSavedProfileRESTVersionsAndConflict(t *testing.T) {
	srv := newServer(t)
	profile := preparation.Profile{SchemaVersion: 1, ProfileID: "rest_fixed", Revision: 1, Name: "fixed", ContentMode: "builtin", Vision: preparation.Provider{Adapter: "builtin"}, Narration: preparation.Provider{Adapter: "builtin"}, Sampling: preparation.Sampling{MaxFrames: 12, MaxBytes: 1024, TimeoutSeconds: 30}, TTSVoice: "Microsoft Huihui Desktop", ExportTarget: "premiere"}
	save := func(p preparation.Profile, expected int, key string) int {
		body, _ := json.Marshal(profileSaveRequest{Profile: p, ExpectedRevision: expected, IdempotencyKey: key})
		return postJSON(t, srv, "/api/processing-profiles", string(body)).Code
	}
	if code := save(profile, 0, "one"); code != http.StatusCreated {
		t.Fatalf("create %d", code)
	}
	if code := save(profile, 0, "one"); code != http.StatusCreated {
		t.Fatalf("retry %d", code)
	}
	profile.Revision = 2
	profile.Name = "next"
	if code := save(profile, 1, "two"); code != http.StatusCreated {
		t.Fatalf("update %d", code)
	}
	if code := save(profile, 1, "three"); code != http.StatusConflict {
		t.Fatalf("stale update %d", code)
	}
	for _, v := range []struct {
		path     string
		revision int
	}{{"1", 1}, {"0", 2}} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, localRequest("GET", "/api/processing-profiles/rest_fixed/revisions/"+v.path, nil))
		var result struct {
			Profile preparation.Profile `json:"profile"`
			SHA     string              `json:"profile_sha256"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || result.Profile.Revision != v.revision || len(result.SHA) != 64 {
			t.Fatalf("get %s: %d %s", v.path, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, localRequest("GET", "/api/processing-profiles?limit=51", nil))
	if rec.Code != 400 {
		t.Fatalf("unbounded list %d", rec.Code)
	}
	if code := postJSON(t, srv, "/api/assets/unknown/prepared-preflight", `{"profile_id":"rest_fixed","revision":0}`).Code; code != 400 {
		t.Fatalf("floating preflight %d", code)
	}
	if code := postJSON(t, srv, "/api/processing-profiles/rest_fixed/revisions/2/external-consent", "").Code; code != 400 {
		t.Fatalf("builtin consent %d", code)
	}
}
