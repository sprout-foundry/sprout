package configuration

// RequireTestConfig is the opt-in sub-flag of the verification section that
// requires a test for new behavior: when enabled, a turn that added new
// behavior must either carry an active-plan acceptance item of kind test or
// add/change a test file — verification flags it otherwise. It lives under
// verification because it is enforced by the verification run, not by a
// separate path.
//
// The flag is off by default; a config layer opts in with
// `{"verification": {"require_test": true}}`. It deliberately has no fields
// of its own: the check reads the active plan's acceptance and the turn's
// changed paths, both already available to the verification run.

// RequireTest reports whether the require-a-test-for-new-behavior check is
// enabled for this config. Off by default; it becomes enabled only when a
// config layer names "verification.require_test" with a truthy value that
// survives the layer merge. A nil config resolves to off.
func (c *Config) RequireTest() bool {
	if c == nil {
		return false
	}
	return c.Verification != nil && c.Verification.RequireTest
}
