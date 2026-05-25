package proxy

import (
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// MergeServers merges default language server configurations with user overrides.
// User overrides take precedence by ID: if a user override has the same ID as
// a default, it replaces the default. If a user override has a new ID not in
// defaults, it is appended to the merged list.
func MergeServers(defaults []LanguageServerConfig, overrides []configuration.LanguageServerOverride) []LanguageServerConfig {
	// Build a map of overrides by ID for quick lookup
	overrideMap := make(map[string]configuration.LanguageServerOverride, len(overrides))
	for _, o := range overrides {
		overrideMap[o.ID] = o
	}

	var merged []LanguageServerConfig

	// Process defaults: replace if overridden, otherwise keep
	for _, d := range defaults {
		if override, ok := overrideMap[d.ID]; ok {
			// Override replaces default
			installHint := d.InstallHint // Keep original install hint by default
			if override.InstallHint != "" {
				installHint = override.InstallHint // Prefer override's hint if non-empty
			}
			merged = append(merged, LanguageServerConfig{
				ID:          override.ID,
				Binary:      override.Binary,
				Args:        override.Args,
				LanguageIDs: override.LanguageIDs,
				InstallHint: installHint,
			})
			delete(overrideMap, d.ID)
		} else {
			merged = append(merged, d)
		}
	}

	// Append any remaining overrides (new IDs not in defaults)
	for _, o := range overrideMap {
		merged = append(merged, LanguageServerConfig{
			ID:          o.ID,
			Binary:      o.Binary,
			Args:        o.Args,
			LanguageIDs: o.LanguageIDs,
			InstallHint: "", // No install hint for user-defined servers
		})
	}

	return merged
}
