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
}

// The default repetition-guard thresholds: a run of six identical short lines
// is degenerate, while four (a plausible list) is not; short means at most 120
// characters.
const (
	DefaultRepetitionMinRepetitions = 6
	DefaultRepetitionMaxLineChars   = 120
)

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
