package events_test

import (
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
)

func TestReviewBusFIFOAcrossTopics(t *testing.T) {
	bus := events.New()
	defer bus.Close()
	received := make(chan int, 8)
	_, _, err := bus.SubscribeManySignal([]string{"created", "updated"}, func(ev events.Envelope) { received <- ev.Payload.(int) })
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		name := "updated"
		if i%2 == 0 {
			name = "created"
		}
		bus.Publish(events.Envelope{Name: name, Payload: i})
	}
	for i := 0; i < 8; i++ {
		select {
		case got := <-received:
			if got != i {
				t.Fatalf("event %d arrived as %d", i, got)
			}
		case <-time.After(time.Second):
			t.Fatal("ordered subscription stalled")
		}
	}
}

func TestReviewBusOverflowSignalsDisconnectedSubscriber(t *testing.T) {
	bus := events.New()
	defer bus.Close()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	_, disconnected, err := bus.SubscribeManySignal([]string{"update"}, func(events.Envelope) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	})
	if err != nil {
		t.Fatal(err)
	}
	bus.Publish(events.Envelope{Name: "update"})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("subscriber never started")
	}
	for i := 0; i < 256; i++ {
		bus.Publish(events.Envelope{Name: "update"})
	}
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("overflow left subscriber appearing live")
	}
}
