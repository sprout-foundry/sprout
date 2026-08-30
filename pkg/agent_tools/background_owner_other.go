//go:build !unix && !windows

package tools

// ownerProcessAlive reports whether the sprout process that owns a
// background session is still running. On js/wasm and plan9 there are no
// POSIX-style process probes (and orphan cleanup doesn't run there), so
// report dead.
func ownerProcessAlive(ownerPID int) bool {
	return false
}
