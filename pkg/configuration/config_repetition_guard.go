package configuration

// RepetitionGuardConfig controls the streamed degenerate-repetition guard:
// while a reply streams, the guard watches the assistant text for a
// repetition loop (the same short line repeated past a threshold) with no
// tool call, cuts the stream and retries the request once with a nudge. The
// guard is ON by default with sane thresholds; a config layer disables it
// with `{"repetition_guard": {"enabled": false}}`, or raises/lowers the
// thresholds for a model that trips it too eagerly or too late.
type RepetitionGuardConfig struct {
	// Enabled gates the guard. On by default; an explicit false in a
	// narrower layer disables a broader layer's enable (explicit-key merge
	// semantics).
	Enabled *bool `json:"enabled,omitempty"`

	// MinRepetitions is how many consecutive identical short lines form a
	// degenerate run before the guard cuts the stream. Zero means "use the
	// default" (DefaultRepetitionMinRepetitions). A value below 2 is
	// treated as unset.
	MinRepetitions int `json:"min_repetitions,omitempty"`

	// MaxLineChars is the longest a line may be to count toward a run.
	// Longer lines are prose, not degenerate tokens. Zero means "use the
	// default" (DefaultRepetitionMaxLineChars).
	MaxLineChars int `json:"max_line_chars,omitempty"`

	// ReasoningEnabled gates the reasoning-channel repetition guard: the
	// same detection applied to the model's streamed reasoning_content.
	// Some model families (DeepSeek V4, Kimi) degenerate inside their
	// thinking — cycling "Let me run it." hundreds of times — while the
	// visible reply stays coherent, so the assistant-text guard never sees
	// it. On by default.
	ReasoningEnabled *bool `json:"reasoning_enabled,omitempty"`

	// ReasoningMinRepetitions is the reasoning-channel run length that
	// triggers detection. Zero means "use the default"
	// (DefaultRepetitionReasoningMinRepetitions), which is lower than the
	// assistant-text default: a thinking loop compounds because the model
	// re-reads its own degenerate reasoning on every later request.
	ReasoningMinRepetitions int `json:"reasoning_min_repetitions,omitempty"`

	// ReasoningMaxLineChars is the longest reasoning line that counts
	// toward a run. Zero means "use the default"
	// (DefaultRepetitionReasoningMaxLineChars).
	ReasoningMaxLineChars int `json:"reasoning_max_line_chars,omitempty"`

	// Recovery controls the escalation that runs when the nudge retry ALSO
	// loops: instead of failing the turn immediately, the provider performs
	// a careful recovery compaction — an in-place de-loop of the
	// degenerate runs plus (optionally) an LLM summary of the middle — and
	// retries once from the re-seeded, shorter state. The degeneration is a
	// self-reinforcing attractor (the model re-reads its own loop back as
	// the immediate prefix on every later request of the turn), so a local
	// nudge often leaves the poisoned state in place; removing the loop
	// text and shortening the context re-seeds the model from a clean entry
	// point. Nil resolves to enabled (the defaults).
	Recovery *RepetitionRecoveryConfig `json:"recovery,omitempty"`
}

// RepetitionRecoveryConfig gates the recovery-compaction escalation of the
// repetition guard (see RepetitionGuardConfig.Recovery).
type RepetitionRecoveryConfig struct {
	// Enabled gates the recovery-compaction escalation. On by default; a
	// narrower layer can disable it with an explicit false (explicit-key
	// merge semantics). When disabled, a second loop fails the turn as
	// before.
	Enabled *bool `json:"enabled,omitempty"`

	// Summary gates the LLM summary of the middle — the "full" compaction.
	// The in-place de-loop + retry always runs when recovery is enabled;
	// Summary adds the lossy middle-summary on top (best-effort: a failed
	// or empty summary falls back to the de-loop-only recovery). On by
	// default.
	Summary *bool `json:"summary,omitempty"`

	// SummarizerEffort is the reasoning effort passed to the recovery
	// summary call. A smaller thinking surface (the default "low") is both
	// faster and less likely to itself degenerate — the failure is most
	// pronounced at the highest effort levels. Unset (or an unrecognized
	// level) resolves to "low"; low/medium/high pass through. This knob
	// degrades to a no-op on models with no reasoning-effort parameter.
	SummarizerEffort string `json:"summarizer_effort,omitempty"`
}

// The default repetition-guard thresholds: a run of six identical short lines
// is degenerate, while four (a plausible list) is not; short means at most 120
// characters.
const (
	DefaultRepetitionMinRepetitions = 6
	DefaultRepetitionMaxLineChars   = 120
)

// The reasoning-channel defaults. A thinking loop is detected earlier than a
// visible one because it is invisible to the user (the reply stays coherent)
// and self-reinforcing (the degenerate reasoning is replayed on every later
// request of the turn, so the model keeps reading its own loop as the
// immediate prefix). Four repeats of a short thinking line is already a
// degenerate run; 80 characters keeps ordinary reasoning sentences — which
// are longer than action announcements — out of the run.
const (
	DefaultRepetitionReasoningMinRepetitions = 4
	DefaultRepetitionReasoningMaxLineChars   = 80
)

// RepetitionGuardReasoningEnabled reports whether the reasoning-channel
// guard is active. On by default; only an explicit
// repetition_guard.reasoning_enabled false turns it off.
func (c *Config) RepetitionGuardReasoningEnabled() bool {
	if c == nil || c.RepetitionGuard == nil || c.RepetitionGuard.ReasoningEnabled == nil {
		return true
	}
	return *c.RepetitionGuard.ReasoningEnabled
}

// RepetitionGuardReasoningMinRepetitions returns the reasoning-channel run
// length that triggers detection. Unset or below 2 falls back to
// DefaultRepetitionReasoningMinRepetitions.
func (c *Config) RepetitionGuardReasoningMinRepetitions() int {
	if c == nil || c.RepetitionGuard == nil || c.RepetitionGuard.ReasoningMinRepetitions < 2 {
		return DefaultRepetitionReasoningMinRepetitions
	}
	return c.RepetitionGuard.ReasoningMinRepetitions
}

// RepetitionGuardReasoningMaxLineChars returns the longest reasoning line
// that counts toward a run. Unset or non-positive falls back to
// DefaultRepetitionReasoningMaxLineChars.
func (c *Config) RepetitionGuardReasoningMaxLineChars() int {
	if c == nil || c.RepetitionGuard == nil || c.RepetitionGuard.ReasoningMaxLineChars <= 0 {
		return DefaultRepetitionReasoningMaxLineChars
	}
	return c.RepetitionGuard.ReasoningMaxLineChars
}

// RepetitionGuardEnabled reports whether the streamed repetition guard is
// active for this config. It is on by default everywhere; only an explicit
// repetition_guard.enabled false turns it off. A nil config resolves to the
// default: enabled.
func (c *Config) RepetitionGuardEnabled() bool {
	if c == nil || c.RepetitionGuard == nil || c.RepetitionGuard.Enabled == nil {
		return true
	}
	return *c.RepetitionGuard.Enabled
}

// RepetitionGuardMinRepetitions returns the run length that triggers the
// guard. Unset or below 2 falls back to DefaultRepetitionMinRepetitions.
func (c *Config) RepetitionGuardMinRepetitions() int {
	if c == nil || c.RepetitionGuard == nil || c.RepetitionGuard.MinRepetitions < 2 {
		return DefaultRepetitionMinRepetitions
	}
	return c.RepetitionGuard.MinRepetitions
}

// RepetitionGuardMaxLineChars returns the longest line that counts toward a
// run. Unset or non-positive falls back to DefaultRepetitionMaxLineChars.
func (c *Config) RepetitionGuardMaxLineChars() int {
	if c == nil || c.RepetitionGuard == nil || c.RepetitionGuard.MaxLineChars <= 0 {
		return DefaultRepetitionMaxLineChars
	}
	return c.RepetitionGuard.MaxLineChars
}

// repetitionRecovery returns the recovery config block, or nil when the
// config or the guard layer is absent. Nil-safe across both layers so the
// accessors below read a single pointer.
func (c *Config) repetitionRecovery() *RepetitionRecoveryConfig {
	if c == nil || c.RepetitionGuard == nil {
		return nil
	}
	return c.RepetitionGuard.Recovery
}

// RepetitionGuardRecoveryEnabled reports whether the recovery-compaction
// escalation is active: when the nudge retry also loops, the provider runs a
// careful de-loop + (optional) LLM-summary compaction and retries once,
// instead of failing the turn. On by default; only an explicit
// repetition_guard.recovery.enabled false turns it off. A nil recovery block
// resolves to the default: enabled.
func (c *Config) RepetitionGuardRecoveryEnabled() bool {
	r := c.repetitionRecovery()
	if r == nil {
		return true
	}
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// RepetitionGuardRecoverySummary reports whether the LLM-summary half of the
// recovery compaction (the "full" middle summary, as opposed to the in-place
// de-loop that always runs) is active. On by default; only an explicit
// repetition_guard.recovery.summary false turns it off. A nil recovery block
// resolves to the default: enabled.
func (c *Config) RepetitionGuardRecoverySummary() bool {
	r := c.repetitionRecovery()
	if r == nil {
		return true
	}
	if r.Summary == nil {
		return true
	}
	return *r.Summary
}

// RepetitionGuardRecoverySummarizerEffort returns the reasoning effort for
// the recovery summary call. A small thinking surface (the default "low") is
// both faster and less likely to itself degenerate. Unset or an unrecognized
// level resolves to "low"; recognized levels (low/medium/high) pass
// through. This knob degrades to a no-op on models with no
// reasoning-effort parameter.
func (c *Config) RepetitionGuardRecoverySummarizerEffort() string {
	const defaultEffort = "low"
	r := c.repetitionRecovery()
	if r == nil {
		return defaultEffort
	}
	switch r.SummarizerEffort {
	case "low", "medium", "high":
		return r.SummarizerEffort
	default:
		return defaultEffort
	}
}
