package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// sseAsset is the row every stream test queues work on. It is created before
// the bus is attached so opening a stream starts from a quiet bus, and a test
// decides for itself which event it forces.
var sseAsset = model.Asset{AssetID: "a_1", Status: model.AssetStatusIngested}

// sseStream is one live connection to the event endpoint.
//
// The connection is real — httptest.NewServer over loopback — because the
// contract under test is streaming behaviour: that bytes leave the process as
// they are written, that they stop when the client is gone, and that the
// headers are on the wire before the first event. An
// httptest.ResponseRecorder's Flush only sets a flag, so it would pass a
// stream that in fact buffered everything until the handler returned.
type sseStream struct {
	ts   *httptest.Server
	resp *http.Response
	rd   *bufio.Reader
	// stop cancels the request, which is how a browser closing a tab
	// ends a stream: net/http cancels r.Context() inside the handler.
	stop func()
}

// sseFrame is one decoded event-source frame.
type sseFrame struct {
	// name is the value after "event: "; it is empty for a comment line,
	// which is how a heartbeat arrives.
	name string
	// data is the concatenation of the "data:" field values of the frame.
	data string
	// lines holds the frame's field lines in the order they arrived, so a
	// test can assert the wire layout rather than only the decoded value.
	lines []string
	// dataLines counts the frame's data fields, which is how a multi-line
	// payload is told apart from a single-line one.
	dataLines int
	// comment marks a frame that was a comment line rather than an event.
	comment bool
}

// sseHarness records the subscriptions one stream takes and releases.
//
// The bus exposes no API reporting who is subscribed, and the fact it is asked
// for is exactly what matters: a subscription that outlived its request would
// leak, because the bus would keep invoking this request's fan-in handler into
// a channel nobody reads any more. Recording through the one indirection the
// handler already has is what makes the teardown observable without adding
// subscriber bookkeeping to the bus itself.
type sseHarness struct {
	mu sync.Mutex
	// taken holds the subscription ids per event name, in subscribe order.
	taken map[string][]int
	// released holds every id handed back so far.
	released []int

	readyOnce sync.Once
	ready     chan struct{}
	// want is how many distinct event names must be subscribed before the
	// harness reports the stream ready.
	want int
}

func newSSEHarness() *sseHarness {
	return &sseHarness{
		taken: map[string][]int{},
		ready: make(chan struct{}),
		want:  len(sseEventNames),
	}
}

// install routes the stream's bus calls through the recorder for the duration
// of one test.
func (h *sseHarness) install(t *testing.T) {
	t.Helper()
	prev := sseOps
	t.Cleanup(func() { sseOps = prev })

	sseOps = streamOps{
		subscribe: func(bus *events.Bus, name string, handler events.Handler) int {
			id := prev.subscribe(bus, name, handler)
			h.mu.Lock()
			h.taken[name] = append(h.taken[name], id)
			if len(h.taken) >= h.want {
				h.readyOnce.Do(func() { close(h.ready) })
			}
			h.mu.Unlock()
			return id
		},
		unsubscribe: func(bus *events.Bus, name string, id int) {
			h.mu.Lock()
			h.released = append(h.released, id)
			h.mu.Unlock()
			prev.unsubscribe(bus, name, id)
		},
	}
}

// waitReady blocks until the handler has subscribed to every event name.
//
// Every publish in a stream test happens after this returns, so no test can
// lose an event to the race between the request arriving and the subscriptions
// being registered.
func (h *sseHarness) waitReady(t *testing.T) {
	t.Helper()
	select {
	case <-h.ready:
	case <-time.After(3 * time.Second):
		t.Fatalf("stream did not subscribe to all %d event names within 3s", h.want)
	}
}

// waitReleased blocks until every subscription the handler took has been
// handed back.
func (h *sseHarness) waitReleased(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.mu.Lock()
		taken, released := len(h.taken), len(h.released)
		h.mu.Unlock()

		if taken > 0 && released >= taken {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d subscriptions were released after the client disconnected",
				released, taken)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// snapshot returns a copy of what the harness has recorded so far.
func (h *sseHarness) snapshot() (map[string][]int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string][]int, len(h.taken))
	for k, v := range h.taken {
		out[k] = append([]int(nil), v...)
	}
	return out, len(h.released)
}

// connectSSE opens a stream against srv and waits until the handler is
// subscribed to every event name.
//
// The client carries a timeout rather than a bare context because a read that
// never returns has to fail the test: without one, a stream that swallows an
// event hangs the whole suite instead of reporting the missing frame.
func connectSSE(t *testing.T, srv *Server) (*sseStream, *sseHarness) {
	t.Helper()
	h := newSSEHarness()
	h.install(t)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	ts.Client().Timeout = 10 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	if err != nil {
		cancel()
		t.Fatalf("build request: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatalf("GET /api/events: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	st := &sseStream{ts: ts, resp: resp, rd: bufio.NewReader(resp.Body), stop: cancel}
	h.waitReady(t)
	return st, h
}

// frame reads the next event-source frame, comment or event alike.
//
// A comment line is returned as its own frame rather than skipped: a heartbeat
// is comment-shaped, and a test asserting an idle stream keeps its ticks has to
// be handed them.
func (s *sseStream) frame(t *testing.T) (sseFrame, error) {
	t.Helper()
	var f sseFrame
	for {
		line, err := s.rawLine()
		if err != nil {
			return f, err
		}
		if line == "" {
			// A blank line terminates a frame. Every writer emits it, so
			// it is padding between reads, not a malformed field.
			continue
		}
		if strings.HasPrefix(line, ":") {
			f.comment = true
			f.lines = append(f.lines, line)
			return f, nil
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			f.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			// Several data lines of one frame are joined with a newline,
			// which is what the event-source grammar prescribes.
			if f.dataLines > 0 || f.data != "" {
				f.data += "\n"
			}
			f.data += strings.TrimPrefix(line, "data: ")
			f.dataLines++
		default:
			t.Fatalf("malformed event-source line: %q", line)
		}
		f.lines = append(f.lines, line)
		if f.name != "" && f.dataLines > 0 {
			return f, nil
		}
	}
}

// rawLine reads one line and strips the terminator. A trailing partial line
// with no terminator is returned without its error; the next call reports the
// end of the stream.
func (s *sseStream) rawLine() (string, error) {
	line, err := s.rd.ReadString('\n')
	if line == "" && err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// waitFrameName reads frames until one carries name.
//
// Other events arriving first are part of the behaviour being tested — a claim
// also implies the create that preceded it — so they are skipped rather than
// treated as a failure.
func (s *sseStream) waitFrameName(t *testing.T, name string) sseFrame {
	t.Helper()
	for i := 0; i < 16; i++ {
		f, err := s.frame(t)
		if err != nil {
			t.Fatalf("read frame: %v (waiting for %s)", err, name)
		}
		if f.name == name {
			return f
		}
	}
	t.Fatalf("no %s frame arrived on the stream", name)
	return sseFrame{}
}

// newBusServer returns a Server whose service publishes to a real event bus,
// the wiring main.go assembles: without a bus attached the endpoint has no
// event source to stream from.
//
// The seed asset is created before the bus is attached so the stream starts
// from a silent bus and each test forces the event it asserts.
func newBusServer(t *testing.T) *Server {
	t.Helper()
	srv := newSeededServer(t, sseAsset)
	bus := events.New()
	srv.svc.SetBus(bus)
	t.Cleanup(bus.Close)
	return srv
}

// TestSSEResponseHeaders pins the two headers an event-source client depends
// on and a proxy must not be allowed to defeat.
//
// They are read off the live response rather than a recorder because headers
// are only observable on the wire once written: a handler that set them after
// the first frame would look right to a recorder inspected at the end of the
// request and would fail every real client.
func TestSSEResponseHeaders(t *testing.T) {
	srv := newBusServer(t)
	st, _ := connectSSE(t, srv)
	defer st.stop()

	if got := st.resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := st.resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
}

// TestSSESubscribesEveryKnownEventName pins the event vocabulary the stream
// carries.
//
// The bus matches names exactly and has no wildcard, so an event the handler
// forgot to subscribe to is never delivered — a WebUI would sit on an open
// connection waiting for a task update that was published and dropped. The
// names are listed here rather than borrowed from the handler's own table so
// that dropping one fails this test.
func TestSSESubscribesEveryKnownEventName(t *testing.T) {
	want := []string{
		"asset_created",
		"asset_updated",
		"task_created",
		"task_updated",
	}

	srv := newBusServer(t)
	st, h := connectSSE(t, srv)
	defer st.stop()

	taken, _ := h.snapshot()
	for _, name := range want {
		if len(taken[name]) == 0 {
			t.Fatalf("no subscription recorded for %q (got %v)", name, taken)
		}
	}
}

// TestSSEStreamsTaskUpdate proves a claim reaches an open stream as one
// task_updated frame carrying the task as the claiming agent received it.
//
// The publish comes from service.ClaimTask rather than a hand-wired bus call:
// this is the API layer, so the behaviour under test is that the operation a
// real client performs shows up on the stream.
func TestSSEStreamsTaskUpdate(t *testing.T) {
	srv := newBusServer(t)
	st, _ := connectSSE(t, srv)
	defer st.stop()

	if err := srv.svc.CreateTask(context.Background(), model.Task{
		TaskID:    "t_1",
		AssetID:   sseAsset.AssetID,
		Type:      model.TaskTypeRecognize,
		AgentRole: "recognizer",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	tk, err := srv.svc.ClaimTask(context.Background(), "agent_1", "recognizer", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}

	f := st.waitFrameName(t, "task_updated")
	var got model.Task
	if err := json.Unmarshal([]byte(f.data), &got); err != nil {
		t.Fatalf("task_updated data is not a JSON task: %v (%q)", err, f.data)
	}
	if got.TaskID != tk.TaskID {
		t.Fatalf("task_id = %q, want %q", got.TaskID, tk.TaskID)
	}
	if got.Status != model.TaskStatusClaimed {
		t.Fatalf("status = %q, want %q", got.Status, model.TaskStatusClaimed)
	}
	if got.AgentID != "agent_1" {
		t.Fatalf("agent_id = %q, want agent_1", got.AgentID)
	}
}

// TestSSEStreamsAssetUpdate proves a human governance change reaches the same
// stream, so a WebUI can refresh the row an operator just edited without
// polling for it.
func TestSSEStreamsAssetUpdate(t *testing.T) {
	srv := newBusServer(t)
	st, _ := connectSSE(t, srv)
	defer st.stop()

	if err := srv.svc.ApproveAsset(context.Background(), "operator", sseAsset.AssetID, true); err != nil {
		t.Fatalf("approve asset: %v", err)
	}

	f := st.waitFrameName(t, "asset_updated")
	var got model.Asset
	if err := json.Unmarshal([]byte(f.data), &got); err != nil {
		t.Fatalf("asset_updated data is not a JSON asset: %v (%q)", err, f.data)
	}
	if !got.HumanApproved {
		t.Fatal("human_approved = false, want the approved asset on the wire")
	}
}

// TestSSEFrameFormatIsWellFormed pins the wire grammar of one event frame.
//
// The event-source grammar is unforgiving in one direction and silent in the
// other: a frame a client cannot see still parses, so the exact field order and
// the single-line JSON payload are asserted from the raw lines.
func TestSSEFrameFormatIsWellFormed(t *testing.T) {
	srv := newBusServer(t)
	st, _ := connectSSE(t, srv)
	defer st.stop()

	if err := srv.svc.ApproveAsset(context.Background(), "operator", sseAsset.AssetID, true); err != nil {
		t.Fatalf("approve asset: %v", err)
	}

	f := st.waitFrameName(t, "asset_updated")
	if len(f.lines) < 2 {
		t.Fatalf("frame = %+v, want an event line followed by a data line", f)
	}
	if want := "event: asset_updated"; f.lines[0] != want {
		t.Fatalf("first line = %q, want %q", f.lines[0], want)
	}
	if want := "data: {"; !strings.HasPrefix(f.lines[1], want) {
		t.Fatalf("second line = %q, want it to start %q", f.lines[1], want)
	}
	// A payload spanning several data lines is legal SSE but must be
	// re-joined by the client; this handler's encoder never emits one, and
	// asserting it keeps a future change from shipping multi-line frames no
	// caller handles.
	if strings.ContainsAny(f.data, "\r\n") {
		t.Fatalf("data spans several lines: %q", f.data)
	}
	if !json.Valid([]byte(f.data)) {
		t.Fatalf("data is not valid JSON: %q", f.data)
	}
}

// TestSSEHeartbeatsWhileIdle proves an idle stream keeps sending.
//
// The comment line is the only payload an idle event source may carry, and it
// exists because proxies and load balancers drop a connection that sends
// nothing for a bounded number of seconds: without it a browser's subscription
// would end silently, with no error to retry on. The interval is shrunk for
// the test because 15 seconds of real time cannot be spent proving a tick that
// production waits for anyway.
func TestSSEHeartbeatsWhileIdle(t *testing.T) {
	restore := shrinkHeartbeat(50 * time.Millisecond)
	defer restore()

	srv := newBusServer(t)
	st, _ := connectSSE(t, srv)
	defer st.stop()

	// Two ticks in a row: one proves the comment line exists, the second
	// proves the stream was not closed after it.
	for i := 0; i < 2; i++ {
		f, err := st.frame(t)
		if err != nil {
			t.Fatalf("read heartbeat %d: %v", i+1, err)
		}
		if !f.comment {
			t.Fatalf("idle frame = %+v, want a comment line (: ping)", f)
		}
		if want := ": ping"; f.lines[0] != want {
			t.Fatalf("comment = %q, want %q", f.lines[0], want)
		}
	}
}

// TestSSEClientDisconnectUnsubscribes proves the subscriptions are torn down
// with the request.
//
// The teardown is the handler's to do — only it knows which ids it was handed
// — and getting it wrong leaks with every client that ever connected. The bus
// reports no subscriber count, so the assertion is on the recorded
// subscribe/unsubscribe pair, which is the contract that matters.
func TestSSEClientDisconnectUnsubscribes(t *testing.T) {
	srv := newBusServer(t)
	st, h := connectSSE(t, srv)

	// One real event first: a stream that never delivered anything could be
	// "cleaned up" correctly because it was never connected in the first
	// place, and this rules that out.
	if err := srv.svc.CreateAsset(context.Background(), model.Asset{
		AssetID: "a_evict",
		Status:  model.AssetStatusIngested,
	}); err != nil {
		t.Fatalf("create asset: %v", err)
	}
	if f := st.waitFrameName(t, "asset_created"); f.name != "asset_created" {
		t.Fatalf("first frame = %+v, want asset_created", f)
	}

	// Hanging up is a cancelled request plus a closed body, exactly what a
	// browser navigating away does to the connection.
	st.stop()
	if err := st.resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}

	h.waitReleased(t)
}

// TestSSEWithoutBusReturnsError proves a server whose service has no bus
// attached refuses the stream instead of opening a connection that can never
// deliver an event.
//
// A plain recorder is the right tool here because the answer is one JSON body:
// the case under test is precisely that the handler does not stream, and a
// server that answered 200 with an empty stream would make a client wait
// forever for events that cannot come.
func TestSSEWithoutBusReturnsError(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(t).Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/events", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := decodeErr(t, rec).Code; got != codeNoEventSource {
		t.Fatalf("error code = %q, want %q", got, codeNoEventSource)
	}
	// The message must name the endpoint, so a client integrating against
	// the plane can tell "the stream is not wired" from a server error.
	if body := rec.Body.String(); !strings.Contains(body, "event stream") {
		t.Fatalf("body = %q, want a message naming the event stream", body)
	}
}

// shrinkHeartbeat overrides the heartbeat interval for one test and returns
// the restore function.
//
// The interval is a package variable rather than a Server field because it is
// a transport default with no per-server meaning: a field would have to be
// threaded through New(svc) for the benefit of no caller in production.
func shrinkHeartbeat(d time.Duration) func() {
	prev := sseHeartbeatInterval
	sseHeartbeatInterval = d
	return func() { sseHeartbeatInterval = prev }
}
