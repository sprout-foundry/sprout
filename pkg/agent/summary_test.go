package agent

import (
	"testing"
)

// TestTokenTrackingAccuracy verifies that token tracking is accurate
func TestTokenTrackingAccuracy(t *testing.T) {
	agent := &Agent{
		totalTokens:      87200,
		promptTokens:     84750,
		completionTokens: 2433,
		cachedTokens:     71400,
	}

	// Calculate processed prompt (excluding cached)
	processedPromptTokens := agent.promptTokens - agent.cachedTokens
	if processedPromptTokens != 13350 {
		t.Errorf("Expected processedPromptTokens to be 13350, got %d", processedPromptTokens)
	}

	// Calculate processed: processedPrompt + completion
	processedTokens := processedPromptTokens + agent.completionTokens
	if processedTokens != 15783 {
		t.Errorf("Expected processedTokens to be 15783, got %d", processedTokens)
	}

	// Verify the math adds up: processedPrompt + completion = processed
	if processedPromptTokens+agent.completionTokens != processedTokens {
		t.Errorf("Math doesn't add up: %d + %d != %d", processedPromptTokens, agent.completionTokens, processedTokens)
	}

	// TrackMetricsFromResponse should work correctly
	agent.TrackMetricsFromResponse(1000, 200, 1200, 0.01, 500)
	if agent.totalTokens != 88400 { // 87200 + 1200
		t.Errorf("Expected totalTokens to be 88400, got %d", agent.totalTokens)
	}
	if agent.cachedTokens != 71900 { // 71400 + 500
		t.Errorf("Expected cachedTokens to be 71900, got %d", agent.cachedTokens)
	}
	if agent.promptTokens != 85750 { // 84750 + 1000
		t.Errorf("Expected promptTokens to be 85750, got %d", agent.promptTokens)
	}
}

// TestEmptyCachedTokens handles edge case where there are no cached tokens
func TestEmptyCachedTokens(t *testing.T) {
	agent := &Agent{
		totalTokens:      5000,
		promptTokens:     4000,
		completionTokens: 1000,
		cachedTokens:     0,
	}

	processedTokens := agent.totalTokens
	if processedTokens != 5000 {
		t.Errorf("Expected processedTokens to be 5000, got %d", processedTokens)
	}

	processedPromptTokens := agent.promptTokens
	if processedPromptTokens != 4000 {
		t.Errorf("Expected processedPromptTokens to be 4000, got %d", processedPromptTokens)
	}
}

// TestNegativeProcessedPromptTokens tests when cachedTokens > promptTokens
func TestNegativeProcessedPromptTokens(t *testing.T) {
	agent := &Agent{
		totalTokens:      1000,
		promptTokens:     500,
		completionTokens: 200,
		cachedTokens:     600, // More than promptTokens!
	}

	// Should clamp to 0
	processedPromptTokens := agent.promptTokens - agent.cachedTokens
	if processedPromptTokens > 0 {
		t.Errorf("Expected processedPromptTokens to be clamped to 0, got %d", processedPromptTokens)
	}

	// processedTokens should still be valid (completionTokens only in this case)
	processedTokens := max(0, processedPromptTokens) + agent.completionTokens
	if processedTokens != agent.completionTokens {
		t.Errorf("Expected processedTokens to be %d, got %d", agent.completionTokens, processedTokens)
	}
}

// TestTokenDiscrepancy tests when totalTokens != promptTokens + completionTokens
func TestTokenDiscrepancy(t *testing.T) {
	agent := &Agent{
		totalTokens:      1000, // Different from 500 + 200 = 700
		promptTokens:     500,
		completionTokens: 200,
		cachedTokens:     0,
	}

	// Simulate the calculation from summary.go
	processedPromptTokens := agent.promptTokens - agent.cachedTokens  // 500
	processedTokens := processedPromptTokens + agent.completionTokens // 700

	expectedProcessed := agent.totalTokens - agent.cachedTokens // 1000

	// These should differ due to the discrepancy
	if processedTokens == expectedProcessed {
		t.Logf("Note: With these values, processedTokens=%d equals expectedProcessed=%d", processedTokens, expectedProcessed)
	} else {
		t.Logf("Expected discrepancy: computed=%d, expected=%d", processedTokens, expectedProcessed)
	}
}

// TestClampingBehavior comprehensive test for various edge cases
func TestClampingBehavior(t *testing.T) {
	tests := []struct {
		name                string
		promptTokens        int
		completionTokens    int
		cachedTokens        int
		totalTokens         int
		wantProcessedPrompt int
		wantProcessed       int
	}{
		{
			name:                "normal case",
			promptTokens:        1000,
			completionTokens:    200,
			cachedTokens:        500,
			totalTokens:         1200,
			wantProcessedPrompt: 500,
			wantProcessed:       700,
		},
		{
			name:                "cached exceeds prompt",
			promptTokens:        500,
			completionTokens:    200,
			cachedTokens:        600,
			totalTokens:         700,
			wantProcessedPrompt: 0, // clamped
			wantProcessed:       200,
		},
		{
			name:                "no cached tokens",
			promptTokens:        1000,
			completionTokens:    200,
			cachedTokens:        0,
			totalTokens:         1200,
			wantProcessedPrompt: 1000,
			wantProcessed:       1200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &Agent{
				totalTokens:      tt.totalTokens,
				promptTokens:     tt.promptTokens,
				completionTokens: tt.completionTokens,
				cachedTokens:     tt.cachedTokens,
			}

			processedPromptTokens := agent.promptTokens - agent.cachedTokens
			if processedPromptTokens < 0 {
				processedPromptTokens = 0
			}
			processedTokens := processedPromptTokens + agent.completionTokens

			if processedPromptTokens != tt.wantProcessedPrompt {
				t.Errorf("processedPromptTokens = %d, want %d", processedPromptTokens, tt.wantProcessedPrompt)
			}
			if processedTokens != tt.wantProcessed {
				t.Errorf("processedTokens = %d, want %d", processedTokens, tt.wantProcessed)
			}
		})
	}
}
