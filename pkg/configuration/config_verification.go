package configuration

// VerificationConfig controls the SP-149 verification run ("verified done").
// The section is off by default in the CLI (SP-149 149e); a config layer —
// global, project (workspace), or an embedding environment writing the
// same layers — opts in with `{"verification": {"enabled": true}}`.
type VerificationConfig struct {
	// Enabled gates the turn-end verification run (SP-149 149e). Off by
	// default; an explicit false in a narrower layer disables a broader
	// layer's enable (explicit-key merge semantics).
	Enabled bool `json:"enabled,omitempty"`

	// RepairAttempts is the repair-attempt limit N for the stopping rule
	// (SP-149 149c): how many repair attempts on the same failing check
	// before the verification loop stops. Zero means "use the default"
	// (DefaultVerificationRepairAttempts).
	RepairAttempts int `json:"repair_attempts,omitempty"`

	// TotalRepairRounds caps the total number of repair rounds the
	// verification loop may run in one turn, on top of the per-check
	// limit N: the loop stops once this many rounds have run even if no
	// single check has exhausted its own attempts. It exists for failure
	// patterns the per-check counters cannot see — two checks
	// alternating failures between rounds (each key's counter stays
	// under N), or interaction checks whose item id (and therefore
	// counter key) is new every round — which would otherwise keep the
	// loop alive indefinitely. Zero means "use the default"
	// (DefaultVerificationRepairTotalRounds).
	TotalRepairRounds int `json:"total_repair_rounds,omitempty"`

	// BuildCommand is the explicit build command for the project's
	// verification run (SP-149 149b). It is the "explicit project
	// configuration" source: a human sets it in the project layer
	// (.sprout/workspace.json) or in global config for projects without a
	// starter manifest (or one that declares no build command). Model
	// output never populates it — the verification runner ignores
	// commands the model proposes.
	BuildCommand string `json:"build_command,omitempty"`

	// TestCommand is the explicit test command for the project's
	// verification run (SP-149 149b). Same source and semantics as
	// BuildCommand.
	TestCommand string `json:"test_command,omitempty"`

	// RequireTest requires a test for new behavior: when enabled, a turn
	// that added new behavior must either carry an active-plan acceptance
	// item of kind test or add/change a test file, and verification flags
	// the turn otherwise. Off by default; it becomes enabled only when a
	// config layer names "verification.require_test" with a truthy value.
	// Enforced through the verification run so the flag flows through the
	// same turn-end reporting and repair loop as the other checks.
	RequireTest bool `json:"require_test,omitempty"`
}

// DefaultVerificationRepairAttempts is the small default for the SP-149
// 149c stopping rule: after this many repair attempts on the same failing
// check the loop stops and reports the failure (149d).
const DefaultVerificationRepairAttempts = 3

// DefaultVerificationRepairTotalRounds is the default total cap on repair
// rounds the verification loop may run in one turn when the per-check
// limit N is also at its default: twice DefaultVerificationRepairAttempts.
// The default actually applied derives from the effective per-check limit
// (defaultVerificationRepairTotalRounds), so it scales with an explicitly
// raised N instead of silently clamping it.
const DefaultVerificationRepairTotalRounds = DefaultVerificationRepairAttempts * 2

// defaultVerificationRepairTotalRounds is the derived default for the
// total repair-rounds cap: twice the effective per-check limit N. A single
// hard check can still burn its full N attempts (the per-check rule keeps
// firing first), while failure patterns the per-check counters cannot see
// — two checks alternating failures between rounds, or interaction checks
// whose counter key is new every round — are stopped after a bounded
// number of rounds.
func defaultVerificationRepairTotalRounds(attemptLimit int) int {
	return attemptLimit * 2
}

// Resolve returns a copy with defaults filled in for zero-value fields.
// Safe to call on nil: an unset section resolves to off with the default
// repair-attempt and total-rounds caps.
func (c *VerificationConfig) Resolve() VerificationConfig {
	result := VerificationConfig{
		RepairAttempts:    DefaultVerificationRepairAttempts,
		TotalRepairRounds: DefaultVerificationRepairTotalRounds,
	}
	if c != nil {
		result.Enabled = c.Enabled
		result.RequireTest = c.RequireTest
		if c.RepairAttempts > 0 {
			result.RepairAttempts = c.RepairAttempts
			// The total-rounds default derives from the effective
			// per-check limit, so an explicitly raised N is never left
			// clamped by the default total.
			result.TotalRepairRounds = defaultVerificationRepairTotalRounds(c.RepairAttempts)
		}
		if c.TotalRepairRounds > 0 {
			result.TotalRepairRounds = c.TotalRepairRounds
		}
	}
	return result
}

// VerificationEnabled reports whether the SP-149 verification run is enabled
// for this config. Off by default in the CLI (SP-149 149e); it becomes
// enabled only when a config layer names "verification.enabled" with a
// truthy value that survives the layer merge. A nil config resolves to off.
func (c *Config) VerificationEnabled() bool {
	if c == nil {
		return false
	}
	return c.Verification != nil && c.Verification.Enabled
}

// VerificationRepairAttempts returns the repair-attempt limit N (SP-149
// 149c) for the verification stopping rule. Unset or non-positive values
// fall back to DefaultVerificationRepairAttempts.
func (c *Config) VerificationRepairAttempts() int {
	if c == nil || c.Verification == nil {
		return DefaultVerificationRepairAttempts
	}
	if c.Verification.RepairAttempts > 0 {
		return c.Verification.RepairAttempts
	}
	return DefaultVerificationRepairAttempts
}

// VerificationRepairTotalRounds returns the total cap on repair rounds the
// verification loop may run in one turn (SP-149 149c). It is a separate,
// explicit bound on top of the per-check limit: whichever of the two
// stopping rules fires first ends the loop, so alternating-failure or
// fresh-counter-key patterns cannot keep the loop alive indefinitely.
//
// An explicit value is honored. Unset or non-positive values fall back to
// a derived default — twice the effective per-check limit
// (VerificationRepairAttempts) — so the total cap scales with an
// explicitly raised N instead of silently clamping it. Set it explicitly
// (e.g. 6) to pin the bound regardless of N.
func (c *Config) VerificationRepairTotalRounds() int {
	if c == nil || c.Verification == nil {
		return defaultVerificationRepairTotalRounds(DefaultVerificationRepairAttempts)
	}
	if c.Verification.TotalRepairRounds > 0 {
		return c.Verification.TotalRepairRounds
	}
	return defaultVerificationRepairTotalRounds(c.VerificationRepairAttempts())
}

// VerificationBuildCommand returns the explicit build command for the
// SP-149 verification run (149b) after layer merge, or "" when no layer
// set it. It is the "explicit project configuration" source of commands,
// set by a human — never by model output.
func (c *Config) VerificationBuildCommand() string {
	if c == nil || c.Verification == nil {
		return ""
	}
	return c.Verification.BuildCommand
}

// VerificationTestCommand returns the explicit test command for the SP-149
// verification run (149b) after layer merge, or "" when no layer set it.
func (c *Config) VerificationTestCommand() string {
	if c == nil || c.Verification == nil {
		return ""
	}
	return c.Verification.TestCommand
}
