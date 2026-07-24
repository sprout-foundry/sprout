package console

import (
	"fmt"
	"strings"
	"sync"
)

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
// Lock ordering contract (MUST be followed to avoid deadlocks):
//
//   AssistantTurnRenderer.mu  →  outputMu  →  StatusFooter.mu
//
// - renderer.mu is always acquired first (by WriteChunk / OnExternalWrite),
//   then outputMu inside the method body.
// - outputMu serializes all terminal-chrome writes; it is acquired
//   second, after renderer.mu.
// - footer.mu is acquired last, only for brief field reads/writes
//   (never during I/O).
// - SetProseStreaming acquires ONLY footer.mu — never outputMu —
//   because it is called from paths that already hold outputMu
//   (resetSegment inside FinalizeAtTurnEnd / OnExternalWrite).
// - The deferred resize fired by SetProseStreaming runs via
//   `go f.Resize()` from a clean stack, so it freely acquires
//   outputMu and footer.mu in the standard order.
var outputMu sync.Mutex

// activeInputReader points to the InputReader whose ReadLine loop is
// currently active (or nil between/before turns). Set by ReadLine on
// entry, cleared on exit. Read by PrintExternal so background goroutines
// (async output worker, tool handlers) can print messages without
// corrupting the in-progress input line rendering.
//
// Access is guarded by outputMu: ReadLine sets/clears it under the lock,
// and PrintExternal reads it under the lock. This avoids a data race
// without adding a separate mutex.
var activeInputReader *InputReader

// activeSteerReader points to the SteerInputReader whose readLoop is
// currently active (or nil). Set by SteerInputReader.Start, cleared by
// Stop. Read by PrintExternal so mid-turn security cautions can print
// above the scroll region without corrupting the pinned steer panel.
//
// The InputReader and SteerInputReader never both register at the same
// time: InputReader.ReadLine is active between turns, SteerInputReader
// during turns. PrintExternal checks the steer slot first (turns), then
// the input slot (between turns), then falls through to fmt.Print.
var activeSteerReader *SteerInputReader

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

// setActiveInputReader records the InputReader whose ReadLine loop is
// active. Must be called under LockOutput. Pass nil to clear.
func setActiveInputReader(ir *InputReader) {
	activeInputReader = ir
}

// setActiveSteerReader records the SteerInputReader whose read loop is
// active. Must be called under LockOutput. Pass nil to clear.
func setActiveSteerReader(sr *SteerInputReader) {
	activeSteerReader = sr
}

// PrintExternal prints a message to the terminal without corrupting an
// active input line. When a ReadLine loop is active (activeInputReader is
// set), the message is printed by clearing the current input line,
// emitting the message (which scrolls within the terminal's scroll
// region), and then redrawing the input prompt + buffer below it.
// When no ReadLine is active, the message is printed directly.
//
// This is the correct path for background messages (security cautions,
// tool-log lines, async output) that arrive while the REPL is waiting
// for user input between turns. The previous code routed these through
// the streaming callback's fallback (fmt.Print), which wrote to stdout
// without cursor management — displacing the cursor from the input
// line and making subsequent keystrokes appear at the wrong position.
func PrintExternal(msg string) {
	// Suspend the activity indicator before writing. Without this, the
	// spinner's render goroutine keeps writing \r\033[K<glyph> to the
	// current cursor row every 80ms — clobbering the message mid-write
	// and leaving ghost glyphs on rows the cursor has since moved away
	// from. The subscriber restarts the indicator on the next ToolStart
	// / QueryStarted event, so suspending here only blanks the spinner
	// for the brief window between this message and the next event.
	SuspendIndicator()

	outputMu.Lock()
	defer outputMu.Unlock()
	// Check steer reader first (active during turns), then input reader
	// (active between turns), then fall through to raw fmt.Print.
	if sr := activeSteerReader; sr != nil {
		sr.printExternalLocked(msg)
		return
	}
	if ir := activeInputReader; ir != nil {
		ir.printExternalLocked(msg)
		return
	}
	fmt.Print(msg)
}

// printExternalLocked prints a message above the active input line and
// redraws the input. Caller MUST hold outputMu.
func (ir *InputReader) printExternalLocked(msg string) {
	// Clear the current line (where the input prompt is rendered).
	fmt.Print("\r\033[K")
	// Print the message. It ends with \n, which advances the cursor
	// to the next line and scrolls within the terminal's scroll region.
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	fmt.Print(msg)
	// Reset geometry tracking: the cursor is now on a fresh line below
	// the message, and the previous input rendering is gone. Telling
	// refreshLocked that we're at the top of a new render (0 previous
	// rows, 0 previous cursor line) makes it redraw from the current
	// position without trying to move up to a stale location.
	ir.lastVisualRows = 0
	ir.currentPhysicalLine = 0
	ir.lastWrapPending = false
	ir.refreshLocked()
}
