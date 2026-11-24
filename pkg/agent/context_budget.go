package agent

import core "github.com/sprout-foundry/seed/core"

import api "github.com/sprout-foundry/sprout/pkg/agent_api"

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

// compactionUnderestimateBound is the worst-case fraction by which the token
// estimate can run BELOW the provider's true count. It is derived from
// token_utils.go's EstimationErrorPercent, which the estimator documents at
// 25-34% on tool-heavy prompts (and the estimator is the value the seed
// compaction trigger compares against — especially right after a compaction
// pass, when the token anchor is invalidated and the trigger runs on the raw
// heuristic). Because the estimate underestimates, a trigger fraction above
// (1 - this bound) can let the *true* prompt size cross the window before
// compaction fires: at the trigger the estimate is `trigger * window`, and
// the worst-case true size is `estimate / (1 - bound)` = `window` only when
// `trigger <= 1 - bound`.
func compactionUnderestimateBound() float64 {
	return float64(api.EstimationErrorPercent) / 100
}

// computeCompactionTriggerFraction returns the share of the context window
// at which seed should trigger compaction. In Low-Context Mode the profile
// overrides this with a tighter fraction (0.85). This is the WINDOW-SHARE
// trigger (rel.2); for a large window the absolute ceiling may bind first —
// see compactionTriggerFractionFor, which takes the lower of the two.
//
// The profile fraction is clamped to the upper bound of
// min(1 - compactionUnderestimateBound, 1 - totalReservedFraction) — the
// stricter of the no-overflow bound (estimator worst-case underestimate must
// still cross the trigger before the true size does) and the headroom bound
// (at least the response/thinking/tool-I/O reservation is left for the
// model's output). Without the clamp, a profile trigger above the bound
// (LCM's 0.85 vs the 0.70 bound) lets the raw, underestimating heuristic sit
// below the trigger while the true size is already over the window — so the
// trigger under-reacts and the provider overflow + recovery path becomes the
// backstop instead of the proactive compaction it is meant to be. Clamping
// down makes the trigger conservative: even at the worst-case underestimate,
// the trigger fires before the true size crosses the window.
func (a *Agent) computeCompactionTriggerFraction() float64 {
	base := 1.0 - totalReservedFraction()
	if f := a.contextProfile.CompactionTriggerFraction; f > 0 {
		base = f
	}
	// The trigger can never exceed the upper bound below, for two reasons that
	// are equal today (both 30%) but must stay independently conservative if
	// either constant is tuned:
	//   1. no-overflow bound (1 - compactionUnderestimateBound): even at the
	//      estimator's documented worst-case underestimate, the trigger must
	//      fire before the TRUE prompt size crosses the window. Without it, an
	//      LCM trigger above 0.70 lets the raw (underestimating) heuristic sit
	//      below the trigger while the true size is already over the window, so
	//      proactive compaction under-reacts and the overflow+recovery path
	//      becomes the backstop it is meant to prevent.
	//   2. headroom bound (1 - totalReservedFraction): the trigger must leave
	//      at least the response/thinking/tool-I/O reservation for the model's
	//      output.
	// Both hold only when T <= min(1-band, 1-reserved) = 1 - max(band, reserved).
	upperBound := 1.0 - max(compactionUnderestimateBound(), totalReservedFraction())
	if base > upperBound {
		base = upperBound
	}
	return base
}

// compactionSettleShare is how far below its trigger a proactive compaction
// settles, as a share of the trigger. Every compaction makes the provider
// re-read the conversation after the cut, which on a self-hosted GPU costs the
// compute other runs are waiting for; settling well below the trigger means
// one re-read per long stretch of work rather than one every few tool calls.
const compactionSettleShare = 0.30

// compactionSettleMarginFor is the settle margin, as a share of the window,
// for a compaction trigger that is itself a share of the window.
func compactionSettleMarginFor(trigger float64) float64 {
	return trigger * compactionSettleShare
}

// A compaction first asks the model for a summary in a request that continues
// the live one, so the provider's prompt cache serves the whole prefix, and
// settles the conversation near continuationTargetTokens: the summary plus the
// latest steps verbatim. The settle margin above is the fallback's.
const (
	continuationTargetTokens  = 24_000
	continuationSummaryTokens = 6_000
	// minContinuationTargetTokens is the smallest target worth a summary
	// call; a model whose trigger leaves less keeps the fallback alone.
	minContinuationTargetTokens = 8_000
)

// continuationCompactionFor is the continuation compaction for a model with
// the given window and trigger fraction, or nil when the trigger leaves too
// little room beneath it.
func continuationCompactionFor(contextLimit int, trigger float64) *core.ContinuationCompaction {
	triggerTokens := int(float64(contextLimit) * trigger)
	target := min(continuationTargetTokens, triggerTokens/2)
	if target < minContinuationTargetTokens {
		return nil
	}
	return &core.ContinuationCompaction{
		TargetTokens:  target,
		SummaryTokens: min(continuationSummaryTokens, target/4),
	}
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
// The trigger is the lower of the window-share fraction, the absolute
// live-context ceiling, and the floor-overflow zone edge. For a 1M window
// with the 300K ceiling this is ~0.29 rather than 0.70, so compaction fires
// at 300K of live conversation, well before quality degrades on a near-full
// context.
//
// A user cap (Config.MaxContextTokens) is an explicit statement
// about how much context to use, so it wins over the ceiling: the trigger is
// never later than the cap. Measuring the ceiling against the capped window
// instead made a 300K ceiling on a 1M model become a 0.70 share of a 300K cap
// (firing at 210K, EARLIER than the uncapped 307K), so raising the cap moved
// the trigger the wrong way.
//
// The floor-overflow zone edge guards small windows: the output budget's
// floor step (token_utils.go) re-inflates a starved budget to MinOutputTokens
// even when remaining is below it, so every input in [window-MinOutputTokens+1,
// window-1] produces a request that overflows the window. On a 32K window the
// zone begins at ~49% of the window — the window-share clamp (0.70) fires deep
// inside it. Clamping the trigger to the zone edge keeps the estimated input
// at or below window-MinOutputTokens, where the floor step reports exactly the
// window, never over it. Windows at or below MinOutputTokens skip the bound
// entirely: the tiny-window exception in the floor step already degrades to
// the remaining space there.
func (a *Agent) compactionTriggerFractionFor(contextLimit int) float64 {
	if contextLimit <= 0 {
		return a.computeCompactionTriggerFraction()
	}
	base := a.computeCompactionTriggerFraction()
	ceiling := a.compactionCeilingFor()
	if ceiling <= 0 {
		return base
	}
	// Resolve the triggers as absolute token counts and take the lower: the
	// window-share trigger, the floor-overflow zone edge, and the absolute
	// ceiling. A user cap is already reflected in contextLimit, so a cap below
	// the ceiling makes the cap's own share (base) the binding trigger and the
	// ceiling simply does not bind — compaction never fires later than the cap.
	triggerTokens := float64(contextLimit) * base
	if zoneEdge := float64(contextLimit - api.MinOutputTokens); zoneEdge > 0 && triggerTokens > zoneEdge {
		triggerTokens = zoneEdge
	}
	if float64(ceiling) < triggerTokens {
		triggerTokens = float64(ceiling)
	}
	return triggerTokens / float64(contextLimit)
}

// CompactionCeilingTokens returns the effective absolute live-context ceiling:
// the configured value when set, otherwise the built-in default.
func (a *Agent) CompactionCeilingTokens() int {
	return a.compactionCeilingFor()
}
