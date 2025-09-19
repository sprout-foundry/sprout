package api

import (
	"fmt"
	"sync"
	"time"
)

// TPSTracker manages tokens per second calculations
// This is a separate file to avoid circular dependencies

type TPSTracker struct {
	mu              sync.Mutex
	tpsHistory      []float64
	totalRequests   int
	totalDuration   time.Duration
	totalTokens     int
	lastRequestTime time.Time
}

// NewTPSTracker creates a new TPS tracker
func NewTPSTracker() *TPSTracker {
	return &TPSTracker{
		tpsHistory: make([]float64, 0),
	}
}

// RecordRequest records the timing and token usage of an API request
func (t *TPSTracker) RecordRequest(duration time.Duration, completionTokens int) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Calculate TPS for this request
	if duration > 0 && completionTokens > 0 {
		durationSeconds := duration.Seconds()
		tps := float64(completionTokens) / durationSeconds

		// Debug suspicious TPS values
		if tps > 1000 {
			fmt.Printf("⚠️  WARNING: Suspiciously high TPS detected!\n")
			fmt.Printf("   Duration: %v (%.3f seconds)\n", duration, durationSeconds)
			fmt.Printf("   Completion Tokens: %d\n", completionTokens)
			fmt.Printf("   Calculated TPS: %.2f\n", tps)
			fmt.Printf("   This suggests duration measurement may be incorrect\n")
		}

		// Store in history (keep last 100 requests)
		t.tpsHistory = append(t.tpsHistory, tps)
		if len(t.tpsHistory) > 100 {
			t.tpsHistory = t.tpsHistory[1:]
		}

		// Update totals
		t.totalRequests++
		t.totalDuration += duration
		t.totalTokens += completionTokens
		t.lastRequestTime = time.Now()

		return tps
	}

	return 0.0
}

// GetCurrentTPS returns the most recent TPS value
func (t *TPSTracker) GetCurrentTPS() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.tpsHistory) == 0 {
		return 0.0
	}
	return t.tpsHistory[len(t.tpsHistory)-1]
}

// GetAverageTPS returns the average TPS across all recorded requests
func (t *TPSTracker) GetAverageTPS() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.totalDuration == 0 || t.totalTokens == 0 {
		return 0.0
	}

	totalSeconds := t.totalDuration.Seconds()
	return float64(t.totalTokens) / totalSeconds
}

// GetSmoothTPS returns an exponentially smoothed TPS value
func (t *TPSTracker) GetSmoothTPS() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.tpsHistory) == 0 {
		return 0.0
	}

	// Use exponential smoothing with alpha=0.3
	alpha := 0.3
	smoothed := t.tpsHistory[0]
	for i := 1; i < len(t.tpsHistory); i++ {
		smoothed = alpha*t.tpsHistory[i] + (1-alpha)*smoothed
	}

	return smoothed
}

// GetStats returns comprehensive TPS statistics
func (t *TPSTracker) GetStats() map[string]interface{} {
	t.mu.Lock()
	defer t.mu.Unlock()

	stats := make(map[string]interface{})
	stats["total_requests"] = t.totalRequests
	stats["total_duration"] = t.totalDuration.String()
	stats["total_tokens"] = t.totalTokens
	stats["last_request_time"] = t.lastRequestTime.Format(time.RFC3339)

	if len(t.tpsHistory) > 0 {
		stats["current_tps"] = t.tpsHistory[len(t.tpsHistory)-1]
		stats["average_tps"] = t.GetAverageTPS()
		stats["smooth_tps"] = t.GetSmoothTPS()
		stats["min_tps"] = t.getMinTPS()
		stats["max_tps"] = t.getMaxTPS()
	}

	return stats
}

// getMinTPS returns the minimum TPS value from history
func (t *TPSTracker) getMinTPS() float64 {
	if len(t.tpsHistory) == 0 {
		return 0.0
	}
	min := t.tpsHistory[0]
	for _, tps := range t.tpsHistory {
		if tps < min {
			min = tps
		}
	}
	return min
}

// getMaxTPS returns the maximum TPS value from history
func (t *TPSTracker) getMaxTPS() float64 {
	if len(t.tpsHistory) == 0 {
		return 0.0
	}
	max := t.tpsHistory[0]
	for _, tps := range t.tpsHistory {
		if tps > max {
			max = tps
		}
	}
	return max
}

// Reset clears all TPS tracking data
func (t *TPSTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.tpsHistory = make([]float64, 0)
	t.totalRequests = 0
	t.totalDuration = 0
	t.totalTokens = 0
	t.lastRequestTime = time.Time{}
}
