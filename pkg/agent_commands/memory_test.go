package commands

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
)

// Helper function to create a test agent with proper environment setup
func createTestAgent(t *testing.T) *agent.Agent {
	// Set test environment to avoid API calls
	originalKey := os.Getenv("OPENROUTER_API_KEY")
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Cleanup(func() {
		if originalKey != "" {
			os.Setenv("OPENROUTER_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENROUTER_API_KEY")
		}
	})

	testAgent, err := agent.NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to agent creation error: %v", err)
	}
	return testAgent
}

func TestMemoryCommand_Name(t *testing.T) {
	cmd := &MemoryCommand{}
	if cmd.Name() != "memory" {
		t.Errorf("Expected name 'memory', got '%s'", cmd.Name())
	}
}

func TestMemoryCommand_Description(t *testing.T) {
	cmd := &MemoryCommand{}
	desc := cmd.Description()
	if !strings.Contains(desc, "conversation memory") {
		t.Errorf("Expected description to contain 'conversation memory', got '%s'", desc)
	}
}

func TestMemoryCommand_Execute_NoArgs(t *testing.T) {
	cmd := &MemoryCommand{}
	err := cmd.Execute([]string{}, nil)
	if err == nil {
		t.Error("Expected error when no arguments provided")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("Expected usage error, got: %v", err)
	}
}

func TestMemoryCommand_Execute_UnknownSubcommand(t *testing.T) {
	cmd := &MemoryCommand{}
	err := cmd.Execute([]string{"invalid"}, nil)
	if err == nil {
		t.Error("Expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown subcommand") {
		t.Errorf("Expected unknown subcommand error, got: %v", err)
	}
}

func TestMemoryCommand_ShowSummary_EmptySummary(t *testing.T) {
	testAgent := createTestAgent(t)

	cmd := &MemoryCommand{}
	err := cmd.showSummary(testAgent)
	if err != nil {
		t.Errorf("Expected no error for empty summary, got: %v", err)
	}
}

func TestMemoryCommand_ShowSummary_WithSummary(t *testing.T) {
	testAgent := createTestAgent(t)

	testSummary := "Test conversation summary"
	testAgent.SetPreviousSummary(testSummary)

	cmd := &MemoryCommand{}
	err := cmd.showSummary(testAgent)
	if err != nil {
		t.Errorf("Expected no error for summary display, got: %v", err)
	}

	// Verify the summary was set correctly
	if testAgent.GetPreviousSummary() != testSummary {
		t.Errorf("Expected summary '%s', got '%s'", testSummary, testAgent.GetPreviousSummary())
	}
}

func TestMemoryCommand_ClearMemory(t *testing.T) {
	testAgent := createTestAgent(t)

	// Set some conversation history
	testAgent.SetPreviousSummary("Some previous summary")

	cmd := &MemoryCommand{}
	err := cmd.clearMemory(testAgent)
	if err != nil {
		t.Errorf("Expected no error for clear memory, got: %v", err)
	}

	// Verify memory was cleared
	if testAgent.GetPreviousSummary() != "" {
		t.Errorf("Expected empty summary after clear, got: %s", testAgent.GetPreviousSummary())
	}
}

func TestMemoryCommand_LoadSession_MissingArgs(t *testing.T) {
	cmd := &MemoryCommand{}
	err := cmd.Execute([]string{"load"}, nil)
	if err == nil {
		t.Error("Expected error when session_id not provided")
	}
	if !strings.Contains(err.Error(), "usage: /memory load") {
		t.Errorf("Expected load usage error, got: %v", err)
	}
}

func TestMemoryCommand_DeleteSession_MissingArgs(t *testing.T) {
	cmd := &MemoryCommand{}
	err := cmd.Execute([]string{"delete"}, nil)
	if err == nil {
		t.Error("Expected error when session_id not provided")
	}
	if !strings.Contains(err.Error(), "usage: /memory delete") {
		t.Errorf("Expected delete usage error, got: %v", err)
	}
}

func TestMemoryCommand_LoadSession_NonExistentSession(t *testing.T) {
	testAgent := createTestAgent(t)

	cmd := &MemoryCommand{}
	err := cmd.loadSession(testAgent, "nonexistent-session")
	if err == nil {
		t.Error("Expected error for non-existent session")
	}
	if !strings.Contains(err.Error(), "failed to load session") {
		t.Errorf("Expected load session error, got: %v", err)
	}
}

func TestMemoryCommand_DeleteSession_NonExistentSession(t *testing.T) {
	cmd := &MemoryCommand{}
	err := cmd.deleteSession("nonexistent-session")
	if err == nil {
		t.Error("Expected error for non-existent session deletion")
	}
	if !strings.Contains(err.Error(), "failed to delete session") {
		t.Errorf("Expected delete session error, got: %v", err)
	}
}

func TestMemoryCommand_ListSessions_NoSessions(t *testing.T) {
	// This test will work with the current state where no sessions exist
	cmd := &MemoryCommand{}
	err := cmd.listSessions()
	if err != nil {
		t.Errorf("Expected no error for listing sessions, got: %v", err)
	}
}

func TestMemoryCommand_SaveLoadDeleteSession_Integration(t *testing.T) {
	testAgent := createTestAgent(t)

	sessionID := "test-session-123"

	// Save the session (even with minimal state, this tests the save mechanism)
	err := testAgent.SaveState(sessionID)
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Test loading the session
	cmd := &MemoryCommand{}
	err = cmd.loadSession(testAgent, sessionID)
	if err != nil {
		t.Errorf("Failed to load session: %v", err)
	}

	// Test deleting the session
	err = cmd.deleteSession(sessionID)
	if err != nil {
		t.Errorf("Failed to delete session: %v", err)
	}

	// Verify session was deleted by trying to load it again
	err = cmd.loadSession(testAgent, sessionID)
	if err == nil {
		t.Error("Expected error loading deleted session")
	}
}

func TestMemoryCommand_FormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m"},
		{3 * time.Minute, "3m"},
		{90 * time.Minute, "1h"},
		{3 * time.Hour, "3h"},
		{25 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
	}

	for _, test := range tests {
		result := formatDuration(test.duration)
		if result != test.expected {
			t.Errorf("formatDuration(%v) = %s, expected %s", test.duration, result, test.expected)
		}
	}
}

func TestMemoryCommand_Execute_AllSubcommands(t *testing.T) {
	testAgent := createTestAgent(t)

	cmd := &MemoryCommand{}

	// Test summary subcommand
	err := cmd.Execute([]string{"summary"}, testAgent)
	if err != nil {
		t.Errorf("Expected no error for summary subcommand, got: %v", err)
	}

	// Test list subcommand
	err = cmd.Execute([]string{"list"}, testAgent)
	if err != nil {
		t.Errorf("Expected no error for list subcommand, got: %v", err)
	}

	// Test clear subcommand
	err = cmd.Execute([]string{"clear"}, testAgent)
	if err != nil {
		t.Errorf("Expected no error for clear subcommand, got: %v", err)
	}

	// Save a test session for load/delete tests
	sessionID := "test-execute-session"
	testAgent.SetPreviousSummary("Test summary for execute")
	err = testAgent.SaveState(sessionID)
	if err != nil {
		t.Fatalf("Failed to save test session: %v", err)
	}

	// Test load subcommand
	err = cmd.Execute([]string{"load", sessionID}, testAgent)
	if err != nil {
		t.Errorf("Expected no error for load subcommand, got: %v", err)
	}

	// Test delete subcommand
	err = cmd.Execute([]string{"delete", sessionID}, testAgent)
	if err != nil {
		t.Errorf("Expected no error for delete subcommand, got: %v", err)
	}
}

func TestMemoryCommand_ListSessions_WithTimestamps(t *testing.T) {
	// Just test that the listSessions command works without error
	// and that the underlying ListSessionsWithTimestamps function works
	cmd := &MemoryCommand{}
	err := cmd.listSessions()
	if err != nil {
		t.Errorf("Expected no error listing sessions with timestamps, got: %v", err)
	}

	// Verify the underlying timestamp function works
	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		t.Errorf("Failed to get sessions with timestamps: %v", err)
	}

	// Just verify we get some sessions and they have timestamps
	if len(sessions) > 0 {
		firstSession := sessions[0]
		if firstSession.LastUpdated.IsZero() {
			t.Error("Expected non-zero timestamp for session")
		}
		if firstSession.SessionID == "" {
			t.Error("Expected non-empty session ID")
		}
	}
}
