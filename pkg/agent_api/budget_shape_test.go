package api

import (
	"testing"
)

// TestCalculateOutputBudget_FloorViolatesHardCap pins the deliberate floor
// posture in the output budget: the floor step re-inflates a starved budget
// to MinOutputTokens even when that puts input+budget ABOVE the context
// limit. That is intentional — degrading to `remaining` near the window edge
// starves reasoning-only turns (a 2–5K max_tokens finish=length no-op), and
// a rejected request is then recovered by the overflow path, which is
// strictly better than a guaranteed no-op turn. The floor step is therefore
// a deliberate trade-off, not a bug.
//
// The consequence is that every input in [contextLimit-MinOutputTokens+1,
// contextLimit-1] produces a request the provider must reject as an overflow.
// The zone is explicit so callers know it exists: the proactive compaction
// trigger (see pkg/agent/context_budget.go) is clamped to the zone edge —
// (window-MinOutputTokens) — so the estimate never enters it, and the
// overflow-recovery path is the backstop for anything that slips through.
func TestCalculateOutputBudget_FloorViolatesHardCap(t *testing.T) {
	for _, c := range []int{32000, 131072, 200000} {
		// Walk input downward from just under the window and collect the
		// contiguous violating range (inputs whose returned budget makes
		// input+budget exceed the window, i.e. a guaranteed-overflow request).
		violLo := -1
		violHi := -1
		for input := c - 1; input >= 0; input-- {
			budget, _ := CalculateOutputBudget(c, input)
			if input+budget > c {
				if violLo < 0 {
					violLo = input // first (highest) violating input
				}
				violHi = input // last (lowest) violating input
			}
		}
		if c <= MinOutputTokens {
			if violLo >= 0 {
				t.Fatalf("window=%d (<= floor): expected no violating inputs, got range [%d,%d]", c, violHi, violLo)
			}
			continue
		}
		// The violating inputs are exactly the top MinOutputTokens: the floor
		// re-inflates the budget to MinOutputTokens even when remaining < floor.
		// The highest violating input is c-1; the lowest is c-MinOutputTokens+1
		// (at c-MinOutputTokens the sum equals the window exactly, not over).
		wantLargest := c - 1
		wantSmallest := c - MinOutputTokens + 1
		if violLo != wantLargest || violHi != wantSmallest {
			t.Fatalf("window=%d: violating input range [%d,%d], want [%d,%d] (the floor overflows the window for these)",
				c, violHi, violLo, wantSmallest, wantLargest)
		}
		// Concrete overflow at the top of the range.
		if budget, _ := CalculateOutputBudget(c, wantLargest); wantLargest+budget <= c {
			t.Fatalf("window=%d: input=%d + budget=%d does not exceed the window (test math broken)", c, wantLargest, budget)
		}
	}
}

// TestLCMTriggerNearGuaranteedOverflowZone ties the LCM trigger to the
// guaranteed-overflow zone above: at a 131072 (132K) window the pre-clamp LCM
// trigger (0.85) sits just below the zone that starts at 131072-MinOutputTokens
// (114688), so a single large tool-result dump crossing ~3.4K tokens pushes
// the prompt into the guaranteed-overflow zone before the next trigger check.
// This is the precondition for the reject-then-recover (or bottomed-out error)
// failure on small windows. The clamp fix (context_budget.go) drops the LCM
// window-share trigger to 0.70, well clear of the zone on 132K+ windows, and
// clamps the effective trigger on small windows to the zone edge itself
// (window-MinOutputTokens), so the estimate never enters the zone there either.
func TestLCMTriggerNearGuaranteedOverflowZone(t *testing.T) {
	const (
		window      = 131072 // 132K LCM window
		preFixFrac  = 0.85   // LCM profile trigger before the no-overflow clamp
		clampedFrac = 0.70   // after the clamp (1 - EstimationErrorPercent/100)
	)
	zoneStart := window - MinOutputTokens // guaranteed-overflow zone begins here

	// Pre-fix: the 0.85 trigger lands a single large-tool-dump below the zone.
	preFixFiresAt := preFixFrac * float64(window)
	gapToZone := float64(zoneStart) - preFixFiresAt // tokens a single dump must add
	if preFixFiresAt >= float64(zoneStart) {
		t.Fatalf("pre-fix 0.85 trigger (%.0f) should sit below the overflow zone (%d)", preFixFiresAt, zoneStart)
	}
	if gapToZone > 5000 {
		t.Fatalf("pre-fix trigger-to-zone gap %.0f is larger than expected (~3.4K); fixture changed", gapToZone)
	}
	t.Logf("LCM 0.85 trigger fires at %.0f; a single %d-token tool dump enters the guaranteed-overflow zone", preFixFiresAt, int(gapToZone)+1)

	// Post-fix: the clamped 0.70 trigger is well clear of the zone.
	postFixFiresAt := clampedFrac * float64(window)
	if postFixFiresAt >= float64(zoneStart) {
		t.Fatalf("clamped 0.70 trigger (%.0f) still touches the overflow zone (%d) — clamp not conservative enough", postFixFiresAt, zoneStart)
	}
	margin := float64(zoneStart) - postFixFiresAt
	t.Logf("clamped 0.70 trigger fires at %.0f; %d tokens of headroom below the overflow zone", postFixFiresAt, int(margin))
}

// TestCalculateOutputBudgetMonotone guards the shape of the budget curve:
// a larger estimated input must never yield a larger output budget. This
// pins the fix for a real discontinuity — the intermediate 3/4-remaining
// taper produced a 17100 -> 49500 JUMP as the estimate crossed the
// worst-case boundary (est=133K on a 200K window), meaning a bigger
// prompt got a 3x bigger budget. The bounded-reserve formulation is
// monotone by construction; this test keeps it that way.
func TestCalculateOutputBudgetMonotone(t *testing.T) {
	windows := []int{4096, 32000, 128000, 131072, 200000, 1000000}
	for _, c := range windows {
		prev := 1 << 30
		for est := 0; est <= c; est += max(1, c/5000) {
			b, ok := CalculateOutputBudget(c, est)
			if !ok {
				b = 0 // over-window reports remaining (>= 0); still not a jump up
			}
			if b > prev {
				t.Fatalf("non-monotone at window=%d est=%d: %d -> %d", c, est, prev, b)
			}
			prev = b
		}
	}
}

// TestCalculateOutputBudgetAnchoredShape pins two invariants of the
// anchored variant across the full input range:
//  1. fully heuristic (anchored=0) matches CalculateOutputBudget exactly;
//  2. the result never exceeds the real remaining space, and the curve is
//     monotone in the heuristic input.
//
// Note the split case (anchored > 0) MAY legitimately exceed the fully
// heuristic budget for the same total input: anchoring moves input mass
// out of the estimation-error-taxed pool. That is the anchored path
// working as designed, not an inversion.
func TestCalculateOutputBudgetAnchoredShape(t *testing.T) {
	const c = 200000
	prevA := 1 << 30
	for h := 0; h <= c; h += 40 {
		a, okA := CalculateOutputBudgetAnchored(c, 0, h)
		p, _ := CalculateOutputBudget(c, h)
		if okA && a != p {
			t.Fatalf("fully-heuristic anchored %d != plain %d at h=%d", a, p, h)
		}
		if a > prevA {
			t.Fatalf("anchored non-monotone at h=%d: %d -> %d", h, prevA, a)
		}
		prevA = a

		split, okS := CalculateOutputBudgetAnchored(c, h/2, h-h/2)
		rem := c - h
		// The split budget may exceed `rem` by at most the floor step: the
		// floor is kept (not degraded to `remaining`) when the heuristic
		// reserves consume the tail, so near the ceiling the budget
		// legitimately sits at MinOutputTokens while remaining is below it.
		// Degrading to `remaining` there was the finish=length no-op-turn
		// bug: providers clamped or rejected, and a rejected request
		// triggers overflow-recovery compaction — but a clamped 2–5K budget
		// silently starved reasoning-only turns. Allowing the floor step
		// keeps reasoning viable; the recovery path handles true overflow.
		if okS && split > rem && split != MinOutputTokens {
			t.Fatalf("anchored split %d exceeds remaining %d at total h=%d without the floor step", split, rem, h)
		}
	}
}

// TestCalculateOutputBudgetNoPrematureCollapse is a regression test for a bug
