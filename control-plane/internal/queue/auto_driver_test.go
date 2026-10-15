package queue_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/queue"
)

type fakeTicker struct {
	calls atomic.Int32
	err   error
}

func (f *fakeTicker) AutoTick(context.Context) error {
	f.calls.Add(1)
	return f.err
}

func TestAutoDriverTicksImmediatelyAndPeriodically(t *testing.T) {
	f := &fakeTicker{}
	d := queue.NewAutoDriver(f, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for f.calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	if f.calls.Load() < 3 {
		t.Fatalf("ticks = %d", f.calls.Load())
	}
}

func TestAutoDriverRecordsAFailureAndKeepsRunning(t *testing.T) {
	boom := errors.New("boom")
	f := &fakeTicker{err: boom}
	d := queue.NewAutoDriver(f, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for f.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if !errors.Is(d.LastErr(), boom) || f.calls.Load() < 2 {
		t.Fatalf("lastErr=%v calls=%d", d.LastErr(), f.calls.Load())
	}
}

func TestAutoDriverZeroIntervalFallsBack(t *testing.T) {
	if d := queue.NewAutoDriver(&fakeTicker{}, 0); d == nil {
		t.Fatal("nil driver")
	}
}
