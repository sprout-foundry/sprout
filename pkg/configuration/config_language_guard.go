package configuration

// LanguageGuardEnabled reports whether the outbound language guard (SP-152)
// is active for this config. The guard is on by default everywhere,
// including the CLI (SP-152 152f); only an explicit disable_language_guard
// in a config layer turns it off. A nil config resolves to the default:
// enabled.
func (c *Config) LanguageGuardEnabled() bool {
	if c == nil {
		return true
	}
	return !c.DisableLanguageGuard
}
