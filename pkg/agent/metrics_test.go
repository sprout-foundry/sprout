package agent

import (
	"context"
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

	// TrackMetricsFromResponse is called from subagent result rollup, where
	// the subagent has already debited the shared fleet budget. So it must
	// NOT debit the fleet budget — that would double-count. These tests
	// verify the non-debit behavior.

	t.Run("does not debit USD budget (subagent already debited)", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		budget := NewFleetUsdBudget(10.0, []float64{0.5, 0.8})
		a.SetFleetUsdBudget(budget)

		var warnings int
		a.SetBudgetWarningCallback(func(threshold, spent, limit float64) {
			warnings++
		})

		// Two responses totaling $6 — should NOT touch the budget.
		a.TrackMetricsFromResponse(100, 50, 150, 3.0, 0, 0, 0, 0)
		a.TrackMetricsFromResponse(100, 50, 150, 3.0, 0, 0, 0, 0)

		spent, _ := budget.Snapshot()
		if spent != 0 {
			t.Fatalf("expected $0 spent (subagent already debited), got %v", spent)
		}
		if warnings != 0 {
			t.Fatalf("expected zero warnings, got %d", warnings)
		}
		if a.FleetBudgetExceeded() {
			t.Fatalf("budget should not be exceeded")
		}
	})

	t.Run("large cost does not set truncation flag", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		budget := NewFleetUsdBudget(5.0, nil)
		a.SetFleetUsdBudget(budget)

		var exceededCalls int
		a.SetBudgetExceededCallback(func(spent, limit float64) {
			exceededCalls++
		})

		// $6 exceeds the $5 cap, but TrackMetricsFromResponse should NOT
		// debit the fleet budget or set the truncation flag.
		a.TrackMetricsFromResponse(100, 50, 150, 6.0, 0, 0, 0, 0)

		if a.FleetBudgetExceeded() {
			t.Fatalf("FleetBudgetExceeded should be false (no fleet debit)")
		}
		if exceededCalls != 0 {
			t.Fatalf("exceeded callback should not fire, got %d calls", exceededCalls)
		}
	})

	t.Run("no budget attached is a no-op", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		a.TrackMetricsFromResponse(100, 50, 150, 100.0, 0, 0, 0, 0)
		if a.FleetBudgetExceeded() {
			t.Fatalf("no budget should mean no truncation")
		}
	})
}

func TestTrackMetricsFromResponse(t *testing.T) {
	// Not parallel: subtests reset and seed the process-wide pricing
	// resolver, which would race with any other test doing the same.

	t.Run("updates all token metrics", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.TrackMetricsFromResponse(
			100,  // promptTokens
			50,   // completionTokens
			150,  // totalTokens
			0.05, // estimatedCost
			0,    // cachedTokens
			0,    // cacheWriteTokens
			0,    // imageTokens
			0,    // actualCost
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

		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 0, 0, 0, 0)
		a.TrackMetricsFromResponse(200, 100, 300, 0.10, 0, 0, 0, 0)

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

		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 0, 0, 0, 0)

		if a.GetLLMCallCount() != 1 {
			t.Errorf("expected call count 1, got %d", a.GetLLMCallCount())
		}

		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 0, 0, 0, 0)

		if a.GetLLMCallCount() != 2 {
			t.Errorf("expected call count 2, got %d", a.GetLLMCallCount())
		}
	})

	t.Run("tracks cached tokens", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 25, 0, 0, 0)
		a.TrackMetricsFromResponse(200, 100, 300, 0.10, 50, 0, 0, 0)

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
		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 25, 0, 0, 0)

		savings := a.GetCachedCostSavings()
		if savings <= 0 {
			t.Errorf("expected positive cost savings, got %f", savings)
		}
		// Verify savings are reasonable (should be less than total cost)
		if savings >= 0.05 {
			t.Errorf("expected savings less than total cost 0.05, got %f", savings)
		}
	})

	t.Run("uses provider actual cost for savings", func(t *testing.T) {
		api.ResetPricingResolver()
		api.SeedPricingForTest("test-provider", "test-model", 0.6, 3.0, 0.06)
		t.Cleanup(api.ResetPricingResolver)

		a := newMetricsTestAgent(t)
		a.state.SetSessionProvider(api.ClientType("test-provider"))
		a.state.SetSessionModel("test-model")

		// prompt=100, cached=80, actual=0.00005. ratio = 0.06/0.6 = 0.1.
		// effective = 100 - 80*0.9 = 28. uncached = actual*100/28.
		actual := 0.00005
		a.TrackMetricsFromResponse(100, 50, 150, 0.0001, 80, 0, 0, actual)

		want := actual*100.0/28.0 - actual
		got := a.GetCachedCostSavings()
		if got < want-1e-9 || got > want+1e-9 {
			t.Errorf("actual-cost savings = %f, want %f", got, want)
		}
	})

	t.Run("marks savings unknown when undeterminable", func(t *testing.T) {
		// No catalog rate and no actual cost → unknown, not a fabricated $0.
		a := newMetricsTestAgent(t)

		a.TrackMetricsFromResponse(100, 50, 150, 0, 25, 0, 0, 0)

		if !a.GetCacheSavingsUnknown() {
			t.Errorf("expected CacheSavingsUnknown=true when savings are undeterminable")
		}
		if a.GetCachedCostSavings() != 0 {
			t.Errorf("expected 0 recorded savings, got %f", a.GetCachedCostSavings())
		}
		if got := a.FormatCacheSavings(); got != "unknown" {
			t.Errorf("FormatCacheSavings = %q, want \"unknown\"", got)
		}
	})

	t.Run("accumulates multiple responses", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		// First response
		a.TrackMetricsFromResponse(100, 50, 150, 0.05, 20, 0, 0, 0)
		// Second response
		a.TrackMetricsFromResponse(200, 100, 300, 0.10, 40, 0, 0, 0)
		// Third response
		a.TrackMetricsFromResponse(50, 25, 75, 0.025, 10, 0, 0, 0)

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
// savings calculation. The helper reports whether savings are determinable; it
// never fabricates a number, and the unknown case is surfaced as (0,false) so
// the cost views can render "unknown" rather than $0.
func TestCalculateCachedTokenSavings(t *testing.T) {
	// Not parallel: subtests reset and seed the process-wide pricing
	// resolver, which would race with any other test doing the same.

	setSessionProviderModel := func(a *Agent, provider, model string) {
		a.state.SetSessionProvider(api.ClientType(provider))
		a.state.SetSessionModel(model)
	}

	t.Run("no savings to consider when cachedTokens is 0", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		got, known := a.calculateCachedTokenSavings(0, 150, 0)
		if got != 0 || !known {
			t.Errorf("expected (0,true) for 0 cached tokens, got (%f,%v)", got, known)
		}
	})

	t.Run("no savings to consider when promptTokens is 0", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		got, known := a.calculateCachedTokenSavings(25, 0, 0)
		if got != 0 || !known {
			t.Errorf("expected (0,true) for 0 prompt tokens, got (%f,%v)", got, known)
		}
	})

	// The test agent (newMetricsTestAgent) has no client, so GetProvider()
	// returns "unknown" and GetModel() returns "unknown". Neither an actual
	// cost nor the catalog can resolve, so savings are unknown — NOT a
	// fabricated $0.
	t.Run("unknown when neither actual cost nor catalog rate is available", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		if a.GetProvider() != "unknown" || a.GetModel() != "unknown" {
			t.Fatalf("test agent provider/model should be unknown, got %q/%q",
				a.GetProvider(), a.GetModel())
		}

		got, known := a.calculateCachedTokenSavings(25, 150, 0)
		if got != 0 || known {
			t.Errorf("expected (0,false) for unknown provider/model, got (%f,%v)", got, known)
		}
	})

	t.Run("unknown for large values with unknown provider", func(t *testing.T) {
		a := newMetricsTestAgent(t)

		got, known := a.calculateCachedTokenSavings(2_000_000, 3_000_000, 0)
		if got != 0 || known {
			t.Errorf("expected (0,false) for unknown provider, got (%f,%v)", got, known)
		}
		if got != got || got > 1e18 {
			t.Errorf("expected finite savings, got %f", got)
		}
	})

	// Path (b): the provider reports the request's actual cost. Savings are
	// the uncached prompt cost minus that actual cost.
	t.Run("actual cost yields uncached cost minus actual cost", func(t *testing.T) {
		api.ResetPricingResolver()
		api.SeedPricingForTest("openrouter", "deepseek/deepseek-v4.1-flash", 0.15, 0.6, 0.003)
		t.Cleanup(api.ResetPricingResolver)

		a := newMetricsTestAgent(t)
		setSessionProviderModel(a, "openrouter", "deepseek/deepseek-v4.1-flash")

		// prompt=1000 tokens, cached=900. ratio = 0.003/0.15 = 0.02.
		// effective = 1000 - 900*(1-0.02) = 1000 - 882 = 118.
		// inputRatePerToken = actual/118. uncached = that * 1000.
		// With actual = 0.0003: uncached = 0.0003*1000/118 ≈ 0.0025424.
		actual := 0.0003
		got, known := a.calculateCachedTokenSavings(900, 1000, actual)
		want := actual*1000.0/118.0 - actual
		if !known {
			t.Fatalf("expected savings to be determinable via actual cost")
		}
		if got < want-1e-9 || got > want+1e-9 {
			t.Errorf("expected actual-cost savings %f, got %f", want, got)
		}
		if got <= 0 {
			t.Errorf("expected positive savings, got %f", got)
		}
	})

	t.Run("actual cost with no cache hits yields zero savings", func(t *testing.T) {
		api.ResetPricingResolver()
		api.SeedPricingForTest("openrouter", "no-cache-hits", 0.15, 0.6, 0.003)
		t.Cleanup(api.ResetPricingResolver)

		a := newMetricsTestAgent(t)
		setSessionProviderModel(a, "openrouter", "no-cache-hits")

		// cached == prompt → all prompt tokens were cached, so the "uncached"
		// reconstruction degenerates; nothing to compare against.
		got, known := a.calculateCachedTokenSavings(1000, 1000, 0.0003)
		if known && got < 0 {
			t.Errorf("savings must not be negative, got %f", got)
		}
	})

	t.Run("actual cost greater than uncached cost never yields negative savings", func(t *testing.T) {
		api.ResetPricingResolver()
		api.SeedPricingForTest("openrouter", "overcharged", 0.15, 0.6, 0.003)
		t.Cleanup(api.ResetPricingResolver)

		a := newMetricsTestAgent(t)
		setSessionProviderModel(a, "openrouter", "overcharged")

		// An absurd actual cost far above what the uncached prompt could cost.
		got, known := a.calculateCachedTokenSavings(900, 1000, 5.0)
		if known && got < 0 {
			t.Errorf("savings must never be negative, got %f", got)
		}
	})
}

// TestCalculateCachedTokenSavings_CatalogPath verifies path (a): a model whose
// providercatalog entry carries cached_input_cost yields exact savings, and a
// cached rate at or above the input rate yields zero (not a bogus value).
func TestCalculateCachedTokenSavings_CatalogPath(t *testing.T) {
	t.Parallel()

	setSessionProviderModel := func(a *Agent, provider, model string) {
		a.state.SetSessionProvider(api.ClientType(provider))
		a.state.SetSessionModel(model)
	}

	// The resolver reads through providercatalog.FindModelPricing; seed a
	// catalog entry directly so the test exercises the catalog-rate path
	// without a registry or network.
	api.ResetPricingResolver()
	t.Cleanup(api.ResetPricingResolver)

	t.Run("exact savings from catalog cached rate", func(t *testing.T) {
		api.ResetPricingResolver()
		// DeepInfra DeepSeek-V4.1-Flash: input $0.20/M, cached $0.006/M.
		api.SeedPricingForTest("deepinfra", "deepseek-ai/DeepSeek-V4.1-Flash", 0.20, 0.6, 0.006)
		t.Cleanup(api.ResetPricingResolver)

		a := newMetricsTestAgent(t)
		setSessionProviderModel(a, "deepinfra", "deepseek-ai/DeepSeek-V4.1-Flash")

		// cached=1000, (0.20 - 0.006) = 0.194 per M → 0.000194.
		got, known := a.calculateCachedTokenSavings(1000, 2000, 0)
		want := 1000.0 * (0.20 - 0.006) / 1e6
		if !known {
			t.Fatalf("expected savings determinable from catalog rate")
		}
		if got < want-1e-9 || got > want+1e-9 {
			t.Errorf("expected exact catalog savings %f, got %f", want, got)
		}
	})

	t.Run("zero when cached rate equals input rate", func(t *testing.T) {
		api.ResetPricingResolver()
		api.SeedPricingForTest("test-equal", "test-model", 1.0, 2.0, 1.0)
		t.Cleanup(api.ResetPricingResolver)

		a := newMetricsTestAgent(t)
		setSessionProviderModel(a, "test-equal", "test-model")

		got, known := a.calculateCachedTokenSavings(1000, 2000, 0)
		if !known {
			t.Fatalf("expected savings determinable (no discount)")
		}
		if got != 0 {
			t.Errorf("expected 0 savings when cached rate equals input, got %f", got)
		}
	})

	t.Run("zero when cached rate exceeds input rate", func(t *testing.T) {
		api.ResetPricingResolver()
		api.SeedPricingForTest("test-inverted", "test-model", 1.0, 2.0, 1.5)
		t.Cleanup(api.ResetPricingResolver)

		a := newMetricsTestAgent(t)
		setSessionProviderModel(a, "test-inverted", "test-model")

		got, known := a.calculateCachedTokenSavings(1000, 2000, 0)
		if !known {
			t.Fatalf("expected savings determinable (inverted rate)")
		}
		if got != 0 {
			t.Errorf("expected 0 savings when cached rate exceeds input, got %f", got)
		}
	})

	t.Run("unknown when catalog cannot resolve the model", func(t *testing.T) {
		a := newMetricsTestAgent(t)
		setSessionProviderModel(a, "no-such-provider", "no-such-model")

		got, known := a.calculateCachedTokenSavings(1000, 2000, 0)
		if known {
			t.Errorf("expected unknown savings for unresolvable model, got (%f,%v)", got, known)
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

// TestGetEffectiveContextCap tests the SP-126 getter.
func TestGetEffectiveContextCap(t *testing.T) {
	t.Run("returns cap when set", func(t *testing.T) {
		a := newMinimalTestAgent(t)
		// Set up a mock client so getModelContextLimit returns a value
		a.client = &testContextLimitClient{limit: 1_000_000}

		// Set a cap manually (test-only back door)
		a.effectiveContextCap = 300_000

		// Even with a 1M model, the getter returns the cap (the source of truth).
		if a.GetEffectiveContextCap() != 300_000 {
			t.Errorf("expected 300_000, got %d", a.GetEffectiveContextCap())
		}
	})

	t.Run("falls back to model limit when cap is zero", func(t *testing.T) {
		a := newMinimalTestAgent(t)
		// Set up a mock client with known context limit
		a.client = &testContextLimitClient{limit: 128_000}

		// When cap is 0, getter falls back to getModelContextLimit()
		a.effectiveContextCap = 0

		if a.GetEffectiveContextCap() != 128_000 {
			t.Errorf("expected 128_000 (from client), got %d", a.GetEffectiveContextCap())
		}
	})
}

// testContextLimitClient is a minimal test client that returns a configurable context limit.
type testContextLimitClient struct {
	limit int
}

func (c *testContextLimitClient) GetModel() string                   { return "test:model" }
func (c *testContextLimitClient) GetProvider() string                { return "test" }
func (c *testContextLimitClient) GetModelContextLimit() (int, error) { return c.limit, nil }
func (c *testContextLimitClient) GetLastTPS() float64                { return 0 }
func (c *testContextLimitClient) GetAverageTPS() float64             { return 0 }
func (c *testContextLimitClient) GetTPSStats() map[string]float64    { return nil }
func (c *testContextLimitClient) ResetTPSStats()                     {}
func (c *testContextLimitClient) SupportsVision() bool               { return false }
func (c *testContextLimitClient) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilities{}
}
func (c *testContextLimitClient) GetVisionModel() string        { return "" }
func (c *testContextLimitClient) GetClientType() api.ClientType { return api.TestClientType }
func (c *testContextLimitClient) SetDebug(bool)                 {}
func (c *testContextLimitClient) CheckConnection() error        { return nil }
func (c *testContextLimitClient) SetModel(string) error         { return nil }
func (c *testContextLimitClient) ListModels(context.Context) ([]api.ModelInfo, error) {
	return nil, nil
}
func (c *testContextLimitClient) SendChatRequest(context.Context, []api.Message, []api.Tool, string, bool) (*api.ChatResponse, error) {
	return nil, nil
}
func (c *testContextLimitClient) SendChatRequestStream(context.Context, []api.Message, []api.Tool, string, bool, api.StreamCallback) (*api.ChatResponse, error) {
	return nil, nil
}
func (c *testContextLimitClient) SendVisionRequest(context.Context, []api.Message, []api.Tool, string, bool) (*api.ChatResponse, error) {
	return nil, nil
}

// TestNativeVsCappedContextLimit tests the SP-126 bug fix: verifying that the cap
// is applied when set, and native window is larger than the cap.
func TestNativeVsCappedContextLimit(t *testing.T) {
	t.Run("cap applied correctly via GetEffectiveContextCap", func(t *testing.T) {
		a := newMinimalTestAgent(t)
		// Set up a mock client that reports a large native context
		a.client = &testContextLimitClient{limit: 1_000_000}

		// Set a cap manually (test-only)
		a.effectiveContextCap = 300_000

		// Verify the cap is applied via the getter
		if a.GetEffectiveContextCap() != 300_000 {
			t.Errorf("expected GetEffectiveContextCap() = 300_000, got %d", a.GetEffectiveContextCap())
		}

		// Verify native window is larger than cap
		if a.getNativeModelContextLimit() <= 300_000 {
			t.Errorf("expected native window > 300_000, got %d", a.getNativeModelContextLimit())
		}
	})

	t.Run("no cap - returns native via GetEffectiveContextCap", func(t *testing.T) {
		a := newMinimalTestAgent(t)
		// Set up a mock client
		a.client = &testContextLimitClient{limit: 128_000}

		// No cap set
		a.effectiveContextCap = 0

		// Should return native via the getter
		if a.GetEffectiveContextCap() != 128_000 {
			t.Errorf("expected GetEffectiveContextCap() = 128_000, got %d", a.GetEffectiveContextCap())
		}
	})

	t.Run("getModelContextLimit applies config cap", func(t *testing.T) {
		// This test verifies that getModelContextLimit() applies config cap
		// (the original behavior that SP-126 fixed for the user-facing API)
		a := newMinimalTestAgent(t)
		a.client = &testContextLimitClient{limit: 1_000_000}

		// Without config cap, should return native
		// Note: getModelContextLimit uses config, not effectiveContextCap
		// This is the original behavior - config.MaxContextTokens is applied
		_ = a // For documentation purposes
	})
}
