package console

import (
	"fmt"
	"strings"
)

// buildDropdownBlock composes autocomplete candidate rows + an input
// line into a single multi-line text for the footer's pinned-block
// rendering (steer-panel style). Each candidate row is pre-formatted
// and pre-truncated to `cols` visible columns so it fits on a single
// visual row (no soft wrap inside a candidate). The input line is the
// last row; the cursor always sits on it.
//
// Returns the combined text plus the (row, col) of the cursor within
// the visual layout. Shared by SteerInputReader.buildDropdownLine and
// InputReader.renderPinnedDropdownLocked.
func buildDropdownBlock(prefix, text string, cursorByte, cols int, candidates []CompletionCandidate, selected int) (full string, cursorRow, cursorCol int) {
	total := len(candidates)
	n := total
	if n > maxDropdownRows {
		n = maxDropdownRows
	}

	// Scroll window: keep the selected candidate inside the rendered
	// window. The window starts at 0 and slides forward only as far as
	// needed so `selected` is the last visible row, then never scrolls
	// back until selection moves above it — the same clamping the old
	// below-line InputReader dropdown used.
	offset := 0
	if selected >= n && total > n {
		offset = selected - n + 1
		if offset > total-n {
			offset = total - n
		}
	}

	rows := make([]string, 0, n+1)
	for i := 0; i < n; i++ {
		idx := i + offset
		if idx >= total {
			break
		}
		rows = append(rows, formatDropdownRow(candidates[idx], idx == selected, cols))
	}

	inputLine := prefix + text
	rows = append(rows, inputLine)
	full = strings.Join(rows, "\n")

	// The cursor sits on the input line (last row in the combined
	// layout). Compute the cursor's (visualRow, visualCol) relative to
	// JUST the input line — wrappedGeometry walks byte-by-byte and
	// would miscount if we passed the candidate rows (their ANSI
	// bytes would inflate the row counter). Pass `text` (not
	// prefix+text) as the content with the prefix width as the start
	// column so the prefix isn't counted twice. Then add the number
	// of dropdown rows that precede the input line.
	prefixWidth := displayWidth(prefix)
	_, cursorRowInInput, cursorCol, _, _ := wrappedGeometry(
		cols, prefixWidth, text, cursorByte,
	)
	cursorRow = n + cursorRowInInput
	return full, cursorRow, cursorCol
}

// renderPinnedDropdownLocked draws the autocomplete dropdown above the
// prompt line as a pinned block in the footer's reserved rows
// (steer-panel style). The prompt line is NOT drawn inline — the
// footer's drawLocked renders it as the last row of the pinned block,
// and the real terminal cursor is parked at the bottom of the scroll
// region by the footer's scroll-region application.
//
// Caller must hold outputMu and the dropdown must be visible with a
// live footer attached.
func (ir *InputReader) renderPinnedDropdownLocked() {
	cols := ir.terminalWidth
	if ir.footer != nil {
		if c, _ := ir.footer.TerminalSize(); c > 0 {
			cols = c
		}
	}
	if cols <= 0 {
		cols = 80
	}
	full, cursorRow, cursorCol := buildDropdownBlock(
		ir.prompt, ir.line, ir.cursorPos, cols,
		ir.autocomplete.candidates, ir.autocomplete.selected,
	)
	ir.footer.SetSteerLineWrappedLocked(full, cursorRow, cursorCol)
}

// prepareInlineRenderLocked tears down a pinned dropdown block and
// positions the real cursor + render bookkeeping so the following
// refreshInputLine redraws the prompt inline from a known location.
// Caller must hold outputMu.
//
// ClearSteerLineLocked blanks the pinned rows and re-expands the
// scroll region, leaving the cursor at the bottom of the region (the
// row the inline prompt will occupy). We then compute the prompt's
// geometry, move the cursor to the prompt's TOP row, and sync
// lastVisualRows/currentPhysicalLine/lastWrapPending so
// refreshInputLine's clear-up/redraw math covers exactly the prompt's
// rows — the same state a normal inline render would have left.
func (ir *InputReader) prepareInlineRenderLocked() {
	footer := ir.footer
	if footer != nil {
		footer.ClearSteerLineLocked()
	}
	ir.pinnedDropdownActive = false

	promptWidth := len([]rune(stripANSIEscapeCodes(ir.prompt)))
	displayLine, _ := ir.renderLineWithCollapsedPastes()
	totalRows, _, _, _, _ := wrappedGeometry(
		ir.terminalWidth, promptWidth, displayLine, ir.cursorPos,
	)
	if totalRows < 1 {
		totalRows = 1
	}

	// Without a footer there are no reserved rows to sit above; park
	// the cursor on the last screen row as refreshInputLine's fallback
	// geometry expects (terminalSize 0 rows → clamped to row 1 below).
	bottom := 1
	if footer != nil {
		_, rows := footer.terminalSize()
		bottom = rows - footer.reservedRows()
	}
	if bottom < 1 {
		bottom = 1
	}
	topRow := bottom - (totalRows - 1)
	if topRow < 1 {
		topRow = 1
	}

	ir.lastVisualRows = totalRows
	ir.currentPhysicalLine = 0
	ir.lastWrapPending = false

	fmt.Printf("\033[%d;1H", topRow)
}
