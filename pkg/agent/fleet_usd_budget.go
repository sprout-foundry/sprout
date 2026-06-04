package agent

import (
	"sort"
	"sync"
	"sync/atomic"
)

// FleetUsdBudget caps the total USD cost across a primary agent and every
// subagent it spawns. It mirrors the token-based fleetBudget mechanism but
// in USD because mixed-provider workflows can't be reasonably capped by
// tokens — a token cap that fits an Opus orchestrator would let a DeepSeek
// coder run effectively unbounded for the same numeric value.
//
// Threshold warnings are emitted at most once per threshold per budget
// instance (warnedIdx is monotonic). The truncation flag (Exceeded) is
// sticky once set — the agent's conversation loop polls it to stop
// gracefully after the current LLM response.
type FleetUsdBudget struct {
	mu         sync.Mutex
	spent      float64
	limit      float64
	warnAt     []float64
	warnedIdx  int
	exceededFl atomic.Bool
}

// NewFleetUsdBudget returns a budget with the given hard cap (USD) and
// warning thresholds (fractions of the cap in (0, 1]). The thresholds are
// copied and sorted so the caller can pass them in any order.
func NewFleetUsdBudget(limit float64, warnAt []float64) *FleetUsdBudget {
	cleaned := make([]float64, 0, len(warnAt))
	for _, t := range warnAt {
		if t > 0 && t <= 1 {
			cleaned = append(cleaned, t)
		}
	}
	sort.Float64s(cleaned)
	return &FleetUsdBudget{limit: limit, warnAt: cleaned}
}

// Add debits a cost to the budget. Returns:
//   - newSpent: the cumulative spend after the addition
//   - crossed: the warning thresholds (as fractions of the limit) that this
//     call newly crossed — empty if none
//   - justExceeded: true only on the call that first pushes spent past limit
//
// When the cap is hit, the exceeded flag is set so the conversation loop can
// observe it via Exceeded() and stop gracefully. Subsequent calls still
// accumulate spend (so reporting stays accurate) but exceeded stays sticky
// and crossed stays empty.
func (b *FleetUsdBudget) Add(cost float64) (newSpent float64, crossed []float64, justExceeded bool) {
	if b == nil || cost <= 0 {
		if b != nil {
			b.mu.Lock()
			newSpent = b.spent
			b.mu.Unlock()
		}
		return newSpent, nil, false
	}
	b.mu.Lock()
	prev := b.spent
	b.spent += cost
	cur := b.spent
	limit := b.limit

	// Threshold crossings — only when limit > 0; otherwise warnings are
	// meaningless.
	if limit > 0 {
		for b.warnedIdx < len(b.warnAt) {
			threshold := b.warnAt[b.warnedIdx] * limit
			if cur >= threshold && prev < threshold {
				crossed = append(crossed, b.warnAt[b.warnedIdx])
				b.warnedIdx++
				continue
			}
			break
		}
	}

	exceededNow := limit > 0 && cur >= limit
	b.mu.Unlock()

	if exceededNow && !b.exceededFl.Swap(true) {
		justExceeded = true
	}
	return cur, crossed, justExceeded
}

// Exceeded reports whether the budget has been reached or surpassed.
func (b *FleetUsdBudget) Exceeded() bool {
	if b == nil {
		return false
	}
	return b.exceededFl.Load()
}

// Snapshot returns the current spend and limit for display purposes.
func (b *FleetUsdBudget) Snapshot() (spent, limit float64) {
	if b == nil {
		return 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spent, b.limit
}
