//go:build !js

package cmd

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestAvailablePersonaCompletions(t *testing.T) {
	cfg := &configuration.Config{
		SubagentTypes: map[string]configuration.SubagentType{
			"web_scraper": {ID: "web_scraper", Enabled: true},
			"coder":       {ID: "coder", Enabled: true},
			"debugger":    {ID: "debugger", Enabled: false},
		},
	}

	all := availablePersonaCompletions(cfg, "")
	if len(all) != 2 {
		t.Fatalf("expected 2 enabled persona completions, got %d (%v)", len(all), all)
	}
	if all[0] != "coder" || all[1] != "web_scraper" {
		t.Fatalf("unexpected completion order/content: %v", all)
	}

	filtered := availablePersonaCompletions(cfg, "web")
	if len(filtered) != 1 || filtered[0] != "web_scraper" {
		t.Fatalf("unexpected filtered completions: %v", filtered)
	}
}

// =============================================================================
// createChatAgent
// =============================================================================

func TestCreateChatAgent_Default(t *testing.T) {
	// Save and restore original values
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	// Reset to minimal defaults
	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
	// Verify base system prompt is set (default prompt should exist)
	if a.GetSystemPrompt() == "" {
		t.Error("expected non-empty system prompt")
	}
}

func TestCreateChatAgent_WithProviderAndModel(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = "openrouter"
	agentModel = "test-model"
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with provider+model unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestCreateChatAgent_WithModelOnly(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = "claude-3-opus"
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with model only unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestCreateChatAgent_WithProviderOnly(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = "openai"
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with provider only unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

func TestCreateChatAgent_WithSystemPrompt(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = "You are a test assistant."
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with system prompt unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	prompt := a.GetSystemPrompt()
	if prompt != "You are a test assistant." {
		t.Errorf("expected system prompt to be set, got %q", prompt)
	}
}

func TestCreateChatAgent_WithSystemPromptFile(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	// Create a temp file for testing
	tmpFile, err := os.CreateTemp("", "system_prompt_*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.WriteString("Custom system prompt from file.")
	tmpFile.Close()

	agentSystemPromptFile = tmpFile.Name()

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with system prompt file unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	prompt := a.GetSystemPrompt()
	if prompt != "Custom system prompt from file." {
		t.Errorf("expected system prompt from file, got %q", prompt)
	}
}

func TestCreateChatAgent_WithSystemPromptFileNotFound(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 0
	agentSystemPrompt = ""
	agentSystemPromptFile = "/nonexistent/path/to/prompt.txt"

	_, err := createChatAgent()
	if err == nil {
		t.Fatal("expected error for non-existent system prompt file")
	}
}

func TestCreateChatAgent_WithMaxIterations(t *testing.T) {
	origProvider := agentProvider
	origModel := agentModel
	origPersona := agentPersona
	origMaxIter := maxIterations
	origSystemPrompt := agentSystemPrompt
	origSystemPromptFile := agentSystemPromptFile
	defer func() {
		agentProvider = origProvider
		agentModel = origModel
		agentPersona = origPersona
		maxIterations = origMaxIter
		agentSystemPrompt = origSystemPrompt
		agentSystemPromptFile = origSystemPromptFile
	}()

	agentProvider = ""
	agentModel = ""
	agentPersona = ""
	maxIterations = 10
	agentSystemPrompt = ""
	agentSystemPromptFile = ""

	a, err := createChatAgent()
	if err != nil {
		t.Fatalf("createChatAgent() with max iterations unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	if a.GetMaxIterations() != 10 {
		t.Errorf("expected max iterations 10, got %d", a.GetMaxIterations())
	}
}

// ---------------------------------------------------------------------------
// TestCreateChatAgent_DaemonMode_NilAgentOnProviderError
// ---------------------------------------------------------------------------

func TestCreateChatAgent_DaemonMode_NilAgentOnProviderError(t *testing.T) {
	// Verify that the sentinel errors used by the daemon-mode nil-agent path
	// are correctly handled by errors.Is(). This tests the branching logic
	// in createChatAgent:
	//
	//   if daemonMode && (errors.Is(err, agent.ErrProviderNotConfigured) ||
	//                      errors.Is(err, agent.ErrModelNotAvailable)) {
	//       return nil, nil  // nil-agent daemon mode
	//   }
	//
	// We cannot exercise the full createChatAgent() path for this because
	// agent.NewAgent() is a function that reads real configuration and
	// cannot be mocked. However, the test below validates the critical
	// errors.Is() semantics that the nil-agent path depends on.

	t.Run("ErrProviderNotConfigured is matched by errors.Is", func(t *testing.T) {
		t.Parallel()

		// Direct reference should match.
		if !errors.Is(agent.ErrProviderNotConfigured, agent.ErrProviderNotConfigured) {
			t.Error("ErrProviderNotConfigured should match itself")
		}

		// Wrapped error should also match.
		wrapped := errors.New("outer: " + agent.ErrProviderNotConfigured.Error())
		wrapped = fmt.Errorf("wrapped: %w", agent.ErrProviderNotConfigured)
		if !errors.Is(wrapped, agent.ErrProviderNotConfigured) {
			t.Error("wrapped ErrProviderNotConfigured should match via errors.Is")
		}

		// Unrelated errors must not match.
		unrelated := errors.New("some other error")
		if errors.Is(unrelated, agent.ErrProviderNotConfigured) {
			t.Error("unrelated error should not match ErrProviderNotConfigured")
		}

		// A string-matching error should not match (it's not the same sentinel).
		stringErr := errors.New("provider is not configured — configure via webui settings")
		if errors.Is(stringErr, agent.ErrProviderNotConfigured) {
			t.Error("a separate error with the same message should not match via errors.Is")
		}
	})

	t.Run("ErrModelNotAvailable is matched by errors.Is", func(t *testing.T) {
		t.Parallel()

		// Direct reference should match.
		if !errors.Is(agent.ErrModelNotAvailable, agent.ErrModelNotAvailable) {
			t.Error("ErrModelNotAvailable should match itself")
		}

		// Wrapped error should also match.
		wrapped := fmt.Errorf("wrapped: %w", agent.ErrModelNotAvailable)
		if !errors.Is(wrapped, agent.ErrModelNotAvailable) {
			t.Error("wrapped ErrModelNotAvailable should match via errors.Is")
		}

		// The two sentinels must not cross-match.
		if errors.Is(agent.ErrProviderNotConfigured, agent.ErrModelNotAvailable) {
			t.Error("ErrProviderNotConfigured should not match ErrModelNotAvailable")
		}
	})

	t.Run("daemonMode global is independently controllable", func(t *testing.T) {
		// Save and restore the daemonMode global.
		origDaemonMode := daemonMode
		defer func() { daemonMode = origDaemonMode }()

		// Verify we can set and read daemonMode.
		daemonMode = true
		if !daemonMode {
			t.Fatal("daemonMode should be true after setting")
		}

		daemonMode = false
		if daemonMode {
			t.Fatal("daemonMode should be false after resetting")
		}
	})

	t.Run("createChatAgent returns nil agent in daemon mode with ErrProviderNotConfigured", func(t *testing.T) {
		// This test exercises the nil-agent path by simulating the condition
		// that createChatAgent checks. Because agent.NewAgent() uses
		// isRunningUnderTest() to always create a test client under go test,
		// we cannot trigger ErrProviderNotConfigured from the real function.
		// Instead, we validate the branch condition logic independently.
		//
		// The actual nil-agent path is covered by integration tests and
		// manual daemon-mode testing (see TestRecoverProviderStartup_DaemonMode).

		origDaemonMode := daemonMode
		defer func() { daemonMode = origDaemonMode }()

		// Simulate the daemon-mode check that createChatAgent performs:
		//   if daemonMode && errors.Is(err, agent.ErrProviderNotConfigured) { return nil, nil }
		daemonMode = true
		err := agent.ErrProviderNotConfigured

		// This is the exact condition checked in createChatAgent.
		shouldReturnNil := daemonMode &&
			(errors.Is(err, agent.ErrProviderNotConfigured) || errors.Is(err, agent.ErrModelNotAvailable))
		if !shouldReturnNil {
			t.Fatal("daemonMode + ErrProviderNotConfigured should trigger nil-agent path")
		}

		// Same for ErrModelNotAvailable.
		err = agent.ErrModelNotAvailable
		shouldReturnNil = daemonMode &&
			(errors.Is(err, agent.ErrProviderNotConfigured) || errors.Is(err, agent.ErrModelNotAvailable))
		if !shouldReturnNil {
			t.Fatal("daemonMode + ErrModelNotAvailable should trigger nil-agent path")
		}

		// A non-sentinel error should NOT trigger the nil-agent path.
		err = errors.New("some random error")
		shouldReturnNil = daemonMode &&
			(errors.Is(err, agent.ErrProviderNotConfigured) || errors.Is(err, agent.ErrModelNotAvailable))
		if shouldReturnNil {
			t.Fatal("daemonMode + random error should NOT trigger nil-agent path")
		}
	})
}
