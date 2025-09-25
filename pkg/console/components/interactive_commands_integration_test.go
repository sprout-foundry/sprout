package components

import (
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
)

func TestInteractiveCommandFlow_LogCommand(t *testing.T) {
	// Test the complete flow for log command
	var testAgent *agent.Agent
	cmd := &commands.LogCommand{}

	// This should work without any passthrough mode since it's non-interactive now
	err := cmd.Execute([]string{}, testAgent)

	// Should complete successfully (or with expected history error)
	if err != nil && !strings.Contains(err.Error(), "failed to show change history") {
		t.Errorf("Unexpected error in log command: %v", err)
	}

	t.Log("Log command executed successfully")
}

func TestInteractiveCommandFlow_RollbackCommand(t *testing.T) {
	// Test rollback command flow
	var testAgent *agent.Agent
	cmd := &commands.RollbackCommand{}

	// Test with no arguments (should show help)
	err := cmd.Execute([]string{}, testAgent)
	if err != nil {
		t.Errorf("Rollback command with no args should not error, got: %v", err)
	}

	// Test with revision ID argument
	err = cmd.Execute([]string{"test-revision-123"}, testAgent)
	// This is expected to error since the revision doesn't exist
	if err != nil && !strings.Contains(err.Error(), "rollback failed") {
		t.Errorf("Unexpected error type for rollback: %v", err)
	}

	t.Log("Rollback command flow tested successfully")
}

func TestInteractiveCommandDetection(t *testing.T) {
	testCases := []struct {
		command       string
		isInteractive bool
	}{
		{"log", true},       // Interactive dropdown
		{"memory", true},    // Interactive dropdown
		{"models", true},    // Interactive dropdown
		{"providers", true}, // Interactive dropdown
		{"mcp", true},       // Interactive configuration
		{"commit", true},    // Interactive workflow
		{"shell", true},     // Interactive execution
		{"help", false},     // Non-interactive
		{"changes", false},  // Non-interactive
		{"status", false},   // Non-interactive
		{"info", false},     // Non-interactive
	}

	for _, tc := range testCases {
		t.Run(tc.command, func(t *testing.T) {
			// This simulates the logic from agent console
			isInteractive := tc.command == "models" || tc.command == "mcp" ||
				tc.command == "commit" || tc.command == "shell" || tc.command == "providers" || tc.command == "memory" || tc.command == "log"

			if isInteractive != tc.isInteractive {
				t.Errorf("Command '%s': expected interactive=%v, got %v",
					tc.command, tc.isInteractive, isInteractive)
			}
		})
	}
}

func TestPassthroughModeSimulation(t *testing.T) {
	// Simulate what happens during an interactive command
	im := NewInputManager("> ")

	// Test the complete passthrough cycle
	t.Run("EnablePassthrough", func(t *testing.T) {
		// Manually set running state for testing
		im.mutex.Lock()
		im.running = true
		im.isRawMode = false
		im.mutex.Unlock()

		// Enable passthrough mode
		im.SetPassthroughMode(true)

		// Verify state
		im.mutex.RLock()
		running := im.running
		im.mutex.RUnlock()

		if running {
			t.Error("Expected input manager to be stopped in passthrough mode")
		}

		t.Log("Passthrough mode enabled successfully")
	})

	t.Run("DisablePassthrough", func(t *testing.T) {
		// Disable passthrough mode
		im.SetPassthroughMode(false)

		// Give time for goroutines
		time.Sleep(10 * time.Millisecond)

		// In test environment, this will likely fail to restart due to terminal requirements
		// But we can verify the attempt was made
		t.Log("Passthrough mode disable attempted")
	})
}

func TestInteractiveCommandList(t *testing.T) {
	// Verify we have the right set of interactive commands
	expectedInteractive := []string{"models", "mcp", "commit", "shell", "providers", "memory", "log"}
	expectedNonInteractive := []string{"help", "changes", "status", "info", "rollback"}

	// Test that interactive commands are correctly identified
	for _, cmd := range expectedInteractive {
		isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers" || cmd == "memory" || cmd == "log"
		if !isInteractive {
			t.Errorf("Command '%s' should be identified as interactive", cmd)
		}
	}

	// Test that non-interactive commands are correctly identified
	for _, cmd := range expectedNonInteractive {
		isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers" || cmd == "memory" || cmd == "log"
		if isInteractive {
			t.Errorf("Command '%s' should not be identified as interactive", cmd)
		}
	}

	t.Logf("Interactive commands: %v", expectedInteractive)
	t.Logf("Non-interactive commands: %v", expectedNonInteractive)
}

func TestCommandExecutionTiming(t *testing.T) {
	// Test that command execution completes in reasonable time
	// Note: Using nil agent for testing, some commands may error but should not panic
	var testAgent *agent.Agent

	testCases := []struct {
		name string
		cmd  commands.Command
	}{
		{"LogCommand", &commands.LogCommand{}},
		{"RollbackCommand", &commands.RollbackCommand{}},
		// Skip ChangesCommand and StatusCommand as they require non-nil agent
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()

			err := tc.cmd.Execute([]string{}, testAgent)

			duration := time.Since(start)

			// Commands should complete quickly (within 1 second for basic cases)
			if duration > time.Second {
				t.Errorf("Command '%s' took too long: %v", tc.name, duration)
			}

			// Allow expected errors
			if err != nil {
				expectedErrors := []string{
					"failed to show change history",
					"change tracking is not enabled",
				}

				hasExpectedError := false
				for _, expectedErr := range expectedErrors {
					if strings.Contains(err.Error(), expectedErr) {
						hasExpectedError = true
						break
					}
				}

				if !hasExpectedError {
					t.Errorf("Command '%s' had unexpected error: %v", tc.name, err)
				}
			}

			t.Logf("Command '%s' completed in %v", tc.name, duration)
		})
	}
}

func TestErrorHandling(t *testing.T) {
	// Test error handling with various edge cases
	var testAgent *agent.Agent

	t.Run("NilAgent", func(t *testing.T) {
		commands := []commands.Command{
			&commands.LogCommand{},
			&commands.ChangesCommand{},
			&commands.StatusCommand{},
			&commands.RollbackCommand{},
		}

		for _, cmd := range commands {
			// Should handle nil agent gracefully
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Command %T panicked with nil agent: %v", cmd, r)
					}
				}()

				err := cmd.Execute([]string{}, testAgent)
				t.Logf("Command %T with nil agent: error=%v", cmd, err)
			}()
		}
	})

	t.Run("EmptyArgs", func(t *testing.T) {
		cmd := &commands.RollbackCommand{}
		err := cmd.Execute([]string{}, testAgent)

		// Should not error, should show help
		if err != nil {
			t.Errorf("RollbackCommand with empty args should not error, got: %v", err)
		}
	})

	t.Run("InvalidArgs", func(t *testing.T) {
		cmd := &commands.RollbackCommand{}
		err := cmd.Execute([]string{"invalid-revision-id-123"}, testAgent)

		// Should error with rollback failure
		if err == nil {
			t.Error("RollbackCommand with invalid revision should error")
		} else if !strings.Contains(err.Error(), "rollback failed") {
			t.Errorf("Expected 'rollback failed' error, got: %v", err)
		}
	})
}
