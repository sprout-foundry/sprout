package agent

import (
	"testing"
)

// =============================================================================
// SP-049-3a: Agent getter delegation for UnsafeShellMode
// =============================================================================

func TestAgent_GetUnsafeShellMode_Delegates(t *testing.T) {
	a := NewTestAgent()

	// Default false.
	if a.GetUnsafeShellMode() {
		t.Error("GetUnsafeShellMode should return false by default")
	}

	// Set via agent getter.
	a.SetUnsafeShellMode(true)
	if !a.GetUnsafeShellMode() {
		t.Error("GetUnsafeShellMode should return true after SetUnsafeShellMode")
	}

	// Reset.
	a.SetUnsafeShellMode(false)
	if a.GetUnsafeShellMode() {
		t.Error("GetUnsafeShellMode should return false after reset")
	}
}

func TestAgent_GetUnsafeMode_Delegates(t *testing.T) {
	a := NewTestAgent()

	// Default false.
	if a.GetUnsafeMode() {
		t.Error("GetUnsafeMode should return false by default")
	}

	// Set via agent getter.
	a.SetUnsafeMode(true)
	if !a.GetUnsafeMode() {
		t.Error("GetUnsafeMode should return true after SetUnsafeMode")
	}

	// Verify independence — setting unsafe should not set unsafe shell.
	if a.GetUnsafeShellMode() {
		t.Error("SetUnsafeMode should not affect GetUnsafeShellMode at the agent level")
	}
}

func TestAgent_UnsafeShellMode_IndependentOfUnsafeMode(t *testing.T) {
	a := NewTestAgent()

	a.SetUnsafeMode(true)
	a.SetUnsafeShellMode(true)

	// Both should be independently true.
	if !a.GetUnsafeMode() {
		t.Error("unsafe mode should be true")
	}
	if !a.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should be true")
	}

	// Reset just unsafe mode — unsafe shell should remain.
	a.SetUnsafeMode(false)
	if a.GetUnsafeMode() {
		t.Error("unsafe mode should be false after reset")
	}
	if !a.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should still be true after resetting unsafe mode")
	}

	// Reset just unsafe shell — unsafe mode should remain false.
	a.SetUnsafeShellMode(false)
	if a.GetUnsafeShellMode() {
		t.Error("unsafe shell mode should be false after reset")
	}
	if a.GetUnsafeMode() {
		t.Error("unsafe mode should still be false after resetting unsafe shell")
	}
}

func TestAgent_NilAgent_GetterGuard(t *testing.T) {
	var a *Agent

	// HasActiveWebUIClients has a nil guard in agent_getters.go.
	// Verify it doesn't panic.
	if a.HasActiveWebUIClients() {
		t.Error("nil agent HasActiveWebUIClients should return false, not panic")
	}
}
