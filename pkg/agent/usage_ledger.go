package agent

import "sync"

// Usage is an amount of model usage: tokens and what they cost.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	ChargedCost      float64
	TokenCost        float64
}

// usageLedger remembers how much of the agent's running usage totals has
// already been booked elsewhere (a cost ledger), so each booking takes only
// what is new.
type usageLedger struct {
	mu     sync.Mutex
	booked Usage
}

func (a *Agent) usageTotals() Usage {
	return Usage{
		PromptTokens:     a.GetPromptTokens(),
		CompletionTokens: a.GetCompletionTokens(),
		ChargedCost:      a.GetChargedCostTotal(),
		TokenCost:        a.GetTokenCostTotal(),
	}
}

// TakeUnbookedUsage returns the usage accrued since the previous call (or
// since the conversation was restored) and marks it booked. The running
// totals only grow within a conversation; a total below its mark means they
// were reset, and everything since the reset is new.
func (a *Agent) TakeUnbookedUsage() Usage {
	a.usage.mu.Lock()
	defer a.usage.mu.Unlock()
	now := a.usageTotals()
	prev := a.usage.booked
	if now.PromptTokens < prev.PromptTokens || now.CompletionTokens < prev.CompletionTokens ||
		now.ChargedCost < prev.ChargedCost || now.TokenCost < prev.TokenCost {
		prev = Usage{}
	}
	a.usage.booked = now
	return Usage{
		PromptTokens:     now.PromptTokens - prev.PromptTokens,
		CompletionTokens: now.CompletionTokens - prev.CompletionTokens,
		ChargedCost:      now.ChargedCost - prev.ChargedCost,
		TokenCost:        now.TokenCost - prev.TokenCost,
	}
}

// markUsageBooked treats the current totals as already booked — they belong
// to a conversation restored from a snapshot, whose turns were booked when
// they ran.
func (a *Agent) markUsageBooked() {
	a.usage.mu.Lock()
	a.usage.booked = a.usageTotals()
	a.usage.mu.Unlock()
}
