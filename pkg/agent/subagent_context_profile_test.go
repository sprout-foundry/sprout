package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// newSubagentProfileTestRunner builds a parent agent with the given context
// window and a SubagentRunner whose testClientFactory returns a mock with the
// given subagent context window. Returns the parent (for cleanup) and the runner.
func newSubagentProfileTestRunner(t *testing.T, parentWindow, subagentWindow int) (*Agent, *SubagentRunner) {
	t.Helper()

	parent := newIsolatedTestAgent(t)
	// Override the parent's client so its context window matches the fixture.
	parent.setClient(NewMockLLMProviderWithLimit(parentWindow), api.TestClientType)

	shared := &SharedState{
		EventBus:      events.NewEventBus(),
		TodoManager:   tools.NewTodoManager(),
		ConfigManager: parent.configManager,
		WorkspaceRoot: parent.workspaceRoot,
	}
	runner := NewSubagentRunner(parent, shared)
	runner.testClientFactory = func(clientType api.ClientType, model string) (api.ClientInterface, error) {
		return NewMockLLMProviderWithLimit(subagentWindow), nil
	}
	return parent, runner
}

// subagentProfileOpts returns SubagentOptions with an explicit "test" provider
// so createSubagent's provider resolution maps to TestClientType (the parent's
// own provider is "mock" from the overridden client, which is not a resolvable
// provider string). The testClientFactory ignores the resolved type and returns
// the fixture mock regardless.
func subagentProfileOpts() SubagentOptions {
	return SubagentOptions{Persona: "coder", Provider: "test"}
}

// TestSubagentProfile_SmallModelActivatesLCM verifies that a 200K parent
// delegating to a 32K subagent model gets LCM auto-activated on the subagent,
// not the parent's full-mode profile. This is the core SP-125 R4 fix.
func TestSubagentProfile_SmallModelActivatesLCM(t *testing.T) {
	parent, runner := newSubagentProfileTestRunner(t, 200_000, 32_000)
	defer parent.Shutdown()

	sub, err := runner.createSubagent(subagentProfileOpts(), context.Background())
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer sub.Shutdown()

	if sub.contextProfile.Mode != configuration.ContextModeLowContext {
		t.Errorf("expected ContextModeLowContext for 32K subagent, got %q", sub.contextProfile.Mode)
	}
	if got := sub.state.GetMaxContextTokens(); got != 32_000 {
		t.Errorf("subagent max context tokens = %d, want 32000", got)
	}
	if got := sub.GetEffectiveContextCap(); got == 0 {
		t.Error("expected non-zero effective context cap")
	}
}

// TestSubagentProfile_LargeModelStaysFull verifies that a 200K subagent model
// does NOT activate LCM, even when the parent is also 200K (full mode).
func TestSubagentProfile_LargeModelStaysFull(t *testing.T) {
	parent, runner := newSubagentProfileTestRunner(t, 200_000, 200_000)
	defer parent.Shutdown()

	sub, err := runner.createSubagent(subagentProfileOpts(), context.Background())
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer sub.Shutdown()

	if sub.contextProfile.Mode == configuration.ContextModeLowContext {
		t.Error("200K subagent should not activate LCM")
	}
	if got := sub.state.GetMaxContextTokens(); got != 200_000 {
		t.Errorf("subagent max context tokens = %d, want 200000", got)
	}
}

// TestSubagentProfile_ExplicitConfigWins verifies that an explicit
// context_mode: "low_context" in the config activates LCM on the subagent
// even when the subagent model has a 200K context window.
func TestSubagentProfile_ExplicitConfigWins(t *testing.T) {
	parent, runner := newSubagentProfileTestRunner(t, 200_000, 200_000)
	defer parent.Shutdown()

	if err := parent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeLowContext
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	sub, err := runner.createSubagent(subagentProfileOpts(), context.Background())
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer sub.Shutdown()

	if sub.contextProfile.Mode != configuration.ContextModeLowContext {
		t.Error("explicit context_mode=low_context should activate LCM even on 200K subagent")
	}
}

// TestSubagentProfile_BelowFloorFails verifies that a subagent model below
// the 8K context floor causes createSubagent to return an error mentioning
// both the floor and the actual context window.
func TestSubagentProfile_BelowFloorFails(t *testing.T) {
	parent, runner := newSubagentProfileTestRunner(t, 200_000, 4_096)
	defer parent.Shutdown()

	_, err := runner.createSubagent(subagentProfileOpts(), context.Background())
	if err == nil {
		t.Fatal("expected error for 4K subagent context, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "8000-token minimum") {
		t.Errorf("error should mention the 8000-token minimum, got: %s", msg)
	}
	if !strings.Contains(msg, "4096") {
		t.Errorf("error should mention the actual context window (4096), got: %s", msg)
	}
}

// TestSubagentProfile_LCMParentSameModelStaysLCM verifies that a 32K parent
// delegating to a 32K subagent produces LCM. The parent's own profile is the
// zero value in this test harness (test agents skip production-only
// initialization), so under the old blind-copy code the subagent would have
// inherited full mode — this test pins the re-resolution behavior even when
// parent and child share the same window.
func TestSubagentProfile_LCMParentSameModelStaysLCM(t *testing.T) {
	parent, runner := newSubagentProfileTestRunner(t, 32_000, 32_000)
	defer parent.Shutdown()

	sub, err := runner.createSubagent(subagentProfileOpts(), context.Background())
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer sub.Shutdown()

	if sub.contextProfile.Mode != configuration.ContextModeLowContext {
		t.Errorf("expected ContextModeLowContext for 32K subagent under 32K parent, got %q", sub.contextProfile.Mode)
	}
	if got := sub.state.GetMaxContextTokens(); got != 32_000 {
		t.Errorf("subagent max context tokens = %d, want 32000", got)
	}
}

// TestSubagentProfile_UserCapActivatesLCM verifies that a user-configured
// MaxContextTokens cap below the 132K threshold activates LCM on a subagent
// whose native model window is large (200K). The capped value must flow
// through getModelContextLimit into ResolveContextProfile, matching the
// primary-agent path.
func TestSubagentProfile_UserCapActivatesLCM(t *testing.T) {
	parent, runner := newSubagentProfileTestRunner(t, 200_000, 200_000)
	defer parent.Shutdown()

	cap := 100_000
	if err := parent.configManager.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.MaxContextTokens = &cap
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	sub, err := runner.createSubagent(subagentProfileOpts(), context.Background())
	if err != nil {
		t.Fatalf("createSubagent failed: %v", err)
	}
	defer sub.Shutdown()

	if sub.contextProfile.Mode != configuration.ContextModeLowContext {
		t.Errorf("200K subagent with 100K cap should activate LCM, got %q", sub.contextProfile.Mode)
	}
	if got := sub.state.GetMaxContextTokens(); got != 100_000 {
		t.Errorf("subagent max context tokens = %d, want 100000 (capped)", got)
	}
}

// TestWorkflowAgentProfile_Smoke verifies that RunWorkflowLoopInProcess
// completes without error when the workflow agent's profile is resolved via
// the new resolveAndApplyContextProfile path. The workflow agent is created
// internally and not returned, and factory.CreateProviderClient has no test
// seam, so the profile cannot be directly asserted. This smoke test exercises
// the code path and confirms it does not error out for a 128K TestClient
// (which is below the 132K LCM threshold and should resolve to LCM).
func TestWorkflowAgentProfile_Smoke(t *testing.T) {
	parent := newIsolatedTestAgent(t)
	defer parent.Shutdown()

	tmpDir := t.TempDir()

	gatePromptFile := filepath.Join(tmpDir, "gate.md")
	if err := os.WriteFile(gatePromptFile, []byte("You are a gate agent."), 0o644); err != nil {
		t.Fatalf("write gate prompt: %v", err)
	}

	todoFile := filepath.Join(tmpDir, "TODO.md")
	if err := os.WriteFile(todoFile, []byte("# Tasks\n\n## Section\n\n- [x] done item\n"), 0o644); err != nil {
		t.Fatalf("write todo file: %v", err)
	}

	wfFile := filepath.Join(tmpDir, "workflow.json")
	wfJSON := fmt.Sprintf(`{
		"description": "test workflow",
		"loop": {
			"todo_file": "TODO.md",
			"gate_prompt_file": %q,
			"max_retries": 1,
			"max_iterations": 5,
			"build_command": ""
		}
	}`, gatePromptFile)
	if err := os.WriteFile(wfFile, []byte(wfJSON), 0o644); err != nil {
		t.Fatalf("write workflow json: %v", err)
	}

	result, err := RunWorkflowLoopInProcess(context.Background(), parent, wfFile, nil)
	if err != nil {
		t.Fatalf("RunWorkflowLoopInProcess failed: %v", err)
	}
	if result.Error != nil {
		t.Errorf("workflow result has error: %v", result.Error)
	}
}
