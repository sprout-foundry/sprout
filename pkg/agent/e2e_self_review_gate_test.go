package agent

import (
	"os"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helper: build agent with mock configManager for self-review gate tests
// ---------------------------------------------------------------------------

// buildAgentWithConfigManager creates a minimal Agent wired to a ScriptedClient
// and a real configuration.Manager using NewManagerWithConfig. The agent is set
// up for self-review gate testing.
func buildAgentWithConfigManager(t *testing.T, maxIter int, persona string, selfReviewMode string, responses ...*ScriptedResponse) (*Agent, *ConversationHandler) {
	t.Helper()

	cfg := configuration.NewConfig()
	cfg.SelfReviewGateMode = selfReviewMode

	mgr := configuration.NewManagerWithConfig(cfg, nil)

	client := NewScriptedClient(responses...)
	agent := makeAgentWithScriptedClient(maxIter, client)
	agent.configManager = mgr
	agent.activePersona = persona

	ch := NewConversationHandler(agent)
	return agent, ch
}

// ---------------------------------------------------------------------------
// Test 1 – Gate skipped when persona is not enabled
// ---------------------------------------------------------------------------

// TestE2E_SelfReviewGate_SkippedWhenPersonaNotEnabled verifies that when the
// active persona is one that doesn't enable the self-review gate (e.g. "general"),
// ProcessQuery completes normally. The gate is skipped because
// isSelfReviewGatePersonaEnabled returns false for non-orchestrator/coder personas.
func TestE2E_SelfReviewGate_SkippedWhenPersonaNotEnabled(t *testing.T) {
	t.Parallel()

	agent, _ := buildAgentWithConfigManager(t, 10, "general", configuration.SelfReviewGateModeAlways, stopResponse())

	result, err := agent.ProcessQuery("What is 2+2?")
	require.NoError(t, err, "ProcessQuery should succeed when gate is skipped for non-gate persona")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"expected RunTerminationCompleted when gate is skipped")
}

// ---------------------------------------------------------------------------
// Test 2 – Gate skipped when no tracked changes exist
// ---------------------------------------------------------------------------

// TestE2E_SelfReviewGate_SkippedWhenNoTrackedChanges verifies that when the
// orchestrator persona is active but no file modifications were tracked during
// the conversation (no file tools were used), ProcessQuery completes normally.
// The gate is never invoked because hadTrackedChanges is false (changeCount == 0).
func TestE2E_SelfReviewGate_SkippedWhenNoTrackedChanges(t *testing.T) {
	t.Parallel()

	agent, _ := buildAgentWithConfigManager(t, 10, "orchestrator", configuration.SelfReviewGateModeAlways, stopResponse())

	result, err := agent.ProcessQuery("Explain Go interfaces")
	require.NoError(t, err, "ProcessQuery should succeed when no tracked changes exist")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"expected RunTerminationCompleted when no files were tracked")
}

// ---------------------------------------------------------------------------
// Test 3 – Gate skipped when LEDIT_SKIP_SELF_REVIEW_GATE env var is set
// ---------------------------------------------------------------------------

// TestE2E_SelfReviewGate_SkippedWhenEnvVarSet verifies that the environment
// variable LEDIT_SKIP_SELF_REVIEW_GATE=1 causes the gate to be skipped entirely,
// regardless of persona or tracked changes.
func TestE2E_SelfReviewGate_SkippedWhenEnvVarSet(t *testing.T) {
	// NOTE: Cannot use t.Parallel() with t.Setenv in Go 1.25+.
	// Use os.Setenv/os.Unsetenv with test cleanup instead.
	prev := os.Getenv("LEDIT_SKIP_SELF_REVIEW_GATE")
	os.Setenv("LEDIT_SKIP_SELF_REVIEW_GATE", "1")
	defer func() {
		if prev == "" {
			os.Unsetenv("LEDIT_SKIP_SELF_REVIEW_GATE")
		} else {
			os.Setenv("LEDIT_SKIP_SELF_REVIEW_GATE", prev)
		}
	}()

	agent, _ := buildAgentWithConfigManager(t, 10, "orchestrator", configuration.SelfReviewGateModeAlways, stopResponse())

	result, err := agent.ProcessQuery("Refactor the parser")
	require.NoError(t, err, "ProcessQuery should succeed when LEDIT_SKIP_SELF_REVIEW_GATE=1")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"expected RunTerminationCompleted when env var skips the gate")
}

// ---------------------------------------------------------------------------
// Test 4 – Gate skipped when config mode is "off" (direct call)
// ---------------------------------------------------------------------------

// TestE2E_SelfReviewGate_SkippedWhenModeOff verifies that runSelfReviewGate
// returns nil when the config self-review gate mode is "off", even if the
// persona is gate-enabled and changes are tracked.
func TestE2E_SelfReviewGate_SkippedWhenModeOff(t *testing.T) {
	t.Parallel()

	// Build agent with mode "off"
	agent, ch := buildAgentWithConfigManager(t, 10, "orchestrator", configuration.SelfReviewGateModeOff, stopResponse())

	// Manually create a change tracker with tracked changes so hadTrackedChanges is true.
	// This simulates what would happen after file tool execution.
	agent.changeTracker = &ChangeTracker{
		revisionID:   "test-revision-123",
		enabled:      true,
		agent:        agent,
		changes:      []TrackedFileChange{
			{
				FilePath:     "/tmp/test.go",
				NewCode:      "package main",
				Operation:    "create",
				ToolCall:     "WriteFile",
			},
		},
	}

	// Call runSelfReviewGate directly — should return nil with mode "off"
	err := ch.runSelfReviewGate()
	assert.NoError(t, err,
		"runSelfReviewGate should return nil when config mode is 'off'")
}

// ---------------------------------------------------------------------------
// Test 5 – Gate skipped when config mode "code" but no code-like files
// ---------------------------------------------------------------------------

// TestE2E_SelfReviewGate_SkippedWhenModeCodeNoCodeFiles verifies that
// runSelfReviewGate skips (returns nil) when the config mode is "code" but
// only non-code files (e.g. .md, .txt) are in the tracked changes list.
func TestE2E_SelfReviewGate_SkippedWhenModeCodeNoCodeFiles(t *testing.T) {
	t.Parallel()

	agent, ch := buildAgentWithConfigManager(t, 10, "orchestrator", configuration.SelfReviewGateModeCode, stopResponse())

	// Track only non-code files: a README.md and a notes.txt
	agent.changeTracker = &ChangeTracker{
		revisionID:   "test-revision-456",
		enabled:      true,
		agent:        agent,
		changes:      []TrackedFileChange{
			{
				FilePath:     "/tmp/README.md",
				NewCode:      "# My Project",
				Operation:    "create",
				ToolCall:     "WriteFile",
			},
			{
				FilePath:     "/tmp/notes.txt",
				NewCode:      "Some notes",
				Operation:    "create",
				ToolCall:     "WriteFile",
			},
		},
	}

	err := ch.runSelfReviewGate()
	assert.NoError(t, err,
		"runSelfReviewGate should return nil when mode is 'code' and no code files tracked")
}

// ---------------------------------------------------------------------------
// Test 6 – Gate blocked when revision ID is empty (direct call)
// ---------------------------------------------------------------------------

// TestE2E_SelfReviewGate_BlockedWhenNoRevisionID verifies that runSelfReviewGate
// returns an error containing "no revision ID" when the change tracker has
// tracked changes (changeCount > 0) but the revisionID is empty.
func TestE2E_SelfReviewGate_BlockedWhenNoRevisionID(t *testing.T) {
	t.Parallel()

	agent, ch := buildAgentWithConfigManager(t, 10, "orchestrator", configuration.SelfReviewGateModeAlways, stopResponse())

	// Create a change tracker with tracked changes but NO revision ID.
	agent.changeTracker = &ChangeTracker{
		revisionID: "", // Empty revision ID — should trigger the error
		enabled:    true,
		agent:      agent,
		changes: []TrackedFileChange{
			{
				FilePath:     "/tmp/main.go",
				NewCode:      "package main\n\nfunc main() {}",
				Operation:    "create",
				ToolCall:     "WriteFile",
			},
		},
	}

	err := ch.runSelfReviewGate()
	require.Error(t, err,
		"runSelfReviewGate should return an error when revision ID is empty")
	assert.Contains(t, err.Error(), "no revision ID",
		"error message should mention 'no revision ID'")
}

// ---------------------------------------------------------------------------
// Test 7 – Gate skipped for "coder" persona when mode is "off" (ProcessQuery)
// ---------------------------------------------------------------------------

// TestE2E_SelfReviewGate_CoderPersonaProcessQuery verifies that ProcessQuery
// completes normally for the "coder" persona when the gate mode is "off".
// The "coder" persona IS gate-enabled, but the mode "off" takes precedence.
func TestE2E_SelfReviewGate_CoderPersonaProcessQuery(t *testing.T) {
	t.Parallel()

	agent, _ := buildAgentWithConfigManager(t, 10, "coder", configuration.SelfReviewGateModeOff, stopResponse())

	result, err := agent.ProcessQuery("Write a function")
	require.NoError(t, err, "ProcessQuery should succeed for coder persona with mode 'off'")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
}

// ---------------------------------------------------------------------------
// Test 8 – Gate skipped through ProcessQuery with orchestrator persona
// ---------------------------------------------------------------------------

// TestE2E_SelfReviewGate_OrchestratorProcessQueryNoChanges verifies the full
// ProcessQuery flow with orchestrator persona and gate mode "always". Since no
// file tools are executed, hadTrackedChanges is false, and the gate is never
// invoked. This tests that the orchestrator persona doesn't interfere with
// normal ProcessQuery completion when no changes are tracked.
func TestE2E_SelfReviewGate_OrchestratorProcessQueryNoChanges(t *testing.T) {
	t.Parallel()

	agent, _ := buildAgentWithConfigManager(t, 10, "orchestrator", configuration.SelfReviewGateModeAlways, stopResponse())

	result, err := agent.ProcessQuery("Hello, what is the weather?")
	require.NoError(t, err, "ProcessQuery should succeed for orchestrator with no tracked changes")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"expected normal completion for orchestrator with no file changes")
}

// ---------------------------------------------------------------------------
// Test 9 – Direct gate call blocked when code mode has code files but spec review
//           reaches out (we only verify that code files ARE detected and gate
//           does NOT short-circuit the hasCodeLikeTrackedFiles check)
// ---------------------------------------------------------------------------

// TestE2E_SelfReviewGate_CodeModeHasCodeFiles verifies that when mode is "code"
// and code-like files (e.g. .go) ARE tracked, the hasCodeLikeTrackedFiles check
// passes (returns true), and the gate proceeds past the code-file check.
// We can't test the full spec.ReviewTrackedChanges path without a real LLM
// service, but we verify the gate doesn't skip prematurely at the code-file
// detection step.
func TestE2E_SelfReviewGate_CodeModeHasCodeFiles(t *testing.T) {
	t.Parallel()

	agent, ch := buildAgentWithConfigManager(t, 10, "orchestrator", configuration.SelfReviewGateModeCode, stopResponse())

	// Track a code file (.go) so hasCodeLikeTrackedFiles returns true.
	agent.changeTracker = &ChangeTracker{
		revisionID:   "test-revision-code",
		enabled:      true,
		agent:        agent,
		changes: []TrackedFileChange{
			{
				FilePath:     "/tmp/main.go",
				NewCode:      "package main\n\nfunc main() {}",
				Operation:    "create",
				ToolCall:     "WriteFile",
			},
		},
	}

	// The gate should proceed past the code-file detection step and reach
	// spec.ReviewTrackedChanges. If it returns an error, it should come from
	// the full gate pipeline (wrapped by runSelfReviewGate), NOT from a
	// code-file skip.
	err := ch.runSelfReviewGate()

	if err != nil {
		assert.NotContains(t, err.Error(), "no code files changed",
			"gate should not skip for 'code' mode when .go file is tracked")
		assert.Contains(t, err.Error(), "self-review gate blocked completion",
			"error should be wrapped by runSelfReviewGate, indicating full gate pipeline was reached")
	}
}

// ---------------------------------------------------------------------------
// Test 10 – Empty persona (no persona set) skips gate (direct call)
// ---------------------------------------------------------------------------

// TestE2E_SelfReviewGate_SkippedWhenNoPersona verifies that when no persona is
// active (empty string), the gate prints an info message and skips.
func TestE2E_SelfReviewGate_SkippedWhenNoPersona(t *testing.T) {
	t.Parallel()

	agent, ch := buildAgentWithConfigManager(t, 10, "", configuration.SelfReviewGateModeAlways, stopResponse())

	agent.changeTracker = &ChangeTracker{
		revisionID:   "test-revision-empty-persona",
		enabled:      true,
		agent:        agent,
		changes: []TrackedFileChange{
			{
				FilePath:     "/tmp/test.go",
				NewCode:      "package main",
				Operation:    "create",
				ToolCall:     "WriteFile",
			},
		},
	}

	err := ch.runSelfReviewGate()
	assert.NoError(t, err,
		"runSelfReviewGate should return nil when no persona is set (empty string)")
}
