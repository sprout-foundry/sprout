//go:build !js

// Tests for SP-070-2 notification logic in cmd/agent_modes.go
package cmd

import (
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/cliui"
)

// TestNotifyTurnCompletion_NilSafety verifies that NotifyTurnCompletion
// does not panic when given a nil agent. In test environments, stdin is
// not a TTY so the function returns early before reaching any dereference.
// These tests confirm the early-return path is safe.
func TestNotifyTurnCompletion_NilSafety(t *testing.T) {
	// nil agent — should return early without panic
	cliui.NotifyTurnCompletion(nil, time.Now(), false)

	// nil agent + skipPrompt — double check the path
	cliui.NotifyTurnCompletion(nil, time.Now(), true)

	// Recent turn start (fast turn) — still returns early in non-TTY env
	cliui.NotifyTurnCompletion(nil, time.Now().Add(-time.Hour), false)
}
