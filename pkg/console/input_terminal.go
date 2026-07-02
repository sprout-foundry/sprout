package console

import (
	"fmt"
	"os"
	"os/signal"

	"golang.org/x/term"
)

func (ir *InputReader) processPendingResize(resizeCh <-chan os.Signal, parser *EscapeParser) bool {
	if resizeCh == nil {
		return false
	}

	handled := false
	for {
		select {
		case <-resizeCh:
			parser.Reset()
			if ir.handleResize() {
				handled = true
			}
		default:
			return handled
		}
	}
}

func (ir *InputReader) handleResize() bool {
	oldWidth := ir.terminalWidth
	ir.updateTerminalWidth()
	return ir.applyTerminalWidthChange(oldWidth, ir.terminalWidth)
}

func (ir *InputReader) applyTerminalWidthChange(oldWidth, newWidth int) bool {
	if newWidth <= 0 {
		newWidth = 80
	}
	if oldWidth == newWidth {
		ir.terminalWidth = newWidth
		return false
	}

	ir.terminalWidth = newWidth
	ir.lastLineLength = 0
	ir.currentPhysicalLine = 0
	ir.lastVisualRows = 0
	ir.lastWrapPending = false

	// After a resize the terminal re-soft-wraps the on-screen rows to the new
	// width, so our captured geometry is stale and the block's top row can't be
	// located reliably without a cursor-position query. Clear from the cursor to
	// the end of the screen (dropping any stale wrapped rows below) and redraw
	// the prompt+line fresh in place. This avoids the extra blank line the prior
	// "\r CLEAR \n" inserted on every resize; rows above the cursor are left to
	// the terminal's own reflow.
	LockOutput()
	fmt.Print("\r\033[J")
	UnlockOutput()
	ir.Refresh()
	return true
}

// updateTerminalWidth gets the current terminal width
func (ir *InputReader) updateTerminalWidth() {
	if width, _, err := term.GetSize(ir.termFd); err == nil {
		ir.terminalWidth = width
	} else {
		ir.terminalWidth = 80 // Fallback to standard width
	}
}

// setupInputTerm enables bracketed paste, SGR mouse tracking, and
// modifyOtherKeys (Shift+Enter reporting), and registers this reader as
// the active one so background goroutines (async output worker, tool
// handlers) can print messages via PrintExternal without corrupting the
// in-progress prompt. Callers are expected to have already entered raw
// mode (term.MakeRaw) before calling this helper.
//
// Returns the resize signal channel (or nil if the platform doesn't
// support SIGWINCH-style resizes) and the resulting non-blocking mode.
// The non-blocking flag is false when the platform doesn't support
// non-blocking reads; callers must handle that gracefully (the read
// loop is still well-defined, just slightly less responsive).
func (ir *InputReader) setupInputTerm() (resizeCh chan os.Signal, nonBlocking bool) {
	fmt.Print(bracketedPasteEnable)
	fmt.Print(MouseTrackingSGR)
	// Ask the terminal to report modified keystrokes (Shift+Enter etc.)
	// as CSI u sequences. Terminals that don't recognize this just
	// ignore the SGR; the new parser branch is a no-op when the
	// sequence never arrives.
	fmt.Print(modifyOtherKeysEnable)

	// Register as the active input reader so background goroutines
	// (async output worker, tool handlers) can print messages via
	// PrintExternal without corrupting the input line. Cleared on
	// return. Must be under LockOutput to race with PrintExternal.
	LockOutput()
	setActiveInputReader(ir)
	UnlockOutput()

	if sig := resizeSignal(); sig != nil {
		resizeCh = make(chan os.Signal, 1)
		signal.Notify(resizeCh, sig)
	}

	// Some terminals/PTYs reject non-blocking mode. When that happens
	// we keep raw mode enabled and fall back to blocking reads.
	nonBlocking = true
	if nbErr := setNonblock(ir.termFd, true); nbErr != nil {
		nonBlocking = false
	}
	return resizeCh, nonBlocking
}

// teardownInputTerm undoes everything setupInputTerm installed: clears the
// active input reader, disables bracketed paste + mouse tracking +
// modifyOtherKeys, and (optionally) flips non-blocking back off. It does
// NOT restore cooked termios — the caller owns that via term.Restore so
// the teardown order stays the same regardless of whether the caller
// installed raw mode before or after setupInputTerm. Must be called via
// defer while the fd is still in raw mode so the disable SGR sequences
// reach the terminal.
func (ir *InputReader) teardownInputTerm() {
	LockOutput()
	setActiveInputReader(nil)
	UnlockOutput()
	fmt.Print(bracketedPasteDisable)
	fmt.Print(MouseTrackingDisable)
	fmt.Print(modifyOtherKeysDisable)
	_ = setNonblock(ir.termFd, false)
}
