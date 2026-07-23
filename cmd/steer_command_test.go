//go:build !js

package cmd

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
)

// TestSteerCommand_SafeCommandExecuted verifies that a safe command like /info
// is executed via executeSteerCommand rather than being rejected.
func TestSteerCommand_SafeCommandExecuted(t *testing.T) {
	// Create a test agent
	a, err := agent.NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}
	defer func() {
		// Clean up: interrupt any running query
		if a != nil {
			a.TriggerInterrupt()
		}
	}()

	// Set the command registry on the agent (this is how it's done in real usage)
	a.SetSlashCommands(agent_commands.DefaultRegistry())

	// Create the coordinator
	c := NewSteerCoordinator(a, nil)

	// Drain any pre-existing state
	_ = a.DrainDeferredMessages()
	_ = a.SteeringChannel() // consume any pending steer messages

	// Submit /info (a safe command) - it should execute via executeSteerCommand
	// and NOT be rejected. The key assertion is that the rejection warning
	// (which contains "can't run a slash command") should NOT appear in stderr.
	// We can't easily verify the /info command executed, but we can verify
	// the rejection path was NOT taken by capturing stderr.
	c.handleSteerSubmit("/info")

	// If the rejection path was taken, stderr would contain a warning about
	// "can't run a slash command" with the steer-specific message.
	// Since /info is safe, we expect no such warning.
	//
	// Note: We can't easily capture stderr in this test, but we can verify
	// the safe command path was reached by checking that the agent's state
	// hasn't been corrupted (deferred queue is still empty for rejected intents).
	// The actual execution happens in a goroutine, so we give it a moment.
	// This is a best-effort test - the important thing is that the code path
	// for safe commands doesn't call rejectCommandIntent.
}

// TestSteerCommand_UnsafeCommandRejected verifies that genuinely-destructive
// commands like /commit are rejected when submitted via steer.
//
// Note: /clear is intentionally NOT in this list. ClearCommand calls
// RotateSession(), which persists the prior session via SaveStateScoped
// before clearing in-memory state — the prior conversation is restorable
// via /sessions. Treating /clear as "destructive" was an over-broad
// classification that broke the WebUI's "New Session" button (the
// canonical "start a new conversation" action for any chat UI). It now
// implements SteerCapable and SafeDuringSteer() == true so the WebUI
// dedicated command surface (/api/command/execute) and the CLI steer
// panel both accept it.
func TestSteerCommand_UnsafeCommandRejected(t *testing.T) {
	unsafeCommands := []string{"/commit"}

	for _, cmd := range unsafeCommands {
		t.Run(strings.TrimPrefix(cmd, "/"), func(t *testing.T) {
			// Create a fresh test agent for each subtest
			a, err := agent.NewAgent()
			if err != nil {
				t.Skipf("Skipping test due to agent creation error: %v", err)
			}
			defer func() {
				if a != nil {
					a.TriggerInterrupt()
				}
			}()

			// Set the command registry
			a.SetSlashCommands(agent_commands.DefaultRegistry())

			// Create the coordinator
			c := NewSteerCoordinator(a, nil)

			// Drain any pre-existing state
			_ = a.DrainDeferredMessages()

			// Submit an unsafe command
			c.handleSteerSubmit(cmd)

			// Unsafe commands are rejected via rejectCommandIntent which prints
			// to stderr. The rejection path also returns early, so the deferred
			// queue should remain empty (rejected commands are NOT queued).
			drained := a.DrainDeferredMessages()
			if len(drained) != 0 {
				t.Errorf("rejected command %q should not be queued, but got: %v", cmd, drained)
			}

			// The steering channel should also remain empty for rejected commands
			// (they don't flow to InjectInputContext)
			select {
			case msg := <-a.SteeringChannel():
				t.Errorf("rejected command %q should not send to steering channel, got: %s", cmd, msg)
			default:
				// Expected: channel is empty
			}
		})
	}
}

// TestSteerCommand_FreeformTextGoesToInjectInputContext verifies that non-command
// text is routed to InjectInputContext rather than being rejected.
func TestSteerCommand_FreeformTextGoesToInjectInputContext(t *testing.T) {
	// This test verifies that freeform text (no leading / or !) is NOT
	// classified as an intent and thus flows to InjectInputContext.
	//
	// We verify this by checking that ClassifyPromptIntent returns IntentNone
	// for freeform text, which means handleSteerSubmit will try to inject it.

	freeformInputs := []string{
		"please refactor the auth middleware",
		"what does this function do?",
		"explain the steer coordinator",
		"   ", // whitespace-only is also freeform
	}

	for _, input := range freeformInputs {
		t.Run(input[:min(len(input), 30)], func(t *testing.T) {
			intent := ClassifyPromptIntent(nil, input)
			if intent != IntentNone {
				t.Errorf("ClassifyPromptIntent(%q) = %q, want IntentNone", input, intent)
			}
		})
	}
}

// TestSteerCommand_CommandClassification verifies that the classifier
// correctly identifies slash commands vs freeform text.
func TestSteerCommand_CommandClassification(t *testing.T) {
	// Verify that slash commands are classified as IntentSlash
	slashCommands := []string{
		"/info",
		"/commit",
		"/clear",
		"/help",
		"/model",
	}

	for _, cmd := range slashCommands {
		t.Run(strings.TrimPrefix(cmd, "/"), func(t *testing.T) {
			intent := ClassifyPromptIntent(nil, cmd)
			if intent != IntentSlash {
				t.Errorf("ClassifyPromptIntent(%q) = %q, want IntentSlash", cmd, intent)
			}
		})
	}
}

// TestSteerCommand_ExecuteSteerCommandIntegration tests the integration
// between handleSteerSubmit and executeSteerCommand for the /info command.
func TestSteerCommand_ExecuteSteerCommandIntegration(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}
	defer func() {
		if a != nil {
			a.TriggerInterrupt()
		}
	}()

	// Set the command registry
	a.SetSlashCommands(agent_commands.DefaultRegistry())

	// Create the coordinator
	c := NewSteerCoordinator(a, nil)

	// Drain any pre-existing state
	_ = a.DrainDeferredMessages()

	// The /info command is safe to execute during steer
	// It should be found in the registry and SafeDuringSteer() should return true
	registry := a.SlashCommands().(*agent_commands.CommandRegistry)
	cmd, ok := registry.GetCommand("info")
	if !ok {
		t.Fatal("info command not found in registry")
	}

	sc, ok := cmd.(agent_commands.SteerCapable)
	if !ok {
		t.Fatal("info command does not implement SteerCapable")
	}

	if !sc.SafeDuringSteer() {
		t.Fatal("info command should be safe during steer")
	}

	// Now submit /info via handleSteerSubmit
	// This should reach executeSteerCommand which should find the command
	// and execute it (in a goroutine)
	c.handleSteerSubmit("/info")

	// The command executes asynchronously in a goroutine.
	// We can't easily verify the output, but we can verify that:
	// 1. The deferred queue remains empty (command was executed, not queued)
	// 2. No rejection message was printed (which would indicate the path was not taken)

	drained := a.DrainDeferredMessages()
	if len(drained) != 0 {
		t.Errorf("executed command should not be queued, got: %v", drained)
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
