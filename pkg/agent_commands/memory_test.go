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

func TestMemoryCommand_Name(t *testing.T) {
	cmd := &MemoryCommand{}
	if cmd.Name() != "memory" {
		t.Errorf("Expected name 'memory', got '%s'", cmd.Name())
	}
}

func TestMemoryCommand_Description(t *testing.T) {
	cmd := &MemoryCommand{}
	desc := cmd.Description()
	expectedDesc := "Show and load previous conversation sessions"
	if desc != expectedDesc {
		t.Errorf("Expected description '%s', got '%s'", expectedDesc, desc)
	}
}

func TestMemoryCommand_Execute_NoArgs(t *testing.T) {
	cmd := &MemoryCommand{}
	err := cmd.Execute([]string{}, nil)
	// Memory command lists sessions interactively, so we expect it to work without error
	// The "invalid session number" error is expected since we're not providing interactive input
	if err != nil && !strings.Contains(err.Error(), "invalid session number") {
		t.Errorf("Expected no error or session number error for memory command with no args, got: %v", err)
	}
}
