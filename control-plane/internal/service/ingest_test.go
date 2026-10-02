package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/ingest"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

const ingester = "ingester"

type ingestEnv struct {
	t        *testing.T
	svc      *service.Service
	delivery string
	recRoot  string
	roots    []ingest.Root
}

func newIngestEnv(t *testing.T) *ingestEnv {
	t.Helper()
	svc := newService(t)
	base := t.TempDir()
	env := &ingestEnv{t: t, svc: svc, delivery: filepath.Join(base, "delivery"), recRoot: filepath.Join(base, "rec 录屏")}
	for _, d := range []string{env.delivery, env.recRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.ConfigureDeliveryRoot(env.delivery); err != nil {
		t.Fatal(err)
	}
	env.roots = []ingest.Root{{ID: "rec", Name: "录屏", Path: env.recRoot}}
	if err := svc.ConfigureIngest(env.roots, nil); err != nil {
		t.Fatal(err)
	}
	return env
}

func (e *ingestEnv) recording(name string, size int) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.recRoot, name), []byte(strings.Repeat("v", size)), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *ingestEnv) write(rel string, v any) string {
	e.t.Helper()
	var raw []byte
	switch b := v.(type) {
	case []byte:
		raw = b
	default:
		var err error
		if raw, err = json.Marshal(v); err != nil {
			e.t.Fatal(err)
		}
	}
	full := filepath.Join(e.delivery, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(full, raw, 0o644); err != nil {
		e.t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (e *ingestEnv) allowIngester(assetID string) {
	e.t.Helper()
	visible, locked, approved := true, false, false
	agents := []string{ingester}
	if _, err := e.svc.PatchAssetGovernance(context.Background(), "human:test", assetID, service.GovernancePatch{
		AgentVisible: &visible, Locked: &locked, HumanApproved: &approved, AllowedAgents: &agents}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *ingestEnv) capability(agent string, roots string) {
	e.t.Helper()
	if err := e.svc.Heartbeat(context.Background(), agent, ingester, "", time.Now().Add(time.Minute)); err != nil {
		e.t.Fatal(err)
	}
	if err := e.svc.ReportIngestCapability(context.Background(), agent, ingester, service.IngestCapability{
		RootsSHA256: roots, FFmpegReady: true, FFprobeReady: true, Operations: []string{"media_probe", "segment", "media_prepare"}, WorkerVersion: "test/1", ExecutionProtocol: service.ExecutionProtocolVersion}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *ingestEnv) claim(agent string) (model.Task, *service.IngestInput) {
	e.t.Helper()
	tk, in, _ := e.claimOpts(agent, agent+"-inst-1", fmt.Sprintf("%s-claim-%d", agent, time.Now().UnixNano()), true)
	return tk, in
}

func (e *ingestEnv) claimOpts(agent, instance, request string, begin bool) (model.Task, *service.IngestInput, service.ExecutionScope) {
	e.t.Helper()
	ctx := context.Background()
	out, err := e.svc.ClaimForExecution(ctx, agent, ingester, time.Now().Add(time.Minute), service.ClaimOptions{
		RuntimeInstanceID: instance, RequestID: request,
	})
	if err != nil || out.Obsolete || !out.Claimed {
		e.t.Fatalf("claim: obsolete=%v claimed=%v err=%v", out.Obsolete, out.Claimed, err)
	}
	if begin {
		if err := e.svc.BeginExecution(ctx, out.Scope); err != nil {
			e.t.Fatalf("begin %s: %v", out.Scope.ExecutionID, err)
		}
	}
	in, err := e.svc.GetTaskInputScoped(ctx, out.Scope)
	if err != nil || in.InputKind != "media_ingest" || in.Ingest == nil {
		e.t.Fatalf("task input = %+v, %v", in, err)
	}
	return out.Task, in.Ingest, out.Scope
}

func receipt(tk model.Task, in *service.IngestInput) ingest.Receipt {
	return ingest.Receipt{SchemaVersion: 1, TaskID: tk.TaskID, ExecutionID: in.ExecutionID, Stage: in.Stage,
		InputSHA256: in.InputSHA256, PolicySHA256: in.PolicySHA256, WorkerVersion: "test/1"}
}

func (e *ingestEnv) submit(agent string, tk model.Task, in *service.IngestInput) error {
	return e.svc.SubmitIngestResult(context.Background(), agent, ingester, service.IngestResultRequest{
		TaskID: tk.TaskID, ExecutionID: in.ExecutionID, RuntimeInstanceID: in.RuntimeInstanceID, Generation: in.Generation,
		PackageDir: in.PackageDir, InputSHA256: in.InputSHA256})
}

func (e *ingestEnv) probePackage(tk model.Task, in *service.IngestInput, size int64) {
	mtime, err := strconv.ParseInt(in.Source.MTimeNs, 10, 64)
	if err != nil {
		e.t.Fatal(err)
	}
	snapRel := in.Source.SourceDir + "/snapshot.mkv"
	snapSHA := e.write(snapRel, []byte(strings.Repeat("v", int(size))))
	e.write(in.PackageDir+"/snapshot.json", ingest.SnapshotDoc{SchemaVersion: 1, SourceID: in.Source.SourceID, SourceVersion: in.Source.SourceVersion,
		SizeBytes: in.Source.SizeBytes, MTimeNs: mtime, SHA256: snapSHA, Snapshot: snapRel, ChunkBytes: 1 << 20, Chunks: 1})
	start := int64(1_500_000)
	e.write(in.PackageDir+"/probe.json", ingest.Probe{SchemaVersion: 1, ProbeVersion: "test", Container: "matroska,webm", SizeBytes: size,
		DurationUs: 20_000_000, Limitations: []string{}, Streams: []ingest.ProbeStream{
			{Index: 0, Type: "video", Width: 320, Height: 240, StartUs: &start, TimeBase: "1/1000", Disposition: map[string]int{}},
			{Index: 1, Type: "audio", SampleRate: 48000, Channels: 2, StartUs: &start, Disposition: map[string]int{}}}})
	e.write(in.PackageDir+"/worker-receipt.json", receipt(tk, in))
}

func TestIngestRequiresConfigurationAndConfinedPaths(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if _, err := svc.RegisterRecording(ctx, "human:test", "a1", "rec", "x.mkv", "k1"); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("unconfigured register err = %v", err)
	}
	env := newIngestEnv(t)
	env.recording("a.mkv", 10)
	for rel, want := range map[string]error{"../a.mkv": model.ErrArgument, "C:/a.mkv": model.ErrArgument, "missing.mkv": model.ErrNotFound, ".": model.ErrArgument} {
		if _, err := env.svc.RegisterRecording(ctx, "human:test", "a1", "rec", rel, "k-"+fmt.Sprint(len(rel))); !errors.Is(err, want) {
			t.Errorf("register %q err = %v, want %v", rel, err, want)
		}
	}
	if _, err := env.svc.RegisterRecording(ctx, "human:test", "a1", "other", "a.mkv", "k9"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("unknown root err = %v", err)
	}
	roots, err := env.svc.ListIngestRoots(ctx)
	raw, _ := json.Marshal(roots)
	if err != nil || !roots.Configured || strings.Contains(string(raw), env.recRoot) || strings.Contains(string(raw), filepath.ToSlash(env.recRoot)) {
		t.Fatalf("roots view leaks the absolute path or is not configured: %s %v", raw, err)
	}
}

func TestIngestEndToEndWithStaleExecutionAndGovernance(t *testing.T) {
	env := newIngestEnv(t)
	ctx := context.Background()
	svc := env.svc
	env.recording("a 录屏.mkv", 4096)
	reg, err := svc.RegisterRecording(ctx, "human:test", "rec1", "rec", "a 录屏.mkv", "reg-1")
	if err != nil {
		t.Fatal(err)
	}
	if reg.Asset.InputKind != model.InputKindRawRecording || reg.Asset.AgentVisible {
		t.Fatalf("registered asset = %+v", reg.Asset)
	}
	again, err := svc.RegisterRecording(ctx, "human:test", "rec1", "rec", "a 录屏.mkv", "reg-1")
	if err != nil || again.Source.SourceID != reg.Source.SourceID {
		t.Fatalf("idempotent replay = %+v %v", again, err)
	}
	env.recording("b.mkv", 10)
	if _, err := svc.RegisterRecording(ctx, "human:test", "rec1", "rec", "b.mkv", "reg-1"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("same key different request err = %v", err)
	}
	if _, err := svc.StartIngestRun(ctx, "human:test", "rec1", reg.Source.SourceID, reg.SourceVersion, "start-1"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("start before governance err = %v", err)
	}
	env.allowIngester("rec1")
	if _, err := svc.StartWorkflow(ctx, "human:test", "rec1", "", "wf-1", "builtin"); err == nil {
		t.Fatal("raw recording started a content workflow")
	}
	if _, err := svc.StartIngestRun(ctx, "human:test", "rec1", reg.Source.SourceID, strings.Repeat("0", 64), "start-0"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale expected_source_version err = %v", err)
	}
	start, err := svc.StartIngestRun(ctx, "human:test", "rec1", reg.Source.SourceID, reg.SourceVersion, "start-1")
	if err != nil || start.Task.Type != ingest.TaskMediaProbe || start.Run.State != ingest.StateQueued {
		t.Fatalf("start = %+v %v", start, err)
	}
	if _, err := svc.StartIngestRun(ctx, "human:test", "rec1", reg.Source.SourceID, reg.SourceVersion, "start-2"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("second active run err = %v", err)
	}

	// Capability gates claims: none, wrong root mapping, then correct.
	if _, err := svc.ClaimTask(ctx, "ing-a", ingester, time.Now().Add(time.Minute)); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("claim without capability err = %v", err)
	}
	env.capability("ing-a", strings.Repeat("1", 64))
	if _, err := svc.ClaimTask(ctx, "ing-a", ingester, time.Now().Add(time.Minute)); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("claim with mismatched roots err = %v", err)
	}
	env.capability("ing-a", ingest.RootsFingerprint(env.roots))
	tk, in1 := env.claim("ing-a")
	if in1.ExecutionID == "" || in1.OutputDir != "ingest/"+start.Run.RunID+"/"+tk.TaskID+"/"+in1.ExecutionID || in1.Source.RootID != "rec" {
		t.Fatalf("ingest input = %+v", in1)
	}
	env.probePackage(tk, in1, 4096)
	cpSHA := env.write(in1.OutputDir+"/checkpoints/00001.json", map[string]any{"offset": 1})
	cp := service.CheckpointRequest{TaskID: tk.TaskID, ExecutionID: in1.ExecutionID, RuntimeInstanceID: in1.RuntimeInstanceID, Generation: in1.Generation, Sequence: 1, InputSHA256: in1.InputSHA256,
		Kind: "copy_chunk", ItemIndex: 1, Ref: in1.OutputDir + "/checkpoints/00001.json", SHA256: cpSHA}
	if _, err := svc.SaveIngestCheckpoint(ctx, "ing-a", ingester, cp); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveIngestCheckpoint(ctx, "ing-a", ingester, cp); err != nil {
		t.Fatalf("checkpoint replay err = %v", err)
	}
	bad := cp
	bad.Sequence, bad.SHA256 = 2, strings.Repeat("0", 64)
	if _, err := svc.SaveIngestCheckpoint(ctx, "ing-a", ingester, bad); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("tampered checkpoint err = %v", err)
	}

	// Lease loss and a re-claim by the same agent issue a new execution id;
	// the earlier execution's package is refused.
	setTaskFixture(t, svc, tk.TaskID, func(task *model.Task) {
		task.Status, task.AgentID, task.LeaseUntil = model.TaskStatusQueued, "", nil
	})
	tk2, in2, scope2 := env.claimOpts("ing-a", "ing-a-inst-1", "ing-a-reclaim", false)
	if tk2.TaskID != tk.TaskID || in2.ExecutionID == in1.ExecutionID {
		t.Fatalf("re-claim kept execution %s", in2.ExecutionID)
	}
	if err := svc.BeginExecution(ctx, scope2); err == nil {
		t.Fatal("new execution began before the previous owner drained")
	}
	oldScope := service.ExecutionScope{AgentID: "ing-a", Role: ingester, RuntimeInstanceID: in1.RuntimeInstanceID, TaskID: tk.TaskID, ExecutionID: in1.ExecutionID, Generation: in1.Generation, InputSHA256: in1.InputSHA256}
	if err := svc.AckExecutionStopped(ctx, oldScope, "stopped"); err != nil {
		t.Fatalf("ack previous execution: %v", err)
	}
	if err := svc.BeginExecution(ctx, scope2); err != nil {
		t.Fatalf("begin after drain: %v", err)
	}
	if len(in2.Checkpoints) != 1 {
		t.Fatalf("verified checkpoint not offered for reuse: %+v", in2.Checkpoints)
	}
	if err := env.submit("ing-a", tk, in1); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale execution submit err = %v", err)
	}
	if _, err := svc.SaveIngestCheckpoint(ctx, "ing-a", ingester, cp); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale execution checkpoint err = %v", err)
	}
	env.probePackage(tk2, in2, 4096)
	if err := env.submit("ing-a", tk2, in2); err != nil {
		t.Fatal(err)
	}
	if err := env.submit("ing-a", tk2, in2); err != nil {
		t.Fatalf("result replay err = %v", err)
	}
	view, err := svc.GetIngestRunView(ctx, start.Run.RunID)
	if err != nil || view.Run.State != ingest.StateAwaitingReview || view.Run.Stage != ingest.StageProbe || view.Probe == nil || !view.Source.HasSnapshot {
		t.Fatalf("after probe view = %+v %v", view.Run, err)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), env.recRoot) || strings.Contains(string(raw), "snapshot.mkv") {
		t.Fatalf("run view leaks local paths: %s", raw)
	}

	// Analysis plan: optimistic version, then segment.
	audio := 1
	areq := service.AnalysisRequest{ExpectedVersion: view.Run.Version - 1, VideoStreamIndex: 0, GameAudioStreamIndex: &audio,
		Segmentation: ingest.Segmentation{Method: "scene_change", Threshold: 0.3, MinSegmentUs: 1_000_000, MaxSegmentUs: 600_000_000}, IdempotencyKey: "plan-1"}
	if _, err := svc.SubmitAnalysisPlan(ctx, "human:test", start.Run.RunID, areq); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale version err = %v", err)
	}
	areq.ExpectedVersion, areq.IdempotencyKey = view.Run.Version, "plan-2"
	seg, err := svc.SubmitAnalysisPlan(ctx, "human:test", start.Run.RunID, areq)
	if err != nil || seg.Task.Type != ingest.TaskSegment {
		t.Fatalf("analysis = %+v %v", seg, err)
	}
	tk3, in3 := env.claim("ing-a")
	segDoc := ingest.SegmentsDoc{SchemaVersion: 1, Method: "scene_change", MethodVersion: "test", PlanSHA256: in3.AnalysisSHA256, RangeUs: [2]int64{0, 20_000_000},
		Config:      ingest.DetectConfig{Threshold: 0.3, MinSegmentUs: 1_000_000, MaxSegmentUs: 600_000_000, ChunkUs: 60_000_000, OverlapUs: 1_000_000, MaxEdge: 640, MinCutGapUs: 250_000},
		ChunksTotal: 1, ChunksDone: 1, Complete: true, Cuts: []ingest.Cut{{TUs: 4_000_000, Score: 0.9}, {TUs: 9_000_000, Score: 0.8}},
		Candidates: []ingest.Candidate{{SegmentID: "seg_0001", StartUs: 0, EndUs: 4_000_000, Reason: "range_start", Thumbnail: "thumbnails/seg_0001.jpg"},
			{SegmentID: "seg_0002", StartUs: 4_000_000, EndUs: 9_000_000, Score: 0.9, Reason: "scene_change"},
			{SegmentID: "seg_0003", StartUs: 9_000_000, EndUs: 20_000_000, Score: 0.8, Reason: "scene_change"}}}
	partial := segDoc
	partial.Complete, partial.ChunksDone = false, 0
	env.write(in3.PackageDir+"/segments.json", partial)
	env.write(in3.PackageDir+"/thumbnails/seg_0001.jpg", []byte("jpg"))
	env.write(in3.PackageDir+"/worker-receipt.json", receipt(tk3, in3))
	if err := env.submit("ing-a", tk3, in3); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("partial scan registered: %v", err)
	}
	env.write(in3.PackageDir+"/segments.json", segDoc)
	if err := env.submit("ing-a", tk3, in3); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListIngestSegments(ctx, start.Run.RunID, 2, 0)
	if err != nil || page.Total != 3 || len(page.Items) != 2 || page.Items[0].ThumbnailKey != "thumb_seg_0001" {
		t.Fatalf("segment page = %+v %v", page, err)
	}
	view, _ = svc.GetIngestRunView(ctx, start.Run.RunID)

	// Selection then prepare; a selection above the frame budget is blocked.
	sreq := service.SelectionRequest{ExpectedVersion: view.Run.Version, BasePlanRevision: view.Run.AnalysisRevision, Output: ingest.Output{FPS: 30, SampleRate: 48000},
		SelectedSegments: []ingest.SelectedSegment{{SegmentID: "seg_0002", StartUs: 4_000_000, EndUs: 9_000_000}, {SegmentID: "seg_0003", StartUs: 10_000_000, EndUs: 12_000_000}},
		IdempotencyKey:   "sel-1"}
	run, err := svc.SubmitSelection(ctx, "human:test", start.Run.RunID, sreq)
	if err != nil {
		t.Fatal(err)
	}
	small := builtinProfile(1)
	small.Sampling.MaxFrames = 1
	if _, err := svc.SaveProcessingProfile(ctx, "human:test", "p-small", 0, small); err != nil {
		t.Fatal(err)
	}
	preq := service.PrepareRequest{ExpectedVersion: run.Version, PlanRevision: run.SelectionRevision, ProfileID: small.ProfileID, ProfileRevision: 1, IdempotencyKey: "prep-0"}
	if _, err := svc.PrepareIngest(ctx, "human:test", start.Run.RunID, preq); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("frame budget not enforced: %v", err)
	}
	if _, err := svc.SaveProcessingProfile(ctx, "human:test", "p-big", 1, builtinProfile(2)); err != nil {
		t.Fatal(err)
	}
	preq.ProfileRevision, preq.IdempotencyKey = 2, "prep-1"
	prep, err := svc.PrepareIngest(ctx, "human:test", start.Run.RunID, preq)
	if err != nil || prep.Task.Type != ingest.TaskMediaPrepare {
		t.Fatalf("prepare = %+v %v", prep, err)
	}
	tk4, in4 := env.claim("ing-a")
	if len(in4.FrameQuotas) != 2 || in4.FrameQuotas[0] < in4.FrameQuotas[1] || in4.Provenance == nil {
		t.Fatalf("prepare input = %+v", in4)
	}
	env.preparePackage(tk4, in4)
	if err := env.submit("ing-a", tk4, in4); err != nil {
		t.Fatal(err)
	}
	view, _ = svc.GetIngestRunView(ctx, start.Run.RunID)
	asset, _ := svc.GetAsset(ctx, "rec1")
	if view.Run.State != ingest.StateReady || asset.InputKind != model.InputKindEDLPackage || asset.IngestRunID != start.Run.RunID || asset.Artifacts["edl"] == "" {
		t.Fatalf("after prepare run=%+v asset=%+v", view.Run, asset)
	}
	f, file, err := svc.OpenIngestFile(ctx, start.Run.RunID, "media_seg_0002")
	if err != nil || !file.Playable {
		t.Fatalf("prepared clip not readable: %+v %v", file, err)
	}
	f.Close()
	if _, _, err := svc.OpenIngestFile(ctx, start.Run.RunID, "../edl.json"); err == nil {
		t.Fatal("path-like file key accepted")
	}
	if _, err := svc.CancelIngestRun(ctx, "human:test", start.Run.RunID, view.Run.Version, "late"); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("cancel of ready run err = %v", err)
	}
}

func (e *ingestEnv) preparePackage(tk model.Task, in *service.IngestInput) {
	e.t.Helper()
	sel := in.Selection
	fps := sel.Output.FPS
	var segs []ingest.MappedSegment
	var video, audio []map[string]any
	var timeline int64
	for i, s := range sel.SelectedSegments {
		frames := ingest.OutputFrames(s.StartUs, s.EndUs, fps)
		samples := ingest.OutputSamples(frames, fps, 48000)
		media := fmt.Sprintf("media/segment_%03d.mp4", i+1)
		sum := e.write(in.PackageDir+"/"+media, []byte(fmt.Sprintf("clip-%d", i)))
		segs = append(segs, ingest.MappedSegment{SegmentID: s.SegmentID, Index: i + 1, Media: media, MediaSHA256: sum, SourceStartUs: s.StartUs, SourceEndUs: s.EndUs,
			TimelineInFrames: timeline, OutputFrames: frames, OutputSamples: samples, MeasuredFrames: frames, MeasuredSamples: samples, FrameQuota: in.FrameQuotas[i]})
		item := map[string]any{"src": media, "in": 0, "out": float64(frames) / float64(fps), "timeline_in": float64(timeline) / float64(fps)}
		audio = append(audio, item)
		v := map[string]any{"src": media, "in": 0, "out": item["out"], "timeline_in": item["timeline_in"], "ingest": map[string]any{"segment_id": s.SegmentID, "frame_quota": in.FrameQuotas[i]}}
		video = append(video, v)
		timeline += frames
	}
	audioIdx := 1
	e.write(in.PackageDir+"/source-map.json", ingest.SourceMap{SchemaVersion: 1, SourceID: in.Source.SourceID, SourceSHA256: sel.SourceSHA256, SelectionSHA256: in.SelectionSHA256,
		VideoStreamIndex: 0, GameAudioStreamIndex: &audioIdx, Output: sel.Output, FrameQuotaVersion: "quota-1", Segments: segs})
	prov := *in.Provenance
	prov.WorkerVersion, prov.MethodVersion = "test/1", "test"
	e.write(in.PackageDir+"/ingest-provenance.json", prov)
	e.write(in.PackageDir+"/edl.json", map[string]any{"timeline": map[string]any{"fps": fps, "sample_rate": 48000}, "video": video, "game_audio": audio,
		"voice": []any{}, "subtitle": []any{}, "music": []any{}})
	artifacts := []map[string]string{{"kind": "edl", "path": "edl.json"}, {"kind": "source_map", "path": "source-map.json"}, {"kind": "ingest_provenance", "path": "ingest-provenance.json"}}
	for _, s := range segs {
		artifacts = append(artifacts, map[string]string{"kind": "video", "path": s.Media})
	}
	e.write(in.PackageDir+"/delivery-manifest.json", map[string]any{"schema_version": 1, "artifacts": artifacts})
	e.write(in.PackageDir+"/worker-receipt.json", receipt(tk, in))
}

func TestIngestCancelInvalidatesInFlightExecution(t *testing.T) {
	env := newIngestEnv(t)
	ctx := context.Background()
	env.recording("c.mkv", 100)
	reg, err := env.svc.RegisterRecording(ctx, "human:test", "rec2", "rec", "c.mkv", "r")
	if err != nil {
		t.Fatal(err)
	}
	env.allowIngester("rec2")
	start, err := env.svc.StartIngestRun(ctx, "human:test", "rec2", reg.Source.SourceID, reg.SourceVersion, "s")
	if err != nil {
		t.Fatal(err)
	}
	env.capability("ing-b", ingest.RootsFingerprint(env.roots))
	tk, in := env.claim("ing-b")
	view, _ := env.svc.GetIngestRunView(ctx, start.Run.RunID)
	if _, err := env.svc.CancelIngestRun(ctx, "human:test", start.Run.RunID, view.Run.Version, `stop C:\Users\me\secret.mkv`); err != nil {
		t.Fatal(err)
	}
	env.probePackage(tk, in, 100)
	if err := env.submit("ing-b", tk, in); err == nil {
		t.Fatal("cancelled execution registered a result")
	}
	got, _ := env.svc.GetIngestRunView(ctx, start.Run.RunID)
	if got.Run.State != ingest.StateCancelled || strings.Contains(got.Run.ErrorMessage, `C:\Users`) {
		t.Fatalf("cancelled run = %+v", got.Run)
	}
	input, err := env.svc.GetTaskInput(ctx, "ing-b", ingester, tk.TaskID)
	if err == nil && !input.Cancelled {
		t.Fatalf("cancelled task input = %+v", input)
	}
	// The source changed after registration: a new run is refused.
	env.recording("c.mkv", 101)
	if _, err := env.svc.StartIngestRun(ctx, "human:test", "rec2", reg.Source.SourceID, reg.SourceVersion, "s2"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("changed source err = %v", err)
	}
}

func TestIngestStartIsAtomicAndSingleActiveUnderConcurrency(t *testing.T) {
	env := newIngestEnv(t)
	ctx := context.Background()
	env.recording("d.mkv", 64)
	reg, err := env.svc.RegisterRecording(ctx, "human:test", "rec3", "rec", "d.mkv", "r")
	if err != nil {
		t.Fatal(err)
	}
	env.allowIngester("rec3")

	// An audit failure rolls back the run, its task and the idempotency receipt.
	db := storeOf(t, env.svc).DB()
	if _, err := db.Exec(`CREATE TRIGGER fail_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.StartIngestRun(ctx, "human:test", "rec3", reg.Source.SourceID, reg.SourceVersion, "k0"); err == nil {
		t.Fatal("injected audit failure ignored")
	}
	var runs, tasks int
	_ = db.QueryRow(`SELECT count(*) FROM ingest_runs`).Scan(&runs)
	_ = db.QueryRow(`SELECT count(*) FROM tasks WHERE asset_id='rec3'`).Scan(&tasks)
	if runs != 0 || tasks != 0 {
		t.Fatalf("rollback left runs=%d tasks=%d", runs, tasks)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_audit`); err != nil {
		t.Fatal(err)
	}

	const n = 8
	errs := make(chan error, n)
	ids := make(chan string, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			out, err := env.svc.StartIngestRun(ctx, "human:test", "rec3", reg.Source.SourceID, reg.SourceVersion, fmt.Sprintf("k%d", i%2+1))
			errs <- err
			ids <- out.Run.RunID
		}(i)
	}
	ok, distinct := 0, map[string]bool{}
	for i := 0; i < n; i++ {
		err := <-errs
		id := <-ids
		if err == nil {
			ok++
			distinct[id] = true
		} else if !errors.Is(err, model.ErrConflict) {
			t.Fatalf("unexpected err %v", err)
		}
	}
	if len(distinct) != 1 || ok < 1 {
		t.Fatalf("concurrent starts produced %d runs (%d ok)", len(distinct), ok)
	}
	_ = db.QueryRow(`SELECT count(*) FROM ingest_runs`).Scan(&runs)
	if runs != 1 {
		t.Fatalf("runs = %d", runs)
	}
}

func TestOrdinaryTaskCreationCannotUseIngestTypes(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if err := svc.CreateAsset(ctx, model.Asset{AssetID: "a", Status: model.AssetStatusIngested, AllowedAgents: []string{}, Artifacts: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	for _, tk := range []model.Task{{TaskID: "t1", AssetID: "a", Type: "media_probe", AgentRole: "ingester"}, {TaskID: "t2", AssetID: "a", Type: "recognize", AgentRole: "ingester"}} {
		if err := svc.CreateTask(ctx, tk); err == nil {
			t.Fatalf("task %s created outside an ingest run", tk.TaskID)
		}
	}
}
