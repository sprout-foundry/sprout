package agent

import (
	"fmt"
	"sync"
	"testing"
)

// TestLanguageGuardMetricsRecordKeepsInvariant pins that a check is always
// counted and a mismatch only additionally (Mismatches <= Checks per model),
// and that an empty model ID is bucketed under "unknown".
func TestLanguageGuardMetricsRecordKeepsInvariant(t *testing.T) {
	m := NewLanguageGuardMetrics()
	m.Record("model-a", "", false)
	m.Record("model-a", "", true)
	m.Record("model-a", "", true)
	m.Record("model-b", "", false)
	m.Record("", "", true) // empty model ID -> "unknown"

	snap := m.Snapshot()
	byID := map[string]LanguageGuardModelStat{}
	for _, s := range snap {
		byID[s.ModelID] = s
	}
	if got := byID["model-a"]; got.Checks != 3 || got.Mismatches != 2 {
		t.Errorf("model-a = %+v, want Checks=3 Mismatches=2", got)
	}
	if got := byID["model-b"]; got.Checks != 1 || got.Mismatches != 0 {
		t.Errorf("model-b = %+v, want Checks=1 Mismatches=0", got)
	}
	if got := byID["unknown"]; got.Checks != 1 || got.Mismatches != 1 {
		t.Errorf("unknown = %+v, want Checks=1 Mismatches=1", got)
	}
	for _, s := range snap {
		if s.Mismatches > s.Checks {
			t.Errorf("model %q violates invariant: Mismatches(%d) > Checks(%d)", s.ModelID, s.Mismatches, s.Checks)
		}
	}
}

// TestLanguageGuardMetricsRate pins per-model and overall rates, including
// the zero-check case (rate 0, no division by zero).
func TestLanguageGuardMetricsRate(t *testing.T) {
	m := NewLanguageGuardMetrics()
	if got := m.OverallRate(); got != 0 {
		t.Errorf("OverallRate on empty = %v, want 0", got)
	}
	if got := m.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot on empty = %d rows, want 0", len(got))
	}

	m.Record("model-a", "", true)
	m.Record("model-a", "", false)
	m.Record("model-b", "", true)
	m.Record("model-b", "", true)

	byID := map[string]LanguageGuardModelStat{}
	for _, s := range m.Snapshot() {
		byID[s.ModelID] = s
	}
	if got := byID["model-a"].Rate(); got != 0.5 {
		t.Errorf("model-a Rate = %v, want 0.5", got)
	}
	if got := byID["model-b"].Rate(); got != 1.0 {
		t.Errorf("model-b Rate = %v, want 1.0", got)
	}
	// 3 mismatches / 4 checks overall.
	if got := m.OverallRate(); got != 0.75 {
		t.Errorf("OverallRate = %v, want 0.75", got)
	}
}

// TestLanguageGuardMetricsSnapshotIsSortedAndCopied pins that Snapshot
// returns a sorted, independent copy (mutating the returned slice must not
// affect the recorder).
func TestLanguageGuardMetricsSnapshotIsSortedAndCopied(t *testing.T) {
	m := NewLanguageGuardMetrics()
	m.Record("zeta", "", false)
	m.Record("alpha", "", false)
	m.Record("mid", "", false)

	snap := m.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("Snapshot = %d rows, want 3", len(snap))
	}
	for i, want := range []string{"alpha", "mid", "zeta"} {
		if snap[i].ModelID != want {
			t.Errorf("Snapshot[%d].ModelID = %q, want %q (sorted)", i, snap[i].ModelID, want)
		}
	}
	// Mutating the copy must not leak into the recorder.
	snap[0].Checks = 999
	if got := m.Snapshot()[0]; got.Checks != 1 {
		t.Errorf("Snapshot copy leaked into recorder: alpha Checks = %d, want 1", got.Checks)
	}
}

// TestLanguageGuardMetricsNilReceiverIsNoOp pins that a nil recorder is a
// safe no-op (callers can hold a nil without guarding).
func TestLanguageGuardMetricsNilReceiverIsNoOp(t *testing.T) {
	var m *LanguageGuardMetrics
	m.Record("x", "", true) // must not panic
	if m.Snapshot() != nil {
		t.Errorf("nil receiver Snapshot = non-nil, want nil")
	}
	if m.OverallRate() != 0 {
		t.Errorf("nil receiver OverallRate = %v, want 0", m.OverallRate())
	}
}

// TestLanguageGuardMetricsConcurrentRecord pins that concurrent Record calls
// from many goroutines are race-free and lossless (the guard runs on the
// agent's query path, which is concurrent across chats).
func TestLanguageGuardMetricsConcurrentRecord(t *testing.T) {
	m := NewLanguageGuardMetrics()
	const goroutines = 32
	const perGoroutine = 1000
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				modelID := fmt.Sprintf("model-%d", g%5)
				m.Record(modelID, "", i%10 == 0) // 10% mismatches
			}
		}(g)
	}
	wg.Wait()

	snap := m.Snapshot()
	if len(snap) != 5 {
		t.Fatalf("Snapshot = %d models, want 5", len(snap))
	}
	totalChecks := int64(0)
	totalMismatches := int64(0)
	for _, s := range snap {
		totalChecks += s.Checks
		totalMismatches += s.Mismatches
		if s.Mismatches > s.Checks {
			t.Errorf("model %q violates invariant after concurrent records", s.ModelID)
		}
	}
	if totalChecks != int64(goroutines*perGoroutine) {
		t.Errorf("total checks = %d, want %d (no lost updates)", totalChecks, goroutines*perGoroutine)
	}
	// 10% of 32000 = 3200 mismatches.
	if totalMismatches != 3200 {
		t.Errorf("total mismatches = %d, want 3200", totalMismatches)
	}
}

// TestLanguageGuardMetricsGlobalSwap pins the test hook: swapping in a
// fresh recorder isolates a test, and cleanup restores the previous one.
func TestLanguageGuardMetricsGlobalSwap(t *testing.T) {
	before := GlobalLanguageGuardMetrics()
	cleanup := SetGlobalLanguageGuardMetricsForTest(NewLanguageGlobalForTest())
	defer cleanup()

	mid := GlobalLanguageGuardMetrics()
	if mid == before {
		t.Fatalf("swap did not install the test recorder")
	}
	mid.Record("test-model", "", true)
	if got := mid.OverallRate(); got != 1.0 {
		t.Errorf("test recorder OverallRate = %v, want 1.0", got)
	}
	cleanup()
	if after := GlobalLanguageGuardMetrics(); after != before {
		t.Errorf("cleanup did not restore the previous global recorder")
	}
}

// NewLanguageGlobalForTest is a tiny helper so the test reads clearly.
func NewLanguageGlobalForTest() *LanguageGuardMetrics {
	return NewLanguageGuardMetrics()
}
