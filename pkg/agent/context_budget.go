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
// overrides this with a tighter fraction (0.85).
func (a *Agent) computeCompactionTriggerFraction() float64 {
	if f := a.contextProfile.CompactionTriggerFraction; f > 0 {
		return f
	}
	return 1.0 - totalReservedFraction()
}
