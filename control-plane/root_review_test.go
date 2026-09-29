package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/config"
)

func reviewApp(t *testing.T) *app {
	t.Helper()
	cfg := config.Default()
	cfg.HttpAddr = "127.0.0.1:0"
	a, err := assemble(filepath.Join(t.TempDir(), "review-main.db"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestReviewRunPropagatesListenerFailureWithoutExternalCancel(t *testing.T) {
	a := reviewApp(t)
	if err := a.httpLn.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	err := a.Run(ctx)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run did not propagate listener error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Run waited for external cancellation (%v), original error: %v", elapsed, err)
	}
}

func TestReviewRunCancellationClosesActiveSSEConnection(t *testing.T) {
	a := reviewApp(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("http://" + a.httpLn.Addr().String() + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE = %d", resp.StatusCode)
	}
	closed := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, resp.Body); closed <- err }()
	cancel()
	select {
	case <-closed:
	case <-time.After(httpShutdownGrace + 2*time.Second):
		t.Error("SSE connection survived application cancellation")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run cancellation = %v", err)
		}
	case <-time.After(httpShutdownGrace + time.Second):
		t.Error("Run did not terminate after SSE cancellation")
	}
}
