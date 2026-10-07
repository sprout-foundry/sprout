package agent

import "sync"

// Usage is an amount of model usage: tokens and what they cost. Role is the
// role the usage is attributed to; it is empty on the overall ledger view
// (TakeUnbookedUsage) and set on the per-role view.
type Usage struct {
	Role             string
	PromptTokens     int
	CompletionTokens int
	ChargedCost      float64
	TokenCost        float64
}

// usageLedger remembers how much of the agent's running usage totals has
// already been booked elsewhere (a cost ledger), so each booking takes only
// what is new. The overall mark (booked) tracks the conversation totals; the
// per-role marks (bookedByRole) track each role's totals independently.
type usageLedger struct {
	mu           sync.Mutex
	booked       Usage
	bookedByRole map[string]Usage
}

func (a *Agent) usageTotals() Usage {
	return Usage{
		PromptTokens:     a.GetPromptTokens(),
		CompletionTokens: a.GetCompletionTokens(),
		ChargedCost:      a.GetChargedCostTotal(),
		TokenCost:        a.GetTokenCostTotal(),
	}
}

// roleUsageTotals returns the agent's per-role running usage totals as a map
// keyed by role. Each value carries its Role.
// When every LLM call records a cost entry with its role, the per-role totals
// sum to the overall totals (the metrics manager accumulates both from the
// same entries).
func (a *Agent) roleUsageTotals() map[string]Usage {
	out := make(map[string]Usage)
	for _, ru := range a.GetRoleUsage() {
		out[ru.Role] = Usage{
			Role:             ru.Role,
			PromptTokens:     ru.PromptTokens,
			CompletionTokens: ru.CompletionTokens,
			ChargedCost:      ru.ChargedCost,
			TokenCost:        ru.TokenCost,
		}
	}
	return out
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

// TakeUnbookedUsageByRole returns the per-role usage accrued since the
// previous call and marks each role booked. The
// delta for each role is computed against that role's own mark, with the
// same reset detection as the overall view (a per-role total below its mark
// means it was reset, so everything since the reset is new). The values are
// Usage with Role set.
//
// The overall and per-role views agree: because the per-role totals feed the
// overall totals, the sum of the per-role deltas equals TakeUnbookedUsage's
// delta (each LLM call records one cost entry that lands in exactly one
// role's bucket and in the overall totals).
func (a *Agent) TakeUnbookedUsageByRole() map[string]Usage {
	a.usage.mu.Lock()
	defer a.usage.mu.Unlock()
	if a.usage.bookedByRole == nil {
		a.usage.bookedByRole = make(map[string]Usage)
	}
	nowByRole := a.roleUsageTotals()
	out := make(map[string]Usage, len(nowByRole))
	for role, now := range nowByRole {
		prev := a.usage.bookedByRole[role]
		if now.PromptTokens < prev.PromptTokens || now.CompletionTokens < prev.CompletionTokens ||
			now.ChargedCost < prev.ChargedCost || now.TokenCost < prev.TokenCost {
			prev = Usage{}
		}
		a.usage.bookedByRole[role] = now
		out[role] = Usage{
			Role:             role,
			PromptTokens:     now.PromptTokens - prev.PromptTokens,
			CompletionTokens: now.CompletionTokens - prev.CompletionTokens,
			ChargedCost:      now.ChargedCost - prev.ChargedCost,
			TokenCost:        now.TokenCost - prev.TokenCost,
		}
	}
	return out
}

// markUsageBooked treats the current totals as already booked — they belong
// to a conversation restored from a snapshot, whose turns were booked when
// they ran. The per-role marks are marked booked the same way.
func (a *Agent) markUsageBooked() {
	a.usage.mu.Lock()
	a.usage.booked = a.usageTotals()
	if a.usage.bookedByRole == nil {
		a.usage.bookedByRole = make(map[string]Usage)
	}
	a.usage.bookedByRole = a.roleUsageTotals()
	a.usage.mu.Unlock()
}
