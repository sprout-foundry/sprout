package api

// TPSBase provides a default implementation of TPS tracking methods
// Other providers can embed this struct to get TPS functionality
type TPSBase struct {
	tpsTracker *TPSTracker
}

// NewTPSBase creates a new TPS base with tracker
func NewTPSBase() *TPSBase {
	return &TPSBase{
		tpsTracker: NewTPSTracker(),
	}
}

// GetTracker returns the underlying TPS tracker
func (t *TPSBase) GetTracker() *TPSTracker {
	if t.tpsTracker == nil {
		t.tpsTracker = NewTPSTracker()
	}
	return t.tpsTracker
}

// GetLastTPS returns the most recent TPS measurement
func (t *TPSBase) GetLastTPS() float64 {
	return t.GetTracker().GetCurrentTPS()
}

// GetAverageTPS returns the average TPS across all requests
func (t *TPSBase) GetAverageTPS() float64 {
	return t.GetTracker().GetAverageTPS()
}

// GetTPSStats returns comprehensive TPS statistics
func (t *TPSBase) GetTPSStats() map[string]float64 {
	stats := t.GetTracker().GetStats()
	// Convert to float64 map for interface compatibility
	result := make(map[string]float64)
	for k, v := range stats {
		if f, ok := v.(float64); ok {
			result[k] = f
		} else if i, ok := v.(int); ok {
			result[k] = float64(i)
		}
	}
	return result
}

// ResetTPSStats clears all TPS tracking data
func (t *TPSBase) ResetTPSStats() {
	t.GetTracker().Reset()
}
