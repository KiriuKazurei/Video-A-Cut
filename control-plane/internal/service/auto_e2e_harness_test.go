package service_test

// Offline, deterministic end-to-end harness for the auto mode (design §9).
//
// Real: the control plane (service + MCP + REST handlers), the Python worker
// (recognize, sort, narrate, tts, subtitle, mix), the Node export worker
// (export stage), ffmpeg/ffprobe.
// Stubbed, all on loopback with a dummy token: the vision and narration model
// endpoints and the stepfun TTS endpoint (/v1/audio/speech returning a valid WAV).
// Simulated: the 720p preview renderer, because the real one needs the
// Microsoft YaHei font (not available off Windows); the simulation plays the
// exporter through the same claim/submit service calls and the production
// verification of the submitted files. The real render and the font are
// therefore NOT covered here.
// Time: the auto driver runs on a fake clock and is advanced by AutoTick only.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/api"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/mcpserver"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preparation"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

const e2eDummyToken = "dummy-e2e-token"

// ------------------------------------------------------------------ stubs ---

// e2eStub plays the vision, narration and TTS endpoints. The test sets what
// the next recognition returns; the stub records what it was asked.
type e2eStub struct {
	srv *httptest.Server
	mu  sync.Mutex

	conf float64 // vision confidence for every clip
	// confOnFeedback, when set, replaces conf for a request that carries
	// revise_feedback: the model "listens" to a rerun's feedback.
	confOnFeedback *float64
	label          string // vision label for every clip
	narration      string // narration text of every clip
	badAuth        int
	vision         []map[string]any // request bodies (images elided)
	narrations     []map[string]any
	speech         int
}

func newE2EStub(t *testing.T) *e2eStub {
	t.Helper()
	s := &e2eStub{conf: 0.95, label: "团战画面", narration: "这是一段精彩的团战画面"}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+e2eDummyToken {
			s.mu.Lock()
			s.badAuth++
			s.mu.Unlock()
			http.Error(w, "bad token", http.StatusUnauthorized)
			return false
		}
		return true
	}
	decode := func(r *http.Request) map[string]any {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		return body
	}
	reply := func(w http.ResponseWriter, v any) {
		raw, _ := json.Marshal(v)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
	mux.HandleFunc("/vision", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		body := decode(r)
		s.mu.Lock()
		conf, label := s.conf, s.label
		if _, revised := body["revise_feedback"]; revised && s.confOnFeedback != nil {
			conf = *s.confOnFeedback
		}
		s.vision = append(s.vision, elideImages(body))
		s.mu.Unlock()
		var scenes []map[string]any
		for _, c := range body["clips"].([]any) {
			clip := c.(map[string]any)
			var evidence []string
			for _, f := range clip["frames"].([]any) {
				evidence = append(evidence, f.(map[string]any)["sha256"].(string))
			}
			scenes = append(scenes, map[string]any{"clip_index": clip["clip_index"], "label": fmt.Sprintf("%s%v", label, clip["clip_index"]), "confidence": conf,
				"sequence_rank": clip["clip_index"], "evidence_frames": evidence})
		}
		reply(w, map[string]any{"scenes": scenes})
	})
	mux.HandleFunc("/narration", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		body := decode(r)
		s.mu.Lock()
		text := s.narration
		s.narrations = append(s.narrations, elideImages(body))
		s.mu.Unlock()
		var out []map[string]any
		for _, c := range body["clips"].([]any) {
			clip := c.(map[string]any)
			out = append(out, map[string]any{"clip_index": clip["clip_index"], "text": text,
				"start": clip["timeline_in"].(float64) + 0.2, "end": clip["timeline_out"].(float64) - 0.2,
				"needs_review": true, "source_scene_label": clip["scene_label"]})
		}
		reply(w, map[string]any{"model_version": "e2e-stub-v1", "narrations": out})
	})
	mux.HandleFunc("/v1/audio/speech", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_ = decode(r)
		s.mu.Lock()
		s.speech++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(sineWAV(24000, 1.5, 440))
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func elideImages(body map[string]any) map[string]any {
	raw, _ := json.Marshal(body)
	var copy map[string]any
	_ = json.Unmarshal(raw, &copy)
	if clips, ok := copy["clips"].([]any); ok {
		for _, c := range clips {
			if frames, ok := c.(map[string]any)["frames"].([]any); ok {
				for _, f := range frames {
					delete(f.(map[string]any), "image_base64")
				}
			}
		}
	}
	return copy
}

func (s *e2eStub) set(conf float64, label, narration string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conf, s.label, s.narration = conf, label, narration
}

func (s *e2eStub) improveOnFeedback(conf float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.confOnFeedback = &conf
}

func (s *e2eStub) seen() (vision, narration []map[string]any, speech, badAuth int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.vision...), append([]map[string]any(nil), s.narrations...), s.speech, s.badAuth
}

// sineWAV is a valid mono 16-bit PCM WAV.
func sineWAV(rate int, seconds float64, hz float64) []byte {
	n := int(float64(rate) * seconds)
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+n*2))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(n*2))
	for i := 0; i < n; i++ {
		_ = binary.Write(&b, binary.LittleEndian, int16(8000*math.Sin(2*math.Pi*hz*float64(i)/float64(rate))))
	}
	return b.Bytes()
}

// -------------------------------------------------------------- the world ---

type e2eEnv struct {
	t       *testing.T
	svc     *service.Service
	st      *store.Store
	root    string
	repo    string
	clock   *fakeClock
	stub    *e2eStub
	server  *httptest.Server
	tokens  map[string]string
	profile preparation.Profile
	sha     string
	events  *recordedEvents
	assetID string
	runID   string
	// failPreview makes the simulated exporter fail the preview task instead of
	// delivering it: the review is then LOW for preview_failed, which no rerun
	// can mend.
	failPreview bool
	workerLogs  []string
}

func needTools(t *testing.T, names ...string) {
	t.Helper()
	if testing.Short() {
		t.Skip("end-to-end run skipped in -short mode")
	}
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			t.Skipf("%s is not installed", n)
		}
	}
}

func newE2EEnv(t *testing.T) *e2eEnv {
	t.Helper()
	needTools(t, "python3", "ffmpeg", "ffprobe", "node")
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	svc := newService(t)
	st := getTestStore(svc)
	root := t.TempDir()
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	svc.SetBus(bus)
	rec := &recordedEvents{}
	for _, n := range []string{"auto.changed", "auto.reviewed", "workflow.changed"} {
		bus.Subscribe(n, rec.add)
	}
	clock := newFakeClock()
	svc.SetClock(clock.Now)
	e := &e2eEnv{t: t, svc: svc, st: st, root: root, repo: repo, clock: clock, stub: newE2EStub(t), events: rec,
		assetID: "e2e_clip", tokens: map[string]string{}}

	// Source media and the package the pipeline starts from.
	src := filepath.Join(root, "source")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"a.mp4", "b.mp4"} {
		pattern := []string{"testsrc2", "smptebars"}[i]
		e.run(nil, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", pattern+"=size=320x180:rate=30:duration=4",
			"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=4", "-c:v", "libx264", "-preset", "ultrafast",
			"-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", filepath.Join(src, name))
	}
	edl := map[string]any{"timeline": map[string]int{"fps": 30, "sample_rate": 48000},
		"video":      []map[string]any{{"src": "a.mp4", "in": 0, "out": 4, "timeline_in": 0}, {"src": "b.mp4", "in": 0, "out": 4, "timeline_in": 4}},
		"game_audio": []map[string]any{{"src": "a.mp4", "in": 0, "out": 4, "timeline_in": 0, "gain_db": -12}, {"src": "b.mp4", "in": 0, "out": 4, "timeline_in": 4, "gain_db": -12}},
		"voice":      []any{}, "subtitle": []any{}, "music": []any{}}
	writeJSON(t, filepath.Join(src, "edl.json"), edl)
	writeJSON(t, filepath.Join(src, "delivery-manifest.json"), map[string]any{"schema_version": 1, "artifacts": []map[string]string{
		{"kind": "edl", "path": "edl.json"}, {"kind": "video", "path": "a.mp4"}, {"kind": "video", "path": "b.mp4"}}})
	if _, err := svc.ImportDelivery(ctx, e.assetID, "source"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateAssetGovernance(ctx, "human:test", e.assetID, model.Asset{AgentVisible: true, AllowedAgents: []string{"recognizer", "narrator", "exporter"}}); err != nil {
		t.Fatal(err)
	}

	// Profile: configured vision/narration on the loopback stub, stepfun TTS
	// on the same stub, one dummy token. All endpoints are loopback, so the
	// profile is not external and needs no egress consent.
	tpl, err := svc.SaveTTSTemplate(ctx, "human:test", tts.SaveRequest{TemplateID: "e2e_tts", Name: "e2e", ExpectedRevision: 0, IdempotencyKey: "tts-1",
		Settings: tts.Settings{Engine: "stepfun", Endpoint: e.stub.srv.URL, Model: "stepaudio-2.5-tts", TokenEnv: "VAC_E2E_DUMMY_TTS",
			Voice: "vibrant-youth", Language: "zh", Speed: 1, Volume: 1}})
	if err != nil {
		t.Fatal(err)
	}
	p := preparation.Profile{SchemaVersion: 1, ProfileID: "e2e", Revision: 1, Name: "e2e stub", ContentMode: "configured", ExportTarget: "premiere",
		Vision:      preparation.Provider{Adapter: "vision", Endpoint: e.stub.srv.URL + "/vision", Model: "stub-v1", TokenEnv: "VAC_E2E_MODEL_TOKEN"},
		Narration:   preparation.Provider{Adapter: "narration", Endpoint: e.stub.srv.URL + "/narration", Model: "stub-v1", TokenEnv: "VAC_E2E_MODEL_TOKEN"},
		Sampling:    preparation.Sampling{MaxFrames: 12, MaxBytes: 12582912, TimeoutSeconds: 60},
		TTSTemplate: &tts.Binding{TemplateID: tpl.TemplateID, Revision: tpl.Revision}}
	saved, err := svc.SaveProcessingProfile(ctx, "human:test", "profile-1", 0, p)
	if err != nil {
		t.Fatal(err)
	}
	if saved.External() {
		t.Fatal("the e2e profile must be loopback-only")
	}
	e.profile = saved
	if e.sha, err = saved.Fingerprint(); err != nil {
		t.Fatal(err)
	}
	e.reportCapabilities()

	// Agents and the HTTP surface the workers talk to.
	var agents []mcpserver.Agent
	for _, role := range []string{"recognizer", "narrator", "exporter"} {
		token := strings.Repeat(role[:1], 8) + "-" + e2eDummyToken + "-" + role
		e.tokens[role] = token
		sum := sha256.Sum256([]byte(token))
		agents = append(agents, mcpserver.Agent{ID: role, Role: role, TokenSHA256: hex.EncodeToString(sum[:])})
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.New(agents, svc, time.Minute))
	mux.Handle("/api/", api.New(svc).Handler())
	e.server = httptest.NewServer(mux)
	t.Cleanup(e.server.Close)
	return e
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *e2eEnv) run(env []string, name string, args ...string) {
	e.t.Helper()
	cmd := exec.CommandContext(e.t.Context(), name, args...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		e.t.Fatalf("%s: %v\n%s", name, err, out)
	}
}

func (e *e2eEnv) reportCapabilities() {
	e.t.Helper()
	yes := true
	for _, role := range []string{"recognizer", "narrator", "exporter"} {
		c := preparation.Capability{ProfileSHA256: e.sha, ToolsReady: true, CredentialsReady: true}
		if role == "narrator" {
			c.TTSReady, c.TTSTemplateSHA256 = &yes, e.profile.TTSTemplate.SHA256
		}
		if err := e.svc.ReportWorkerCapability(e.t.Context(), role, role, c); err != nil {
			e.t.Fatal(err)
		}
	}
}

// -------------------------------------------------------------- the run ---

func (e *e2eEnv) startAuto(brief string, scope service.AutoScopeRequest) service.AutoView {
	e.t.Helper()
	ctx := e.t.Context()
	report, err := e.svc.PreparedPreflight(ctx, e.assetID, e.profile.ProfileID, 1)
	if err != nil {
		e.t.Fatal(err)
	}
	if !report.CanStart {
		e.t.Fatalf("preflight: %+v", report)
	}
	notice, err := e.svc.AutoNoticeFor(ctx, e.profile.ProfileID, 1)
	if err != nil {
		e.t.Fatal(err)
	}
	visible, err := e.svc.VisibleSet(ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	run, view, err := e.svc.StartAutoRun(ctx, "human:webui", service.AutoRunRequest{AssetID: e.assetID, ProfileID: e.profile.ProfileID, ProfileRevision: 1,
		ExpectedAssetVersion: report.ExpectedAssetVersion, Brief: brief, Scope: scope, NoticeSHA256: notice.NoticeSHA256, VisibleSetSHA: visible.SHA256, IdempotencyKey: "e2e-" + e.t.Name()})
	if err != nil {
		e.t.Fatal(err)
	}
	e.runID = run.RunID
	return view
}

// auto is the current overlay.
func (e *e2eEnv) auto() model.WorkflowAuto {
	e.t.Helper()
	a, err := e.st.GetWorkflowAuto(e.t.Context(), e.runID)
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

func (e *e2eEnv) view() service.AutoView {
	e.t.Helper()
	v, err := e.svc.GetAuto(e.t.Context(), e.runID)
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

type queuedTask struct{ id, kind, role string }

func (e *e2eEnv) queued() []queuedTask {
	e.t.Helper()
	rows, err := e.st.DB().QueryContext(e.t.Context(), `SELECT task_id, type, agent_role FROM tasks WHERE asset_id = ? AND status = 'queued' ORDER BY updated_at, task_id`, e.assetID)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []queuedTask
	for rows.Next() {
		var q queuedTask
		if err := rows.Scan(&q.id, &q.kind, &q.role); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, q)
	}
	return out
}

// pump alternates the driver's tick with the one worker that has a task, until
// done reports true or nothing can move. It returns whether done was reached.
func (e *e2eEnv) pump(done func() bool) bool {
	e.t.Helper()
	for i := 0; i < 80; i++ {
		registerValidCapability(e.t, e.svc, "agent-exp-1") // the simulated renderer stays "fresh"
		e.reportCapabilities()
		if err := e.svc.AutoTick(e.t.Context()); err != nil {
			e.t.Fatalf("AutoTick: %v", err)
		}
		if done() {
			return true
		}
		q := e.queued()
		if len(q) == 0 {
			return false
		}
		e.runTask(q[0])
	}
	e.t.Fatalf("the run did not settle in 80 steps: %+v", e.auto())
	return false
}

func (e *e2eEnv) runTask(q queuedTask) {
	e.t.Helper()
	switch q.kind {
	case model.TaskTypeRecognize, model.TaskTypeSort:
		e.python("recognizer")
	case model.TaskTypeNarrate, model.TaskTypeTTS, model.TaskTypeSubtitle, model.TaskTypeMix:
		e.python("narrator")
	case model.TaskTypeExport:
		e.node()
	case model.TaskTypePreview:
		e.simulatePreview(q)
	default:
		e.t.Fatalf("no worker for task %+v", q)
	}
}

func (e *e2eEnv) python(role string) {
	e.t.Helper()
	profile := map[string]any{}
	raw, _ := json.Marshal(e.profile)
	_ = json.Unmarshal(raw, &profile)
	cfg := filepath.Join(e.t.TempDir(), "worker.json")
	writeJSON(e.t, cfg, map[string]any{"mcp_url": e.server.URL + "/mcp", "token_env": "VAC_E2E_WORKER_TOKEN", "delivery_root": e.root,
		"role": role, "profile_sha256": e.sha, "processing_profile": profile, "poll_interval_ms": 50, "heartbeat_interval_ms": 500,
		"task_timeout_ms": 120000, "content_provider": "builtin"})
	cmd := exec.CommandContext(e.t.Context(), "python3", "-m", "vac_worker", "--config", cfg, "--once")
	cmd.Dir = filepath.Join(e.repo, "workers", "python")
	cmd.Env = append(os.Environ(), "VAC_E2E_WORKER_TOKEN="+e.tokens[role], "VAC_E2E_MODEL_TOKEN="+e2eDummyToken, "VAC_E2E_DUMMY_TTS="+e2eDummyToken, "PYTHONDONTWRITEBYTECODE=1")
	out, err := cmd.CombinedOutput()
	e.workerLogs = append(e.workerLogs, string(out))
	if err != nil {
		e.t.Fatalf("python %s worker: %v\n%s", role, err, out)
	}
}

func (e *e2eEnv) node() {
	e.t.Helper()
	cfg := filepath.Join(e.t.TempDir(), "export.json")
	writeJSON(e.t, cfg, map[string]any{"mcp_url": e.server.URL + "/mcp", "delivery_root": e.root, "token_env": "VAC_E2E_EXPORT_TOKEN", "profile_sha256": e.sha,
		"cli_path":         filepath.Join(e.repo, "workers/node/timeline-cli/src/cli.mjs"),
		"adapters_path":    filepath.Join(e.repo, "workers/node/timeline-cli/adapters/index.mjs"),
		"poll_interval_ms": 50, "heartbeat_interval_ms": 500, "task_timeout_ms": 120000})
	cmd := exec.CommandContext(e.t.Context(), "node", filepath.Join(e.repo, "workers/node/export-worker/src/main.mjs"), "--config", cfg, "--once")
	cmd.Env = append(os.Environ(), "VAC_E2E_EXPORT_TOKEN="+e.tokens["exporter"])
	out, err := cmd.CombinedOutput()
	e.workerLogs = append(e.workerLogs, string(out))
	if err != nil {
		e.t.Fatalf("export worker: %v\n%s", err, out)
	}
}

// simulatePreview plays the exporter for a queued 720p preview task: it claims
// the task and submits an output whose media matches the export's timeline.
func (e *e2eEnv) simulatePreview(q queuedTask) {
	e.t.Helper()
	ctx := e.t.Context()
	run, _, err := e.svc.GetWorkflow(ctx, e.runID)
	if err != nil {
		e.t.Fatal(err)
	}
	rev, err := e.st.GetRevision(ctx, run.CurrentRevisionID)
	if err != nil {
		e.t.Fatal(err)
	}
	var edl struct {
		Video []struct{ In, Out, TimelineIn float64 } `json:"video"`
	}
	raw, err := os.ReadFile(filepath.Join(e.root, filepath.FromSlash(rev.PackageRef), "edl.json"))
	if err != nil {
		e.t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(raw), "timeline_in", "TimelineIn")), &edl); err != nil {
		e.t.Fatal(err)
	}
	var total float64
	for _, c := range edl.Video {
		total = math.Max(total, c.TimelineIn+c.Out-c.In)
	}
	frames := int64(math.Round(total * 30))
	media := preview.Media{Width: 1280, Height: 720, FPS: 30, FrameCount: frames, DurationSeconds: float64(frames) / 30,
		VideoCodec: "h264", AudioCodec: "aac", SampleRate: 48000, Channels: 2,
		AudioInterval: &preview.AudioInterval{StartPTS: 0, DurationTS: frames * 1600, TimeBase: "1/48000", StartSeconds: 0, DurationSeconds: float64(frames) / 30}}
	e.svc.SetMediaProber(&testProber{media: media})
	if _, err := e.svc.ClaimForExecution(ctx, "agent-exp-1", "exporter", time.Now().Add(time.Minute), previewClaimOptions("agent-exp-1")); err != nil {
		e.t.Fatal(err)
	}
	jobs, err := e.st.ListPreviewJobs(ctx, e.runID)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, job := range jobs {
		if job.TaskID != q.id {
			continue
		}
		if e.failPreview {
			if err := e.svc.FailTaskScoped(ctx, previewTestScope(e.t, e.svc, "agent-exp-1", job.TaskID), "render failed"); err != nil {
				e.t.Fatalf("FailTask: %v", err)
			}
			return
		}
		dir := filepath.Join("previews", job.AssetID, job.RevisionID, job.PreviewID)
		writePreviewOutput(e.t, e.root, dir, job, media)
		if err := e.svc.SubmitPreviewScoped(ctx, previewTestScope(e.t, e.svc, "agent-exp-1", job.TaskID), filepath.ToSlash(dir)); err != nil {
			e.t.Fatalf("SubmitPreview: %v", err)
		}
		return
	}
	e.t.Fatalf("no preview job for task %s", q.id)
}

// writePreviewOutput writes the files an exporter delivers for a preview whose
// media is the given one, with the five passed checks of a real render.
func writePreviewOutput(t *testing.T, root, dir string, job model.PreviewJob, media preview.Media) {
	t.Helper()
	abs := filepath.Join(root, dir)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatal(err)
	}
	mp4 := []byte("e2e-simulated-720p-preview")
	hash := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	receipt, _ := json.Marshal(preview.Receipt{SchemaVersion: 1, PreviewID: job.PreviewID, TaskID: job.TaskID, InputSHA256: job.InputSHA256,
		PresetSHA256: job.PresetSHA256, RendererVersion: "e2e", FFmpegVersion: "e2e",
		// A real render of a package with subtitles names the font it burned in.
		Font: &preview.Font{Family: "Microsoft YaHei", SHA256: hash([]byte("e2e-simulated-font"))}})
	report, _ := json.Marshal(preview.Report{SchemaVersion: 1, InputSHA256: job.InputSHA256, TargetFrames: media.FrameCount, TargetSamples: media.FrameCount * 48000 / 30, Media: media,
		Checks: []preview.Check{{Code: "source_fingerprint", Status: "passed", Message: "verified"}, {Code: "timeline_frames", Status: "passed", Message: fmt.Sprintf("%d frames", media.FrameCount)},
			{Code: "audio_alignment", Status: "passed", Message: "aligned"}, {Code: "subtitles", Status: "passed", Message: "passed"}, {Code: "media_streams", Status: "passed", Message: "valid"}}})
	manifest, _ := json.Marshal(preview.Manifest{SchemaVersion: 1, PreviewID: job.PreviewID, TaskID: job.TaskID, InputSHA256: job.InputSHA256, PresetSHA256: job.PresetSHA256,
		Artifacts: []preview.Artifact{
			{Key: "mp4", Kind: "preview", Path: "final.mp4", MIME: "video/mp4", Size: int64(len(mp4)), SHA256: hash(mp4)},
			{Key: "report", Kind: "render_report", Path: "render-report.json", MIME: "application/json", Size: int64(len(report)), SHA256: hash(report)},
			{Key: "receipt", Kind: "receipt", Path: "preview-receipt.json", MIME: "application/json", Size: int64(len(receipt)), SHA256: hash(receipt)}}})
	for name, data := range map[string][]byte{"final.mp4": mp4, "render-report.json": report, "preview-receipt.json": receipt, "preview-manifest.json": manifest} {
		if err := os.WriteFile(filepath.Join(abs, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// settled reports whether the auto run has left the active state.
func (e *e2eEnv) settled() bool { return e.auto().State != model.AutoStateActive }

func (e *e2eEnv) get(path string) (int, []byte) {
	e.t.Helper()
	resp, err := http.Get(e.server.URL + path)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (e *e2eEnv) post(path string, body any) (int, []byte) {
	e.t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(e.server.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

var _ = context.Background
