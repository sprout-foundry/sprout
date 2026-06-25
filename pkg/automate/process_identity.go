//go:build !js

package automate

import "time"

// VerifyProcessStartedBefore returns true if the process at the given PID
// is confirmed to have started before the cutoff time, providing protection
// against PID reuse. The caller should record the session's start time and
// pass it here before signaling.
//
// Returns false when the process at this PID started after the cutoff
// (indicating the original process died and the OS recycled the PID).
// Returns true when the process is not alive (nothing to signal) or on
// platforms where the check is unavailable (fail-open).
func VerifyProcessStartedBefore(pid int, startedAt time.Time) bool {
	if !IsProcessAlive(pid) {
		return true // process is gone — not a PID-reuse scenario
	}
	// Allow a generous grace window: the recorded start time may be slightly
	// later than the OS-visible process creation time due to clock skew
	// between session-file write and actual fork+exec.
	return processStartedBefore(pid, startedAt.Add(time.Minute))
}
