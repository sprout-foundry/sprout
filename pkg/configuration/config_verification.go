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
}

// DefaultVerificationRepairAttempts is the small default for the SP-149
// 149c stopping rule: after this many repair attempts on the same failing
// check the loop stops and reports the failure (149d).
const DefaultVerificationRepairAttempts = 3

// Resolve returns a copy with defaults filled in for zero-value fields.
// Safe to call on nil: an unset section resolves to off with the default
// repair-attempt limit.
func (c *VerificationConfig) Resolve() VerificationConfig {
	result := VerificationConfig{
		RepairAttempts: DefaultVerificationRepairAttempts,
	}
	if c != nil {
		result.Enabled = c.Enabled
		if c.RepairAttempts > 0 {
			result.RepairAttempts = c.RepairAttempts
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
