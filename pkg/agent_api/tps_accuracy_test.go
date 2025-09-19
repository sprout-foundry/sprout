package api

import (
	"testing"
	"time"
)

func TestTPSCalculationAccuracy(t *testing.T) {
	tracker := NewTPSTracker()

	tests := []struct {
		name             string
		duration         time.Duration
		completionTokens int
		expectedTPS      float64
	}{
		{
			name:             "1 second, 100 tokens",
			duration:         1 * time.Second,
			completionTokens: 100,
			expectedTPS:      100.0,
		},
		{
			name:             "500ms, 50 tokens",
			duration:         500 * time.Millisecond,
			completionTokens: 50,
			expectedTPS:      100.0,
		},
		{
			name:             "2 seconds, 300 tokens",
			duration:         2 * time.Second,
			completionTokens: 300,
			expectedTPS:      150.0,
		},
		{
			name:             "250ms, 125 tokens",
			duration:         250 * time.Millisecond,
			completionTokens: 125,
			expectedTPS:      500.0,
		},
		{
			name:             "10 seconds, 1000 tokens",
			duration:         10 * time.Second,
			completionTokens: 1000,
			expectedTPS:      100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualTPS := tracker.RecordRequest(tt.duration, tt.completionTokens)

			// Allow for small floating-point differences
			tolerance := 0.001
			if diff := abs(actualTPS - tt.expectedTPS); diff > tolerance {
				t.Errorf("TPS calculation mismatch: expected %.2f, got %.2f (diff: %.4f)",
					tt.expectedTPS, actualTPS, diff)
			}

			// Also verify GetCurrentTPS returns the same value
			currentTPS := tracker.GetCurrentTPS()
			if diff := abs(currentTPS - actualTPS); diff > tolerance {
				t.Errorf("GetCurrentTPS mismatch: expected %.2f, got %.2f",
					actualTPS, currentTPS)
			}
		})
	}
}

func TestTPSAverageCalculation(t *testing.T) {
	tracker := NewTPSTracker()

	// Record multiple requests
	tracker.RecordRequest(1*time.Second, 100)        // 100 TPS
	tracker.RecordRequest(2*time.Second, 200)        // 100 TPS
	tracker.RecordRequest(500*time.Millisecond, 250) // 500 TPS

	// Total: 3.5 seconds, 550 tokens
	// Expected average: 550/3.5 = 157.14 TPS
	expectedAvg := 550.0 / 3.5
	actualAvg := tracker.GetAverageTPS()

	tolerance := 0.1
	if diff := abs(actualAvg - expectedAvg); diff > tolerance {
		t.Errorf("Average TPS mismatch: expected %.2f, got %.2f",
			expectedAvg, actualAvg)
	}
}

func TestTPSEdgeCases(t *testing.T) {
	tracker := NewTPSTracker()

	// Test zero duration
	tps := tracker.RecordRequest(0, 100)
	if tps != 0.0 {
		t.Errorf("Expected 0 TPS for zero duration, got %.2f", tps)
	}

	// Test zero tokens
	tps = tracker.RecordRequest(1*time.Second, 0)
	if tps != 0.0 {
		t.Errorf("Expected 0 TPS for zero tokens, got %.2f", tps)
	}

	// Test negative values (shouldn't happen but defensive)
	tps = tracker.RecordRequest(-1*time.Second, 100)
	if tps != 0.0 {
		t.Errorf("Expected 0 TPS for negative duration, got %.2f", tps)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
