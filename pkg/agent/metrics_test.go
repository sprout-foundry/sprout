package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// newMetricsTestAgent creates a minimal Agent with state and client initialized for testing metrics.
func newMetricsTestAgent(t *testing.T) *Agent {
	t.Helper()
	a := newMinimalTestAgent(t)

	// Set some initial values for testing
	a.maxIterations = 10

	return a
}

// newMetricsTestAgentWithClient creates an Agent with a mock client for testing TPS metrics.
func newMetricsTestAgentWithClient(t *testing.T) *Agent {
	t.Helper()
	a := newMinimalTestAgent(t)

	// Create a test client
	testClient := &factory.TestClient{}
	a.client = testClient
	a.maxIterations = 10

	return a
}

func TestSetMaxIterations(t *testing.T) {
	t.Parallel()

	t.Run("sets max iterations to positive value", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.SetMaxIterations(50)
		if a.GetMaxIterations() != 50 {
			t.Errorf("expected max iterations 50, got %d", a.GetMaxIterations())
		}
	})

	t.Run("sets max iterations to zero", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.SetMaxIterations(0)
		if a.GetMaxIterations() != 0 {
			t.Errorf("expected max iterations 0, got %d", a.GetMaxIterations())
		}
	})

	t.Run("clamps negative values to zero", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.SetMaxIterations(-5)
		if a.GetMaxIterations() != 0 {
			t.Errorf("expected max iterations clamped to 0, got %d", a.GetMaxIterations())
		}

		a.SetMaxIterations(-100)
		if a.GetMaxIterations() != 0 {
			t.Errorf("expected max iterations clamped to 0, got %d", a.GetMaxIterations())
		}
	})

	t.Run("allows changing from positive to zero", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.SetMaxIterations(20)
		if a.GetMaxIterations() != 20 {
			t.Errorf("expected max iterations 20, got %d", a.GetMaxIterations())
		}

		a.SetMaxIterations(0)
		if a.GetMaxIterations() != 0 {
			t.Errorf("expected max iterations 0, got %d", a.GetMaxIterations())
		}
	})
}

func TestGetMaxIterations(t *testing.T) {
	t.Parallel()

	t.Run("returns initial value", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetMaxIterations() != 10 {
			t.Errorf("expected initial max iterations 10, got %d", a.GetMaxIterations())
		}
	})

	t.Run("returns updated value", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.SetMaxIterations(100)
		if a.GetMaxIterations() != 100 {
			t.Errorf("expected max iterations 100, got %d", a.GetMaxIterations())
		}
	})
}

func TestGetTotalTokens(t *testing.T) {
	t.Parallel()

	t.Run("returns initial zero tokens", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetTotalTokens() != 0 {
			t.Errorf("expected 0 tokens, got %d", a.GetTotalTokens())
		}
	})

	t.Run("returns tracked tokens after updates", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		// Simulate tracking tokens through state
		a.state.SetTotalTokens(1000)
		a.state.SetTotalTokens(2000)

		if a.GetTotalTokens() != 2000 {
			t.Errorf("expected 2000 tokens, got %d", a.GetTotalTokens())
		}
	})
}

func TestGetPromptTokens(t *testing.T) {
	t.Parallel()

	t.Run("returns initial zero tokens", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetPromptTokens() != 0 {
			t.Errorf("expected 0 prompt tokens, got %d", a.GetPromptTokens())
		}
	})

	t.Run("returns tracked prompt tokens", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.state.SetPromptTokens(500)
		a.state.SetPromptTokens(1000)

		if a.GetPromptTokens() != 1000 {
			t.Errorf("expected 1000 prompt tokens, got %d", a.GetPromptTokens())
		}
	})
}

func TestGetCompletionTokens(t *testing.T) {
	t.Parallel()

	t.Run("returns initial zero tokens", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetCompletionTokens() != 0 {
			t.Errorf("expected 0 completion tokens, got %d", a.GetCompletionTokens())
		}
	})

	t.Run("returns tracked completion tokens", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.state.SetCompletionTokens(300)
		a.state.SetCompletionTokens(750)

		if a.GetCompletionTokens() != 750 {
			t.Errorf("expected 750 completion tokens, got %d", a.GetCompletionTokens())
		}
	})
}

func TestGetLLMCallCount(t *testing.T) {
	t.Parallel()

	t.Run("returns initial zero calls", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetLLMCallCount() != 0 {
			t.Errorf("expected 0 LLM calls, got %d", a.GetLLMCallCount())
		}
	})

	t.Run("returns tracked call count", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.state.IncrementLLMCallCount()
		a.state.IncrementLLMCallCount()
		a.state.IncrementLLMCallCount()

		if a.GetLLMCallCount() != 3 {
			t.Errorf("expected 3 LLM calls, got %d", a.GetLLMCallCount())
		}
	})
}

func TestTrackMetricsFromResponse_UsdBudgetWiring(t *testing.T) {
	t.Parallel()

	t.Run("debits cost to attached USD budget and fires warning callback", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		budget := NewFleetUsdBudget(10.0, []float64{0.5, 0.8})
		a.SetFleetUsdBudget(budget)

		var warnings []float64
		a.SetBudgetWarningCallback(func(threshold, spent, limit float64) {
			warnings = append(warnings, threshold)
		})

		// Two responses totaling $6 — crosses the 50% threshold once.
		a.TrackMetricsFromResponse(100, 50, 150, 3.0, 0, 0)
		a.TrackMetricsFromResponse(100, 50, 150, 3.0, 0, 0)

		spent, _ := budget.Snapshot()
		if spent < 5.99 || spent > 6.01 {
			t.Fatalf("expected $6 spent, got %v", spent)
		}
		if len(warnings) != 1 || warnings[0] != 0.5 {
			t.Fatalf("expected one 50%% warning, got %v", warnings)
		}
	})

	t.Run("hitting cap sets the truncation flag and fires exceeded callback", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		budget := NewFleetUsdBudget(5.0, nil)
		a.SetFleetUsdBudget(budget)

		var exceededCalls int
		a.SetBudgetExceededCallback(func(spent, limit float64) {
			exceededCalls++
		})

		a.TrackMetricsFromResponse(100, 50, 150, 6.0, 0, 0)

		if !a.FleetBudgetExceeded() {
			t.Fatalf("FleetBudgetExceeded should be true after USD cap hit")
		}
		if exceededCalls != 1 {
			t.Fatalf("exceeded callback should fire exactly once, got %d", exceededCalls)
		}

		// Subsequent debit should NOT re-fire the exceeded callback.
		a.TrackMetricsFromResponse(100, 50, 150, 1.0, 0, 0)
		if exceededCalls != 1 {
			t.Fatalf("exceeded callback should not re-fire, got %d", exceededCalls)
		}
	})

	t.Run("zero cost responses do not trigger callbacks", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		budget := NewFleetUsdBudget(10.0, []float64{0.5})
		a.SetFleetUsdBudget(budget)

		var warnings int
		a.SetBudgetWarningCallback(func(threshold, spent, limit float64) { warnings++ })

		a.TrackMetricsFromResponse(100, 50, 150, 0, 0, 0)

		if warnings != 0 {
			t.Fatalf("zero-cost response should not fire warnings, got %d", warnings)
		}
		if a.FleetBudgetExceeded() {
			t.Fatalf("zero-cost response should not exceed budget")
		}
	})

	t.Run("no budget attached is a no-op", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		a.TrackMetricsFromResponse(100, 50, 150, 100.0, 0, 0)
		if a.FleetBudgetExceeded() {
			t.Fatalf("no budget should mean no truncation")
		}
	})
}

func TestTrackMetricsFromResponse(t *testing.T) {
	t.Parallel()

	t.Run("updates all token metrics", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.TrackMetricsFromResponse(
			100,  // promptTokens
			50,   // completionTokens
			150,  // totalTokens
			0.05, // estimatedCost
			0,    // cachedTokens
			0,    // cacheWriteTokens
		)

		if a.GetTotalTokens() != 150 {
			t.Errorf("expected total tokens 150, got %d", a.GetTotalTokens())
		}
		if a.GetPromptTokens() != 100 {
			t.Errorf("expected prompt tokens 100, got %d", a.GetPromptTokens())
		}
		if a.GetCompletionTokens() != 50 {
			t.Errorf("expected completion tokens 50, got %d", a.GetCompletionTokens())
		}
	})

	t.Run("updates cost correctly", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 0, 0)
		a.TrackMetricsFromResponse(200, 100, 300, 0.10, 0, 0)

		cost := a.GetTotalCost()
		// Use approximate comparison for floating point
		if cost < 0.149 || cost > 0.151 {
			t.Errorf("expected total cost approx 0.15, got %f", cost)
		}
	})

	t.Run("increments LLM call count", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetLLMCallCount() != 0 {
			t.Errorf("expected initial call count 0, got %d", a.GetLLMCallCount())
		}

		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 0, 0)

		if a.GetLLMCallCount() != 1 {
			t.Errorf("expected call count 1, got %d", a.GetLLMCallCount())
		}

		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 0, 0)

		if a.GetLLMCallCount() != 2 {
			t.Errorf("expected call count 2, got %d", a.GetLLMCallCount())
		}
	})

	t.Run("tracks cached tokens", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 25, 0)
		a.TrackMetricsFromResponse(200, 100, 300, 0.10, 50, 0)

		if a.GetCachedTokens() != 75 {
			t.Errorf("expected 75 cached tokens, got %d", a.GetCachedTokens())
		}
	})

	t.Run("calculates cost savings from cached tokens", func(t *testing.T) {
		// Seed the resolver so the agent has a known cached rate to compute
		// against. The test agent has no real provider/model, so without
		// seeding this would correctly return 0 (no fabrication).
		api.ResetPricingResolver()
		api.SeedPricingForTest("test-provider", "test-model", 0.6, 3.0, 0.06)
		t.Cleanup(api.ResetPricingResolver)

		a := newMetricsTestAgent(t)
		a.state.SetSessionProvider(api.ClientType("test-provider"))
		a.state.SetSessionModel("test-model")

		// With 0.05 cost for 150 tokens and cached=25, savings = 25 * (0.6 - 0.06) / 1e6 = 0.0000135
		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 25, 0)

		savings := a.GetCachedCostSavings()
		if savings <= 0 {
			t.Errorf("expected positive cost savings, got %f", savings)
		}
		// Verify savings are reasonable (should be less than total cost)
		if savings >= 0.05 {
			t.Errorf("expected savings less than total cost 0.05, got %f", savings)
		}
	})

	t.Run("accumulates multiple responses", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		// First response
		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 20, 0)
		// Second response
		a.TrackMetricsFromResponse(200, 100, 300, 0.10, 40, 0)
		// Third response
		a.TrackMetricsFromResponse(50, 25, 75, 0.025, 10, 0)

		if a.GetTotalTokens() != 525 {
			t.Errorf("expected total tokens 525, got %d", a.GetTotalTokens())
		}
		if a.GetPromptTokens() != 350 {
			t.Errorf("expected prompt tokens 350, got %d", a.GetPromptTokens())
		}
		if a.GetCompletionTokens() != 175 {
			t.Errorf("expected completion tokens 175, got %d", a.GetCompletionTokens())
		}
		if a.GetLLMCallCount() != 3 {
			t.Errorf("expected 3 calls, got %d", a.GetLLMCallCount())
		}
		if a.GetCachedTokens() != 70 {
			t.Errorf("expected 70 cached tokens, got %d", a.GetCachedTokens())
		}
	})
}

func TestGetCachedTokens(t *testing.T) {
	t.Parallel()

	t.Run("returns initial zero", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetCachedTokens() != 0 {
			t.Errorf("expected 0 cached tokens, got %d", a.GetCachedTokens())
		}
	})

	t.Run("returns tracked cached tokens", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.state.SetCachedTokens(100)
		a.state.SetCachedTokens(250)

		if a.GetCachedTokens() != 250 {
			t.Errorf("expected 250 cached tokens, got %d", a.GetCachedTokens())
		}
	})
}

func TestGetCachedCostSavings(t *testing.T) {
	t.Parallel()

	t.Run("returns initial zero", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetCachedCostSavings() != 0 {
			t.Errorf("expected 0 cost savings, got %f", a.GetCachedCostSavings())
		}
	})

	t.Run("returns tracked cost savings", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.state.SetCachedCostSavings(0.05)
		a.state.SetCachedCostSavings(0.15)

		if a.GetCachedCostSavings() != 0.15 {
			t.Errorf("expected 0.15 cost savings, got %f", a.GetCachedCostSavings())
		}
	})
}

// TestCalculateCachedTokenSavings verifies the provider-aware cached-token
// savings calculation. The helper estimates how much money was saved by
// prompt-cache hits, but only when the (provider, model) pair resolves to a
// known cached-input rate strictly less than the standard input rate. When
// the rate is unknown or the provider/model can't be resolved, the function
// returns 0 rather than fabricating a number.
func TestCalculateCachedTokenSavings(t *testing.T) {
	t.Parallel()

	t.Run("returns zero when cachedTokens is 0", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		got := a.calculateCachedTokenSavings(0, 150, 0.05)
		if got != 0 {
			t.Errorf("expected 0 savings for 0 cached tokens, got %f", got)
		}
	})

	t.Run("returns zero when totalTokens is 0", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		got := a.calculateCachedTokenSavings(25, 0, 0.05)
		if got != 0 {
			t.Errorf("expected 0 savings for 0 total tokens, got %f", got)
		}
	})

	t.Run("returns zero when estimatedCost is 0", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		got := a.calculateCachedTokenSavings(25, 150, 0)
		if got != 0 {
			t.Errorf("expected 0 savings for 0 cost, got %f", got)
		}
	})

	t.Run("returns zero when estimatedCost is negative", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		got := a.calculateCachedTokenSavings(25, 150, -0.05)
		if got != 0 {
			t.Errorf("expected 0 savings for negative cost, got %f", got)
		}
	})

	// The test agent (newMetricsTestAgent) has no client, so GetProvider()
	// returns "unknown" and GetModel() returns "unknown". ResolveModelPricing
	// cannot resolve that pair, so the function returns 0. This subtest
	// pins the "no fabrication when unknown" behavior — the previous 0.9
	// heuristic has been removed to avoid displaying fake savings numbers.
	t.Run("returns zero for unknown provider/model", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		// Sanity: confirm the provider/model are unknown so the exact
		// branch is genuinely unreachable for this agent.
		if a.GetProvider() != "unknown" || a.GetModel() != "unknown" {
			t.Fatalf("test agent provider/model should be unknown, got %q/%q",
				a.GetProvider(), a.GetModel())
		}

		// cachedTokens=25, totalTokens=150, estimatedCost=0.05
		// Old heuristic: 25 * (0.05/150) * 0.9 = 0.0075
		// New behavior: 0 (no fabrication)
		got := a.calculateCachedTokenSavings(25, 150, 0.05)
		if got != 0 {
			t.Errorf("expected 0 savings for unknown provider/model, got %f", got)
		}
	})

	t.Run("returns zero for large values with unknown provider", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		// Large token counts with a realistic cost, but unknown provider.
		// Old heuristic: 2_000_000 * (100.0/3_000_000) * 0.9 = 60.0
		// New behavior: 0 (no fabrication)
		got := a.calculateCachedTokenSavings(2_000_000, 3_000_000, 100.0)
		if got != 0 {
			t.Errorf("expected 0 savings for unknown provider, got %f", got)
		}
		// Sanity: result must be finite (no NaN/Inf).
		if got != got || got > 1e18 {
			t.Errorf("expected finite savings, got %f", got)
		}
	})
}

// TestCalculateCachedTokenSavings_PricingAware verifies the exact-savings
// branch: when a (provider, model) resolves to a known cached rate strictly
// less than the standard input rate, the function returns the precise
// unrealized cost difference (cachedTokens × (inputPrice − cachedPrice) / 1M).
func TestCalculateCachedTokenSavings_PricingAware(t *testing.T) {
	t.Parallel()

	// Seed the resolver with a model that has a distinct cached rate.
	// DeepSeek-style: input $0.14/M, cached $0.0028/M, output $0.28/M.
	api.ResetPricingResolver()
	api.SeedPricingForTest("deepseek", "deepseek-chat", 0.14, 0.28, 0.0028)
	t.Cleanup(api.ResetPricingResolver)

	// setSessionProviderModel sets the agent's session-scoped provider/model
	// without going through SetProvider (which requires a configManager and
	// would try to create a real client).
	setSessionProviderModel := func(a *Agent, provider, model string) {
		a.state.SetSessionProvider(api.ClientType(provider))
		a.state.SetSessionModel(model)
	}

	t.Run("exact savings when cached rate is known", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		setSessionProviderModel(a, "deepseek", "deepseek-chat")

		// cachedTokens=1000, (input - cached) = 0.14 - 0.0028 = 0.1372 per M
		// savings = 1000 * 0.1372 / 1e6 = 0.0001372
		got := a.calculateCachedTokenSavings(1000, 2000, 0.05)
		want := 1000.0 * (0.14 - 0.0028) / 1e6
		if got < want-1e-9 || got > want+1e-9 {
			t.Errorf("expected exact savings %f, got %f", want, got)
		}
	})

	t.Run("returns zero when cached rate equals input rate", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		// Seed with cached == input to exercise the no-discount branch.
		api.SeedPricingForTest("test-equal", "test-model", 1.0, 2.0, 1.0)
		setSessionProviderModel(a, "test-equal", "test-model")

		// Provider reports cache hits but bills at standard rate.
		got := a.calculateCachedTokenSavings(1000, 2000, 0.05)
		if got != 0 {
			t.Errorf("expected 0 savings when cached rate equals input, got %f", got)
		}
	})
}

func TestGetContextWarningIssued(t *testing.T) {
	t.Parallel()

	t.Run("returns initial false", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetContextWarningIssued() {
			t.Errorf("expected initial false for context warning")
		}
	})

	t.Run("returns tracked state", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.state.SetContextWarningIssued(true)
		if !a.GetContextWarningIssued() {
			t.Errorf("expected true after setting")
		}

		a.state.SetContextWarningIssued(false)
		if a.GetContextWarningIssued() {
			t.Errorf("expected false after unsetting")
		}
	})
}

func TestIsDebugMode(t *testing.T) {
	t.Parallel()

	t.Run("returns false when not in debug mode", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.IsDebugMode() {
			t.Errorf("expected false for non-debug agent")
		}
	})

	t.Run("returns true when debug mode is enabled", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		a.debug = true

		if !a.IsDebugMode() {
			t.Errorf("expected true when debug is enabled")
		}
	})
}

func TestGetLastRunTerminationReason(t *testing.T) {
	t.Parallel()

	t.Run("returns initial empty string", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetLastRunTerminationReason() != "" {
			t.Errorf("expected empty string initially, got '%s'", a.GetLastRunTerminationReason())
		}
	})

	t.Run("returns tracked termination reason", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.state.SetLastRunTerminationReason("completed")
		if a.GetLastRunTerminationReason() != "completed" {
			t.Errorf("expected 'completed', got '%s'", a.GetLastRunTerminationReason())
		}

		a.state.SetLastRunTerminationReason("interrupted")
		if a.GetLastRunTerminationReason() != "interrupted" {
			t.Errorf("expected 'interrupted', got '%s'", a.GetLastRunTerminationReason())
		}
	})
}

func TestGetCurrentIteration(t *testing.T) {
	t.Parallel()

	t.Run("returns initial zero", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetCurrentIteration() != 0 {
			t.Errorf("expected initial iteration 0, got %d", a.GetCurrentIteration())
		}
	})

	t.Run("returns tracked iteration", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.state.SetCurrentIteration(5)
		if a.GetCurrentIteration() != 5 {
			t.Errorf("expected iteration 5, got %d", a.GetCurrentIteration())
		}
	})
}

func TestGetCurrentContextTokens(t *testing.T) {
	t.Parallel()

	t.Run("returns initial zero", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetCurrentContextTokens() != 0 {
			t.Errorf("expected initial context tokens 0, got %d", a.GetCurrentContextTokens())
		}
	})

	t.Run("returns tracked context tokens", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.state.SetCurrentContextTokens(5000)
		if a.GetCurrentContextTokens() != 5000 {
			t.Errorf("expected context tokens 5000, got %d", a.GetCurrentContextTokens())
		}
	})
}

func TestGetMaxContextTokens(t *testing.T) {
	t.Parallel()

	t.Run("returns value from client", func(t *testing.T) {
		a := newMetricsTestAgentWithClient(t)

		maxTokens := a.GetMaxContextTokens()
		// TestClient likely returns 0 or a default value
		// Just verify it doesn't panic and is non-negative
		if maxTokens < 0 {
			t.Errorf("expected non-negative max context tokens, got %d", maxTokens)
		}
	})
}

func TestGetEstimatedTokenResponses(t *testing.T) {
	t.Parallel()

	t.Run("returns initial zero", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetEstimatedTokenResponses() != 0 {
			t.Errorf("expected initial estimated responses 0, got %d", a.GetEstimatedTokenResponses())
		}
	})

	t.Run("returns tracked estimated responses", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.state.SetEstimatedTokenResponses(3)
		a.state.SetEstimatedTokenResponses(5)

		if a.GetEstimatedTokenResponses() != 5 {
			t.Errorf("expected 5 estimated responses, got %d", a.GetEstimatedTokenResponses())
		}
	})
}

func TestMarkEstimatedTokenUsageResponse(t *testing.T) {
	t.Parallel()

	t.Run("increments estimated token response count", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.MarkEstimatedTokenUsageResponse()
		if a.GetEstimatedTokenResponses() != 1 {
			t.Errorf("expected 1 estimated response, got %d", a.GetEstimatedTokenResponses())
		}

		a.MarkEstimatedTokenUsageResponse()
		a.MarkEstimatedTokenUsageResponse()
		if a.GetEstimatedTokenResponses() != 3 {
			t.Errorf("expected 3 estimated responses, got %d", a.GetEstimatedTokenResponses())
		}
	})
}

func TestGetLastTPS(t *testing.T) {
	t.Parallel()

	t.Run("returns zero when client is nil", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		a.client = nil

		if a.GetLastTPS() != 0.0 {
			t.Errorf("expected 0.0 when client is nil, got %f", a.GetLastTPS())
		}
	})

	t.Run("returns value from test client", func(t *testing.T) {
		a := newMetricsTestAgentWithClient(t)

		tps := a.GetLastTPS()
		// Test client should return some value (likely 0 or a test value)
		if tps < 0 {
			t.Errorf("expected non-negative TPS, got %f", tps)
		}
	})
}

func TestGetCurrentTPS(t *testing.T) {
	t.Parallel()

	t.Run("is alias for GetLastTPS", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		lastTPS := a.GetLastTPS()
		currentTPS := a.GetCurrentTPS()

		if lastTPS != currentTPS {
			t.Errorf("expected GetCurrentTPS to equal GetLastTPS, got %f vs %f", currentTPS, lastTPS)
		}
	})
}

func TestGetAverageTPS(t *testing.T) {
	t.Parallel()

	t.Run("returns zero when client is nil", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		a.client = nil

		if a.GetAverageTPS() != 0.0 {
			t.Errorf("expected 0.0 when client is nil, got %f", a.GetAverageTPS())
		}
	})

	t.Run("returns value from test client", func(t *testing.T) {
		a := newMetricsTestAgentWithClient(t)

		avgTPS := a.GetAverageTPS()
		// Test client should return some value
		if avgTPS < 0 {
			t.Errorf("expected non-negative average TPS, got %f", avgTPS)
		}
	})
}

func TestGetTPSStats(t *testing.T) {
	t.Parallel()

	t.Run("returns empty map when client is nil", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		a.client = nil

		stats := a.GetTPSStats()
		if stats == nil {
			t.Errorf("expected map, got nil")
		}
		if len(stats) != 0 {
			t.Errorf("expected empty map, got %d entries", len(stats))
		}
	})

	t.Run("returns stats from test client", func(t *testing.T) {
		a := newMetricsTestAgentWithClient(t)

		stats := a.GetTPSStats()
		if stats == nil {
			t.Errorf("expected map, got nil")
		}
		// Test client may return empty map or some test stats
		if stats != nil && len(stats) > 100 {
			t.Errorf("expected reasonable number of stats, got %d", len(stats))
		}
	})
}
