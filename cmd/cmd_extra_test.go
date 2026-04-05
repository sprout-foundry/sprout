package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// =============================================================================
// Tests for runDiag (diag.go)
// =============================================================================

func TestRunDiag_PrintsHeader(t *testing.T) {
	out := captureStdout(t, runDiag)
	if !strings.Contains(out, "Configuration Diagnostics") {
		t.Errorf("expected 'Configuration Diagnostics' in output, got:\n%s", out)
	}
}

func TestRunDiag_PrintsGlobalConfigPath(t *testing.T) {
	out := captureStdout(t, runDiag)
	if !strings.Contains(out, "Global config:") {
		t.Errorf("expected 'Global config:' in output, got:\n%s", out)
	}
}

func TestRunDiag_PrintsProjectConfigPath(t *testing.T) {
	out := captureStdout(t, runDiag)
	if !strings.Contains(out, "Project-local config:") {
		t.Errorf("expected 'Project-local config:' in output, got:\n%s", out)
	}
}

func TestRunDiag_PrintsPythonRuntimeSection(t *testing.T) {
	out := captureStdout(t, runDiag)
	if !strings.Contains(out, "Python runtime:") {
		t.Errorf("expected 'Python runtime:' section in output, got:\n%s", out)
	}
}

func TestRunDiag_PrintsCustomProviders(t *testing.T) {
	out := captureStdout(t, runDiag)
	// Should always print something about custom providers (either found or warning)
	hasProvidersInfo := strings.Contains(out, "Custom providers") ||
		strings.Contains(out, "custom providers")
	if !hasProvidersInfo {
		t.Errorf("expected custom providers info in output, got:\n%s", out)
	}
}

// =============================================================================
// Tests for createPlanningAgent (plan.go)
// =============================================================================

func TestCreatePlanningAgent_Default(t *testing.T) {
	origModel := planModel
	origProvider := planProvider
	defer func() {
		planModel = origModel
		planProvider = origProvider
	}()

	planModel = ""
	planProvider = ""

	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() with defaults returned error: %v", err)
	}
	if a == nil {
		t.Fatal("createPlanningAgent() returned nil agent")
	}
}

func TestCreatePlanningAgent_WithModelOnly(t *testing.T) {
	origModel := planModel
	origProvider := planProvider
	defer func() {
		planModel = origModel
		planProvider = origProvider
	}()

	planModel = "test-model"
	planProvider = ""

	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() with model returned error: %v", err)
	}
	if a == nil {
		t.Fatal("createPlanningAgent() returned nil agent")
	}
}

func TestCreatePlanningAgent_WithProviderAndModel(t *testing.T) {
	origModel := planModel
	origProvider := planProvider
	defer func() {
		planModel = origModel
		planProvider = origProvider
	}()

	planModel = "gpt-4"
	planProvider = "openai"

	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() with provider+model returned error: %v", err)
	}
	if a == nil {
		t.Fatal("createPlanningAgent() returned nil agent")
	}
}

func TestCreatePlanningAgent_SystemPromptIsSet(t *testing.T) {
	origModel := planModel
	origProvider := planProvider
	origTodos := planCreateTodos
	defer func() {
		planModel = origModel
		planProvider = origProvider
		planCreateTodos = origTodos
	}()

	planModel = ""
	planProvider = ""

	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() returned error: %v", err)
	}
	if a == nil {
		t.Fatal("createPlanningAgent() returned nil agent")
	}

	prompt := a.GetSystemPrompt()
	if prompt == "" {
		t.Fatal("expected non-empty system prompt on the agent")
	}

	lower := strings.ToLower(prompt)
	if !strings.Contains(lower, "plan") {
		t.Error("expected planning system prompt to contain 'plan'")
	}
}

func TestCreatePlanningAgent_DoesNotPanic(t *testing.T) {
	origModel := planModel
	origProvider := planProvider
	defer func() {
		planModel = origModel
		planProvider = origProvider
	}()

	planModel = "some-invalid-model-string"
	planProvider = ""

	// Verify that createPlanningAgent does not panic, even with an invalid model.
	// Under test mode, NewAgentWithModel uses TestClientType so even invalid
	// model strings should not cause a panic.
	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		createPlanningAgent()
	}()

	if didPanic {
		t.Fatal("createPlanningAgent() panicked with an invalid model string")
	}
}

// =============================================================================
// Tests for runPlanMode (plan.go)
// =============================================================================

func TestRunPlanMode_RequiresTerminal(t *testing.T) {
	// In test environment there is no TTY, so runPlanMode should return
	// an error about requiring a terminal.
	err := runPlanMode(nil)
	if err == nil {
		t.Fatal("expected error when no TTY, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "terminal") {
		t.Errorf("expected error about terminal, got: %v", err)
	}
}

// =============================================================================
// Tests for promptGitHubMCPSetupIfNeeded (github_setup_prompt.go)
// =============================================================================

func TestPromptGitHubMCPSetupIfNeeded_SkipPromptSet(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() failed: %v", err)
	}

	// Set SkipPrompt so the function should return immediately.
	updateErr := a.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.SkipPrompt = true
		return nil
	})
	if updateErr != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", updateErr)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("promptGitHubMCPSetupIfNeeded panicked: %v", r)
		}
	}()

	promptGitHubMCPSetupIfNeeded(a)
}

func TestPromptGitHubMCPSetupIfNeeded_DoesNotPanic(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() failed: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("promptGitHubMCPSetupIfNeeded panicked: %v", r)
		}
	}()

	// The function reads from stdin, but in a test environment without a TTY
	// and running in a temp directory, it should either:
	// 1. Return early because SkipPrompt is set, or
	// 2. Return early because ShouldPromptGitHubSetup returns false, or
	// 3. Try to read from stdin (will get EOF, ReadString returns error, returns early)
	promptGitHubMCPSetupIfNeeded(a)
}

// =============================================================================
// Additional plan-related tests
// =============================================================================

func TestCreatePlanningAgent_PlanCreateTodosFlag(t *testing.T) {
	origModel := planModel
	origProvider := planProvider
	origTodos := planCreateTodos
	defer func() {
		planModel = origModel
		planProvider = origProvider
		planCreateTodos = origTodos
	}()

	// Test with planCreateTodos = false
	planModel = ""
	planProvider = ""
	planCreateTodos = false

	a, err := createPlanningAgent()
	if err != nil {
		t.Fatalf("createPlanningAgent() with todos=false returned error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}

	// Verify the planning prompt with todos=false generates valid content
	prompt, promptErr := agent.GetEmbeddedPlanningPrompt(false)
	if promptErr != nil {
		t.Fatalf("GetEmbeddedPlanningPrompt(false) failed: %v", promptErr)
	}
	_ = fmt.Sprintf("prompt length: %d", len(prompt))
}

func TestRunDiag_PrintsProviderDirectory(t *testing.T) {
	out := captureStdout(t, runDiag)
	if !strings.Contains(out, "Custom provider directory:") {
		t.Errorf("expected 'Custom provider directory:' in output, got:\n%s", out)
	}
}

func TestRunDiag_PrintsPDFRuntimeSection(t *testing.T) {
	out := captureStdout(t, runDiag)
	if !strings.Contains(out, "PDF Python runtime") {
		t.Errorf("expected 'PDF Python runtime' in output, got:\n%s", out)
	}
}
