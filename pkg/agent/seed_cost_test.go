package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestAccumulateResponseCost_UncataloguedModelWarn (rel.3) verifies that a
// pay-per-token response with no provider-reported cost AND no catalog/registry
// pricing trips the once-only warn: the agent's cost tracking is blind for the
// model (cost estimates are $0 and the USD budget guard is a no-op), so the
// user is told. A response that DOES carry a provider-reported cost must NOT
// set the flag (that model is priced; no warning).
func TestAccumulateResponseCost_UncataloguedModelWarn(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	sp := &sproutProvider{agent: a}

	// Precondition: the test model "test:test" is not in the embedded catalog,
	// so an uncatalogued response starts the flag false.
	if a.costPricingUnknown.Load() {
		t.Fatal("flag should start false")
	}

	// Pay-per-token model, no cost fields, no catalog pricing → chargedCost 0 →
	// the warn fires and latches the flag.
	sp.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}})
	if !a.costPricingUnknown.Load() {
		t.Fatalf("expected costPricingUnknown to latch true after an uncatalogued pay-per-token response")
	}

	// Idempotent: a second uncatalogued response keeps it true (no error, no
	// toggle back).
	sp.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{TotalTokens: 15}})
	if !a.costPricingUnknown.Load() {
		t.Fatal("flag must stay true")
	}

	// The Agent runtime path must share the same once-flag (no double warn).
	a.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{TotalTokens: 15}})
	if !a.costPricingUnknown.Load() {
		t.Fatal("agent path must share the latched flag")
	}
}

// TestAccumulateResponseCost_PricedNoWarn verifies the warn is scoped to
// UNCATALOGUED pay-per-token models: a response that carries a provider-
// reported cost (a priced model) must NOT set the flag — cost tracking is
// working, nothing to warn about.
func TestAccumulateResponseCost_PricedNoWarn(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	sp := &sproutProvider{agent: a}

	// A provider-reported cost means the model is priced → no uncatalogued warn.
	sp.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{
		EstimatedCost: 0.01,
		PromptTokens:  100,
		TotalTokens:   150,
	}})
	if a.costPricingUnknown.Load() {
		t.Fatal("a priced (cost-bearing) response must not set the uncatalogued flag")
	}
}

// TestAccumulateResponseCost guards the footer-cost fix: provider-reported
// cost (e.g. DeepInfra's estimated_cost, OpenRouter's cost) must flow into the
// lifetime cost counter the footer reads. seed's loop tracks tokens but never
// cost, so before this propagation the footer always showed $0.
func TestAccumulateResponseCost(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	sp := &sproutProvider{agent: a}

	const eps = 1e-9
	near := func(got, want float64) bool { d := got - want; return d < eps && d > -eps }

	start := a.state.GetTotalCost()

	sp.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{EstimatedCost: 0.0025}})
	if got := a.state.GetTotalCost(); !near(got, start+0.0025) {
		t.Fatalf("after estimated_cost: got %v, want %v", got, start+0.0025)
	}

	// Cost accumulates across calls; actual Cost is preferred over the estimate.
	sp.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{Cost: 0.001, EstimatedCost: 0.999}})
	if got := a.state.GetTotalCost(); !near(got, start+0.0035) {
		t.Fatalf("after cost: got %v, want %v", got, start+0.0035)
	}

	// Zero-cost responses leave the total unchanged.
	sp.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{TotalTokens: 100}})
	if got := a.state.GetTotalCost(); !near(got, start+0.0035) {
		t.Fatalf("zero-cost changed total: got %v", got)
	}

	// Nil guards.
	sp.accumulateResponseCost(nil)
	(&sproutProvider{}).accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{Cost: 1}})
}

// TestAccumulateResponseCost_ImageTokens_SeedProvider verifies that ImageTokens
// flow into the CostEntry and are accumulated in the agent state when using the
// seed provider path.
func TestAccumulateResponseCost_ImageTokens_SeedProvider(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()
	sp := &sproutProvider{agent: a}

	// Initial image tokens should be 0
	if got := a.state.GetImageTokens(); got != 0 {
		t.Fatalf("initial GetImageTokens: want 0, got %d", got)
	}

	// Accumulate with ImageTokens=42
	sp.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{
		PromptTokens:     50,
		CompletionTokens: 30,
		TotalTokens:      80,
		ImageTokens:      42,
	}})

	if got := a.state.GetImageTokens(); got != 42 {
		t.Errorf("after first call: want 42, got %d", got)
	}

	// Accumulate again with ImageTokens=10 — total should be 52
	sp.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
		ImageTokens:      10,
	}})

	if got := a.state.GetImageTokens(); got != 52 {
		t.Errorf("after second call: want 52, got %d", got)
	}

	// Accumulate with ImageTokens=0 — total should NOT change
	sp.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{
		PromptTokens:     5,
		CompletionTokens: 5,
		TotalTokens:      10,
		ImageTokens:      0,
	}})

	if got := a.state.GetImageTokens(); got != 52 {
		t.Errorf("after zero ImageTokens: want 52 (unchanged), got %d", got)
	}
}

// TestAccumulateResponseCost_ImageTokens_AgentRuntime verifies that ImageTokens
// flow into the CostEntry and are accumulated in the agent state when using the
// Agent.accumulateResponseCost path (agent_runtime.go).
func TestAccumulateResponseCost_ImageTokens_AgentRuntime(t *testing.T) {
	a := newIsolatedTestAgent(t)
	defer a.Shutdown()

	// Initial image tokens should be 0
	if got := a.state.GetImageTokens(); got != 0 {
		t.Fatalf("initial GetImageTokens: want 0, got %d", got)
	}

	// Accumulate with ImageTokens=42
	a.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{
		PromptTokens:     50,
		CompletionTokens: 30,
		TotalTokens:      80,
		ImageTokens:      42,
	}})

	if got := a.state.GetImageTokens(); got != 42 {
		t.Errorf("after first call: want 42, got %d", got)
	}

	// Accumulate again with ImageTokens=10 — total should be 52
	a.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
		ImageTokens:      10,
	}})

	if got := a.state.GetImageTokens(); got != 52 {
		t.Errorf("after second call: want 52, got %d", got)
	}

	// Accumulate with ImageTokens=0 — total should NOT change
	a.accumulateResponseCost(&api.ChatResponse{Usage: api.ChatUsage{
		PromptTokens:     5,
		CompletionTokens: 5,
		TotalTokens:      10,
		ImageTokens:      0,
	}})

	if got := a.state.GetImageTokens(); got != 52 {
		t.Errorf("after zero ImageTokens: want 52 (unchanged), got %d", got)
	}

	// Test nil guard
	a.accumulateResponseCost(nil)
	// State should be unchanged after nil call
	if got := a.state.GetImageTokens(); got != 52 {
		t.Errorf("after nil call: want 52 (unchanged), got %d", got)
	}
}
