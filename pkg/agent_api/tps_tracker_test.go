package api

import (
	"testing"
	"time"
)

func TestTPSTracker(t *testing.T) {
	tracker := NewTPSTracker()

	// Test recording a request
	duration := 2 * time.Second
	completionTokens := 100
	
	tps := tracker.RecordRequest(duration, completionTokens)
	expectedTPS := 50.0 // 100 tokens / 2 seconds = 50 TPS
	
	if tps != expectedTPS {
		t.Errorf("Expected TPS %.1f, got %.1f", expectedTPS, tps)
	}

	// Test current TPS
	currentTPS := tracker.GetCurrentTPS()
	if currentTPS != expectedTPS {
		t.Errorf("Expected current TPS %.1f, got %.1f", expectedTPS, currentTPS)
	}

	// Test average TPS
	averageTPS := tracker.GetAverageTPS()
	if averageTPS != expectedTPS {
		t.Errorf("Expected average TPS %.1f, got %.1f", expectedTPS, averageTPS)
	}

	// Test stats
	stats := tracker.GetStats()
	if stats["total_requests"] != 1 {
		t.Errorf("Expected total_requests 1, got %v", stats["total_requests"])
	}
	if stats["total_tokens"] != 100 {
		t.Errorf("Expected total_tokens 100, got %v", stats["total_tokens"])
	}

	// Test reset
	tracker.Reset()
	stats = tracker.GetStats()
	if stats["total_requests"] != 0 {
		t.Errorf("Expected total_requests 0 after reset, got %v", stats["total_requests"])
	}
}