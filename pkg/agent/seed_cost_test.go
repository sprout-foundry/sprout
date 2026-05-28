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
