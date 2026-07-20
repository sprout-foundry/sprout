//go:build js && !windows

package pidalive

// IsAlive is a stub for WASM/JS targets where process checking is not available.
// Returns false as there's no way to determine process liveness in the browser sandbox.
func IsAlive(pid int) bool {
	return false
}
