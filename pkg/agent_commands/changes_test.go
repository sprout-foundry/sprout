package commands

import (
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
)

func TestLogCommand_Name(t *testing.T) {
	cmd := &LogCommand{}
	if cmd.Name() != "log" {
		t.Errorf("Expected command name 'log', got '%s'", cmd.Name())
	}
}

func TestLogCommand_Description(t *testing.T) {
	cmd := &LogCommand{}
	desc := cmd.Description()
	if desc == "" {
		t.Error("Expected non-empty description")
	}
	if !strings.Contains(strings.ToLower(desc), "history") {
		t.Errorf("Expected description to contain 'history', got '%s'", desc)
	}
}

func TestLogCommand_ExecuteWithNoHistory(t *testing.T) {
	// Create a test agent (can be nil for this test since we're just testing the command structure)
	var testAgent *agent.Agent

	cmd := &LogCommand{}

	// Test execution - should not panic even with no history
	err := cmd.Execute([]string{}, testAgent)

	// The actual behavior depends on whether there's history data available
	// We're mainly testing that the command doesn't crash
	if err != nil {
		// Check if it's a reasonable error (like "failed to show change history")
		if !strings.Contains(err.Error(), "failed to show change history") {
			t.Errorf("Unexpected error: %v", err)
		}
	}
}

func TestLogCommand_ExecuteWithArgs(t *testing.T) {
	var testAgent *agent.Agent
	cmd := &LogCommand{}

	// Test with various argument combinations
	testCases := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"single arg", []string{"test"}},
		{"multiple args", []string{"arg1", "arg2"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := cmd.Execute(tc.args, testAgent)
			// Should handle all argument cases gracefully
			if err != nil && !strings.Contains(err.Error(), "failed to show change history") {
				t.Errorf("Unexpected error for case '%s': %v", tc.name, err)
			}
		})
	}
}

func TestChangesCommand_Name(t *testing.T) {
	cmd := &ChangesCommand{}
	if cmd.Name() != "changes" {
		t.Errorf("Expected command name 'changes', got '%s'", cmd.Name())
	}
}

func TestStatusCommand_Name(t *testing.T) {
	cmd := &StatusCommand{}
	if cmd.Name() != "status" {
		t.Errorf("Expected command name 'status', got '%s'", cmd.Name())
	}
}

func TestRollbackCommand_Name(t *testing.T) {
	cmd := &RollbackCommand{}
	if cmd.Name() != "rollback" {
		t.Errorf("Expected command name 'rollback', got '%s'", cmd.Name())
	}
}

func TestRollbackCommand_ExecuteWithNoArgs(t *testing.T) {
	var testAgent *agent.Agent
	cmd := &RollbackCommand{}

	// Should handle no arguments gracefully and show help
	err := cmd.Execute([]string{}, testAgent)

	// This should not error, it should just show help
	if err != nil {
		t.Errorf("Expected no error when called with no args, got: %v", err)
	}
}

func TestRollbackCommand_ExecuteWithRevisionID(t *testing.T) {
	var testAgent *agent.Agent
	cmd := &RollbackCommand{}

	// Test with a revision ID argument
	err := cmd.Execute([]string{"test-revision-123"}, testAgent)

	// This might error if the revision doesn't exist, which is expected
	if err != nil && !strings.Contains(err.Error(), "rollback failed") {
		t.Errorf("Unexpected error type: %v", err)
	}
}

// TestCommandsImplementCommandInterface ensures all commands implement the Command interface correctly
func TestCommandsImplementCommandInterface(t *testing.T) {
	commands := []Command{
		&ChangesCommand{},
		&StatusCommand{},
		&LogCommand{},
		&RollbackCommand{},
	}

	for _, cmd := range commands {
		// Test that Name() returns a non-empty string
		name := cmd.Name()
		if name == "" {
			t.Errorf("Command %T returned empty name", cmd)
		}

		// Test that Description() returns a non-empty string
		desc := cmd.Description()
		if desc == "" {
			t.Errorf("Command %T returned empty description", cmd)
		}

		// Test that Execute doesn't panic with nil agent
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Command %T panicked with nil agent: %v", cmd, r)
				}
			}()
			cmd.Execute([]string{}, nil)
		}()
	}
}
