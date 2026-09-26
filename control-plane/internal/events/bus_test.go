package events_test

import (
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// TestPublishReachesSubscriber verifies a subscribed handler receives the
// published envelope with its payload intact.
func TestPublishReachesSubscriber(t *testing.T) {
	b := events.New()
	defer b.Close()

	got := make(chan events.Envelope, 1)
	b.Subscribe("task_updated", func(ev events.Envelope) { got <- ev })

	b.Publish(events.Envelope{
		Name: "task_updated",
		Payload: model.Task{
			TaskID:   "t_001",
			Status:   model.TaskStatusRunning,
			Progress: 0.42,
		},
	})

	select {
	case ev := <-got:
		if ev.Name != "task_updated" {
			t.Fatalf("event name: got %q, want %q", ev.Name, "task_updated")
		}
		task, ok := ev.Payload.(model.Task)
		if !ok {
			t.Fatalf("payload type: got %T, want model.Task", ev.Payload)
		}
		if task.TaskID != "t_001" {
			t.Fatalf("task id: got %q, want %q", task.TaskID, "t_001")
		}
		if task.Status != model.TaskStatusRunning {
			t.Fatalf("task status: got %q, want %q", task.Status, model.TaskStatusRunning)
		}
		if task.Progress != 0.42 {
			t.Fatalf("task progress: got %v, want %v", task.Progress, 0.42)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive the envelope within 1s")
	}
}

// TestSlowSubscriberDoesNotBlockPublisher verifies the dispatch contract: a
// handler that never returns must not stall the publisher.
func TestSlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	b := events.New()
	defer b.Close()

	// A handler that never signals and never returns.
	never := make(chan struct{})
	b.Subscribe("task_updated", func(events.Envelope) { <-never })

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			b.Publish(events.Envelope{
				Name: "task_updated",
				Payload: model.Task{
					TaskID: "t_002",
					Status: model.TaskStatusRunning,
				},
			})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publisher blocked on a subscriber that never returns")
	}
}

// TestUnsubscribeStopsDelivery verifies that the id returned by Subscribe
// actually removes that handler from the fan-out set.
func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := events.New()
	defer b.Close()

	got := make(chan events.Envelope, 1)
	id := b.Subscribe("task_updated", func(ev events.Envelope) { got <- ev })
	b.Unsubscribe("task_updated", id)

	b.Publish(events.Envelope{
		Name:    "task_updated",
		Payload: model.Task{TaskID: "t_003", Status: model.TaskStatusRunning},
	})

	select {
	case ev := <-got:
		t.Fatalf("delivered after unsubscribe: %+v", ev)
	case <-time.After(200 * time.Millisecond):
		// No delivery within the window: the unsubscribe held.
	}
}

// TestSubscriberPanicIsolated verifies a panicking handler cannot take down its
// co-subscribers or the publisher.
func TestSubscriberPanicIsolated(t *testing.T) {
	b := events.New()
	defer b.Close()

	b.Subscribe("task_updated", func(events.Envelope) { panic("boom") })

	got := make(chan events.Envelope, 1)
	b.Subscribe("task_updated", func(ev events.Envelope) { got <- ev })

	b.Publish(events.Envelope{
		Name:    "task_updated",
		Payload: model.Task{TaskID: "t_004", Status: model.TaskStatusRunning},
	})

	select {
	case ev := <-got:
		task, ok := ev.Payload.(model.Task)
		if !ok {
			t.Fatalf("payload type: got %T, want model.Task", ev.Payload)
		}
		if task.TaskID != "t_004" {
			t.Fatalf("task id: got %q, want %q", task.TaskID, "t_004")
		}
	case <-time.After(time.Second):
		t.Fatal("healthy subscriber starved because another one panicked")
	}
}
