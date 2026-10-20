package agent

// Model-aware context reservations for compaction trigger calculation.
// We compute the trigger fraction by subtracting conservative reservations
// for the response, thinking budget, and tool I/O.

const (
	reservedForResponseFraction = 0.15
	reservedForThinkingFraction = 0.10
	reservedForToolIOFraction   = 0.05
)

func totalReservedFraction() float64 {
	return reservedForResponseFraction + reservedForThinkingFraction + reservedForToolIOFraction
}

// computeCompactionTriggerFraction returns the share of the context window
// at which seed should trigger compaction. In Low-Context Mode the profile
// overrides this with a tighter fraction (0.85). This is the WINDOW-SHARE
// trigger (rel.2); for a large window the absolute ceiling may bind first —
// see compactionTriggerFractionFor, which takes the lower of the two.
func (a *Agent) computeCompactionTriggerFraction() float64 {
	if f := a.contextProfile.CompactionTriggerFraction; f > 0 {
		return f
	}
	return 1.0 - totalReservedFraction()
}

// defaultCompactionCeilingTokens is the built-in absolute ceiling on the live
// conversation (rel.2). For a 1M-token window the window-share trigger
// (0.70 → ~734K) would let an automate coordinator run for hundreds of
// thousands of tokens with nothing compacted; an absolute 160K ceiling keeps
// the working set bounded no matter how large the model's window is.
const defaultCompactionCeilingTokens = 160 * 1024

// compactionCeilingFor returns the configured absolute live-context ceiling,
// or the built-in 160K default when unset. A value of 0 means "use the
// built-in default" (the field is an optional override, not a switch).
func (a *Agent) compactionCeilingFor() int {
	if cfg := a.GetConfig(); cfg != nil && cfg.CompactionCeilingTokens > 0 {
		return cfg.CompactionCeilingTokens
	}
	return defaultCompactionCeilingTokens
}

// compactionTriggerFractionFor is the effective compaction trigger fraction
// for a model with the given context window (rel.2): the lower of the
// window-share fraction and the absolute live-context ceiling expressed as a
// share of the window. For a 1M window with the 160K ceiling this is ~0.16
// rather than 0.70, so compaction fires at 160K of live conversation, well
// before the quality degrades on a near-full context.
func (a *Agent) compactionTriggerFractionFor(contextLimit int) float64 {
	base := a.computeCompactionTriggerFraction()
	ceiling := a.compactionCeilingFor()
	if contextLimit <= 0 || ceiling <= 0 {
		return base
	}
	ceilingFraction := float64(ceiling) / float64(contextLimit)
	// Whichever is lower binds: a small window's share is already below the
	// ceiling; a large window's share is not, so the ceiling takes over.
	if ceilingFraction < base {
		return ceilingFraction
	}
	return base
}
