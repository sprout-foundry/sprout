//go:build !js

// Tests for SP-070-2 notification logic in cmd/agent_modes.go
package cmd

import (
	"testing"
	"time"
)

// TestNotifyTurnCompletion_NilSafety verifies that notifyTurnCompletion
// does not panic when given a nil agent. In test environments, stdin is
// not a TTY so the function returns early before reaching any dereference.
// These tests confirm the early-return path is safe.
func TestNotifyTurnCompletion_NilSafety(t *testing.T) {
	// nil agent — should return early without panic
	notifyTurnCompletion(nil, time.Now(), false)

	// nil agent + skipPrompt — double check the path
	notifyTurnCompletion(nil, time.Now(), true)

	// Recent turn start (fast turn) — still returns early in non-TTY env
	notifyTurnCompletion(nil, time.Now().Add(-time.Hour), false)
}
