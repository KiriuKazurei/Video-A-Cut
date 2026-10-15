package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// GET /api/workflows/{run}/auto/reviews returns what the scorer stored, oldest
// first, with reasons as codes and numbers only.
func TestAutoReviewsEndpointReturnsStoredReviews(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := New(service.New(st))
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	if err := st.CreateAsset(ctx, model.Asset{AssetID: "a", Status: model.AssetStatusExported, AgentVisible: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertRevision(ctx, model.Revision{RevisionID: "rev", AssetID: "a", SchemaVersion: 1, PackageRef: "p", EDLSHA256: "e",
		EvidenceManifestSHA256: "m", CreatedAt: now, CreatedBy: "human:test", Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertWorkflow(ctx, model.WorkflowRun{RunID: "run", AssetID: "a", BaseRevisionID: "rev", CurrentRevisionID: "rev",
		Status: model.WorkflowReadyAcceptance, Stage: "acceptance", Version: 1, ContentMode: "configured", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertWorkflowAuto(ctx, model.WorkflowAuto{RunID: "run", AutoVersion: 1, State: model.AutoStateActive, AuthorizedBy: "human:webui",
		AuthorizedAt: now, Scope: model.AutoScope{Thresholds: map[string]float64{}}, CreatedAt: now, UpdatedAt: now, DeadlineAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if rec := get(t, srv, "/api/workflows/run/auto/reviews"); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty = %d %q", rec.Code, rec.Body.String())
	}
	for i, band := range []string{"low", "high"} {
		if err := st.InsertAutoReview(ctx, model.AutoReview{ReviewID: "arv_" + band, RunID: "run", Round: i, RevisionID: "rev", PreviewID: "prv_" + band,
			ScorerVersion: "v1", Score: 0.5 + float64(i)*0.45, Band: band,
			Reasons:    json.RawMessage(`[{"code":"low_confidence","severity":"warn","value":0.5,"threshold":0.6,"improvable":true}]`),
			Inputs:     json.RawMessage(`{"facts":{"scenes":1}}`),
			Thresholds: json.RawMessage(`{"high_score_min":0.85}`), CreatedAt: now.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	rec := get(t, srv, "/api/workflows/run/auto/reviews")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	var rows []struct {
		ReviewID      string           `json:"review_id"`
		RunID         string           `json:"run_id"`
		Round         int              `json:"round"`
		PreviewID     string           `json:"preview_id"`
		ScorerVersion string           `json:"scorer_version"`
		Score         float64          `json:"score"`
		Band          string           `json:"band"`
		Reasons       []map[string]any `json:"reasons"`
		Inputs        map[string]any   `json:"inputs"`
		Thresholds    map[string]any   `json:"thresholds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Band != "low" || rows[1].Band != "high" || rows[1].PreviewID != "prv_high" || rows[1].Score != 0.95 ||
		rows[0].ScorerVersion != "v1" || len(rows[0].Reasons) != 1 || rows[0].Reasons[0]["code"] != "low_confidence" ||
		rows[0].Inputs["facts"] == nil || rows[0].Thresholds["high_score_min"] != 0.85 {
		t.Fatalf("rows = %s", rec.Body.String())
	}
	var view struct {
		LatestReview *struct {
			ReviewID string `json:"review_id"`
		} `json:"latest_review"`
	}
	if err := json.Unmarshal(get(t, srv, "/api/workflows/run/auto").Body.Bytes(), &view); err != nil || view.LatestReview == nil || view.LatestReview.ReviewID != "arv_high" {
		t.Fatalf("latest_review = %+v (%v)", view.LatestReview, err)
	}
	if rec := get(t, srv, "/api/workflows/nope/auto/reviews"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown run = %d", rec.Code)
	}
}
