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
// the 13-tool allowlist (ask_user added in a83ced640 for the cloud IDE
// ask_user/edit-approval flows), the lite system prompt, and the tighter
// compaction trigger.
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

	// (b) Exactly the LCM allowlist registered (core edit + navigation + web
	// + delegation + ask_user; design loop tools and MCP meta-tools added
	// in 2e6239417 / 5546f6dff — the single source of truth is
	// configuration.lowContextProfile.ToolAllowlist).
	tools := agent.getOptimizedToolDefinitions(nil)
	expectedTools := map[string]bool{
		"shell_command": true, "read_file": true, "write_file": true,
		"edit_file": true, "search": true, "repo_map": true,
		"web_search": true, "fetch_url": true,
		"commit": true, "list_changes": true, "recover_file": true,
		"run_subagent": true, "review_changes": true, "ask_user": true,
		// SP-140 design loop (pure Go, valid in every tier).
		"design_assets": true, "design_validate": true,
		"design_brief": true, "design_export_tokens": true, "design_sync": true,
		// MCP setup/discovery meta-tools.
		"mcp_tools": true, "mcp_refresh": true,
		// Skill activation, which both prompts direct the agent to use.
		"list_skills": true, "activate_skill": true,
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

	// (d) Compaction trigger should be 0.85.
	trigger := agent.computeCompactionTriggerFraction()
	if trigger != 0.85 {
		t.Errorf("expected compaction trigger 0.85, got %.2f", trigger)
	}

	// (e) Recent turns to preserve should be 2.
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

// TestRel2_CeilingBindsOnLargeWindow is the rel.2 core: for a large model
// context the absolute live-context ceiling (default 300K) must bind before
// the window-share fraction, so compaction fires at min(share, ceiling) of the
// conversation rather than letting a 1M-token window run to ~734K of live
// context before anything is compacted.
func TestRel2_CeilingBindsOnLargeWindow(t *testing.T) {
	a := &Agent{} // bare agent: default 300K ceiling, base window-share 0.70.
	got := a.compactionTriggerFractionFor(1_000_000)
	want := float64(defaultCompactionCeilingTokens) / 1_000_000 // 0.3072
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("1M window: trigger = %v, want %v (300K ceiling fraction)", got, want)
	}
	// The ceiling must actually bind (drive the fraction below the 0.70 share).
	if got >= 0.70 {
		t.Fatalf("1M window: fraction %v did not drop below the 0.70 share — ceiling not binding", got)
	}
}

// TestRel2_SmallWindowUnaffected verifies the ceiling does not regress small
// models: a 128K window has its share (0.70) below the 300K ceiling fraction
// (2.34), so the window-share trigger is unchanged.
func TestRel2_SmallWindowUnaffected(t *testing.T) {
	a := &Agent{}
	want := 1.0 - totalReservedFraction() // 0.70
	if got := a.compactionTriggerFractionFor(128 * 1024); math.Abs(got-want) > 1e-9 {
		t.Fatalf("128K window: trigger = %v, want %v (share unchanged)", got, want)
	}
}

// TestRel2_ZeroLimitReturnsShare guards the degenerate path: with no usable
// context limit the ceiling is skipped and the plain share is returned.
func TestRel2_ZeroLimitReturnsShare(t *testing.T) {
	a := &Agent{}
	want := 1.0 - totalReservedFraction()
	if got := a.compactionTriggerFractionFor(0); got != want {
		t.Fatalf("zero limit: trigger = %v, want %v (share, ceiling skipped)", got, want)
	}
}

// TestRel2_ConfiguredCeilingHonorsConfig verifies a user-set ceiling tighter
// than the built-in 300K is honored (e.g. 80K on a 1M window → 0.08).
func TestRel2_ConfiguredCeilingHonorsConfig(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.CompactionCeilingTokens = 80 * 1024
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave failed: %v", err)
	}
	client := NewMockLLMProviderWithLimit(1_000_000)
	agent, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	defer agent.Shutdown()

	got := agent.compactionTriggerFractionFor(1_000_000)
	want := float64(80*1024) / 1_000_000 // 0.08
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("configured 80K ceiling on 1M window: trigger = %v, want %v", got, want)
	}
}

// TestRel2_CapWinsOverCeiling pins the integration between the user's context
// cap (Config.MaxContextTokens, /max-context) and the absolute ceiling: the
// cap is an explicit statement about how much context to use, so the trigger
// never fires later than the cap, and raising the cap never moves the trigger
// earlier.
//
// Before the fix the ceiling was measured against the CAPPED window, so a
// 300K ceiling on a 1M model became a 0.70 share of a 300K cap — firing at
// 210K, earlier than the uncapped 307K. Raising the cap moved the trigger the
// wrong way.
func TestRel2_CapWinsOverCeiling(t *testing.T) {
	ceiling := defaultCompactionCeilingTokens // 300K

	for _, tc := range []struct {
		name   string
		cap    int
		wantAt float64
	}{
		{"no cap: ceiling binds", 0, float64(ceiling)},
		{"cap above ceiling: ceiling still binds", 500_000, float64(ceiling)},
		{"cap at ceiling: cap binds", 300_000, 300_000 * (1.0 - totalReservedFraction())},
		{"cap below ceiling: cap binds", 200_000, 200_000 * (1.0 - totalReservedFraction())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAgentWithContextCap(t, 1_000_000, tc.cap)
			effective := a.getModelContextLimit()
			frac := a.compactionTriggerFractionFor(effective)
			gotAt := frac * float64(effective)

			if math.Abs(gotAt-tc.wantAt) > 1 {
				t.Fatalf("trigger fires at %.0f tokens, want %.0f (cap=%d, effective=%d)",
					gotAt, tc.wantAt, tc.cap, effective)
			}
			// The trigger must never exceed the effective window.
			if gotAt > float64(effective) {
				t.Fatalf("trigger %.0f exceeds the effective window %d", gotAt, effective)
			}
		})
	}
}

// TestRel2_CapIsMonotonic pins the property the fix exists to restore: a
// larger cap never makes compaction fire earlier.
func TestRel2_CapIsMonotonic(t *testing.T) {
	caps := []int{0, 500_000, 400_000, 300_000, 250_000, 200_000, 150_000, 120_000}
	prev := -1.0
	for _, capTokens := range caps {
		a := newAgentWithContextCap(t, 1_000_000, capTokens)
		effective := a.getModelContextLimit()
		firesAt := a.compactionTriggerFractionFor(effective) * float64(effective)
		if prev >= 0 && firesAt > prev+1 {
			t.Fatalf("cap %d fires at %.0f, later than the smaller cap's %.0f — trigger is not monotonic",
				capTokens, firesAt, prev)
		}
		prev = firesAt
	}
}

// newAgentWithContextCap builds a test agent on a native window with an
// optional MaxContextTokens cap (0 = uncapped).
func newAgentWithContextCap(t *testing.T, nativeWindow, capTokens int) *Agent {
	t.Helper()
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if capTokens > 0 {
		capVal := capTokens
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.MaxContextTokens = &capVal
			return nil
		}); err != nil {
			t.Fatalf("UpdateConfigNoSave failed: %v", err)
		}
	}
	agent, err := NewAgentWithClient(NewMockLLMProviderWithLimit(nativeWindow), api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient failed: %v", err)
	}
	t.Cleanup(agent.Shutdown)
	return agent
}
