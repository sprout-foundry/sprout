package envutil

import "os"

// ResolveColorPreference applies the no-color.org environment overrides to
// a caller-supplied preference. SP-048-4a.
//
// Precedence (no-color.org-mandated):
//   - NO_COLOR set to any non-empty value → colors OFF (always wins)
//   - FORCE_COLOR set to any non-empty value → colors ON (unless NO_COLOR)
//   - otherwise → caller's want value
//
// Lives in pkg/envutil because pkg/console depends on pkg/configuration
// (transitively pkg/utils) so pkg/utils can't import pkg/console without
// creating a cycle. pkg/envutil is the existing zero-dependency leaf used
// for env-var helpers.
func ResolveColorPreference(want bool) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	return want
}
