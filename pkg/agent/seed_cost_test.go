package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

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
