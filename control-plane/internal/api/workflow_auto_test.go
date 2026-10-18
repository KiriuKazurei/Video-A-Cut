package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
)

// autoAPIFixture returns a server with one ingested asset, a loopback
// configured profile and worker capabilities, ready for an authorization.
func autoAPIFixture(t *testing.T) (*Server, preparation.Profile, string) {
	t.Helper()
	srv := newServer(t)
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(dir, "samples"), 0o700); err != nil {
		t.Fatal(err)
	}
	frame := []byte("frame fixture")
	sum := sha256.Sum256(frame)
	digest := hex.EncodeToString(sum[:])
	files := map[string]any{
		"edl.json": map[string]any{
			"timeline":  map[string]any{"fps": 30, "sample_rate": 48000},
			"video":     []any{map[string]any{"src": "a.mp4", "in": 0, "out": 1, "timeline_in": 0}},
			"scenes":    []any{map[string]any{"scene_id": "scn_1", "index": 0, "label": "关卡", "method": "vision", "evidence_frames": []string{digest}}},
			"narration": []any{}, "game_audio": []any{}, "voice": []any{}, "subtitle": []any{}, "music": []any{},
		},
		"samples/evidence-manifest.json": map[string]any{"frames": []any{map[string]any{"path": "samples/frame.jpg", "sha256": digest, "clip_index": 0, "timestamp": 0.5}}},
		"delivery-manifest.json": map[string]any{"schema_version": 1, "artifacts": []any{
			map[string]string{"kind": "edl", "path": "edl.json"}, map[string]string{"kind": "sample", "path": "samples/frame.jpg"},
			map[string]string{"kind": "evidence_manifest", "path": "samples/evidence-manifest.json"}}},
	}
	if err := os.WriteFile(filepath.Join(dir, "samples", "frame.jpg"), frame, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, value := range files {
		body, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := srv.svc.CreateAsset(t.Context(), model.Asset{AssetID: "clip", Status: model.AssetStatusIngested, AgentVisible: true,
		AllowedAgents: []string{"recognizer", "narrator", "exporter"}, Artifacts: map[string]string{"edl": "src/edl.json"}}); err != nil {
		t.Fatal(err)
	}
	p := preparation.Profile{
		SchemaVersion: 1, ProfileID: "auto_api", Revision: 1, Name: "自动模式", ContentMode: "configured",
		Vision:    preparation.Provider{Adapter: "vision", Endpoint: "http://127.0.0.1:18080/vision", Model: "fixture", TokenEnv: "VISION_TEST"},
		Narration: preparation.Provider{Adapter: "narration", Endpoint: "http://127.0.0.1:18080/narration", Model: "fixture", TokenEnv: "NARRATION_TEST"},
		Sampling:  preparation.Sampling{MaxFrames: 12, MaxBytes: 12 * 1024 * 1024, TimeoutSeconds: 60},
		TTSVoice:  "Microsoft Huihui Desktop", ExportTarget: "premiere",
	}
	if _, err := srv.svc.SaveProcessingProfile(t.Context(), "human:test", "profile", 0, p); err != nil {
		t.Fatal(err)
	}
	sha, _ := p.Fingerprint()
	for _, role := range []string{"recognizer", "narrator", "exporter"} {
		if err := srv.svc.ReportWorkerCapability(t.Context(), role, role, preparation.Capability{ProfileSHA256: sha, ToolsReady: true, CredentialsReady: true, TTSVoices: []string{p.TTSVoice}}); err != nil {
			t.Fatal(err)
		}
	}
	report, err := srv.svc.PreparedPreflight(t.Context(), "clip", p.ProfileID, 1)
	if err != nil {
		t.Fatal(err)
	}
	return srv, p, report.ExpectedAssetVersion
}

func noticeHash(t *testing.T, srv *Server, p preparation.Profile) string {
	t.Helper()
	rec := get(t, srv, "/api/processing-profiles/"+p.ProfileID+"/revisions/1/auto-notice")
	if rec.Code != http.StatusOK {
		t.Fatalf("auto-notice = %d %s", rec.Code, rec.Body.String())
	}
	var notice struct {
		ProfileSHA256 string `json:"profile_sha256"`
		External      bool   `json:"external"`
		Notice        string `json:"notice"`
		NoticeSHA256  string `json:"notice_sha256"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &notice); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(notice.Notice))
	if notice.External || len(notice.ProfileSHA256) != 64 || notice.NoticeSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("notice = %+v", notice)
	}
	return notice.NoticeSHA256
}

func visibleSHA(t *testing.T, srv *Server) string {
	t.Helper()
	rec := get(t, srv, "/api/visible-set")
	if rec.Code != http.StatusOK {
		t.Fatalf("visible-set = %d %s", rec.Code, rec.Body.String())
	}
	var view struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil || len(view.SHA256) != 64 {
		t.Fatalf("visible-set sha: %s (%v)", rec.Body.String(), err)
	}
	return view.SHA256
}

func autoBody(version, notice, key, visible string) string {
	body := map[string]any{"profile_id": "auto_api", "revision": 1, "expected_asset_version": version, "idempotency_key": key, "brief": "只保留团战",
		"authorization": map[string]any{"notice_sha256": notice, "visible_set_sha": visible}}
	raw, _ := json.Marshal(body)
	return string(raw)
}

// 授权缺/错 notice_sha256 → 400。
func TestAutoRunRequiresNoticeHash(t *testing.T) {
	srv, p, version := autoAPIFixture(t)
	good := noticeHash(t, srv, p)
	sha := visibleSHA(t, srv)
	for name, notice := range map[string]string{"missing": "", "wrong": strings.Repeat("0", 64), "truncated": good[:63]} {
		rec := postJSON(t, srv, "/api/assets/clip/auto-runs", autoBody(version, notice, "k-"+name, sha))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s notice: status %d, want 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}
	noKey := map[string]any{"profile_id": "auto_api", "revision": 1, "expected_asset_version": version, "authorization": map[string]any{"notice_sha256": good}}
	raw, _ := json.Marshal(noKey)
	if rec := postJSON(t, srv, "/api/assets/clip/auto-runs", string(raw)); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key: %d", rec.Code)
	}
	if rec := postJSON(t, srv, "/api/assets/clip/auto-runs", `{"profile_id":"auto_api","revision":1,"extra":true}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}
	runs, _ := srv.svc.ListWorkflows(t.Context(), "clip")
	if len(runs) != 0 {
		t.Fatalf("rejected authorizations created %d runs", len(runs))
	}
}

func TestAutoRunEndpointsShapesAndErrors(t *testing.T) {
	srv, p, version := autoAPIFixture(t)
	notice := noticeHash(t, srv, p)
	sha := visibleSHA(t, srv)
	rec := postJSON(t, srv, "/api/assets/clip/auto-runs", autoBody(version, notice, "k1", sha))
	if rec.Code != http.StatusCreated {
		t.Fatalf("start = %d %s", rec.Code, rec.Body.String())
	}
	var started struct {
		Run  model.WorkflowRun `json:"run"`
		Auto map[string]any    `json:"auto"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.Run.RunID == "" || started.Auto["state"] != "active" || started.Auto["authorized_by"] != "human:webui" {
		t.Fatalf("start body = %s", rec.Body.String())
	}
	run := started.Run.RunID
	rec = get(t, srv, "/api/workflows/"+run+"/auto")
	if rec.Code != http.StatusOK {
		t.Fatalf("get auto = %d %s", rec.Code, rec.Body.String())
	}
	var view map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"state", "round", "max_rounds", "total_rounds", "max_total_rounds", "scope", "brief_sha256", "delivery_hold", "rounds", "latest_review", "decisions", "auto_version"} {
		if _, ok := view[key]; !ok {
			t.Fatalf("GET auto lacks %q: %s", key, rec.Body.String())
		}
	}
	if string(view["rounds"]) != "[]" || string(view["latest_review"]) != "null" {
		t.Fatalf("rounds=%s latest_review=%s", view["rounds"], view["latest_review"])
	}
	if rec := get(t, srv, "/api/workflows/"+run+"/auto/reviews"); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("reviews = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get(t, srv, "/api/workflows/run_missing/auto"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown run = %d", rec.Code)
	}
	if rec := postJSON(t, srv, "/api/assets/clip/auto-runs", autoBody(version, notice, "k2", sha)); rec.Code != http.StatusConflict {
		t.Fatalf("second authorization = %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
	// decision while active is refused; revoke works and is human-only.
	rec = postJSON(t, srv, "/api/workflows/"+run+"/auto/decision", `{"decision":"export","expected_auto_version":1,"idempotency_key":"d1"}`)
	if rec.Code != http.StatusConflict && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("decision while active = %d %s", rec.Code, rec.Body.String())
	}
	if rec := postJSON(t, srv, "/api/workflows/"+run+"/auto/revoke", `{"reason":"","expected_auto_version":1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("revoke without reason = %d", rec.Code)
	}
	rec = postJSON(t, srv, "/api/workflows/"+run+"/auto/revoke", `{"reason":"算了","expected_auto_version":1}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke = %d %s", rec.Code, rec.Body.String())
	}
	var revoked struct {
		Auto struct {
			State     string `json:"state"`
			RevokedBy string `json:"revoked_by"`
		} `json:"auto"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &revoked); err != nil || revoked.Auto.State != "revoked" || revoked.Auto.RevokedBy != "human:webui" {
		t.Fatalf("revoke body = %s (%v)", rec.Body.String(), err)
	}
	if rec := postJSON(t, srv, "/api/workflows/"+run+"/auto/revoke", `{"reason":"again","expected_auto_version":2}`); rec.Code != http.StatusConflict && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("second revoke = %d", rec.Code)
	}
	if rec := get(t, srv, "/api/processing-profiles/auto_api/revisions/0/auto-notice"); rec.Code != http.StatusBadRequest {
		t.Fatalf("notice for revision 0 = %d", rec.Code)
	}
}

// The auto overlay must not leak into the response of an ordinary run.
func TestNonAutoRunResponseHasNoAutoKey(t *testing.T) {
	srv, _, _ := autoAPIFixture(t)
	view, err := srv.svc.VisibleSet(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.svc.ConfirmVisibleSet(t.Context(), "human:test", view.SHA256, false); err != nil {
		t.Fatal(err)
	}
	run, err := srv.svc.StartWorkflow(t.Context(), "human:webui", "clip", "", "plain", "configured")
	if err != nil {
		t.Fatal(err)
	}
	rec := get(t, srv, "/api/workflows/"+run.RunID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get workflow = %d %s", rec.Code, rec.Body.String())
	}
	var generic map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &generic); err != nil {
		t.Fatal(err)
	}
	if _, ok := generic["auto"]; ok {
		t.Fatalf("response has an auto key: %s", rec.Body.String())
	}
	if runObj, ok := generic["run"].(map[string]any); ok {
		if _, has := runObj["auto"]; has {
			t.Fatalf("run has an auto key: %s", rec.Body.String())
		}
	}
	if strings.Contains(rec.Body.String(), `"auto`) {
		t.Fatalf("an auto-related field appeared: %s", rec.Body.String())
	}
	// Ids and times vary per run; everything else is compared byte for byte.
	got := normalizeWorkflowJSON(t, rec.Body.Bytes(), run)
	if os.Getenv("VAC_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(filepath.Join("testdata", "workflow_nonauto.golden.json"), append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(filepath.Join("testdata", "workflow_nonauto.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Fatalf("non-auto run response changed.\n got: %s\nwant: %s", got, want)
	}
}

// normalizeWorkflowJSON replaces the per-run identifiers and timestamps with
// fixed placeholders and prints the document with stable key order.
func normalizeWorkflowJSON(t *testing.T, body []byte, run model.WorkflowRun) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				switch {
				case k == "created_at" || k == "updated_at" || k == "lease_until":
					if val != nil {
						x[k] = "TIME"
					}
				case k == "run_id":
					x[k] = "RUN"
				case k == "task_id":
					x[k] = "TASK"
				case strings.HasSuffix(k, "revision_id"):
					if s, _ := val.(string); s != "" {
						x[k] = "REVISION"
					}
				default:
					x[k] = walk(val)
				}
			}
			return x
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
			return x
		case string:
			if x == run.RunID {
				return "RUN"
			}
			return x
		}
		return v
	}
	out, err := json.MarshalIndent(walk(doc), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAutoIdempotencyKeyLengthIsCapped(t *testing.T) {
	srv, p, version := autoAPIFixture(t)
	notice := noticeHash(t, srv, p)
	sha := visibleSHA(t, srv)
	if rec := postJSON(t, srv, "/api/assets/clip/auto-runs", autoBody(version, notice, strings.Repeat("k", 129), sha)); rec.Code != http.StatusBadRequest {
		t.Fatalf("129-byte key on start = %d %s, want 400", rec.Code, rec.Body.String())
	}
	if rec := postJSON(t, srv, "/api/workflows/run_x/auto/decision", `{"decision":"export","expected_auto_version":1,"idempotency_key":"`+strings.Repeat("d", 129)+`"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("129-byte key on decision = %d %s, want 400", rec.Code, rec.Body.String())
	}
	// 128 bytes is accepted, and the refused attempt above left nothing behind.
	rec := postJSON(t, srv, "/api/assets/clip/auto-runs", autoBody(version, notice, strings.Repeat("k", 128), sha))
	if rec.Code != http.StatusCreated {
		t.Fatalf("128-byte key on start = %d %s, want 201", rec.Code, rec.Body.String())
	}
}

// The cap is in bytes, not characters: 43 three-byte characters are 129 bytes.
func TestAutoIdempotencyKeyLengthCountsBytesNotRunes(t *testing.T) {
	srv, p, version := autoAPIFixture(t)
	notice := noticeHash(t, srv, p)
	sha := visibleSHA(t, srv)
	for _, n := range []int{43, 64} {
		key := strings.Repeat("键", n)
		if rec := postJSON(t, srv, "/api/assets/clip/auto-runs", autoBody(version, notice, key, sha)); rec.Code != http.StatusBadRequest {
			t.Fatalf("%d runes (%d bytes) on start = %d, want 400", n, len(key), rec.Code)
		}
		if rec := postJSON(t, srv, "/api/workflows/run_x/auto/decision", `{"decision":"export","expected_auto_version":1,"idempotency_key":"`+key+`"}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("%d runes on decision = %d, want 400", n, rec.Code)
		}
	}
	key := strings.Repeat("键", 42) // 126 bytes
	if rec := postJSON(t, srv, "/api/assets/clip/auto-runs", autoBody(version, notice, key, sha)); rec.Code != http.StatusCreated {
		t.Fatalf("126-byte multibyte key = %d %s, want 201", rec.Code, rec.Body.String())
	}
}
