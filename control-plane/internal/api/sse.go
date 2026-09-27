package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
)

// sseEventNames is the vocabulary this stream carries, and it is exactly the
// set the service publishes.
//
// The bus matches event names exactly and offers no wildcard, so the handler
// must subscribe once per name: an event it did not name is published and
// dropped with no error and no delivery, which a subscriber can only observe
// as an event that never arrives. Listing the names in one place is what keeps
// that set from drifting out of the service's reach.
var sseEventNames = []string{
	"asset_created",
	"asset_updated",
	"task_created",
	"task_updated",
}

// sseHeartbeatInterval is how often an idle stream writes a comment line.
//
// It exists because an event source is a request a proxy believes it can idle
// out: load balancers and corporate proxies close a connection that has sent
// nothing for a bounded number of seconds — commonly 30 to 60 — which would
// end a browser's subscription silently, with no error and no reason to
// reconnect. A comment line costs nothing on the wire and no API on the client
// (an event source ignores comments entirely), so fifteen seconds sits well
// inside every proxy's patience.
var sseHeartbeatInterval = 15 * time.Second

// sseBuffer is the capacity of the fan-in channel between the bus and the
// writer goroutine.
//
// The bus calls every handler in its own goroutine, so a handler that blocks
// pins a goroutine for as long as it waits; the fix for that is a buffered
// send that gives up when the buffer is full. Sixty-four covers a burst — a
// queue of tasks being claimed in sequence — while bounding how much a slow
// client can make the bus hold for it.
const sseBuffer = 64

// codeNoEventSource is the answer's code when the stream has nothing to
// subscribe to. It is distinct from a generic internal error because the two
// mean different things to a client: this one is a server that was assembled
// without an event bus, which is a wiring fault an operator fixes, while a 500
// is a fault the client cannot see or act on.
const codeNoEventSource = "no_event_source"

// streamOps is the bus surface the stream handler uses, kept behind a struct
// so a test can observe the subscribe/unsubscribe pairing.
//
// The indirection is not decoration: whether a subscription is released with
// its request is this endpoint's only resource-management behaviour, and the
// bus exposes no API that reports who is subscribed. Rather than widen the
// bus's contract for one transport's test, the handler goes through these two
// calls and the test wraps them.
type streamOps struct {
	subscribe   func(bus *events.Bus, name string, handler events.Handler) int
	unsubscribe func(bus *events.Bus, name string, id int)
}

// sseOps is the live wiring of streamOps. Tests replace it for the duration of
// one test and restore it, which is why it is a package variable and not a
// Server field: it is a test seam, not configuration.
var sseOps = streamOps{
	subscribe: func(bus *events.Bus, name string, handler events.Handler) int {
		return bus.Subscribe(name, handler)
	},
	unsubscribe: func(bus *events.Bus, name string, id int) {
		bus.Unsubscribe(name, id)
	},
}

// streamEvents answers GET /api/events with a server-sent event stream of
// every state change the plane makes.
//
// The endpoint holds no rule of its own: what it may publish is decided by the
// service, which owns the publish calls, and what a frame looks like is decided
// by the SSE grammar below. A handler that filtered or reshaped the events
// would be a second copy of a rule that belongs to the service, which is the
// failure mode §11.2 warns about.
//
// The response is written incrementally from the first byte on. That is the
// whole point of the endpoint — a WebUI that subscribed to a governance change
// wants to learn about it while it happens — and it is also what makes the
// headers observable here rather than at the end of the request.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	// A service with no bus attached publishes nowhere, so this request
	// could never deliver an event. Answering 503 says that plainly;
	// answering 200 would open a connection a client would sit on
	// forever, which is the one outcome worse than a refused request.
	bus := s.svc.Bus()
	if bus == nil {
		writeError(w, http.StatusServiceUnavailable, codeNoEventSource,
			"event stream is not wired: the control plane was assembled without an event bus")
		return
	}

	// Written before anything else so a client learns the response type
	// from the status line's headers and not from a sniffed body. The
	// flush straight after commits them through the wrapper, which holds a
	// response back until a streaming handler releases it.
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// A writer that cannot flush cannot stream: the frames would sit in the
	// wrapper's buffer until the handler returned and arrive all at once.
	// That is a degraded stream rather than a broken one, so it is not an
	// error — but the writes below have to survive it.
	fl, _ := w.(http.Flusher)
	if fl != nil {
		fl.Flush()
	}

	// The fan-in channel is what makes the handler safe to run at all. The
	// bus calls each subscription's handler in its own goroutine, so all
	// four arrive concurrently and an http.ResponseWriter must only be
	// written from one goroutine. Each handler therefore does nothing but
	// deposit its envelope here, and a single loop reads them in order.
	stream := make(chan events.Envelope, sseBuffer)
	fanin := func(ev events.Envelope) {
		select {
		case stream <- ev:
		default:
			// The client is not keeping up and the buffer is full. The
			// send must not block: the bus documents that a handler
			// may not block, because doing so pins a goroutine and
			// back-pressures every other subscriber of the same event.
			// An event this client missed is the better failure.
		}
	}

	// Every subscription belongs to this request and is released when it
	// ends. Collecting the ids as they are handed out is what makes the
	// teardown total: there is no per-request table to keep, and none to
	// leak, because nothing outlives the deferred loop below.
	type sub struct {
		name string
		id   int
	}
	subs := make([]sub, 0, len(sseEventNames))
	for _, name := range sseEventNames {
		subs = append(subs, sub{name: name, id: sseOps.subscribe(bus, name, fanin)})
	}
	defer func() {
		for _, sb := range subs {
			sseOps.unsubscribe(bus, sb.name, sb.id)
		}
	}()

	tick := time.NewTicker(sseHeartbeatInterval)
	defer tick.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			// The client hung up, or the server is shutting down. Either
			// way the request is over: returning runs the deferred
			// unsubscribe, and net/http destroys the connection.
			return
		case ev := <-stream:
			if err := writeSSEEvent(w, ev); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		case <-tick.C:
			if err := writeSSEComment(w, sseHeartbeatComment); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
	}
}

// sseHeartbeatComment is the text of an idle-stream comment line. It carries
// no meaning to a client, which is why it may be sent at a cadence no event
// follows.
const sseHeartbeatComment = "ping"

// writeSSEEvent writes one event frame.
//
// The format is the event-source grammar exactly: an "event" line naming the
// event, one "data" line per line of the payload, and a blank line to end the
// frame. The blank line is not optional — without it the client holds the
// fields and waits for the event to end.
//
// The payload is encoded with a plain encoder rather than writeJSON because
// this is a frame field, not a response body: it carries no Content-Type and
// no status, and the single-line form the encoder produces is what lets one
// frame be one "data" line.
func writeSSEEvent(w http.ResponseWriter, ev events.Envelope) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(ev.Payload); err != nil {
		// The payload is a domain value the service already encoded once
		// for its own purposes, so a failure here means a value no JSON
		// can express. Dropping the frame is the only honest answer: a
		// half-written event would be read by the client as a complete
		// one.
		return err
	}

	// The encoder terminates every value with a newline, and the grammar
	// has no notion of one: a payload holding a newline would look like
	// the end of the field. The split below turns any such payload into
	// several data lines, which the client is required to re-join.
	payload := strings.Split(strings.TrimRight(buf.String(), "\r\n"), "\n")

	if _, err := fmt.Fprintf(w, "event: %s\n", ev.Name); err != nil {
		return err
	}
	for _, line := range payload {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	_, err := fmt.Fprint(w, "\n")
	return err
}

// writeSSEComment writes one comment line — a frame with no fields, which the
// client ignores and which keeps an idle connection alive.
func writeSSEComment(w http.ResponseWriter, text string) error {
	_, err := fmt.Fprintf(w, ": %s\n\n", text)
	return err
}
