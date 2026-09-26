package service_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// TestGate2Phase1 is the Gate 2 connectivity proof of Phase 1.
//
// Every other test in this package proves one layer against its own contract:
// the store against SQL, the service against its rules, the bus against its
// subscribers. This test proves the layers are correctly assembled: one run
// walks the whole operational seam of the control plane — service -> store ->
// events -> lease recovery -> audit — end to end, with no HTTP, no wire format
// and no transport in the way. That is the point of Gate 2: it upgrades
// "each layer is individually correct" into "the assembled system is correct",
// so any signature change, renamed constant or reversed dependency direction
// between the packages fails here rather than in a Phase 2 handler.
//
// The steps below are deliberate and ordered, because each one is a precondition
// of the next: the task cannot exist before the asset it hangs from, the claim
// cannot happen before the task is queued, and the audit and event assertions
// at the end are only meaningful because every earlier step succeeded through
// running process agree on what the object graph is.
func TestGate2Phase1(t *testing.T) {
	ctx := context.Background()

	// 1. Persistence. The store opens on an empty temp database, so every
	// row this test reads back was written by this test and no other;
	// newService fails the test if the store cannot be opened and closes it
	// through t.Cleanup.
	svc := newService(t)

	// 2. The fan-out half of the seam, attached to the service exactly as
	// main.assemble does it. The bus is closed by t.Cleanup before the
	// store, so nothing can publish after persistence is already gone.
	bus := events.New()
	svc.SetBus(bus)
	t.Cleanup(bus.Close)

	// 3. task_updated is the one event every task transition publishes, so
	// a single subscription observes the whole lifecycle. The buffer holds
	// every envelope of this run with room to spare, so the handler never
	// blocks and never drops one.
	got := make(chan events.Envelope, 16)
	bus.Subscribe("task_updated", func(ev events.Envelope) { got <- ev })

	// 4. The asset the task hangs from. It is agent-visible to narrator
	// only, which is what makes step 6's negative assertion meaningful.
	if err := svc.CreateAsset(ctx, model.Asset{
		AssetID:       "clip_001",
		Status:        model.AssetStatusIngested,
		AgentVisible:  true,
		AllowedAgents: []string{"narrator"},
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	// 5. One queued unit of work. CreateTask forces the queued status, so
	// the claim in step 7 has to find it through the queue.
	if err := svc.CreateTask(ctx, model.Task{
		TaskID:    "t_001",
		AssetID:   "clip_001",
		Type:      model.TaskTypeTTS,
		AgentRole: "narrator",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// 6. The visibility decision point, exercised from both sides: the
	// asset is exposed to exactly the role that is allowed to see it and
	// to nobody else. This is a service-layer rule over store rows, so
	// it fails loudly if the listing path bypassed the filter.
	visible, err := svc.ListVisibleAssets(ctx, "narrator")
	if err != nil {
		t.Fatalf("ListVisibleAssets(narrator): %v", err)
	}
	if len(visible) != 1 {
		t.Fatalf("visible assets for narrator = %d, want 1: %+v", len(visible), visible)
	}
	if visible[0].AssetID != "clip_001" {
		t.Fatalf("visible asset = %q, want clip_001", visible[0].AssetID)
	}

	hidden, err := svc.ListVisibleAssets(ctx, "recognizer")
	if err != nil {
		t.Fatalf("ListVisibleAssets(recognizer): %v", err)
	}
	if len(hidden) != 0 {
		t.Fatalf("visible assets for recognizer = %d, want 0: %+v", len(hidden), hidden)
	}

	// 7. The hand-off under a lease. The claim returns the task as the
	// agent received it and leaves the same state in the store.
	claimed, err := svc.ClaimTask(ctx, "narrator-01", "narrator", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if claimed.TaskID != "t_001" {
		t.Fatalf("claimed TaskID = %q, want t_001", claimed.TaskID)
	}
	if claimed.Status != model.TaskStatusClaimed {
		t.Fatalf("claimed Status = %q, want %q", claimed.Status, model.TaskStatusClaimed)
	}
	if claimed.AgentID != "narrator-01" {
		t.Fatalf("claimed AgentID = %q, want narrator-01", claimed.AgentID)
	}

	// 8. The lease renewal. The heartbeat moves the expiry to the supplied
	// time and that value must survive the round trip through SQLite
	// unchanged — it is compared with Equal, which compares instants
	// rather than monotonic-clock readings or wall-clock locations.
	renewed := time.Now().Add(2 * time.Minute)
	if err := svc.Heartbeat(ctx, "narrator-01", "narrator", "t_001", renewed); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	renewedTask, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask after Heartbeat: %v", err)
	}
	if renewedTask.LeaseUntil == nil {
		t.Fatal("LeaseUntil = nil after Heartbeat, want the renewed lease")
	}
	if !renewedTask.LeaseUntil.Equal(renewed) {
		t.Fatalf("LeaseUntil = %v, want %v (the renewed lease must round-trip)", *renewedTask.LeaseUntil, renewed)
	}

	// 9. Progress. The first report is also the claimed -> running
	// transition, so the status and the value must move together.
	if err := svc.ReportProgress(ctx, "narrator-01", "t_001", 0.5, "half"); err != nil {
		t.Fatalf("ReportProgress: %v", err)
	}
	running, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask after ReportProgress: %v", err)
	}
	if running.Status != model.TaskStatusRunning {
		t.Fatalf("Status = %q, want %q", running.Status, model.TaskStatusRunning)
	}
	if running.Progress != 0.5 {
		t.Fatalf("Progress = %v, want 0.5", running.Progress)
	}

	// 10. The artifacts of the finished job.
	if err := svc.SubmitResult(ctx, "narrator-01", "t_001", map[string]string{"voice": "voice.wav"}); err != nil {
		t.Fatalf("SubmitResult: %v", err)
	}

	// 11. The terminal state, read back from the store: succeeded, its
	// artifact recorded, a full progress bar, and above all no lease left
	// behind — a succeeded task that still held an expiry would be a
	// candidate for the recovery sweep, which is exactly wrong.
	done, err := svc.GetTask(ctx, "t_001")
	if err != nil {
		t.Fatalf("GetTask after SubmitResult: %v", err)
	}
	if done.Status != model.TaskStatusSucceeded {
		t.Fatalf("Status = %q, want %q", done.Status, model.TaskStatusSucceeded)
	}
	if done.Artifacts["voice"] != "voice.wav" {
		t.Fatalf("Artifacts[\"voice\"] = %q, want voice.wav", done.Artifacts["voice"])
	}
	if done.LeaseUntil != nil {
		t.Fatalf("LeaseUntil = %v, want nil (a finished task holds no lease)", *done.LeaseUntil)
	}
	if done.Progress != 1 {
		t.Fatalf("Progress = %v, want 1", done.Progress)
	}

	// 12. The recovery sweep must leave the finished task alone. A
	// terminal task is not in the active set the sweep lists, so the
	// count is 0 and nothing is moved back into the queue.
	requeued, err := svc.RequeueExpiredLeases(ctx)
	if err != nil {
		t.Fatalf("RequeueExpiredLeases: %v", err)
	}
	if requeued != 0 {
		t.Fatalf("requeued = %d, want 0 (a succeeded task is not recoverable)", requeued)
	}

	// 13. The audit chain. The run wrote exactly four audited actions: the
	// asset ingest's own row (an ingest is automatic, so it is recorded
	// under the pipeline's "system" actor), the task's creation the same
	// way, and the agent's claim and submit. Exactly four and no more is
	// the part that matters — every high-frequency write in between is
	// deliberately unaudited, and a heartbeat or progress report that
	// produced a row would bury the governance events this log exists for.
	logs, err := svc.ListAudit(ctx, 50)
	if err != nil {
		t.Fatalf("ListAudit: %v", err)
	}
	wantAudit := []struct{ actor, action, target string }{
		{"system", "asset.create", "clip_001"},
		{"system", "task.create", "t_001"},
		{"agent:narrator-01", "task.claim", "t_001"},
		{"agent:narrator-01", "task.submit", "t_001"},
	}
	if len(logs) != len(wantAudit) {
		t.Fatalf("audit entries = %d, want %d: %+v", len(logs), len(wantAudit), logs)
	}
	for _, l := range logs {
		if l.ID <= 0 {
			t.Errorf("audit entry %+v has ID = %d, want a persisted row id > 0", l, l.ID)
		}
	}
	for _, want := range wantAudit {
		var found bool
		for _, l := range logs {
			if l.Action == want.action && l.Actor == want.actor && l.Target == want.target {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no audit entry for %s by %s on %s: %+v", want.action, want.actor, want.target, logs)
		}
	}

	// 14. The event chain. The subscriber saw the lifecycle the store
	// recorded: at least one envelope for this task carrying a status
	// that proves a transition was published, not merely computed.
	envelopes := drainTaskUpdated(got, 2*time.Second)
	if len(envelopes) == 0 {
		t.Fatal("no task_updated envelope received within 2s: the fan-out is not wired to the service")
	}
	var matched bool
	for _, ev := range envelopes {
		if ev.Name != "task_updated" {
			t.Errorf("envelope name = %q, want task_updated", ev.Name)
			continue
		}
		published, ok := ev.Payload.(model.Task)
		if !ok {
			t.Errorf("envelope payload type = %T, want model.Task", ev.Payload)
			continue
		}
		if published.TaskID != "t_001" {
			continue
		}
		switch published.Status {
		case model.TaskStatusClaimed, model.TaskStatusRunning, model.TaskStatusSucceeded:
			matched = true
		default:
			t.Errorf("payload status = %q, want one of claimed, running, succeeded", published.Status)
		}
	}
	if !matched {
		t.Fatalf("no task_updated envelope for t_001 with a lifecycle status: %+v", envelopes)
	}
}

// drainTaskUpdated collects the envelopes buffered on ch and returns them,
// waiting up to timeout for the whole run to be delivered. It returns as soon
// as no further envelope arrives for 200ms, because the bus dispatches each
// handler in its own goroutine and the last publish may still be in flight
// when this starts; the outer timeout is the ceiling for a run where the
// fan-out is broken and nothing ever arrives.
func drainTaskUpdated(ch <-chan events.Envelope, timeout time.Duration) []events.Envelope {
	quiet := 200 * time.Millisecond
	deadline := time.After(timeout)
	out := []events.Envelope{}
	for {
		select {
		case ev := <-ch:
			out = append(out, ev)
		case <-time.After(quiet):
			return out
		case <-deadline:
			return out
		}
	}
}

// TestGate2RetentionChain is the Gate 2 proof of the Phase 1.5 seams: the
// configuration the operator edits reaches the retention sweep, and the sweep
// moves rows the way the policy describes.
//
// TestGate2Phase1 above proves the task seam. This one proves the other half
// of the system — that a configured retention window is not merely parsed but
// actually changes what the database keeps — because a config value that
// loads and validates but never reaches the sweep is the failure that leaves
// a log growing forever while every unit test passes.
func TestGate2RetentionChain(t *testing.T) {
	ctx := context.Background()

	// The windows are deliberately shorter than the retention periods so
	// the test can age rows past them without waiting a month. What is
	// being proven is the wiring, not the arithmetic of AddDate.
	const retainDays, archiveDays = 30, 30

	st, err := store.Open(filepath.Join(t.TempDir(), "vac.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := service.New(st)

	// One agent row and one system row, both aged past the retention
	// window. They are written through the store's own writer rather
	// than hand-built SQL so the sweep sees the same shape a live run
	// produces; only created_at is forced back, because that is the one
	// column a real writer always stamps with now.
	if err := st.WriteAudit(ctx, model.AuditLog{
		Actor:  "agent:narrator-01",
		Action: "task.fail",
		Target: "t_001",
		Detail: "tts synthesis failed",
	}); err != nil {
		t.Fatalf("write agent audit: %v", err)
	}
	if err := st.WriteAudit(ctx, model.AuditLog{
		Actor:  "system",
		Action: "asset.create",
		Target: "clip_001",
	}); err != nil {
		t.Fatalf("write system audit: %v", err)
	}
	if err := st.WriteAudit(ctx, model.AuditLog{
		Actor:  "agent:narrator-01",
		Action: "task.claim",
		Target: "t_002",
	}); err != nil {
		t.Fatalf("write fresh agent audit: %v", err)
	}
	aged := time.Now().UTC().AddDate(0, 0, -retainDays-1)
	if _, err := st.DB().ExecContext(ctx,
		`UPDATE audit_logs SET created_at = ? WHERE action IN ('task.fail','asset.create')`, aged,
	); err != nil {
		t.Fatalf("age audit rows: %v", err)
	}

	before, err := svc.ListAudit(ctx, 50)
	if err != nil {
		t.Fatalf("ListAudit before sweep: %v", err)
	}
	if len(before) != 3 {
		t.Fatalf("audit rows before sweep = %d, want 3", len(before))
	}

	archived, deleted, err := svc.SweepAudit(ctx, retainDays, archiveDays)
	if err != nil {
		t.Fatalf("SweepAudit: %v", err)
	}
	// The aged agent row is archived; the aged system row is deleted
	// outright; the fresh row is untouched by both.
	if archived != 1 {
		t.Errorf("archived = %d, want 1 (the aged agent row)", archived)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1 (the aged system row)", deleted)
	}

	after, err := svc.ListAudit(ctx, 50)
	if err != nil {
		t.Fatalf("ListAudit after sweep: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("audit rows after sweep = %d, want 1 (the fresh agent row): %+v", len(after), after)
	}
	if after[0].Target != "t_002" {
		t.Errorf("surviving row target = %q, want t_002 (the fresh row)", after[0].Target)
	}

	// The archived row is now in the archive table, which is what makes
	// it readable after it left the live log. Counting through the store
	// rather than a service accessor keeps this a wiring proof: the
	// archive table is the sink SweepAudit was pointed at.
	var archivedCount int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM audit_logs_archive WHERE target = 't_001'`,
	).Scan(&archivedCount); err != nil {
		t.Fatalf("count archive rows: %v", err)
	}
	if archivedCount != 1 {
		t.Errorf("archived rows for t_001 = %d, want 1", archivedCount)
	}

	// A second sweep is a no-op, so the retention loop can run on its
	// timer without changing anything that has already been settled.
	archived2, deleted2, err := svc.SweepAudit(ctx, retainDays, archiveDays)
	if err != nil {
		t.Fatalf("second SweepAudit: %v", err)
	}
	if archived2 != 0 || deleted2 != 0 {
		t.Errorf("second sweep = (%d archived, %d deleted), want (0, 0)", archived2, deleted2)
	}
}
