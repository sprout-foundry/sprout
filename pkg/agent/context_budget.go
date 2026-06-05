package agent

// SP-066 Phase 1: model-aware context reservations.
//
// seed's chat loop triggers substitution + LLM-fall-through compaction when
// the prompt would exceed `CompactionTriggerFraction × max_context_tokens`.
// Its default (0.85) leaves 15% of the window for the model's response, which
// is too tight for thinking-budget models (Gemini 2.5+, Claude extended
// thinking, etc.) — the model spends the remaining tokens on thinking and
// emits nothing user-visible.
//
// We compute the trigger fraction here by subtracting conservative
// reservations for the response, thinking budget, and tool I/O. The
// reservations are intentionally generous because, under substitution-first
// context management (SP-066), substitution itself is free; the reservation
// only gates the rare LLM-fall-through compaction path. A more aggressive
// reservation costs nothing in healthy operation.

const (
	// reservedForResponseFraction is the share of max_context_tokens
	// reserved for the model's output (max_output_tokens worst case).
	reservedForResponseFraction = 0.15

	// reservedForThinkingFraction is the share reserved for the model's
	// internal thinking budget. For thinking-enabled models that don't
	// expose a configured budget, this is a worst-case allowance.
	reservedForThinkingFraction = 0.10

	// reservedForToolIOFraction is the share reserved for tool inputs and
	// outputs that arrive *after* prompt build (the next iteration's tool
	// results need to fit alongside the existing context).
	reservedForToolIOFraction = 0.05
)

// totalReservedFraction is the share of max_context_tokens we keep
// unused so the model has room to think + respond + accept tool I/O.
func totalReservedFraction() float64 {
	return reservedForResponseFraction + reservedForThinkingFraction + reservedForToolIOFraction
}

// computeCompactionTriggerFraction returns the share of the context window
// at which seed should trigger compaction. Substitution fires before the
// prompt consumes (1 − reservation) × max_context_tokens — leaving the
// reserved share available for response, thinking, and tool I/O.
//
// With the conservative 15/10/5 defaults the trigger fires at 70% of the
// context window instead of seed's hardcoded 85%, which restores enough
// headroom to keep thinking-budget models from emitting empty responses.
func (a *Agent) computeCompactionTriggerFraction() float64 {
	return 1.0 - totalReservedFraction()
}
