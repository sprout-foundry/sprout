// Package noninteractive provides utilities for detecting and handling
// provider-not-configured errors in non-interactive environments
// (daemons, CI, piped stdin).
package noninteractive

import "strings"

// HelpHint is the canonical guidance shown when a provider is not configured
// in non-interactive environments (daemons, CI, piped stdin).
const HelpHint = "Set LEDIT_PROVIDER / configure ~/.ledit/config.json, or run `ledit agent` interactively"

// IsNonInteractiveHint checks if an error message contains the HelpHint text.
// This is a sentinel check for callers to detect
// provider-not-configured-in-non-interactive errors.
func IsNonInteractiveHint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), HelpHint)
}
