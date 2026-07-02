package console

import (
	"fmt"
	"strings"
)

// renderLine asks the footer to repaint the pinned input row with the
// current buffer and a mode-specific prefix. The prefix is included
// here (not in the footer) so the footer stays content-agnostic and
// any future modes don't require footer changes.
//
// SP-078 Phase 1: width-aware. The buffer is soft-wrapped to the
// terminal width (cols) and the cursor position is mapped to a
// (visualRow, colOnRow) pair so the caret lands at the correct cell
// even when the buffer overflows the panel width or contains
// hard-break \n that produce multi-row input. The footer's
// SetSteerLineWrapped handles the scroll-region reservation, the
// caret row placement, and the maxSteerRows cap.
//
// When Ctrl-R reverse-search is active, the line instead shows the
// search prompt (query + best match) so the user sees what they're
// searching for and what will be loaded on Enter. Reverse-search is
// intentionally single-line — it always fits in one row, so it uses
// the legacy SetSteerLineWithCursor path.
func (r *SteerInputReader) renderLine() {
	if r.footer == nil {
		return
	}
	r.mu.Lock()

	if r.searchMode {
		// Reverse-search is a single-line prompt; legacy path is fine.
		var text string
		if r.searchResult != "" {
			display := strings.ReplaceAll(r.searchResult, "\n", "\\n")
			text = fmt.Sprintf("(search)'%s': %s", r.searchQuery, display)
		} else {
			text = fmt.Sprintf("(search)'%s': ", r.searchQuery)
		}
		prefix := SteerPromptPrefix
		if r.submitMode == SteerSubmitModeQueue {
			prefix = QueuePromptPrefix
		}
		r.mu.Unlock()
		r.footer.SetSteerLineWithCursor(fmt.Sprintf("%s%s", prefix, text), len(prefix)+len(text))
		return
	}

	text := string(r.buffer)
	cursorByte := r.cursorPos
	prefix := SteerPromptPrefix
	if r.submitMode == SteerSubmitModeQueue {
		prefix = QueuePromptPrefix
	}
	r.mu.Unlock()

	// Width-aware wrap. The footer's TerminalSize returns (cols, rows)
	// but on non-TTY fd=-1 the values are 0; fall back to 80 cols so
	// tests still produce a sensible layout.
	cols, _ := r.footer.TerminalSize()
	if cols <= 0 {
		cols = 80
	}
	full := prefix + text
	// prefixWidth accounts for wide-rune prefixes; for ASCII prefixes
	// this equals len(prefix). Use displayWidth directly to avoid
	// re-counting ANSI bytes (the steer buffer is plain text so the
	// difference only matters if a future prefix includes color).
	prefixWidth := displayWidth(prefix)
	_, cursorRow, cursorCol, _, _ := wrappedGeometry(
		cols, prefixWidth, full, len(prefix)+cursorByte,
	)
	// wrappedGeometry uses cols-promptWidth budgeting internally; we
	// passed `prefixWidth` as the prompt width, which means cursorCol
	// already accounts for the prefix columns. Footer takes (row, col)
	// in the post-wrap visual-line array.
	r.footer.SetSteerLineWrapped(full, cursorRow, cursorCol)
}

// printExternalLocked prints a message in the scrollable area without
// disturbing the steer panel's pinned rows. Caller MUST hold outputMu.
//
// The scroll region is ALREADY set to exclude the pinned rows (footer +
// steer panel) by applyScrollRegionLocked when the steer reader
// started. Writing at the last row of that region and letting \n scroll
// keeps the pinned rows stationary — that's the whole point of scroll
// regions. We must NOT reset the region to full screen (\033[r) here:
// doing so makes the pinned rows part of the scrollable area, so the
// message's trailing \n scrolls the steer panel and footer up off the
// screen, destroying the terminal layout ("breaking the CLI").
//
// We bypass r.renderLine() here because it routes through
// footer.SetSteerLineWithCursor → footer.draw(), which re-acquires
// outputMu. We already hold outputMu (from PrintExternal), and
// sync.Mutex is non-reentrant, so the re-acquire would deadlock.
// Instead we set the footer's steer state directly and call
// drawLocked(), the lock-free variant.
func (r *SteerInputReader) printExternalLocked(msg string) {
	if r.footer == nil {
		fmt.Print(msg)
		return
	}
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	// Position the cursor at the bottom of the existing scrollable
	// area (just above the reserved rows). Do NOT reset the scroll
	// region — the region already protects the pinned rows, and
	// resetting it lets the message's \n scroll them away.
	_, rows := r.footer.terminalSize()
	reserved := r.footer.reservedRows()
	if rows > reserved+1 {
		fmt.Fprintf(r.footer.w, "\033[%d;1H", rows-reserved)
	}
	// Write the message. The trailing \n scrolls within the scroll
	// region, pushing prior conversation content up while the pinned
	// steer panel and footer stay fixed.
	fmt.Fprint(r.footer.w, msg)
	// Position the cursor at the bottom of the scrollable area again
	// (the \n above left it on the first pinned row).
	if rows > reserved+1 {
		fmt.Fprintf(r.footer.w, "\033[%d;1H", rows-reserved)
	}
	// Re-render the footer's pinned rows so the user sees their typed
	// buffer in the right place. Set the footer's steer state directly
	// (the same mutation renderLine → SetSteerLineWithCursor would do)
	// and call drawLocked, the lock-free variant — we hold outputMu and
	// footer.draw() would try to re-acquire it, deadlocking.
	r.mu.Lock()
	text := string(r.buffer)
	cursor := r.cursorPos
	prefix := SteerPromptPrefix
	if r.submitMode == SteerSubmitModeQueue {
		prefix = QueuePromptPrefix
	}
	r.mu.Unlock()
	r.footer.mu.Lock()
	r.footer.steerActive = true
	r.footer.steerLine = fmt.Sprintf("%s%s", prefix, text)
	r.footer.steerCursor = len(prefix) + cursor
	r.footer.mu.Unlock()
	r.footer.drawLocked()
}
