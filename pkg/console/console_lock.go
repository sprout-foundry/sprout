package console

import "sync"

// outputMu serializes terminal-chrome writes (InputReader render, status
// footer draw, activity-indicator spinner clear / replace) so the
// cursor-positioning sequences they emit can't interleave. Without
// this lock a footer Refresh fired by a late tool event can land in
// the middle of the InputReader's render sequence and displace the
// cursor — subsequent keystrokes then print at the wrong screen
// position, which looks like "characters were dropped" even though
// they're in the line buffer.
//
// Hold the lock only for the duration of a single atomic render. Do
// not call user-supplied callbacks or block on I/O while holding it.
var outputMu sync.Mutex

// LockOutput acquires the console output mutex.
func LockOutput() { outputMu.Lock() }

// UnlockOutput releases the console output mutex.
func UnlockOutput() { outputMu.Unlock() }

// TryLockOutput attempts to acquire the console output mutex without blocking.
// Returns true if the lock was acquired, false if it is held by another goroutine.
// Callers MUST check the return value and only call UnlockOutput on true.
func TryLockOutput() bool {
	return outputMu.TryLock()
}

// WithOutput runs fn while holding the console output mutex. Use this
// wrapper for short, self-contained ANSI render sequences.
func WithOutput(fn func()) {
	outputMu.Lock()
	defer outputMu.Unlock()
	fn()
}
