package main

import (
	"context"
	"net/http"
	"sync"
)

// requestTracker counts HTTP handlers independently from connection state.
// Server.Close cancels active request contexts; the tracker lets app.Close
// wait for those handlers to release service/store work before closing SQLite.
type requestTracker struct {
	mu      sync.Mutex
	active  int
	closing bool
	done    chan struct{}
	closed  bool
}

func newRequestTracker() *requestTracker {
	return &requestTracker{done: make(chan struct{})}
}

func (t *requestTracker) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !t.begin() {
			http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
			return
		}
		defer t.end()
		next.ServeHTTP(w, r)
	})
}

func (t *requestTracker) begin() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closing {
		return false
	}
	t.active++
	return true
}

func (t *requestTracker) end() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active--
	t.closeDoneLocked()
}

func (t *requestTracker) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closing = true
	t.closeDoneLocked()
}

func (t *requestTracker) closeDoneLocked() {
	if t.closing && t.active == 0 && !t.closed {
		close(t.done)
		t.closed = true
	}
}

func (t *requestTracker) wait(ctx context.Context) error {
	select {
	case <-t.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
