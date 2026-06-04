package agent

import (
	"math"
	"sync"
	"testing"
)

func TestFleetUsdBudget_AddBelowLimit(t *testing.T) {
	b := NewFleetUsdBudget(10.0, []float64{0.5, 0.8})
	spent, crossed, exceeded := b.Add(2.0)
	if math.Abs(spent-2.0) > 1e-9 {
		t.Fatalf("spent = %v, want 2", spent)
	}
	if len(crossed) != 0 {
		t.Fatalf("crossed = %v, want none", crossed)
	}
	if exceeded {
		t.Fatalf("exceeded = true, want false")
	}
	if b.Exceeded() {
		t.Fatalf("Exceeded() = true, want false")
	}
}

func TestFleetUsdBudget_ThresholdsFireOncePerThreshold(t *testing.T) {
	b := NewFleetUsdBudget(10.0, []float64{0.5, 0.8})

	// Cross 50% in one call ($5)
	_, crossed, _ := b.Add(5.0)
	if len(crossed) != 1 || crossed[0] != 0.5 {
		t.Fatalf("first add crossed = %v, want [0.5]", crossed)
	}

	// Stay below 80% — no crossing
	_, crossed, _ = b.Add(2.0)
	if len(crossed) != 0 {
		t.Fatalf("second add crossed = %v, want none", crossed)
	}

	// Cross 80% with a $1 bump ($5 + $2 + $1 = $8)
	_, crossed, _ = b.Add(1.0)
	if len(crossed) != 1 || crossed[0] != 0.8 {
		t.Fatalf("third add crossed = %v, want [0.8]", crossed)
	}

	// Another bump that doesn't re-cross
	_, crossed, _ = b.Add(0.5)
	if len(crossed) != 0 {
		t.Fatalf("fourth add crossed = %v, want none (no re-fire)", crossed)
	}
}

func TestFleetUsdBudget_SingleCallCrossesMultipleThresholds(t *testing.T) {
	b := NewFleetUsdBudget(10.0, []float64{0.5, 0.8})
	_, crossed, _ := b.Add(9.0)
	if len(crossed) != 2 {
		t.Fatalf("crossed = %v, want both thresholds", crossed)
	}
	if crossed[0] != 0.5 || crossed[1] != 0.8 {
		t.Fatalf("crossed order = %v, want [0.5, 0.8]", crossed)
	}
}

func TestFleetUsdBudget_ExceededIsSticky(t *testing.T) {
	b := NewFleetUsdBudget(5.0, nil)
	_, _, exceeded := b.Add(6.0)
	if !exceeded {
		t.Fatalf("first over-limit add should report exceeded=true")
	}
	if !b.Exceeded() {
		t.Fatalf("Exceeded() should be true after cap hit")
	}
	// Second call doesn't re-fire justExceeded
	_, _, exceeded = b.Add(1.0)
	if exceeded {
		t.Fatalf("subsequent adds should NOT re-fire justExceeded")
	}
	if !b.Exceeded() {
		t.Fatalf("Exceeded() should stay true (sticky)")
	}
}

func TestFleetUsdBudget_NoLimitMeansNoCap(t *testing.T) {
	b := NewFleetUsdBudget(0, []float64{0.5})
	_, crossed, exceeded := b.Add(1000.0)
	if len(crossed) != 0 {
		t.Fatalf("no-limit budget should not cross thresholds, got %v", crossed)
	}
	if exceeded {
		t.Fatalf("no-limit budget should never be exceeded")
	}
}

func TestFleetUsdBudget_ConcurrentAddsAccumulate(t *testing.T) {
	b := NewFleetUsdBudget(1000.0, nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Add(1.0)
		}()
	}
	wg.Wait()
	spent, _ := b.Snapshot()
	if math.Abs(spent-100.0) > 1e-6 {
		t.Fatalf("concurrent add total = %v, want 100", spent)
	}
}

func TestFleetUsdBudget_NilSafe(t *testing.T) {
	var b *FleetUsdBudget
	spent, crossed, exceeded := b.Add(1.0)
	if spent != 0 || len(crossed) != 0 || exceeded {
		t.Fatalf("nil budget Add should return zeros")
	}
	if b.Exceeded() {
		t.Fatalf("nil budget Exceeded should be false")
	}
	s, l := b.Snapshot()
	if s != 0 || l != 0 {
		t.Fatalf("nil budget Snapshot should return zeros")
	}
}

func TestFleetUsdBudget_ZeroOrNegativeCostIsNoop(t *testing.T) {
	b := NewFleetUsdBudget(10.0, []float64{0.5})
	if _, crossed, _ := b.Add(0); len(crossed) != 0 {
		t.Fatalf("zero cost should not cross thresholds")
	}
	if _, crossed, _ := b.Add(-5); len(crossed) != 0 {
		t.Fatalf("negative cost should not cross thresholds")
	}
	spent, _ := b.Snapshot()
	if spent != 0 {
		t.Fatalf("zero/negative cost should not accumulate, got %v", spent)
	}
}
