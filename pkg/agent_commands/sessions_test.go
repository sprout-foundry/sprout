package commands

import (
	"os"
	"strings"
	"testing"

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

func TestSessionsCommand_Name(t *testing.T) {
	cmd := &SessionsCommand{}
	if cmd.Name() != "sessions" {
		t.Errorf("Expected name 'sessions', got '%s'", cmd.Name())
	}
}

func TestSessionsCommand_Description(t *testing.T) {
	cmd := &SessionsCommand{}
	desc := cmd.Description()
	expectedDesc := "Show and load previous conversation sessions"
	if desc != expectedDesc {
		t.Errorf("Expected description '%s', got '%s'", expectedDesc, desc)
	}
}

func TestSessionsCommand_Execute_NoArgs(t *testing.T) {
	cmd := &SessionsCommand{}
	err := cmd.Execute([]string{}, nil)
	// Sessions command lists sessions interactively, so we expect it to work without error
	// The "invalid session number" error is expected since we're not providing interactive input
	if err != nil && !strings.Contains(err.Error(), "invalid session number") {
		t.Errorf("Expected no error or session number error for sessions command with no args, got: %v", err)
	}
}
