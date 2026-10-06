// Package noninteractive provides utilities for detecting and handling
// provider-not-configured errors in non-interactive environments
// (daemons, CI, piped stdin).
package noninteractive

import "strings"

// HelpHint is the canonical guidance shown when a provider is not configured
// in non-interactive environments (daemons, CI, piped stdin).
const HelpHint = "Run `sprout keys set <provider>` or export the provider's API key (e.g. DEEPINFRA_API_KEY), or run `sprout` in a terminal for guided setup"

// IsNonInteractiveHint checks if an error message contains the HelpHint text.
// This is a sentinel check for callers to detect
// provider-not-configured-in-non-interactive errors.
func IsNonInteractiveHint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), HelpHint)
}
