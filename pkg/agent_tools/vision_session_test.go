package tools

import (
	"sync"
	"testing"
)

// TestRecordVisionUsage_PerSession verifies that two VisionProcessor instances
// each track their own usage independently — no cross-talk between sessions.
func TestRecordVisionUsage_PerSession(t *testing.T) {
	ClearLastVisionUsage()

	vp1 := &VisionProcessor{}
	vp2 := &VisionProcessor{}

	usage1 := &VisionUsageInfo{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		EstimatedCost:    0.001,
	}
	usage2 := &VisionUsageInfo{
		PromptTokens:     200,
		CompletionTokens: 100,
		TotalTokens:      300,
		EstimatedCost:    0.002,
	}

	// Record usage for both processors
	recordVisionUsage(vp1, usage1)
	recordVisionUsage(vp2, usage2)

	// Per-session: each processor should have its own usage
	got1 := vp1.LastUsage()
	if got1 == nil {
		t.Fatal("vp1.LastUsage() = nil, want non-nil")
	}
	if got1.PromptTokens != 100 {
		t.Errorf("vp1 LastUsage PromptTokens = %v, want 100", got1.PromptTokens)
	}
	if got1.TotalTokens != 150 {
		t.Errorf("vp1 LastUsage TotalTokens = %v, want 150", got1.TotalTokens)
	}

	got2 := vp2.LastUsage()
	if got2 == nil {
		t.Fatal("vp2.LastUsage() = nil, want non-nil")
	}
	if got2.PromptTokens != 200 {
		t.Errorf("vp2 LastUsage PromptTokens = %v, want 200", got2.PromptTokens)
	}
	if got2.TotalTokens != 300 {
		t.Errorf("vp2 LastUsage TotalTokens = %v, want 300", got2.TotalTokens)
	}

	// Verify no cross-talk: vp1 should still have usage1, not usage2
	if vp1.LastUsage().PromptTokens != 100 {
		t.Errorf("vp1 LastUsage changed after vp2 record: PromptTokens = %v, want 100", vp1.LastUsage().PromptTokens)
	}
}

// TestRecordVisionUsage_GlobalMirror verifies that the cross-session global
// mirror always reflects the most recent write, even across different processors.
func TestRecordVisionUsage_GlobalMirror(t *testing.T) {
	ClearLastVisionUsage()

	vp1 := &VisionProcessor{}
	vp2 := &VisionProcessor{}

	usage1 := &VisionUsageInfo{TotalTokens: 100, EstimatedCost: 0.001}
	usage2 := &VisionUsageInfo{TotalTokens: 200, EstimatedCost: 0.002}

	// Record for vp1 — global mirror should reflect usage1
	recordVisionUsage(vp1, usage1)
	global := GetLastVisionUsage()
	if global == nil {
		t.Fatal("GetLastVisionUsage() = nil after recording vp1")
	}
	if global.TotalTokens != 100 {
		t.Errorf("Global mirror TotalTokens = %v, want 100", global.TotalTokens)
	}

	// Record for vp2 — global mirror should now reflect usage2 (the most recent)
	recordVisionUsage(vp2, usage2)
	global = GetLastVisionUsage()
	if global == nil {
		t.Fatal("GetLastVisionUsage() = nil after recording vp2")
	}
	if global.TotalTokens != 200 {
		t.Errorf("Global mirror TotalTokens = %v, want 200 (most recent)", global.TotalTokens)
	}

	// Per-session should still be independent
	if vp1.LastUsage().TotalTokens != 100 {
		t.Errorf("vp1 LastUsage TotalTokens = %v, want 100", vp1.LastUsage().TotalTokens)
	}
	if vp2.LastUsage().TotalTokens != 200 {
		t.Errorf("vp2 LastUsage TotalTokens = %v, want 200", vp2.LastUsage().TotalTokens)
	}

	// nil vp should still update global mirror
	recordVisionUsage(nil, &VisionUsageInfo{TotalTokens: 999, EstimatedCost: 0.099})
	global = GetLastVisionUsage()
	if global.TotalTokens != 999 {
		t.Errorf("Global mirror after nil vp: TotalTokens = %v, want 999", global.TotalTokens)
	}

	// nil usage should be a no-op
	ClearLastVisionUsage()
	recordVisionUsage(vp1, nil)
	if GetLastVisionUsage() != nil {
		t.Errorf("Global mirror after nil usage = %v, want nil", GetLastVisionUsage())
	}
}

// TestGetLastVisionUsage_Concurrency verifies that concurrent reads and writes
// to the global mirror are race-free. Run with -race to detect data races.
func TestGetLastVisionUsage_Concurrency(t *testing.T) {
	ClearLastVisionUsage()

	const numReaders = 100
	const numRounds = 50

	var wg sync.WaitGroup

	// Single writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numRounds; i++ {
			recordVisionUsage(nil, &VisionUsageInfo{
				TotalTokens:   i * 10,
				EstimatedCost: float64(i) * 0.001,
			})
			// Also test ClearLastVisionUsage
			if i%10 == 0 {
				ClearLastVisionUsage()
			}
		}
	}()

	// Many reader goroutines
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numRounds; i++ {
				_ = GetLastVisionUsage()
			}
		}()
	}

	wg.Wait()
}

// TestVisionProcessor_LastUsage verifies that LastUsage() returns nil before
// any call, and returns the correct usage after recordVisionUsage is called.
func TestVisionProcessor_LastUsage(t *testing.T) {
	vp := &VisionProcessor{}

	// Before any recording, LastUsage should be nil
	if vp.LastUsage() != nil {
		t.Errorf("LastUsage() before any call = %v, want nil", vp.LastUsage())
	}

	// Record usage
	usage := &VisionUsageInfo{
		PromptTokens:     500,
		CompletionTokens: 250,
		TotalTokens:      750,
		EstimatedCost:    0.005,
	}
	recordVisionUsage(vp, usage)

	// After recording, LastUsage should return the recorded usage
	got := vp.LastUsage()
	if got == nil {
		t.Fatal("LastUsage() after recording = nil, want non-nil")
	}
	if got.PromptTokens != 500 {
		t.Errorf("LastUsage PromptTokens = %v, want 500", got.PromptTokens)
	}
	if got.CompletionTokens != 250 {
		t.Errorf("LastUsage CompletionTokens = %v, want 250", got.CompletionTokens)
	}
	if got.TotalTokens != 750 {
		t.Errorf("LastUsage TotalTokens = %v, want 750", got.TotalTokens)
	}
	if got.EstimatedCost != 0.005 {
		t.Errorf("LastUsage EstimatedCost = %v, want 0.005", got.EstimatedCost)
	}

	// Record a second usage — LastUsage should update to the new one
	usage2 := &VisionUsageInfo{TotalTokens: 1000, EstimatedCost: 0.01}
	recordVisionUsage(vp, usage2)
	if vp.LastUsage().TotalTokens != 1000 {
		t.Errorf("LastUsage after second record: TotalTokens = %v, want 1000", vp.LastUsage().TotalTokens)
	}
}
