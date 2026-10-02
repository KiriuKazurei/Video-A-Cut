package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
)

var sseEventNames = []string{"asset_created", "asset_updated", "task_created", "task_updated", "workflow.changed", "review.changed", "acceptance.changed", "profile.changed", "capability.changed", "ingest.changed"}

var sseHeartbeatInterval = 15 * time.Second

const (
	sseBuffer         = 64
	sseWriteTimeout   = 5 * time.Second
	codeNoEventSource = "no_event_source"
)

type streamOps struct {
	subscribe   func(bus *events.Bus, names []string, handler events.Handler) (int, <-chan struct{}, error)
	unsubscribe func(bus *events.Bus, id int)
}

var sseOps = streamOps{
	subscribe: func(bus *events.Bus, names []string, handler events.Handler) (int, <-chan struct{}, error) {
		return bus.SubscribeManySignal(names, handler)
	},
	unsubscribe: func(bus *events.Bus, id int) { bus.UnsubscribeID(id) },
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, "method HEAD is not allowed for /api/events")
		return
	}
	bus := s.svc.Bus()
	if bus == nil {
		writeError(w, http.StatusServiceUnavailable, codeNoEventSource,
			"event stream is not wired: the control plane was assembled without an event bus")
		return
	}

	stream := make(chan events.Envelope, sseBuffer)
	overflow := make(chan struct{}, 1)
	fanin := func(ev events.Envelope) {
		select {
		case stream <- ev:
		default:
			select {
			case overflow <- struct{}{}:
			default:
			}
		}
	}
	id, busDone, err := sseOps.subscribe(bus, sseEventNames, fanin)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, codeNoEventSource,
			"event stream is not available: the event bus is closed")
		return
	}
	defer sseOps.unsubscribe(bus, id)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(w)
	if err := prepareSSEWrite(controller); err != nil {
		slog.Default().Warn("sse: cannot set initial write deadline", "err", err)
		return
	}
	// Send a complete frame immediately. A bare header flush can be held by a
	// development proxy until the first heartbeat, delaying EventSource.onopen.
	if err := writeSSEComment(w, sseOpenComment); err != nil {
		slog.Default().Debug("sse: initial comment failed", "err", err)
		return
	}
	if err := controller.Flush(); err != nil {
		slog.Default().Debug("sse: initial flush failed", "err", err)
		return
	}

	tick := time.NewTicker(sseHeartbeatInterval)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-busDone:
			// The bus closes a subscription on shutdown, unsubscribe, or queue
			// overflow. This stream has no replay: an EventSource client must
			// fetch a REST snapshot on every open/reconnect. Reconnecting the
			// stream alone cannot recover events already dropped.
			return
		case <-overflow:
			// The transport buffer overflowed after the bus queue delivered.
			// Disconnect so the client can reconnect and fetch a REST
			// snapshot; reconnecting EventSource alone does not fetch one.
			return
		case ev := <-stream:
			if err := prepareSSEWrite(controller); err != nil {
				slog.Default().Debug("sse: write deadline failed", "err", err)
				return
			}
			if err := writeSSEEvent(w, ev); err != nil {
				slog.Default().Debug("sse: event write failed", "err", err)
				return
			}
			if err := controller.Flush(); err != nil {
				slog.Default().Debug("sse: event flush failed", "err", err)
				return
			}
		case <-tick.C:
			if err := prepareSSEWrite(controller); err != nil {
				slog.Default().Debug("sse: heartbeat deadline failed", "err", err)
				return
			}
			if err := writeSSEComment(w, sseHeartbeatComment); err != nil {
				slog.Default().Debug("sse: heartbeat write failed", "err", err)
				return
			}
			if err := controller.Flush(); err != nil {
				slog.Default().Debug("sse: heartbeat flush failed", "err", err)
				return
			}
		}
	}
}

func prepareSSEWrite(controller *http.ResponseController) error {
	err := controller.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

const sseHeartbeatComment = "ping"
const sseOpenComment = "connected"

func writeSSEEvent(w http.ResponseWriter, ev events.Envelope) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(ev.Payload); err != nil {
		return err
	}
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

func writeSSEComment(w http.ResponseWriter, text string) error {
	_, err := fmt.Fprintf(w, ": %s\n\n", text)
	return err
}
