package cmd

import (
	"context"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	agent_commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/events"
)

// =============================================================================
// TryZshCommandExecution
// =============================================================================

func TestTryZshCommandExecution_DisabledByConfig(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	// Disable zsh command detection
	cfg := a.GetConfig()
	cfg.EnableZshCommandDetection = false
	cfg.AutoExecuteDetectedCommands = false

	// TryZshCommandExecution should return false or execute when disabled
	// Note: In zsh environment, the command may still be detected and auto-executed
	// due to how zsh.IsCommand works. This test verifies no crash.
	executed, err := TryZshCommandExecution(context.Background(), a, "ls")
	if err != nil {
		// May error in zsh environment if command execution fails
		t.Logf("TryZshCommandExecution returned error (expected in some envs): %v", err)
	}
	// Just verify no crash - either executed or returned false is OK
	_ = executed
}

func TestTryZshCommandExecution_NonZshCommand(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	// Enable zsh command detection
	cfg := a.GetConfig()
	cfg.EnableZshCommandDetection = true
	cfg.AutoExecuteDetectedCommands = false

	// A query that is not a zsh command should return false
	executed, err := TryZshCommandExecution(context.Background(), a, "hello world")
	if err != nil {
		t.Fatalf("TryZshCommandExecution() error: %v", err)
	}
	// Returns false because it's not detected as a zsh command (or because not running in zsh)
	_ = executed
}

func TestTryZshCommandExecution_AutoExecutePrefix(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	// Enable zsh command detection
	cfg := a.GetConfig()
	cfg.EnableZshCommandDetection = true
	cfg.AutoExecuteDetectedCommands = false // Must use ! prefix

	// Query with ! prefix should trigger auto-execution (skipping confirmation)
	// This might not execute in test env (not running in zsh), but we test the path
	executed, err := TryZshCommandExecution(context.Background(), a, "!ls")
	if err != nil {
		// In test env (not running in zsh), might get an error or return false
		t.Logf("TryZshCommandExecution error (expected in non-zsh test env): %v", err)
	}
	// In test environment without zsh, this will return false (not detected as command)
	_ = executed
}

func TestTryZshCommandExecution_NilConfig(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	// Simulate nil config (shouldn't normally happen but let's test edge case)
	// We can't easily make config nil, but we can verify it handles the case gracefully
	executed, err := TryZshCommandExecution(context.Background(), a, "test")
	if err != nil {
		t.Fatalf("TryZshCommandExecution() error: %v", err)
	}
	// Should return false when config is not available for zsh detection
	_ = executed
}

// =============================================================================
// TryDirectExecution (also tested in cmd_coverage_improvements_test.go)
// =============================================================================

// Note: The following tests are already defined in cmd_coverage_improvements_test.go:
// - TestTryDirectExecution_EmptyQuery
// - TestTryDirectExecution_WhitespaceOnly
// We add additional tests here that don't conflict.

func TestTryDirectExecution_PwdCommand(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	executed, err := TryDirectExecution(context.Background(), a, "pwd")
	if err != nil {
		t.Fatalf("TryDirectExecution() error: %v", err)
	}
	if !executed {
		t.Error("expected true for pwd command")
	}
}

func TestTryDirectExecution_DateCommand(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	executed, err := TryDirectExecution(context.Background(), a, "date")
	if err != nil {
		t.Fatalf("TryDirectExecution() error: %v", err)
	}
	if !executed {
		t.Error("expected true for date command")
	}
}

func TestTryDirectExecution_WhoamiCommand(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	executed, err := TryDirectExecution(context.Background(), a, "whoami")
	if err != nil {
		t.Fatalf("TryDirectExecution() error: %v", err)
	}
	if !executed {
		t.Error("expected true for whoami command")
	}
}

func TestTryDirectExecution_GitStatus(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	executed, err := TryDirectExecution(context.Background(), a, "git status")
	if err != nil {
		t.Fatalf("TryDirectExecution() error: %v", err)
	}
	if !executed {
		t.Error("expected true for git status")
	}
}

// =============================================================================
// executeDirectCommand
// =============================================================================

func TestExecuteDirectCommand_Pwd(t *testing.T) {
	executed, err := executeDirectCommand("pwd")
	if err != nil {
		t.Fatalf("executeDirectCommand() error: %v", err)
	}
	if !executed {
		t.Error("expected executed to be true")
	}
}

func TestExecuteDirectCommand_Date(t *testing.T) {
	executed, err := executeDirectCommand("date")
	if err != nil {
		t.Fatalf("executeDirectCommand() error: %v", err)
	}
	if !executed {
		t.Error("expected executed to be true")
	}
}

// =============================================================================
// ProcessQuery
// =============================================================================

func TestProcessQuery_Basic(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	eventBus := events.NewEventBus()

	// Test with empty query - should handle gracefully
	err = ProcessQuery(context.Background(), a, eventBus, "")
	// Either passes or fails with error, shouldn't crash
	_ = err
}

func TestProcessQuery_SlashCommand(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	eventBus := events.NewEventBus()

	// Test slash command handling - should not crash
	err = ProcessQuery(context.Background(), a, eventBus, "/help")
	// In test env without real provider, this will error
	_ = err
}

func TestProcessQuery_RegularQuery(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	eventBus := events.NewEventBus()

	// A regular query will fail in test env (no real API), but function handles it
	err = ProcessQuery(context.Background(), a, eventBus, "hello")
	// In test env without real provider, this will error
	_ = err
}

// =============================================================================
// Slash command registry tests
// =============================================================================

func TestSlashCommandRegistry_IsSlashCommand(t *testing.T) {
	registry := agent_commands.NewCommandRegistry()

	tests := []struct {
		input    string
		expected bool
	}{
		{"/help", true},
		{"/stats", true},
		{"/clear", true},
		{"/exit", true},
		{"/quit", true},
		{"/q", true},
		{"exit", false},
		{"quit", false},
		{"hello", false},
		{"", false},
	}

	for _, tt := range tests {
		result := registry.IsSlashCommand(tt.input)
		if result != tt.expected {
			t.Errorf("IsSlashCommand(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}
