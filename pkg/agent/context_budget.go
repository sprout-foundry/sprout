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
// thousands of tokens with nothing compacted; an absolute 300K ceiling keeps
// the working set bounded no matter how large the model's window is.
//
// 300K is a continuity/quality tradeoff, not a hard limit: a large-window
// model pays for its window, so compacting at ~16% of it discards context the
// model could still use. The ceiling sits below the range where long-context
// tool-call reliability degrades on the large-window families in use, while
// leaving enough live history that turns rarely re-read what they already
// read. A tighter value (e.g. 160K) trades continuity for a smaller working
// set; compaction_ceiling_tokens sets either per project.
const defaultCompactionCeilingTokens = 300 * 1024

// compactionCeilingFor returns the configured absolute live-context ceiling,
// or the built-in 300K default when unset. A value of 0 means "use the
// built-in default" (the field is an optional override, not a switch).
func (a *Agent) compactionCeilingFor() int {
	if cfg := a.GetConfig(); cfg != nil && cfg.CompactionCeilingTokens > 0 {
		return cfg.CompactionCeilingTokens
	}
	return defaultCompactionCeilingTokens
}

// compactionTriggerFractionFor is the effective compaction trigger fraction
// for a model with the given effective context window (rel.2).
//
// The trigger is the lower of the window-share fraction and the absolute
// live-context ceiling. For a 1M window with the 300K ceiling this is ~0.29
// rather than 0.70, so compaction fires at 300K of live conversation, well
// before quality degrades on a near-full context.
//
// A user cap (Config.MaxContextTokens, /max-context) is an explicit statement
// about how much context to use, so it wins over the ceiling: the trigger is
// never later than the cap. Measuring the ceiling against the capped window
// instead made a 300K ceiling on a 1M model become a 0.70 share of a 300K cap
// (firing at 210K, EARLIER than the uncapped 307K), so raising the cap moved
// the trigger the wrong way.
func (a *Agent) compactionTriggerFractionFor(contextLimit int) float64 {
	if contextLimit <= 0 {
		return a.computeCompactionTriggerFraction()
	}
	base := a.computeCompactionTriggerFraction()
	ceiling := a.compactionCeilingFor()
	if ceiling <= 0 {
		return base
	}
	// Resolve both triggers as absolute token counts and take the lower: the
	// window-share trigger, and the absolute ceiling. A user cap is already
	// reflected in contextLimit, so a cap below the ceiling makes the cap's
	// own share (base) the binding trigger and the ceiling simply does not
	// bind — compaction never fires later than the cap.
	triggerTokens := float64(contextLimit) * base
	if float64(ceiling) < triggerTokens {
		triggerTokens = float64(ceiling)
	}
	return triggerTokens / float64(contextLimit)
}
