//go:build !js

// runner_metrics_test.go — the per-task metrics tests: the run's
// tokens and cost come from the conversation totals, the run carries its role
// usage, and the language-guard mismatch is counted. The fixtures and helpers
// live in runner_test.go (same package).

package benchmark

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// Per-task metrics: tokens, cost, language guard
// ---------------------------------------------------------------------------

// Reliably detectable prose fixtures, mirrored from pkg/agent's
// language-guard test fixtures (the trigram detector reports them
// above its reliability bar).
const (
	benchLgEnglishProse  = "The build succeeded after applying the patch, so the tests can run and the release is ready to ship."
	benchLgEnglishProse2 = "All the tests passed after the patch was applied, so the release is now ready to be shipped."
	benchLgSpanishProse  = "El paquete está listo para compilar ahora mismo y las pruebas pasan sin errores."
)

// TestRunner_TokensAndCostAreTheConversationTotals pins the token and
// cost capture (the benchmark's "existing cost tracking"): the run's
// agent is fresh, so the agent's conversation totals ARE the run's
// usage. Every scripted response of one passing turn carries an
// explicit non-zero Usage (resolveUsage would otherwise fall back to
// its defaults for zero-usage responses), and the run's token total is
// the scripted sum — the seed loop accumulates each response's
// TotalTokens into the conversation total (two responses: the
// write_file tool call, 100 tokens, and the final answer, 200 → 300).
// Cost is wired from the agent's existing cost total: the scripted
// client produces no cost (no provider-reported cost, no pricing for
// the test model), so the assertion pins the wiring to the accessor
// rather than a constant.
func TestRunner_TokensAndCostAreTheConversationTotals(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	var (
		built  *agent.Agent
		client *agent.ScriptedClient
	)
	factory := func(runDir string, spec ModelSpec) (*agent.Agent, error) {
		args := fmt.Sprintf(`{"path":%q,"content":"function bench() { return 1; }"}`,
			filepath.Join(runDir, "src", "bench.js"))
		toolResp := agent.NewToolCallResponse("write_file", args)
		toolResp.Usage = agent.ScriptedTokenUsage{PromptTokens: 80, CompletionTokens: 20, TotalTokens: 100}
		answerResp := agent.NewStopResponse("Done!")
		answerResp.Usage = agent.ScriptedTokenUsage{PromptTokens: 150, CompletionTokens: 50, TotalTokens: 200}
		client = agent.NewScriptedClient(toolResp, answerResp)
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.ContextMode = configuration.ContextModeFull
			cfg.SkipPrompt = true
			cfg.Verification = &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("harness: configure run config: %w", err)
		}
		ag, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
		if err != nil {
			return nil, fmt.Errorf("harness: build scripted agent: %w", err)
		}
		ag.SetMaxIterations(10)
		ag.SetWorkspaceRoot(runDir)
		built = ag
		return ag, nil
	}

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  factory,
		ShapeCopy:     shapeManifest("echo fixture-bench-ok"),
		RunsPerTask:   1,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if !run.Passed {
		t.Fatalf("Passed = false, want true (the build command passes): %+v", run)
	}

	// Exactly the two scripted responses were consumed (the tool call
	// and the answer — no extra model calls), so the run's token total
	// is exactly the scripted sum: 100 + 200.
	if calls := len(client.GetSentRequests()); calls != 2 {
		t.Errorf("model calls = %d, want 2 (the tool-call response and the answer)", calls)
	}
	if run.Tokens != 300 {
		t.Errorf("Tokens = %d, want 300 (the scripted sum: 100 for the tool call + 200 for the answer — the fresh agent's conversation total is the run's usage)", run.Tokens)
	}
	if run.Tokens != built.GetTotalTokens() {
		t.Errorf("Tokens = %d, agent GetTotalTokens = %d, want the accessor's value (existing cost tracking)", run.Tokens, built.GetTotalTokens())
	}
	// Cost: the scripted client produces no cost (no provider-reported
	// cost, no pricing for the test model), so the value is 0 and the
	// assertion pins the wiring to the accessor.
	if run.Cost != built.GetTotalCost() {
		t.Errorf("Cost = %v, agent GetTotalCost = %v, want the accessor's value (the existing cost tracking)", run.Cost, built.GetTotalCost())
	}
	if run.Cost < 0 {
		t.Errorf("Cost = %v, want >= 0", run.Cost)
	}
	assertWallTime(t, run)
}

// TestRunner_RunCarriesRoleUsage pins the run's per-role token/cost
// breakdown: the Run record
// carries the run's agent's per-role usage, so a benchmark can attribute
// spend to the model role each call was made under. A scripted passing
// turn is consumed entirely by the primary agent (the coder role), so the
// run's RoleUsage is the coder's conversation split — the same scripted
// totals the tokens/cost test pins (100 for the tool call + 200 for the
// answer = 300, 230 prompt / 70 completion) — and the assertion is
// cross-checked against the built agent's own per-role accessor.
func TestRunner_RunCarriesRoleUsage(t *testing.T) {
	shAvailable(t)
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	var (
		built  *agent.Agent
		client *agent.ScriptedClient
	)
	factory := func(runDir string, spec ModelSpec) (*agent.Agent, error) {
		args := fmt.Sprintf(`{"path":%q,"content":"function bench() { return 1; }"}`,
			filepath.Join(runDir, "src", "bench.js"))
		toolResp := agent.NewToolCallResponse("write_file", args)
		toolResp.Usage = agent.ScriptedTokenUsage{PromptTokens: 80, CompletionTokens: 20, TotalTokens: 100}
		answerResp := agent.NewStopResponse("Done!")
		answerResp.Usage = agent.ScriptedTokenUsage{PromptTokens: 150, CompletionTokens: 50, TotalTokens: 200}
		client = agent.NewScriptedClient(toolResp, answerResp)
		if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.ContextMode = configuration.ContextModeFull
			cfg.SkipPrompt = true
			cfg.Verification = &configuration.VerificationConfig{Enabled: true, RepairAttempts: 1}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("harness: configure run config: %w", err)
		}
		ag, err := agent.NewAgentWithClient(client, api.TestClientType, mgr)
		if err != nil {
			return nil, fmt.Errorf("harness: build scripted agent: %w", err)
		}
		ag.SetMaxIterations(10)
		ag.SetWorkspaceRoot(runDir)
		built = ag
		return ag, nil
	}

	runner := &Runner{
		ConfigManager: mgr,
		AgentFactory:  factory,
		ShapeCopy:     shapeManifest("echo fixture-bench-ok"),
		RunsPerTask:   1,
	}

	runs, err := runner.RunTask(context.Background(), benchTask(), ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if !run.Passed {
		t.Fatalf("Passed = false, want true (the build command passes): %+v", run)
	}

	// The run's per-role usage mirrors the built agent's accessor (the
	// wiring), and since the whole turn ran under the primary agent's
	// role it is a single entry for that role.
	if got := run.RoleUsage; len(got) == 0 {
		t.Fatalf("Run.RoleUsage is empty, want the run agent's per-role usage")
	}
	if len(built.GetRoleUsage()) == 0 {
		t.Fatalf("agent GetRoleUsage() is empty, want the per-role usage")
	}
	var coder *agent.RoleUsage
	for i, ru := range run.RoleUsage {
		if ru.Role == run.Model || ru.Role == built.GetRole() {
			coder = &run.RoleUsage[i]
			break
		}
	}
	if coder == nil {
		// The primary agent's role is the coder; fall back to the sole
		// entry (a single-role scripted run has exactly one).
		if len(run.RoleUsage) == 1 {
			coder = &run.RoleUsage[0]
		} else {
			t.Fatalf("Run.RoleUsage = %+v, want an entry for the primary role %q", run.RoleUsage, built.GetRole())
		}
	}
	// The coder's split is the scripted conversation total: 100 (tool
	// call) + 200 (answer) = 300 tokens, 230 prompt / 70 completion.
	if coder.PromptTokens != 230 || coder.CompletionTokens != 70 || coder.Tokens != 300 {
		t.Errorf("%s role split = %+v, want 230 prompt / 70 completion / 300 total (the scripted sum)", coder.Role, coder)
	}
	// Cross-check the wiring against the built agent's accessor.
	if len(run.RoleUsage) != len(built.GetRoleUsage()) {
		t.Errorf("Run.RoleUsage has %d entries, agent has %d, want the same per-role split", len(run.RoleUsage), len(built.GetRoleUsage()))
	}
	assertWallTime(t, run)
}

// TestRunner_LanguageGuardMismatchCounted pins the run's share of the
// process-wide language-guard metric (recorded by the
// guard's existing Record call site — the benchmark reads the delta, it does
// not build new meters): a scripted run whose final message is in a
// different language than the request records one check and one
// mismatch for the run's model (a fresh recorder is installed so the
// delta is isolated from other tests), and the matching-language mirror
// records the check with no mismatch. The scripts carry the
// regeneration response the guard path consumes (mirroring the same
// pattern).
func TestRunner_LanguageGuardMismatchCounted(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)

	task := benchTask()
	task.Request = benchLgEnglishProse

	runner := &Runner{
		ConfigManager: mgr,
		ShapeCopy:     shapeManifest("echo fixture-bench-ok"),
		RunsPerTask:   1,
	}

	// Mismatch: the request is reliably English, the scripted final
	// message is reliably Spanish; the guard regenerates once (the
	// script's second response).
	mismatchMetrics := agent.NewLanguageGuardMetrics()
	t.Cleanup(agent.SetGlobalLanguageGuardMetricsForTest(mismatchMetrics))
	runner.AgentFactory = scriptedFactory(t, mgr, false, benchLgSpanishProse, benchLgEnglishProse2)
	runs, err := runner.RunTask(context.Background(), task, ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask (mismatch run): %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if run.LangChecks != 1 {
		t.Errorf("LangChecks = %d, want 1 (one judged final message)", run.LangChecks)
	}
	if run.LangMismatches != 1 {
		t.Errorf("LangMismatches = %d, want 1 (the Spanish final message mismatches the English request)", run.LangMismatches)
	}
	assertWallTime(t, run)

	// Mirror: a matching-language run judges (a check) but records no
	// mismatch. A second fresh recorder isolates this phase's delta.
	passMetrics := agent.NewLanguageGuardMetrics()
	t.Cleanup(agent.SetGlobalLanguageGuardMetricsForTest(passMetrics))
	runner.AgentFactory = scriptedFactory(t, mgr, false, benchLgEnglishProse2)
	runs, err = runner.RunTask(context.Background(), task, ModelSpec{Model: "bench-model"})
	if err != nil {
		t.Fatalf("RunTask (matching run): %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	run = runs[0]
	if run.LangChecks != 1 {
		t.Errorf("LangChecks = %d, want 1 (the final message was judged)", run.LangChecks)
	}
	if run.LangMismatches != 0 {
		t.Errorf("LangMismatches = %d, want 0 (the final message is in the request's language)", run.LangMismatches)
	}
}
