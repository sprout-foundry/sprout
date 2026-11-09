package console

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils"
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
//		AssistantTurnRenderer.mu  →  outputMu  →  StatusFooter.mu
//
//	  - renderer.mu is always acquired first (by WriteChunk / OnExternalWrite),
//	    then outputMu inside the method body.
//	  - outputMu serializes all terminal-chrome writes; it is acquired
//	    second, after renderer.mu.
//	  - footer.mu is acquired last, only for brief field reads/writes
//	    (never during I/O).
//	  - SetProseStreaming acquires ONLY footer.mu — never outputMu —
//	    because it is called from paths that already hold outputMu
//	    (resetSegment inside FinalizeAtTurnEnd / OnExternalWrite).
//	  - The deferred resize fired by SetProseStreaming runs via
//	    `go f.Resize()` from a clean stack, so it freely acquires
//	    outputMu and footer.mu in the standard order.
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

// activeSelectList is the picker on screen, so background output can print
// above it and a live footer's resize can clear and redraw it. Guarded by
// outputMu.
var activeSelectList *SelectList

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
//
// It panics on a re-entrant acquisition from the goroutine that already
// holds it. sync.Mutex is non-reentrant, so that call can never succeed: it
// blocks forever, and when the blocked goroutine is the REPL's read loop or
// a picker's teardown the CLI dies silently — no prompt is ever redrawn,
// keystrokes echo from the tty's line discipline, and Enter only moves to a
// new line. Panicking at the call site names the offender instead. Callers
// under the lock must use the *Locked variants (drawLocked,
// ClearSteerLineLocked, renderLocked, ...).
func LockOutput() {
	if id := currentGoroutineID(); outputHolder.Load() == id {
		panic("console: re-entrant LockOutput — outputMu is not reentrant, call the *Locked variant")
	}
	outputMu.Lock()
	outputHolder.Store(currentGoroutineID())
}

// UnlockOutput releases the console output mutex.
func UnlockOutput() {
	outputHolder.Store(0)
	outputMu.Unlock()
}

// TryLockOutput attempts to acquire the console output mutex without blocking.
// Returns true if the lock was acquired, false if it is held by another goroutine.
// Callers MUST check the return value and only call UnlockOutput on true.
func TryLockOutput() bool {
	if !outputMu.TryLock() {
		return false
	}
	outputHolder.Store(currentGoroutineID())
	return true
}

// outputHolder identifies the goroutine holding outputMu, so a re-entrant
// LockOutput is reported instead of wedging the process. Written while the
// lock is held and read only by a goroutine about to take it.
var outputHolder atomic.Uint64

// currentGoroutineID returns the calling goroutine's id, used solely to
// attribute outputMu ownership. The value is opaque. It never returns 0:
// ownership tracking is only as good as this parser, so a format it cannot
// read falls back to a per-call unique sentinel, which can never match a
// stored holder. That keeps the re-entrancy detector on — a parser that
// quietly returned 0 would make every check pass and hide the very
// deadlock it exists to catch.
func currentGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	s := buf[:n]
	const prefix = "goroutine "
	if bytes.HasPrefix(s, []byte(prefix)) {
		var id uint64
		for _, c := range s[len(prefix):] {
			if c < '0' || c > '9' {
				break
			}
			id = id*10 + uint64(c-'0')
		}
		if id != 0 {
			return id
		}
	}
	return unparsedGoroutineID.Add(1)
}

// unparsedGoroutineID hands out the unique sentinels described above.
var unparsedGoroutineID atomic.Uint64

// WithOutput runs fn while holding the console output mutex. Use this
// wrapper for short, self-contained ANSI render sequences.
func WithOutput(fn func()) {
	LockOutput()
	defer UnlockOutput()
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
	if sl := activeSelectList; sl != nil {
		sl.printExternalLocked(msg)
		return
	}
	// No ReadLine loop owns these rows, so nothing below redraws them. Record
	// the write as unmirrored: a grow replay that would clear these rows must
	// refuse, because the console cannot reproduce them.
	markTranscriptUnmirrored()
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
	appendTranscriptLines(msg)
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

// PrintLine writes a finished line of background output. With a prompt or
// steer panel on screen it goes through PrintExternal, since a bare write
// lands on the prompt row and leaves the cursor where the editor no longer
// expects it; otherwise it is written to stderr as before.
func PrintLine(line string) {
	printBackground(line, os.Stderr)
}

func printBackground(line string, fallback *os.File) {
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	// Wait for the writer that holds the lock (a spinner frame, another
	// subagent's line) rather than writing around it: a raw write skips the
	// scroll-region parking and can land on the pinned rows. The wait is
	// bounded because the holder may be this goroutine's own caller, and
	// the lock is not reentrant.
	if !tryLockOutputFor(backgroundLockWait) {
		if fallback == os.Stdout {
			// The degraded write below lands where the transcript lives, on a
			// row the console cannot reproduce.
			markTranscriptUnmirrored()
		}
		_, _ = fmt.Fprint(fallback, line)
		return
	}
	if activeSteerReader != nil || activeInputReader != nil || activeSelectList != nil {
		// PrintExternal suspends the spinner before taking the lock, which
		// cannot happen under it (the spinner's render loop needs the lock
		// to stop).
		UnlockOutput()
		PrintExternal(line)
		return
	}
	if footer := GetGlobalStatusFooter(); footer != nil && footer.canPinInput() {
		writeTranscriptLocked(fallback, line)
	} else {
		// No pinned region to respect: plain bytes, so a piped or logged
		// stream does not pick up the transcript writer's CRLFs. The rows
		// the console cannot reproduce are recorded as unmirrored.
		markTranscriptUnmirrored()
		_, _ = fmt.Fprint(fallback, line)
	}
	UnlockOutput()
}

// backgroundLockWait bounds how long a background line waits for the
// output lock before writing without it.
const backgroundLockWait = 250 * time.Millisecond

// tryLockOutputFor polls for the output lock until d elapses.
func tryLockOutputFor(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if TryLockOutput() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func init() {
	utils.SetProcessStepSink(func(line string) { printBackground(line, os.Stdout) })
}
