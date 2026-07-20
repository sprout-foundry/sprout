package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// newTestAgent creates a minimal agent for unit tests using the test client path.
// It is backed by a hermetic temp config directory so tests never touch the
// real user config. This is the recommended helper for all agent tests.
func newTestAgent(t *testing.T) *Agent {
	return newIsolatedTestAgent(t)
}

// prepareMessagesForAgent prepares messages for the given agent, suitable for
// low-level messaging tests.
func prepareMessagesForAgent(t *testing.T, agent *Agent, tools []api.Tool) []api.Message {
	t.Helper()
	agent.initSubManagers()
	if agent.state == nil {
		agent.state = NewAgentStateManager(false)
	}
	// Use the system prompt from the agent and strip any system messages from history
	optimizedMessages := agent.state.GetMessages()

	// Strip all system messages from conversation history
	filtered := make([]api.Message, 0, len(optimizedMessages))
	for _, m := range optimizedMessages {
		if m.Role == "system" {
			continue
		}
		filtered = append(filtered, m)
	}
	optimizedMessages = filtered

	// Build the system message
	allMessages := []api.Message{{Role: "system", Content: agent.systemPrompt}}
	allMessages = append(allMessages, optimizedMessages...)
	allMessages = collapseSystemMessagesToFront(allMessages)

	return allMessages
}

// newIsolatedTestAgent creates a minimal agent backed by a temp config
// directory so that tests never read or modify the caller's real ~/.sprout
// config.
func newIsolatedTestAgent(t *testing.T) *Agent {
	t.Helper()

	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir failed: %v", err)
	}

	// Persist LEDIT_CONFIG for the test lifetime so any code path that reads
	// the env var directly (bypassing configManager) stays isolated.
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	agent, err := NewAgentWithModel("test:test")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}
	// Replace the real-user-config manager with the isolated one.
	agent.configManager = mgr
	// Tests must never block on an interactive security prompt. SkipPrompt
	// drives utils.GetLogger(cfg.SkipPrompt) → non-interactive at every gate
	// (Gate 1 in tool_security.go, Gate 2 in risk_prompt.go), so approval
	// paths resolve deterministically instead of hanging on stdin.
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.SkipPrompt = true
		return nil
	}); err != nil {
		t.Fatalf("set SkipPrompt on isolated test config: %v", err)
	}
	return agent
}

// TestNewAgentWithClient pins the public constructor that WASM/SDK
// consumers use to bypass the interactive provider-resolution dance in
// newAgentWithConfigManager. The contract is: caller hands us an
// already-built client + configManager, we wire up the rest of the
// agent without prompting for API keys or running connection checks.
func TestNewAgentWithClient(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	mgr, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir failed: %v", err)
	}

	client, err := factory.CreateProviderClient(api.TestClientType, "")
	if err != nil {
		t.Fatalf("CreateProviderClient(TestClientType) failed: %v", err)
	}

	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	defer ag.Shutdown()

	if ag.GetProviderType() != api.TestClientType {
		t.Errorf("GetProviderType = %q, want %q", ag.GetProviderType(), api.TestClientType)
	}
	if ag.GetSystemPrompt() == "" {
		t.Error("system prompt should be populated from the embedded default")
	}
	if ag.configManager != mgr {
		t.Error("agent should hold the passed-in configManager")
	}
}

func TestNewAgentWithClient_NilGuards(t *testing.T) {
	mgr, err := configuration.NewManagerWithDir(t.TempDir() + "/.sprout")
	if err != nil {
		t.Fatalf("NewManagerWithDir failed: %v", err)
	}
	client, err := factory.CreateProviderClient(api.TestClientType, "")
	if err != nil {
		t.Fatalf("CreateProviderClient failed: %v", err)
	}

	if _, err := NewAgentWithClient(nil, api.TestClientType, mgr); err == nil {
		t.Error("expected error when client is nil")
	}
	if _, err := NewAgentWithClient(client, api.TestClientType, nil); err == nil {
		t.Error("expected error when configManager is nil")
	}
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
	agent.state.SetActivePersona("")
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

// TestContextCapActivationNotice tests that when a context cap is set lower than the
// native window, the effectiveContextCap is properly set on the agent.
func TestContextCapActivationNotice(t *testing.T) {
	// Create a temp config directory with a config that has MaxContextTokens set
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	// Test: When cap is set lower than native, the agent should have effectiveContextCap set
	t.Run("agent has effectiveContextCap when cap is below native", func(t *testing.T) {
		// Create a fresh isolated config directory
		configDir := t.TempDir() + "/sprout_cap"
		mgr, err := configuration.NewManagerWithDir(configDir)
		if err != nil {
			t.Fatalf("NewManagerWithDir failed: %v", err)
		}
		t.Setenv("LEDIT_CONFIG", configDir)
		t.Setenv("SPROUT_CONFIG", configDir)

		// Set a cap of 64K (lower than test client's 128K context)
		cap := 64_000
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.MaxContextTokens = &cap
			return nil
		}); err != nil {
			t.Fatalf("failed to set MaxContextTokens: %v", err)
		}

		// Create agent with the test client (128K native) - this should apply the cap
		agent, err := NewAgentWithClient(
			&factory.TestClient{},
			api.TestClientType,
			mgr,
		)
		if err != nil {
			t.Fatalf("NewAgentWithClient failed: %v", err)
		}
		defer agent.Shutdown()

		// Verify that effectiveContextCap is set to the cap (64K), not the native (128K)
		effectiveCap := agent.GetEffectiveContextCap()
		if effectiveCap != 64_000 {
			t.Errorf("expected effectiveContextCap = 64000, got %d", effectiveCap)
		}

		// Verify that native is larger than cap
		native := agent.getNativeModelContextLimit()
		if native <= effectiveCap {
			t.Errorf("expected native (%d) > effectiveCap (%d)", native, effectiveCap)
		}
	})

	t.Run("agent uses native when cap is not set", func(t *testing.T) {
		// Create a fresh isolated config manager
		configDir := t.TempDir() + "/sprout"
		mgr, err := configuration.NewManagerWithDir(configDir)
		if err != nil {
			t.Fatalf("NewManagerWithDir failed: %v", err)
		}
		t.Setenv("LEDIT_CONFIG", configDir)
		t.Setenv("SPROUT_CONFIG", configDir)

		// Create agent without setting MaxContextTokens
		agent, err := NewAgentWithClient(
			&factory.TestClient{},
			api.TestClientType,
			mgr,
		)
		if err != nil {
			t.Fatalf("NewAgentWithClient failed: %v", err)
		}
		defer agent.Shutdown()

		// When no cap is set, effective should equal native
		effectiveCap := agent.GetEffectiveContextCap()
		native := agent.getNativeModelContextLimit()
		if effectiveCap != native {
			t.Errorf("expected effectiveCap (%d) == native (%d)", effectiveCap, native)
		}
	})
}
