package commands

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func TestClearCommand_Name(t *testing.T) {
	cmd := &ClearCommand{}
	if got := cmd.Name(); got != "clear" {
		t.Errorf("ClearCommand.Name() = %q, want \"clear\"", got)
	}
}

func TestClearCommand_Description(t *testing.T) {
	cmd := &ClearCommand{}
	want := "Close the current session and start a new one"
	if got := cmd.Description(); got != want {
		t.Errorf("ClearCommand.Description() = %q, want %q", got, want)
	}
}

func TestClearCommand_Usage(t *testing.T) {
	cmd := &ClearCommand{}
	usage := cmd.Usage()
	if !strings.Contains(usage, "/clear") {
		t.Errorf("ClearCommand.Usage() = %q; expected to mention /clear", usage)
	}
	if !strings.Contains(usage, "previous session") {
		t.Errorf("ClearCommand.Usage() = %q; expected to mention prior session preservation", usage)
	}
}

func TestClearCommand_Execute(t *testing.T) {
	// Create a real agent (in test mode)
	chatAgent, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}

	cmd := &ClearCommand{}

	// Test nil agent case
	err = cmd.Execute(nil, nil)
	if err == nil {
		t.Error("ClearCommand.Execute() with nil agent should return error")
	}

	// Test clearing conversation
	err = cmd.Execute(nil, chatAgent)
	if err != nil {
		t.Errorf("ClearCommand.Execute() error = %v", err)
	}

	// Verify messages are cleared
	messages := chatAgent.GetMessages()
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages after clear, got %d", len(messages))
	}

	// After clear the agent must be assigned a fresh session ID so the next
	// auto-save does not overwrite the previous history.
	if sid := chatAgent.GetSessionID(); sid == "" {
		t.Errorf("Expected non-empty session ID after /clear, got empty string")
	}
}

// TestClearCommand_RotatesSessionID verifies that a known pre-set session ID is
// replaced with a fresh one after /clear, and that the new ID has the
// established session_<...> format.
func TestClearCommand_RotatesSessionID(t *testing.T) {
	chatAgent, err := agent.NewAgentWithModel("")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}

	priorID := "session_known_prior_for_test"
	chatAgent.SetSessionID(priorID)

	cmd := &ClearCommand{}
	if err := cmd.Execute(nil, chatAgent); err != nil {
		t.Fatalf("ClearCommand.Execute() error = %v", err)
	}

	newID := chatAgent.GetSessionID()
	if newID == "" {
		t.Fatal("session ID was empty after /clear")
	}
	if newID == priorID {
		t.Errorf("session ID did not rotate: still %q", priorID)
	}
	if !strings.HasPrefix(newID, "session_") {
		t.Errorf("new session ID %q does not start with %q", newID, "session_")
	}
}
