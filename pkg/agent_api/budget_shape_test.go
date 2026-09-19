package api

import "testing"

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
		if okS && split > rem {
			t.Fatalf("anchored split %d exceeds remaining %d at total h=%d", split, rem, h)
		}
	}
}

// TestCalculateOutputBudgetNoPrematureCollapse is a regression test for a bug
