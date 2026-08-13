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
	changed := ir.applyTerminalWidthChange(oldWidth, ir.terminalWidth)
	// Notify resize subscribers (e.g. the active turn renderer) so width-
	// dependent consumers update their snapshots. This is needed when the
	// resize arrives while the input reader is processing a SIGWINCH that
	// the footer's watcher might not have fired yet (two independent
	// handlers, order is not guaranteed).
	if changed {
		notifyResizeSubscribers()
	}
	return changed
}

func (ir *InputReader) applyTerminalWidthChange(oldWidth, newWidth int) bool {
	if newWidth <= 0 {
		newWidth = 80
	}
	if oldWidth == newWidth {
		ir.terminalWidth = newWidth
		return false
	}

	// Compute how many physical rows the OLD content occupies at the
	// new width. The terminal has already reflowed the on-screen rows,
	// so a prompt that fit on one line at the old width may now wrap
	// across multiple rows. We set lastVisualRows to this count so
	// refreshInputLine's clear loop (which uses max(current, previous)
	// rows) will clear all stale wrapped copies before redrawing.
	oldContentLength := ir.lastLineLength
	reflowedRows := 1
	if oldContentLength > 0 && newWidth > 0 {
		reflowedRows = (oldContentLength-1)/newWidth + 1
	}

	ir.terminalWidth = newWidth
	ir.lastLineLength = 0
	// After the terminal's reflow, the cursor is at the bottom of the
	// reflowed prompt block. Set currentPhysicalLine and lastVisualRows
	// so refreshInputLine's clear loop moves up to the TOP of the block
	// before clearing each row. Without this, the clear loop stays at
	// the cursor's current row and misses stale wrapped copies above.
	ir.currentPhysicalLine = reflowedRows - 1
	ir.lastVisualRows = reflowedRows
	ir.lastWrapPending = false

	// Redraw in place. refreshInputLine will move up lastVisualRows
	// lines, clear each one with \033[K (per-line clear, NOT \033[J
	// which would wipe the footer below), then redraw the prompt.
	// The footer's own SIGWINCH handler (watchResize → Resize) takes
	// care of clearing and redrawing the footer rows independently.
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
	//
	// Skipped in the sprout webui terminal: xterm.js (which backs it)
	// does not implement CSI u / modifyOtherKeys, and silently dropping
	// the enable would also drop the modified-keystroke reports. The
	// legacy xterm parser still decodes Shift+Enter + modified arrows
	// via the conventional ESC[1;Nm encodings, so the net effect is
	// that those keys keep working in the webui REPL.
	writeModifyOtherKeysEnable(os.Stdout)

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
	writeModifyOtherKeysDisable(os.Stdout)
	_ = setNonblock(ir.termFd, false)
}
