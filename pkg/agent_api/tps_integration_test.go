package api

import (
	"testing"
	"time"
)

// TestTPSTrackingIntegration verifies TPS tracking works correctly
func TestTPSTrackingIntegration(t *testing.T) {
	// Test the TPS tracking functionality directly
	tracker := NewTPSTracker()

	// Simulate API calls with different response times and token counts
	testCases := []struct {
		duration         time.Duration
		completionTokens int
		expectedTPS      float64
		description      string
	}{
		{
			duration:         1 * time.Second,
			completionTokens: 100,
			expectedTPS:      100.0,
			description:      "Standard 1 second response",
		},
		{
			duration:         500 * time.Millisecond,
			completionTokens: 250,
			expectedTPS:      500.0,
			description:      "Fast 500ms response",
		},
		{
			duration:         2 * time.Second,
			completionTokens: 150,
			expectedTPS:      75.0,
			description:      "Slower 2 second response",
		},
		{
			duration:         250 * time.Millisecond,
			completionTokens: 400,
			expectedTPS:      1600.0,
			description:      "Very fast response with many tokens",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Record the request
			actualTPS := tracker.RecordRequest(tc.duration, tc.completionTokens)

			// Check the TPS calculation
			tolerance := 0.001
			if diff := abs(actualTPS - tc.expectedTPS); diff > tolerance {
				t.Errorf("TPS mismatch: expected %.2f, got %.2f (diff: %.4f)",
					tc.expectedTPS, actualTPS, diff)
			}

			// Verify GetCurrentTPS returns the same value
			currentTPS := tracker.GetCurrentTPS()
			if diff := abs(currentTPS - actualTPS); diff > tolerance {
				t.Errorf("GetCurrentTPS mismatch: expected %.2f, got %.2f",
					actualTPS, currentTPS)
			}
		})
	}

	// Test average TPS calculation
	// We've recorded: 100 tokens/1s + 250 tokens/0.5s + 150 tokens/2s + 400 tokens/0.25s
	// Total: 900 tokens / 3.75 seconds = 240 TPS average
	expectedAverage := 900.0 / 3.75
	actualAverage := tracker.GetAverageTPS()
	tolerance := 0.1

	if diff := abs(actualAverage - expectedAverage); diff > tolerance {
		t.Errorf("Average TPS mismatch: expected %.2f, got %.2f",
			expectedAverage, actualAverage)
	}

	// Test GetStats
	stats := tracker.GetStats()

	// Verify stats contains expected fields
	requiredFields := []string{
		"total_requests",
		"total_duration",
		"total_tokens",
		"current_tps",
		"average_tps",
		"smooth_tps",
		"min_tps",
		"max_tps",
	}

	for _, field := range requiredFields {
		if _, ok := stats[field]; !ok {
			t.Errorf("Stats missing required field: %s", field)
		}
	}

	// Check specific stat values
	if totalRequests, ok := stats["total_requests"].(int); ok {
		if totalRequests != 4 {
			t.Errorf("Expected 4 total requests, got %d", totalRequests)
		}
	}

	if totalTokens, ok := stats["total_tokens"].(int); ok {
		if totalTokens != 900 {
			t.Errorf("Expected 900 total tokens, got %d", totalTokens)
		}
	}

	// Test smoothed TPS
	smoothTPS := tracker.GetSmoothTPS()
	if smoothTPS <= 0 {
		t.Error("Smoothed TPS should be positive")
	}

	// Test reset
	tracker.Reset()

	if tps := tracker.GetCurrentTPS(); tps != 0.0 {
		t.Errorf("Expected TPS after reset to be 0, got %.2f", tps)
	}

	if avg := tracker.GetAverageTPS(); avg != 0.0 {
		t.Errorf("Expected average TPS after reset to be 0, got %.2f", avg)
	}

	resetStats := tracker.GetStats()
	if totalRequests, ok := resetStats["total_requests"].(int); ok && totalRequests != 0 {
		t.Errorf("Expected 0 total requests after reset, got %d", totalRequests)
	}
}
