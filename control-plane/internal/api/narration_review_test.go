package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestNarrationReviewRESTRecordsTheDraftHash(t *testing.T) {
	srv := newSeededServer(t, model.Asset{AssetID: "clip", Status: model.AssetStatusNarrated})
	rec := postJSON(t, srv, "/api/assets/clip/narration-reviews",
		`{"text":"已审批解说","start":0.2,"end":1,"source":"Boss"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	want := service.NarrationDraftHash("已审批解说", 0.2, 1, "Boss")
	if !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	forged := postJSON(t, srv, "/api/assets/clip/narration-reviews",
		`{"text":"已审批解说","start":0.2,"end":1,"source":"Boss","draft_hash":"abcd"}`)
	if forged.Code != http.StatusBadRequest {
		t.Fatalf("forged hash status = %d, want 400 (%s)", forged.Code, forged.Body.String())
	}
}

func postJSON(t *testing.T, srv *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := localRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	return rec
}
