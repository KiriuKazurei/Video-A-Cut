package preview

import (
	"bytes"
	"context"
)

const (
	maxProbeStdout = 1 << 20
	maxProbeStderr = 16 << 10
)

// The exec copier writes from one goroutine. Run waits for it before reading.
// Cancel immediately on overflow and keep draining without storing more bytes.
type probeOutput struct {
	buffer   bytes.Buffer
	exceeded bool
	cancel   context.CancelFunc
}

func (w *probeOutput) Write(p []byte) (int, error) {
	remaining := maxProbeStdout - w.buffer.Len()
	if len(p) > remaining {
		_, _ = w.buffer.Write(p[:remaining])
		w.exceeded = true
		w.cancel()
	} else {
		_, _ = w.buffer.Write(p)
	}
	return len(p), nil
}

// Keep only a bounded diagnostic suffix, including when one write is huge.
type probeTail struct{ data []byte }

func (w *probeTail) Write(p []byte) (int, error) {
	if len(p) >= maxProbeStderr {
		w.data = append(w.data[:0], p[len(p)-maxProbeStderr:]...)
	} else {
		if excess := len(w.data) + len(p) - maxProbeStderr; excess > 0 {
			copy(w.data, w.data[excess:])
			w.data = w.data[:len(w.data)-excess]
		}
		w.data = append(w.data, p...)
	}
	return len(p), nil
}

func (w *probeTail) String() string { return string(w.data) }
