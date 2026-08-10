package cmd

import (
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// isLocalProvider reports whether the active provider is sprout-local,
// either via SPROUT_PROVIDER env var or the persisted config.
func isLocalProvider() bool {
	if p := strings.TrimSpace(os.Getenv("SPROUT_PROVIDER")); p == "sprout-local" {
		return true
	}
	cfg, err := configuration.Load()
	if err != nil || cfg == nil {
		return false
	}
	return cfg.LastUsedProvider == "sprout-local"
}
