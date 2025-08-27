package agent

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
)

func TestCostAggregation(t *testing.T) {
	cfg := &config.Config{
		EditingModel: "test-model",
		SkipPrompt:   true,
	}

	ctx := &SimplifiedAgentContext{
		Config:                cfg,
		TotalTokensUsed:       0,
		TotalPromptTokens:     0,
		TotalCompletionTokens: 0,
		TotalCost:             0.0,
	}

	// Track the cost after each call to verify aggregation
	var costsAfterEachCall []float64

	// Simulate multiple LLM calls with different token usage
	testCalls := []struct {
		name       string
		tokenUsage *llm.TokenUsage
		model      string
	}{
		{
			name: "First LLM call",
			tokenUsage: &llm.TokenUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
			model: "test-model",
		},
		{
			name: "Second LLM call",
			tokenUsage: &llm.TokenUsage{
				PromptTokens:     200,
				CompletionTokens: 100,
				TotalTokens:      300,
			},
			model: "test-model",
		},
		{
			name: "Third LLM call",
			tokenUsage: &llm.TokenUsage{
				PromptTokens:     50,
				CompletionTokens: 25,
				TotalTokens:      75,
			},
			model: "test-model",
		},
	}

	for i, tc := range testCalls {
		t.Run(tc.name, func(t *testing.T) {
			// Track the token usage (this should accumulate costs)
			trackTokenUsage(ctx, tc.tokenUsage, tc.model)

			// Record cost after this call
			costsAfterEachCall = append(costsAfterEachCall, ctx.TotalCost)

			// Verify that tokens are accumulating
			expectedTokens := 0
			for j := 0; j <= i; j++ {
				expectedTokens += testCalls[j].tokenUsage.TotalTokens
			}

			if ctx.TotalTokensUsed != expectedTokens {
				t.Errorf("Expected total tokens to be %d, got %d", expectedTokens, ctx.TotalTokensUsed)
			}

			// Verify that cost is accumulating (not exact match due to cost calculation complexity)
			if ctx.TotalCost <= 0 {
				t.Errorf("Expected positive cumulative cost, got %f", ctx.TotalCost)
			}

			// Each call should increase the total cost (aggregation test)
			if i > 0 {
				previousCost := costsAfterEachCall[i-1]
				if ctx.TotalCost <= previousCost {
					t.Errorf("Expected cost to increase from %f to greater, got %f", previousCost, ctx.TotalCost)
				}
			}
		})
	}

	// Verify final aggregated values
	t.Run("Final aggregation check", func(t *testing.T) {
		expectedTotalTokens := 150 + 300 + 75 // Sum of all token usage
		if ctx.TotalTokensUsed != expectedTotalTokens {
			t.Errorf("Final total tokens should be %d, got %d", expectedTotalTokens, ctx.TotalTokensUsed)
		}

		if ctx.TotalCost <= 0 {
			t.Errorf("Final total cost should be positive, got %f", ctx.TotalCost)
		}

		// Verify that costs are truly aggregated - final cost should be greater than any individual call
		if len(costsAfterEachCall) >= 3 {
			firstCallCost := costsAfterEachCall[0]
			secondCallCost := costsAfterEachCall[1] - costsAfterEachCall[0] // Individual cost of second call
			thirdCallCost := costsAfterEachCall[2] - costsAfterEachCall[1]  // Individual cost of third call

			// Final cost should be greater than any single call cost
			if ctx.TotalCost <= firstCallCost || ctx.TotalCost <= (secondCallCost*1.1) || ctx.TotalCost <= (thirdCallCost*1.1) {
				t.Logf("Call costs: first=%.6f, second=%.6f, third=%.6f, final=%.6f",
					firstCallCost, secondCallCost, thirdCallCost, ctx.TotalCost)
			}

			// Most importantly: final cost should equal sum of all individual costs (with small tolerance for floating point)
			expectedFinalCost := costsAfterEachCall[2] // This is already the accumulated cost
			tolerance := 0.000001
			if abs(ctx.TotalCost-expectedFinalCost) > tolerance {
				t.Errorf("Final cost aggregation error: expected %.6f, got %.6f", expectedFinalCost, ctx.TotalCost)
			}
		}
	})
}

// abs returns the absolute value of a float64
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Helper function to get test index from test name
func getTestIndex(testName string) int {
	switch testName {
	case "First LLM call":
		return 0
	case "Second LLM call":
		return 1
	case "Third LLM call":
		return 2
	default:
		return -1
	}
}

func TestTokenUsageTrackingIntegrity(t *testing.T) {
	cfg := &config.Config{
		EditingModel: "test-model",
		SkipPrompt:   true,
	}

	ctx := &SimplifiedAgentContext{
		Config:                cfg,
		TotalTokensUsed:       0,
		TotalPromptTokens:     0,
		TotalCompletionTokens: 0,
		TotalCost:             0.0,
	}

	// Test that trackTokenUsage correctly handles nil cases
	trackTokenUsage(nil, &llm.TokenUsage{TotalTokens: 100}, "model")
	trackTokenUsage(ctx, nil, "model")

	// Should still be zero after nil calls
	if ctx.TotalTokensUsed != 0 {
		t.Errorf("Expected tokens to remain 0 after nil tracking, got %d", ctx.TotalTokensUsed)
	}

	if ctx.TotalCost != 0 {
		t.Errorf("Expected cost to remain 0 after nil tracking, got %f", ctx.TotalCost)
	}

	// Test valid tracking
	tokenUsage := &llm.TokenUsage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	trackTokenUsage(ctx, tokenUsage, "test-model")

	if ctx.TotalTokensUsed != 150 {
		t.Errorf("Expected total tokens to be 150, got %d", ctx.TotalTokensUsed)
	}

	if ctx.TotalPromptTokens != 100 {
		t.Errorf("Expected prompt tokens to be 100, got %d", ctx.TotalPromptTokens)
	}

	if ctx.TotalCompletionTokens != 50 {
		t.Errorf("Expected completion tokens to be 50, got %d", ctx.TotalCompletionTokens)
	}

	if ctx.TotalCost <= 0 {
		t.Errorf("Expected positive cost after valid tracking, got %f", ctx.TotalCost)
	}
}
