// Package events provides the in-process event bus that fans state changes out
// to subscribers such as the Phase 2 SSE handler. It knows nothing about HTTP
// or any other transport: a subscriber receives an Envelope and decides itself
// how to encode it, so the bus is usable standalone before Phase 2 exists.
package events

import (
	"log/slog"
	"sync"
)

// Envelope is a single bus event.
//
// Name is the SSE event name the Phase 2 handler writes on the wire; Payload is
// the domain value that handler serialises.
type Envelope struct {
	Name    string
	Payload any
}

// Handler consumes one Envelope.
//
// A handler must not block. The bus dispatches every handler in its own
// goroutine, so blocking work does not delay the publisher, but it does pin a
// goroutine for as long as it blocks. Long work belongs in a goroutine owned by
// the handler itself.
//
// A handler should also avoid panicking: the bus recovers, logs and discards
// the event, so a panic costs only its own delivery — never the publisher.
type Handler func(Envelope)

// Bus fans envelopes out to every subscriber of the matching event name.
//
// Handlers of one name receive envelopes in an unspecified order. The zero
// value is unusable; call New. A Bus is safe for concurrent use.
type Bus struct {
	// subs holds, per event name, the subscribers of that name keyed by the
	// id Subscribe handed out.
	subs map[string]map[int]Handler
	// next is the id the next Subscribe call returns. It starts at 1 so a
	// subscription id is never the zero value.
	next int

	// mu guards subs, next and done.
	mu sync.RWMutex
	// done is the shutdown sentinel: Close closes it and then nils it, which
	// is what makes Close idempotent. Publish delivers nothing once done is
	// nil.
	done chan struct{}
	// log records subscriber panics recovered during dispatch.
	log *slog.Logger
}

// New returns a started Bus ready to take subscriptions.
func New() *Bus {
	return &Bus{
		subs: make(map[string]map[int]Handler),
		done: make(chan struct{}),
		log:  slog.Default(),
	}
}

// Subscribe registers h for name and returns the subscription id to hand back
// to Unsubscribe. Several handlers may subscribe to the same name.
func (b *Bus) Subscribe(name string, h Handler) int {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.next++
	id := b.next
	if b.subs[name] == nil {
		b.subs[name] = make(map[int]Handler)
	}
	b.subs[name][id] = h
	return id
}

// Unsubscribe removes the subscription id that Subscribe returned for name.
// Unknown names and ids are ignored, so a double unsubscribe is harmless.
func (b *Bus) Unsubscribe(name string, id int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	hs, ok := b.subs[name]
	if !ok {
		return
	}
	delete(hs, id)
	// Drop the now-empty inner map so names do not accumulate over time.
	if len(hs) == 0 {
		delete(b.subs, name)
	}
}

// Publish hands ev to every subscriber of ev.Name.
//
// The handler set is copied under the read lock and the lock is released before
// any dispatch. That is what keeps a slow or panicking subscriber from blocking
// the publisher: no handler code runs while the lock is held, so the publisher
// never waits on a subscriber, and concurrent Subscribe, Unsubscribe and
// Publish calls never queue behind handler work either.
//
// Each handler then runs in its own goroutine with a deferred recover, so a
// panic is logged and contained instead of crashing the process. Nothing is
// delivered once the bus has been closed.
func (b *Bus) Publish(ev Envelope) {
	b.mu.RLock()
	if b.done == nil {
		b.mu.RUnlock()
		return
	}
	handlers := make([]Handler, 0, len(b.subs[ev.Name]))
	for _, h := range b.subs[ev.Name] {
		handlers = append(handlers, h)
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		go func(h Handler) {
			defer func() {
				if r := recover(); r != nil {
					b.log.Error("events: subscriber panic", "event", ev.Name, "recover", r)
				}
			}()
			h(ev)
		}(h)
	}
}

// Close shuts the bus down; a later Publish delivers nothing. It is idempotent,
// so calling it twice is safe.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.done == nil {
		return
	}
	close(b.done)
	b.done = nil
}
