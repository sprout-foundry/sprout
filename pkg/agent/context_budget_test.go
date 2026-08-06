package agent

import (
	"math"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestTotalReservedFraction verifies the reservation constants sum to the
// documented total. SP-066 Phase 1 ships with 15% response + 10% thinking
// + 5% tool I/O = 30%; if any constant is tuned the total should be
// inspected for headroom side effects.
func TestTotalReservedFraction(t *testing.T) {
	got := totalReservedFraction()
	want := 0.30
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("totalReservedFraction = %v, want %v", got, want)
	}
}

// TestComputeCompactionTriggerFraction verifies the trigger fraction is
// (1 − total_reserved) so seed substitutes before the prompt consumes the
// reserved share of the context window. With the default 30% reservation,
// the trigger fires at 70% of max context — well below seed's hardcoded
// 0.85 default, restoring headroom for thinking-budget models.
func TestComputeCompactionTriggerFraction(t *testing.T) {
	a := &Agent{}
	got := a.computeCompactionTriggerFraction()
	want := 0.70
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("computeCompactionTriggerFraction = %v, want %v", got, want)
	}
}

// TestCompactionTriggerFractionLowerThanSeedDefault guards against the
// regression where sprout's reservation math drifts above seed's hardcoded
// 0.85 default (defaultCompactionTriggerFraction). The whole point of
// SP-066 Phase 1 is to be more conservative than seed's default; if we
// ever exceed it, we're not solving the empty-reply problem we set out
// to solve.
func TestCompactionTriggerFractionLowerThanSeedDefault(t *testing.T) {
	const seedDefault = 0.85
	a := &Agent{}
	got := a.computeCompactionTriggerFraction()
	if got >= seedDefault {
		t.Fatalf("trigger fraction %v must be less than seed default %v", got, seedDefault)
	}
}

// TestCompactionTriggerFractionInRange verifies the computed fraction is a
// valid value for seed (clamped to (0, 1]). A misconfigured constant
// (e.g., negative reservation) would otherwise produce a value > 1 or ≤ 0,
// which seed silently replaces with its own default.
func TestCompactionTriggerFractionInRange(t *testing.T) {
	a := &Agent{}
	got := a.computeCompactionTriggerFraction()
	if got <= 0 || got > 1 {
		t.Fatalf("trigger fraction %v out of seed's accepted range (0, 1]", got)
	}
}

// TestSP125_LowContextMode_32K verifies that an agent created against a 32K
// context model auto-activates Low-Context Mode with the expected levers:
// the 12-tool allowlist, the lite system prompt, proactive context disabled,
// and the tighter compaction trigger.
func TestSP125_LowContextMode_32K(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	client := NewMockLLMProviderWithLimit(32_000)
	agent, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	defer agent.Shutdown()

	// (a) Profile should be low_context.
	if agent.contextProfile.Mode != configuration.ContextModeLowContext {
		t.Errorf("expected ContextModeLowContext, got %q", agent.contextProfile.Mode)
	}

	// (b) Exactly 12 tools registered (core edit + navigation + web + delegation).
	tools := agent.getOptimizedToolDefinitions(nil)
	expectedTools := map[string]bool{
		"shell_command": true, "read_file": true, "write_file": true,
		"edit_file": true, "search": true, "repo_map": true,
		"web_search": true, "fetch_url": true,
		"commit": true, "list_changes": true, "recover_file": true,
		"run_subagent": true,
	}
	if len(tools) != len(expectedTools) {
		var names []string
		for _, tool := range tools {
			names = append(names, tool.Function.Name)
		}
		t.Errorf("expected %d tools, got %d: %v", len(expectedTools), len(tools), names)
	}
	for _, tool := range tools {
		if !expectedTools[tool.Function.Name] {
			t.Errorf("unexpected tool in LCM allowlist: %s", tool.Function.Name)
		}
	}

	// (c) System prompt should be the lite variant. The total includes
	// AGENTS.md (~4K) which is always injected, so ~5K total is expected.
	// The full prompt alone is ~6.6K, so > 8K means the full prompt leaked.
	prompt := agent.GetSystemPrompt()
	promptTokens := EstimateTokens(prompt)
	if promptTokens > 8000 {
		t.Errorf("lite prompt + AGENTS.md should be < 8K tokens, got ~%d (full prompt leaked?)", promptTokens)
	}
	if promptTokens < 1500 {
		t.Errorf("lite prompt + AGENTS.md should be > 1.5K tokens, got ~%d (empty?)", promptTokens)
	}

	// (d) Proactive context should be disabled.
	if !agent.contextProfile.SkipProactiveContext {
		t.Error("expected SkipProactiveContext=true in LCM")
	}

	// (e) Compaction trigger should be 0.85.
	trigger := agent.computeCompactionTriggerFraction()
	if trigger != 0.85 {
		t.Errorf("expected compaction trigger 0.85, got %.2f", trigger)
	}

	// (f) Recent turns to preserve should be 2.
	if agent.recentTurnsToPreserveFor() != 2 {
		t.Errorf("expected recentTurnsToPreserve=2, got %d", agent.recentTurnsToPreserveFor())
	}
}

// TestSP125_FullContextMode_200K verifies that a 200K model gets full mode
// with all tools and the default compaction trigger.
func TestSP125_FullContextMode_200K(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	client := NewMockLLMProviderWithLimit(200_000)
	agent, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	defer agent.Shutdown()

	// Profile should be full (zero-value, not low_context).
	if agent.contextProfile.Mode == configuration.ContextModeLowContext {
		t.Error("200K model should not activate LCM")
	}

	// Tools should not be filtered to 9 (should have many more).
	tools := agent.getOptimizedToolDefinitions(nil)
	if len(tools) <= 9 {
		var names []string
		for _, tool := range tools {
			names = append(names, tool.Function.Name)
		}
		t.Errorf("200K model should have more than 9 tools, got %d: %v", len(tools), names)
	}

	// Compaction trigger should be the default (0.70), not 0.85.
	trigger := agent.computeCompactionTriggerFraction()
	if trigger >= 0.85 {
		t.Errorf("full mode trigger should be < 0.85 (default 0.70), got %.2f", trigger)
	}
}

// TestSP125_ContextFloor_4K verifies that a model below the 8K floor
// produces a clear error rather than starting a broken session.
func TestSP125_ContextFloor_4K(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	client := NewMockLLMProviderWithLimit(4_096)
	_, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err == nil {
		t.Fatal("expected floor error for 4K context, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "8000-token minimum") {
		t.Errorf("error should mention the 8000-token minimum, got: %s", msg)
	}
	if !strings.Contains(msg, "4096") {
		t.Errorf("error should mention the actual context window (4096), got: %s", msg)
	}
}

// TestSP125_ExplicitLowContextConfig verifies that explicitly setting
// context_mode: "low_context" activates LCM even on a 128K model.
func TestSP125_ExplicitLowContextConfig(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	// Set context_mode explicitly via the manager's mutator (GetConfig
	// returns a clone, so direct mutation wouldn't persist).
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeLowContext
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}

	client := NewMockLLMProviderWithLimit(128_000)
	agent, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	defer agent.Shutdown()

	if agent.contextProfile.Mode != configuration.ContextModeLowContext {
		t.Error("explicit config_mode=low_context should activate LCM even on 128K model")
	}
}
