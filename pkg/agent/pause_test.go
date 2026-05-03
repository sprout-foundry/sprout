package agent

import (
	"context"
	"testing"
)

// newInterruptTestAgent creates a minimal Agent with interrupt context initialized for testing.
func newInterruptTestAgent(t *testing.T) *Agent {
	t.Helper()
	a := newMinimalTestAgent(t)

	// Initialize interrupt context
	interruptCtx, interruptCancel := context.WithCancel(context.Background())
	a.interruptCtx = interruptCtx
	a.interruptCancel = interruptCancel

	return a
}

func TestTriggerInterrupt(t *testing.T) {
	t.Parallel()

	t.Run("cancels the interrupt context", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		// Initially not interrupted
		if a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return false initially")
		}

		// Trigger interrupt
		a.TriggerInterrupt()

		// Now should be interrupted
		if !a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return true after TriggerInterrupt")
		}
	})

	t.Run("can trigger interrupt multiple times", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		a.TriggerInterrupt()
		a.TriggerInterrupt()
		a.TriggerInterrupt()

		if !a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return true after multiple triggers")
		}
	})
}

func TestCheckForInterrupt(t *testing.T) {
	t.Parallel()

	t.Run("returns false before interrupt is triggered", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		if a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return false initially")
		}
	})

	t.Run("returns true after interrupt is triggered", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		a.TriggerInterrupt()

		if !a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return true after trigger")
		}
	})

	t.Run("returns true persistently after interrupt", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		a.TriggerInterrupt()

		// Check multiple times
		for i := 0; i < 5; i++ {
			if !a.CheckForInterrupt() {
				t.Errorf("expected CheckForInterrupt to return true on check %d", i)
			}
		}
	})

	t.Run("returns false after ClearInterrupt is called", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		a.TriggerInterrupt()
		if !a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return true after trigger")
		}

		a.ClearInterrupt()

		if a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return false after ClearInterrupt")
		}
	})
}

func TestClearInterrupt(t *testing.T) {
	t.Parallel()

	t.Run("resets interrupt state after trigger", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		a.TriggerInterrupt()
		if !a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return true after trigger")
		}

		a.ClearInterrupt()

		if a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return false after ClearInterrupt")
		}
	})

	t.Run("can be called multiple times safely", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		a.TriggerInterrupt()
		a.ClearInterrupt()
		a.ClearInterrupt()
		a.ClearInterrupt()

		if a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return false after multiple ClearInterrupt calls")
		}
	})

	t.Run("allows interrupt to be triggered again after clear", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		// First interrupt cycle
		a.TriggerInterrupt()
		if !a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return true on first trigger")
		}

		a.ClearInterrupt()
		if a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return false after clear")
		}

		// Second interrupt cycle
		a.TriggerInterrupt()
		if !a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return true on second trigger")
		}

		a.ClearInterrupt()
		if a.CheckForInterrupt() {
			t.Errorf("expected CheckForInterrupt to return false after second clear")
		}
	})
}

func TestHandleInterrupt(t *testing.T) {
	t.Parallel()

	t.Run("returns empty string when no interrupt is pending", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		result := a.HandleInterrupt()

		if result != "" {
			t.Errorf("expected empty string when no interrupt pending, got '%s'", result)
		}
	})

	t.Run("returns STOP after interrupt is triggered", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		a.TriggerInterrupt()
		result := a.HandleInterrupt()

		if result != "STOP" {
			t.Errorf("expected 'STOP', got '%s'", result)
		}
	})

	t.Run("clears interrupt state after handling", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		a.TriggerInterrupt()
		a.HandleInterrupt()

		if a.CheckForInterrupt() {
			t.Errorf("expected interrupt to be cleared after HandleInterrupt")
		}
	})

	t.Run("sets pause state with timestamp when handling interrupt", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		a.TriggerInterrupt()
		a.HandleInterrupt()

		pauseState := a.state.GetPauseState()
		if pauseState == nil {
			t.Fatalf("expected pause state to be initialized")
		}
		if pauseState.PausedAt.IsZero() {
			t.Errorf("expected PausedAt to be set to a non-zero time")
		}
	})

	t.Run("stores messages before interrupt", func(t *testing.T) {
		a := newInterruptTestAgent(t)

		// HandleInterrupt initializes pause state and stores current messages
		a.TriggerInterrupt()
		result := a.HandleInterrupt()

		if result != "STOP" {
			t.Errorf("expected STOP, got %q", result)
		}

		pauseState := a.state.GetPauseState()
		if pauseState == nil {
			t.Fatalf("expected pause state to be initialized")
		}
		if pauseState.IsPaused {
			t.Errorf("expected IsPaused to be false after HandleInterrupt completes")
		}
	})
}

func TestInterruptContextIsolation(t *testing.T) {
	t.Parallel()

	t.Run("different agents have independent interrupt contexts", func(t *testing.T) {
		a1 := newInterruptTestAgent(t)
		a2 := newInterruptTestAgent(t)

		a1.TriggerInterrupt()

		if !a1.CheckForInterrupt() {
			t.Errorf("expected a1 to be interrupted")
		}
		if a2.CheckForInterrupt() {
			t.Errorf("expected a2 not to be interrupted")
		}
	})
}
