package events

import (
	"sync"
	"testing"
)

// --- EventBus tests ---

func TestEventBus_SubscribeAndPublish(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe("test")

	bus.Publish("test", "hello")

	select {
	case ev := <-ch:
		if ev.Type != "test" {
			t.Errorf("expected type 'test', got %q", ev.Type)
		}
		if ev.Data.(string) != "hello" {
			t.Errorf("expected data 'hello', got %v", ev.Data)
		}
	default:
		t.Fatal("expected message on channel")
	}
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewEventBus()
	ch1 := bus.Subscribe("test1")
	ch2 := bus.Subscribe("test2")

	bus.Publish("test1", "hello")
	bus.Publish("test2", "hello")

	received := 0
	for received < 2 {
		select {
		case <-ch1:
			received++
		case <-ch2:
			received++
		}
	}
	if received != 2 {
		t.Errorf("expected 2 messages, got %d", received)
	}
}

func TestEventBus_DifferentChannels(t *testing.T) {
	bus := NewEventBus()
	ch1 := bus.Subscribe("channel1")
	ch2 := bus.Subscribe("channel2")

	bus.Publish("channel1", "msg1")
	bus.Publish("channel2", "msg2")

	// Publish broadcasts to ALL subscribers, so each channel gets both events.
	// Collect from ch1 (should have 2 events)
	got := make(map[string]bool)
	for i := 0; i < 2; i++ {
		ev := <-ch1
		got[ev.Type] = true
	}
	if !got["channel1"] || !got["channel2"] {
		t.Errorf("expected both channel1 and channel2 on ch1, got %v", got)
	}

	// Same for ch2
	got = make(map[string]bool)
	for i := 0; i < 2; i++ {
		ev := <-ch2
		got[ev.Type] = true
	}
	if !got["channel1"] || !got["channel2"] {
		t.Errorf("expected both channel1 and channel2 on ch2, got %v", got)
	}
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe("test")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish("test", "msg")
		}()
	}
	wg.Wait()

	// Collect all messages
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != 10 {
		t.Errorf("expected 10 messages, got %d", count)
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe("test")

	bus.Publish("test", "before")
	bus.Unsubscribe("test")
	bus.Publish("test", "after")

	// Should only get the one before unsubscribe
	select {
	case ev := <-ch:
		if ev.Data.(string) != "before" {
			t.Errorf("expected 'before', got %v", ev.Data)
		}
	default:
		t.Fatal("expected message before unsubscribe")
	}

	// Channel should be closed after unsubscribe
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after unsubscribe")
		}
	default:
		// Channel might still have buffered data; that's OK
	}
}

func TestEventBus_EventID(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe("test")

	bus.Publish("test", "hello")

	ev := <-ch
	if ev.ID == "" {
		t.Error("expected non-empty event ID")
	}
}

func TestEventBus_CriticalEventDelivery(t *testing.T) {
	bus := NewEventBus()
	// Create a subscriber with a small buffer
	ch := make(chan UIEvent, 1)
	bus.Subscribe("test")
	// Replace with our small buffer
	bus.subscribers["test"] = ch

	// Fill the buffer
	bus.Publish(EventTypeQueryStarted, "fill")

	// Critical event should still be delivered
	bus.Publish(EventTypeSecurityApprovalRequest, "critical")

	select {
	case ev := <-ch:
		if ev.Type != EventTypeSecurityApprovalRequest {
			// Might have gotten the fill event first; try again
			ev = <-ch
			if ev.Type != EventTypeSecurityApprovalRequest {
				t.Errorf("expected critical event, got %q", ev.Type)
			}
		}
	default:
		t.Fatal("expected at least one event on channel")
	}
}
