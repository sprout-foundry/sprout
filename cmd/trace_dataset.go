//go:build !js

package cmd

import "github.com/sprout-foundry/sprout/pkg/configuration"

// getTraceDatasetDir returns trace dataset directory from flag or env var
func getTraceDatasetDir(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	// Check env var as fallback
	dir, ok := configuration.LookupEnv("TRACE_DATASET_DIR")
	if ok && dir != "" {
		return dir
	}
	return ""
}
