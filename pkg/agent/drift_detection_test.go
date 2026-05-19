package agent

import (
	"sync"
	"testing"
)

func TestNewDriftDetector_Defaults(t *testing.T) {
	d := NewDriftDetector(0, 0)

	if d.threshold != DefaultDriftThreshold {
		t.Errorf("threshold = %v, want %v", d.threshold, DefaultDriftThreshold)
	}
	if d.checkInterval != DefaultDriftCheckInterval {
		t.Errorf("checkInterval = %v, want %v", d.checkInterval, DefaultDriftCheckInterval)
	}
	if d.driftCount != 0 {
		t.Errorf("driftCount = %v, want 0", d.driftCount)
	}
	if d.rejectionCount != 0 {
		t.Errorf("rejectionCount = %v, want 0", d.rejectionCount)
	}
	if d.suppressed != false {
		t.Errorf("suppressed = %v, want false", d.suppressed)
	}
}

func TestNewDriftDetector_CustomValues(t *testing.T) {
	d := NewDriftDetector(0.75, 10)

	if d.threshold != 0.75 {
		t.Errorf("threshold = %v, want 0.75", d.threshold)
	}
	if d.checkInterval != 10 {
		t.Errorf("checkInterval = %v, want 10", d.checkInterval)
	}
}

func TestShouldCheck_EveryNthTurn(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	tests := []struct {
		turn     int
		expected bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, false},
		{4, false},
		{5, true},
		{6, false},
		{9, false},
		{10, true},
		{11, false},
		{14, false},
		{15, true},
	}

	for _, tc := range tests {
		got := d.ShouldCheck(tc.turn)
		if got != tc.expected {
			t.Errorf("ShouldCheck(%d) = %v, want %v", tc.turn, got, tc.expected)
		}
	}
}

func TestShouldCheck_SuppressedReturnsFalse(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	// Suppress the detector
	d.RecordRejection()
	d.RecordRejection()
	d.RecordRejection()

	if !d.IsSuppressed() {
		t.Fatal("detector should be suppressed after 3 rejections")
	}

	// ShouldCheck should return false for all turns when suppressed
	for turn := 1; turn <= 20; turn++ {
		if d.ShouldCheck(turn) {
			t.Errorf("ShouldCheck(%d) = true, want false (suppressed)", turn)
		}
	}
}

// Helper to create test embeddings with known cosine similarity.
// Creates two 4-dimensional vectors with a predictable relationship.
func makeSimilarEmbedding(t *testing.T) (sessionIntent []float32, current []float32, similarity float64) {
	// Two identical unit vectors → similarity = 1.0
	v := []float32{1, 0, 0, 0}
	return append([]float32{}, v...), append([]float32{}, v...), 1.0
}

func makeDifferentEmbedding(t *testing.T) (sessionIntent []float32, current []float32, similarity float64) {
	// Two orthogonal vectors → similarity = 0.0
	a := []float32{1, 0, 0, 0}
	b := []float32{0, 1, 0, 0}
	return append([]float32{}, a...), append([]float32{}, b...), 0.0
}

func TestCheckDrift_BelowThreshold_Detected(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	sessionIntent, current, _ := makeDifferentEmbedding(t)
	isDrift, sim := d.CheckDrift(sessionIntent, current)

	if !isDrift {
		t.Errorf("isDrift = false, want true (similarity %.4f < threshold %.2f)", sim, d.threshold)
	}
	if sim != 0 {
		t.Errorf("similarity = %v, want 0 (orthogonal vectors)", sim)
	}

	// Record the drift and verify counter
	d.RecordDrift()
	if d.DriftCount() != 1 {
		t.Errorf("DriftCount() = %v, want 1", d.DriftCount())
	}
}

func TestCheckDrift_AboveThreshold_NotDetected(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	sessionIntent, current, _ := makeSimilarEmbedding(t)
	isDrift, sim := d.CheckDrift(sessionIntent, current)

	if isDrift {
		t.Errorf("isDrift = true, want false (similarity %.4f >= threshold %.2f)", sim, d.threshold)
	}
	if sim < 0.999 {
		t.Errorf("similarity = %v, want ~1.0 (identical vectors)", sim)
	}
}

func TestCheckDrift_NilIntent_NoOp(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	current := []float32{1, 0, 0, 0}
	isDrift, sim := d.CheckDrift(nil, current)

	if isDrift {
		t.Error("isDrift = true, want false (nil intent should be no-op)")
	}
	if sim != 0 {
		t.Errorf("similarity = %v, want 0 (nil intent should be no-op)", sim)
	}
}

func TestCheckDrift_EmptyIntent_NoOp(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	current := []float32{1, 0, 0, 0}
	isDrift, sim := d.CheckDrift([]float32{}, current)

	if isDrift {
		t.Error("isDrift = true, want false (empty intent should be no-op)")
	}
	if sim != 0 {
		t.Errorf("similarity = %v, want 0 (empty intent should be no-op)", sim)
	}
}

func TestRecordRejection_SuppressAfter3(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	// First rejection
	d.RecordRejection()
	if d.RejectionCount() != 1 {
		t.Errorf("RejectionCount() = %v, want 1", d.RejectionCount())
	}
	if d.IsSuppressed() {
		t.Error("detector should not be suppressed after 1 rejection")
	}

	// Second rejection
	d.RecordRejection()
	if d.RejectionCount() != 2 {
		t.Errorf("RejectionCount() = %v, want 2", d.RejectionCount())
	}
	if d.IsSuppressed() {
		t.Error("detector should not be suppressed after 2 rejections")
	}

	// Third rejection — should suppress
	d.RecordRejection()
	if d.RejectionCount() != 3 {
		t.Errorf("RejectionCount() = %v, want 3", d.RejectionCount())
	}
	if !d.IsSuppressed() {
		t.Error("detector should be suppressed after 3 rejections")
	}

	// Fourth rejection — still suppressed
	d.RecordRejection()
	if d.RejectionCount() != 4 {
		t.Errorf("RejectionCount() = %v, want 4", d.RejectionCount())
	}
	if !d.IsSuppressed() {
		t.Error("detector should remain suppressed after 4 rejections")
	}
}

func TestDriftDetector_ConcurrentAccess(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	sessionIntent := []float32{1, 0, 0, 0}
	current := []float32{0, 1, 0, 0}

	var wg sync.WaitGroup

	// Concurrent ShouldCheck calls
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(turn int) {
			defer wg.Done()
			d.ShouldCheck(turn)
		}(i + 1)
	}

	// Concurrent CheckDrift calls
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.CheckDrift(sessionIntent, current)
		}()
	}

	// Concurrent RecordDrift calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.RecordDrift()
		}()
	}

	// Concurrent RecordRejection calls
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.RecordRejection()
		}()
	}

	// Concurrent getter calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.IsSuppressed()
			d.DriftCount()
			d.RejectionCount()
		}()
	}

	wg.Wait()

	// Verify final state is consistent
	if d.DriftCount() != 50 {
		t.Errorf("DriftCount() = %v, want 50", d.DriftCount())
	}
	if d.RejectionCount() != 10 {
		t.Errorf("RejectionCount() = %v, want 10", d.RejectionCount())
	}
	if !d.IsSuppressed() {
		t.Error("detector should be suppressed after 10 rejections")
	}
}
