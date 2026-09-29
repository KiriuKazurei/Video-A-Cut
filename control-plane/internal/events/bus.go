// Package events provides the bounded in-process event fan-out used by SSE.
// It contains no transport or business rules.
package events

import (
	"errors"
	"log/slog"
	"sync"
)

const subscriberBuffer = 64

var ErrClosed = errors.New("events: bus is closed")

type Envelope struct {
	Name    string
	Payload any
}

type Handler func(Envelope)

type subscription struct {
	id      int
	names   map[string]struct{}
	handler Handler
	queue   chan Envelope
	done    chan struct{}
}

// Bus delivers each subscriber's events in Publish order using one bounded
// queue and one worker per subscription. A full queue removes and closes that
// subscription; queued events drain and no later event is silently appended.
// A slow or panicking subscriber cannot block other subscribers or create a
// goroutine per event.
type Bus struct {
	mu     sync.Mutex
	subs   map[int]*subscription
	next   int
	closed bool
	log    *slog.Logger
}

func New() *Bus {
	return &Bus{subs: make(map[int]*subscription), log: slog.Default()}
}

// Subscribe registers h for one event name. It returns 0 if the bus is closed.
func (b *Bus) Subscribe(name string, h Handler) int {
	id, err := b.SubscribeMany([]string{name}, h)
	if err != nil {
		return 0
	}
	return id
}

// SubscribeMany registers one ordered subscription for several event names.
// Using one subscription for a stream preserves order across event topics.
func (b *Bus) SubscribeMany(names []string, h Handler) (int, error) {
	id, _, err := b.SubscribeManySignal(names, h)
	return id, err
}

// SubscribeManySignal is SubscribeMany plus a channel closed when the bus
// cancels this subscription because it was removed, overflowed, or the bus
// closed.
func (b *Bus) SubscribeManySignal(names []string, h Handler) (int, <-chan struct{}, error) {
	filters := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name != "" {
			filters[name] = struct{}{}
		}
	}
	if len(filters) == 0 || h == nil {
		return 0, nil, errors.New("events: subscription requires names and a handler")
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, nil, ErrClosed
	}
	b.next++
	sub := &subscription{
		id:      b.next,
		names:   filters,
		handler: h,
		queue:   make(chan Envelope, subscriberBuffer),
		done:    make(chan struct{}),
	}
	b.subs[sub.id] = sub
	go b.run(sub)
	return sub.id, sub.done, nil
}

// Unsubscribe removes the subscription returned for name. The name is kept in
// the signature for source compatibility; ids are unique across the bus.
func (b *Bus) Unsubscribe(_ string, id int) { b.UnsubscribeID(id) }

func (b *Bus) UnsubscribeID(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.removeLocked(id)
}

func (b *Bus) removeLocked(id int) {
	if sub, ok := b.subs[id]; ok {
		delete(b.subs, id)
		close(sub.queue)
		close(sub.done)
	}
}

// Publish enqueues an event for every matching subscriber. Calls made
// sequentially are observed in that order by each subscriber, including
// subscribers registered for several event names.
func (b *Bus) Publish(ev Envelope) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for id, sub := range b.subs {
		if _, ok := sub.names[ev.Name]; !ok {
			continue
		}
		select {
		case sub.queue <- ev:
		default:
			b.log.Warn("events: subscriber queue overflow; closing subscription",
				"subscription", id, "event", ev.Name)
			b.removeLocked(id)
		}
	}
}

func (b *Bus) run(sub *subscription) {
	for ev := range sub.queue {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					b.log.Error("events: subscriber panic", "event", ev.Name, "recover", recovered)
				}
			}()
			sub.handler(ev)
		}()
	}
}

// Close prevents new subscriptions and publishing, and releases every
// subscriber queue. A handler currently running is allowed to return; Close
// does not wait on user callback code.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id := range b.subs {
		b.removeLocked(id)
	}
}
