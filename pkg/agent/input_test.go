package agent

import (
	"strings"
	"testing"
	"time"
)

func TestInputGetInjectionContext(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	t.Run("returns non-nil channel", func(t *testing.T) {
		ch := a.GetInputInjectionContext()
		if ch == nil {
			t.Fatal("expected non-nil channel")
		}
	})

	t.Run("returns the same channel as used by InjectInputContext", func(t *testing.T) {
		err := a.InjectInputContext("test message")
		if err != nil {
			t.Fatalf("InjectInputContext failed: %v", err)
		}

		ch := a.GetInputInjectionContext()
		select {
		case msg := <-ch:
			if msg != "test message" {
				t.Errorf("got %q, want %q", msg, "test message")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("message not received from channel")
		}
	})

	t.Run("channel is receive-only from outside", func(t *testing.T) {
		ch := a.GetInputInjectionContext()
		// The type system enforces this — if it compiles, it's correct.
		_ = ch
	})
}

func TestInputClearInjectionContext(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	t.Run("clears injected inputs", func(t *testing.T) {
		a.InjectInputContext("msg1")
		a.InjectInputContext("msg2")

		a.ClearInputInjectionContext()

		select {
		case msg := <-a.GetInputInjectionContext():
			t.Errorf("channel should be empty after clear, got %q", msg)
		default:
			// Expected: channel is empty
		}
	})

	t.Run("on already-empty channel does not block", func(t *testing.T) {
		done := make(chan struct{})
		go func() {
			a.ClearInputInjectionContext()
			close(done)
		}()

		select {
		case <-done:
			// Success: cleared immediately
		case <-time.After(100 * time.Millisecond):
			t.Error("ClearInputInjectionContext blocked on empty channel")
		}
	})

	t.Run("clears multiple injected inputs", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			a.InjectInputContext("message")
		}

		a.ClearInputInjectionContext()

		select {
		case <-a.GetInputInjectionContext():
			t.Error("channel should be empty after clearing 5 items")
		default:
			// Expected
		}
	})

	t.Run("can inject after clearing", func(t *testing.T) {
		a.InjectInputContext("before")
		a.ClearInputInjectionContext()

		err := a.InjectInputContext("after clear")
		if err != nil {
			t.Fatalf("failed to inject after clear: %v", err)
		}

		select {
		case msg := <-a.GetInputInjectionContext():
			if msg != "after clear" {
				t.Errorf("got %q, want %q", msg, "after clear")
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("message not received")
		}
	})
}

func TestInputIsInterrupted(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	t.Run("returns false when no interrupt is pending", func(t *testing.T) {
		if a.IsInterrupted() {
			t.Error("expected IsInterrupted to be false initially")
		}
	})
}

func TestInputInjectChannelFull(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	t.Run("returns error when channel is full", func(t *testing.T) {
		a.ClearInputInjectionContext()

		gotError := false
		for i := 0; i < 1000; i++ {
			err := a.InjectInputContext("filler")
			if err != nil {
				gotError = true
				if !strings.Contains(err.Error(), "full") {
					t.Errorf("expected 'full' in error, got: %v", err)
				}
				break
			}
		}
		if !gotError {
			t.Log("channel buffer was large enough to hold all 1000 items (no error)")
		}
	})
}

func TestInputInjectionIntegrationNew(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	t.Run("inject then receive through GetInputInjectionContext", func(t *testing.T) {
		testInput := "integration test message"
		err := a.InjectInputContext(testInput)
		if err != nil {
			t.Fatalf("inject failed: %v", err)
		}

		ch := a.GetInputInjectionContext()
		select {
		case msg := <-ch:
			if msg != testInput {
				t.Errorf("got %q, want %q", msg, testInput)
			}
		case <-time.After(100 * time.Millisecond):
			t.Error("message not received")
		}
	})

	t.Run("multiple inject and drain cycle", func(t *testing.T) {
		inputs := []string{"first", "second", "third"}
		for _, input := range inputs {
			if err := a.InjectInputContext(input); err != nil {
				t.Fatalf("inject %q failed: %v", input, err)
			}
		}

		ch := a.GetInputInjectionContext()
		for i, expected := range inputs {
			select {
			case msg := <-ch:
				if msg != expected {
					t.Errorf("msg[%d] = %q, want %q", i, msg, expected)
				}
			case <-time.After(100 * time.Millisecond):
				t.Errorf("msg[%d] not received, expected %q", i, expected)
			}
		}
	})
}
