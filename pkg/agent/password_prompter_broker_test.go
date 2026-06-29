package agent

import (
	"fmt"
	"sync"
	"testing"
)

func TestPasswordPrompterBroker_RegisterRespond(t *testing.T) {
	b := &passwordPrompterBrokerType{
		pending: make(map[string]chan string),
	}

	ch := b.register("req-1")
	if ch == nil {
		t.Fatal("register returned nil channel")
	}

	if !b.respond("req-1", "hunter2") {
		t.Fatal("respond should succeed for registered request")
	}

	got, ok := <-ch
	if !ok {
		t.Fatal("channel was closed")
	}
	if got != "hunter2" {
		t.Fatalf("expected password 'hunter2', got %q", got)
	}
}

func TestPasswordPrompterBroker_RespondUnknownID(t *testing.T) {
	b := &passwordPrompterBrokerType{
		pending: make(map[string]chan string),
	}

	if b.respond("nonexistent", "password") {
		t.Fatal("respond should return false for unknown request ID")
	}
}

func TestPasswordPrompterBroker_Cleanup(t *testing.T) {
	b := &passwordPrompterBrokerType{
		pending: make(map[string]chan string),
	}

	b.register("req-1")
	if len(b.pending) != 1 {
		t.Fatalf("expected 1 pending entry after register, got %d", len(b.pending))
	}

	b.cleanup("req-1")
	if len(b.pending) != 0 {
		t.Fatalf("expected 0 pending entries after cleanup, got %d", len(b.pending))
	}
}

func TestPasswordPrompterBroker_DoubleRespond(t *testing.T) {
	b := &passwordPrompterBrokerType{
		pending: make(map[string]chan string),
	}

	ch := b.register("req-1")

	// First respond should succeed.
	if !b.respond("req-1", "first") {
		t.Fatal("first respond should succeed")
	}

	// Second respond should fail (channel is full with size-1 buffer).
	if b.respond("req-1", "second") {
		t.Fatal("second respond should return false (channel full)")
	}

	// Drain the channel to verify it only has the first password.
	got, ok := <-ch
	if !ok {
		t.Fatal("channel was closed")
	}
	if got != "first" {
		t.Fatalf("expected 'first', got %q", got)
	}
}

func TestPasswordPrompterBroker_ConcurrentSafety(t *testing.T) {
	b := &passwordPrompterBrokerType{
		pending: make(map[string]chan string),
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-req-%d", i)
			ch := b.register(id)
			ok := b.respond(id, "password")
			if !ok {
				t.Errorf("respond failed for goroutine %d", i)
			}
			// Drain channel.
			<-ch
			b.cleanup(id)
		}(i)
	}

	wg.Wait()
}
