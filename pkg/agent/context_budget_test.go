package agent

import (
	"math"
	"testing"
)

// TestTotalReservedFraction verifies the reservation constants sum to the
// documented total. SP-066 Phase 1 ships with 15% response + 10% thinking
// + 5% tool I/O = 30%; if any constant is tuned the total should be
// inspected for headroom side effects.
func TestTotalReservedFraction(t *testing.T) {
	got := totalReservedFraction()
	want := 0.30
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("totalReservedFraction = %v, want %v", got, want)
	}
}

// TestComputeCompactionTriggerFraction verifies the trigger fraction is
// (1 − total_reserved) so seed substitutes before the prompt consumes the
// reserved share of the context window. With the default 30% reservation,
// the trigger fires at 70% of max context — well below seed's hardcoded
// 0.85 default, restoring headroom for thinking-budget models.
func TestComputeCompactionTriggerFraction(t *testing.T) {
	a := &Agent{}
	got := a.computeCompactionTriggerFraction()
	want := 0.70
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("computeCompactionTriggerFraction = %v, want %v", got, want)
	}
}

// TestCompactionTriggerFractionLowerThanSeedDefault guards against the
// regression where sprout's reservation math drifts above seed's hardcoded
// 0.85 default (defaultCompactionTriggerFraction). The whole point of
// SP-066 Phase 1 is to be more conservative than seed's default; if we
// ever exceed it, we're not solving the empty-reply problem we set out
// to solve.
func TestCompactionTriggerFractionLowerThanSeedDefault(t *testing.T) {
	const seedDefault = 0.85
	a := &Agent{}
	got := a.computeCompactionTriggerFraction()
	if got >= seedDefault {
		t.Fatalf("trigger fraction %v must be less than seed default %v", got, seedDefault)
	}
}

// TestCompactionTriggerFractionInRange verifies the computed fraction is a
// valid value for seed (clamped to (0, 1]). A misconfigured constant
// (e.g., negative reservation) would otherwise produce a value > 1 or ≤ 0,
// which seed silently replaces with its own default.
func TestCompactionTriggerFractionInRange(t *testing.T) {
	a := &Agent{}
	got := a.computeCompactionTriggerFraction()
	if got <= 0 || got > 1 {
		t.Fatalf("trigger fraction %v out of seed's accepted range (0, 1]", got)
	}
}
