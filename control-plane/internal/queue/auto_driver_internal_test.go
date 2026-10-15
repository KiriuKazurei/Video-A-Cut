package queue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type scriptTicker struct {
	calls atomic.Int32
	f     func(n int32) error
}

func (s *scriptTicker) AutoTick(context.Context) error { return s.f(s.calls.Add(1)) }

func TestAutoDriverNextWaitBacksOffAndCaps(t *testing.T) {
	d := NewAutoDriver(&scriptTicker{}, 5*time.Second)
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, time.Minute, time.Minute, time.Minute}
	for i, w := range want {
		d.fails = i
		if got := d.nextWait(); got != w {
			t.Fatalf("after %d failures wait = %v, want %v", i, got, w)
		}
	}
	d.fails = 1000
	if got := d.nextWait(); got != time.Minute {
		t.Fatalf("many failures wait = %v", got)
	}
	// An interval already above the cap is never shortened.
	long := NewAutoDriver(&scriptTicker{}, 5*time.Minute)
	long.fails = 3
	if got := long.nextWait(); got != 5*time.Minute {
		t.Fatalf("long interval wait = %v", got)
	}
}

func TestAutoDriverClearsLastErrOnSuccessAndRestoresCadence(t *testing.T) {
	boom := errors.New("boom")
	f := &scriptTicker{f: func(n int32) error {
		if n <= 2 {
			return boom
		}
		return nil
	}}
	d := NewAutoDriver(f, time.Millisecond)
	d.tick(t.Context())
	d.tick(t.Context())
	if !errors.Is(d.LastErr(), boom) || d.fails != 2 {
		t.Fatalf("lastErr=%v fails=%d", d.LastErr(), d.fails)
	}
	d.tick(t.Context())
	if d.LastErr() != nil || d.fails != 0 || d.nextWait() != time.Millisecond {
		t.Fatalf("not recovered: lastErr=%v fails=%d wait=%v", d.LastErr(), d.fails, d.nextWait())
	}
}

func TestAutoDriverSurvivesAPanicAndKeepsTicking(t *testing.T) {
	f := &scriptTicker{f: func(n int32) error {
		if n == 1 {
			panic("bad step")
		}
		return nil
	}}
	d := NewAutoDriver(f, time.Millisecond)
	d.tick(t.Context())
	if err := d.LastErr(); err == nil || d.fails != 1 {
		t.Fatalf("a panic must be recorded as a failure: lastErr=%v fails=%d", err, d.fails)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for f.calls.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	<-done
	if f.calls.Load() < 4 {
		t.Fatalf("the driver stopped after a panic: %d ticks", f.calls.Load())
	}
}

// A cancelled context is shutdown, not a failure: it is neither logged nor kept.
func TestAutoDriverCancelledTickIsNotAFailure(t *testing.T) {
	f := &scriptTicker{f: func(int32) error { return context.Canceled }}
	d := NewAutoDriver(f, time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d.tick(ctx)
	if d.LastErr() != nil || d.fails != 0 {
		t.Fatalf("lastErr=%v fails=%d", d.LastErr(), d.fails)
	}
}
