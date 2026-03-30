package agent

import (
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// newTestAgent creates a minimal agent for unit tests using the test client path.
// It is backed by a hermetic temp config directory so tests never touch the
// real user config. This is the recommended helper for all agent tests.
func newTestAgent(t *testing.T) *Agent {
	return newIsolatedTestAgent(t)
}

// newIsolatedTestAgent creates a minimal agent backed by a temp config
// directory so that tests never read or modify the caller's real ~/.ledit
// config.
func newIsolatedTestAgent(t *testing.T) *Agent {
	t.Helper()

	configDir := t.TempDir() + "/.ledit"
	mgr, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir failed: %v", err)
	}

	// Persist LEDIT_CONFIG for the test lifetime so any code path that reads
	// the env var directly (bypassing configManager) stays isolated.
	t.Setenv("LEDIT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}
	// Replace the real-user-config manager with the isolated one.
	agent.configManager = mgr
	return agent
}

func TestNewAgentWithModelCreation(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	if agent == nil {
		t.Fatal("expected non-nil agent")
	}
	if agent.client == nil {
		t.Fatal("expected non-nil client")
	}
	// Verify it uses the test client under go test
	if got := agent.GetProviderType(); got != "test" {
		t.Errorf("GetProviderType = %q, want %q", got, "test")
	}
}

func TestAgentGetModel(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	model := agent.GetModel()
	if model != "test:test" {
		t.Errorf("GetModel = %q, want %q", model, "test:test")
	}
}

func TestSetModel(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	if err := agent.SetModel("gpt-4o"); err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}
	if got := agent.GetModel(); got != "gpt-4o" {
		t.Errorf("GetModel after SetModel = %q, want %q", got, "gpt-4o")
	}
}

func TestSetWorkspaceRoot(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	agent.SetWorkspaceRoot("/tmp/myproject")
	if got := agent.GetWorkspaceRoot(); got != "/tmp/myproject" {
		t.Errorf("GetWorkspaceRoot = %q, want %q", got, "/tmp/myproject")
	}
}

func TestSetSystemPrompt(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	customPrompt := "You are a helpful coding assistant."
	agent.SetSystemPrompt(customPrompt)

	if got := agent.GetSystemPrompt(); got != customPrompt {
		t.Errorf("GetSystemPrompt = %q, want %q", got, customPrompt)
	}
}

func TestGetMessagesEmpty(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	msgs := agent.GetMessages()
	if msgs == nil {
		t.Fatal("GetMessages returned nil, want non-nil slice")
	}
	if len(msgs) != 0 {
		t.Errorf("GetMessages length = %d, want 0", len(msgs))
	}
}

func TestGetTotalCostZero(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	if got := agent.GetTotalCost(); got != 0.0 {
		t.Errorf("GetTotalCost = %f, want 0.0", got)
	}
}

func TestGetActivePersonaDefault(t *testing.T) {
	// Unset LEDIT_PERSONA to test the default value.
	t.Setenv("LEDIT_PERSONA", "")

	agent := newTestAgent(t)
	defer agent.Shutdown()

	persona := agent.GetActivePersona()
	// Under go test the default is "orchestrator" unless LEDIT_PERSONA is set.
	if persona != "orchestrator" {
		t.Errorf("GetActivePersona = %q, want %q", persona, "orchestrator")
	}
}

func TestGetActivePersonaEmpty(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Clear the active persona field directly (same package access).
	agent.activePersona = ""
	persona := agent.GetActivePersona()
	if persona != "" {
		t.Errorf("GetActivePersona = %q, want empty string", persona)
	}
}

func TestAgentDebugField(t *testing.T) {
	// Unset LEDIT_DEBUG to ensure debug defaults to false.
	t.Setenv("LEDIT_DEBUG", "")

	agent := newTestAgent(t)
	defer agent.Shutdown()

	// Verify the debug field is accessible and false by default.
	if agent.debug {
		t.Error("agent.debug should be false when LEDIT_DEBUG is unset")
	}

	// Toggle the debug field (same-package access).
	agent.debug = true
	if !agent.debug {
		t.Error("agent.debug should be true after assignment")
	}
}

func TestAgentGetProvider(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	provider := strings.ToLower(agent.GetProvider())
	if provider == "" || provider == "unknown" {
		t.Errorf("GetProvider = %q, want non-empty value", provider)
	}
}

func TestAgentAddMessage(t *testing.T) {
	agent := newTestAgent(t)
	defer agent.Shutdown()

	if len(agent.GetMessages()) != 0 {
		t.Fatalf("expected empty messages, got %d", len(agent.GetMessages()))
	}

	agent.AddMessage(api.Message{Role: "user", Content: "hello"})
	msgs := agent.GetMessages()
	if len(msgs) != 1 {
		t.Fatalf("after AddMessage, got %d messages, want 1", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("message content = %q, want %q", msgs[0].Content, "hello")
	}
}
