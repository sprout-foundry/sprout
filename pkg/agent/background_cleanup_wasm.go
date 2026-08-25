//go:build js

package agent

// cleanupOrphanedBackgroundProcesses is a no-op in WASM — background processes
// are a desktop/daemon-only feature with no equivalent in the browser.
func cleanupOrphanedBackgroundProcesses(debug bool) {
}
