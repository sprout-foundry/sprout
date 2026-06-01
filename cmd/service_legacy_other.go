//go:build !linux && !darwin && !js

package cmd

// removeLegacyServices is a no-op on platforms without dedicated legacy
// service-manager integrations (Windows, etc.). The legacy paths only
// existed on Linux (systemd) and macOS (launchd); other platforms never
// shipped them, so there is nothing to clean up.
func removeLegacyServices(_ []string) error {
	return nil
}
