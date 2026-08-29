//go:build !unix || js

package console

import "time"

// waitForStdinReadable is the non-Unix fallback: it reports "ready" so
// the caller's Read runs and blocks as before. Platforms without POSIX
// poll semantics (Windows console, js/wasm) can't cheaply detect
// readability, so the wake flag is observed before the NEXT read
// attempt instead — the degraded-but-correct behavior. Background
// wakeup batches also prepend to the next query via
// ProcessQueryWithContinuityAs, so no notification is ever lost.
func waitForStdinReadable(fd int, timeout time.Duration) bool {
	_ = fd
	_ = timeout
	return true
}
