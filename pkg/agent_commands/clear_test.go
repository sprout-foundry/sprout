package commands

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
)

func TestClearCommand_Name(t *testing.T) {
	cmd := &ClearCommand{}
	if got := cmd.Name(); got != "clear" {
		t.Errorf("ClearCommand.Name() = %q, want \"clear\"", got)
	}
}

func TestClearCommand_Description(t *testing.T) {
	cmd := &ClearCommand{}
	if got := cmd.Description(); got != "Clears conversation history" {
		t.Errorf("ClearCommand.Description() = %q, want \"Clears conversation history\"", got)
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
}
