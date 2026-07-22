package console

import (
	"fmt"
	"strings"
)

// maxSteerDropdownRows is how many candidate rows the steer-panel
// dropdown will render above the input line. The footer caps the steer
// panel at maxSteerRows=6, so we reserve the last row for the input
// line and render up to 5 candidates above it.
const maxSteerDropdownRows = 5

// formatSteerDropdownRow renders a single candidate as a steer-panel
// row. The selected row uses a "▶" marker prefix; unselected rows use
// two spaces. This deliberately avoids embedding ANSI escape codes
// (reverse video) in the text because the footer's WrapSteerLayout
// is not ANSI-aware — ANSI bytes would be counted as visible width
// and cause incorrect wrapping/truncation.
func formatSteerDropdownRow(c CompletionCandidate, selected bool, cols int) string {
	const marker = "▶ "
	const markerOff = "  "
	prefix := markerOff
	if selected {
		prefix = marker
	}

	body := " " + c.Text
	if c.Description != "" {
		body = body + "  " + c.Description
	}
	budget := cols - displayWidth(prefix)
	if budget < 1 {
		budget = 1
	}
	body = truncateLinePreservingANSI(body, budget)
	return prefix + body
}

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
// SP-078 Phase 3: when the buffer starts with "/" and the richCompleter
// returns candidates, the dropdown rows are rendered ABOVE the input
// line within the same steer panel. The combined text uses \n as a
// hard break between candidate rows and the input line; the cursor
// always sits on the input line (last row) so typing never lands in
// the dropdown.
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

	// SP-078 Phase 3: refresh the dropdown candidate list from the
	// richCompleter. Inline so the snapshot is consistent with the
	// buffer we read above — a separate goroutine could race with
	// edits and yield mismatched candidates for the rendered line.
	// update() short-circuits when the line hasn't changed, so this
	// is cheap on every keystroke.
	dropdownVisible := r.refreshDropdownLocked(text, cursorByte)

	// Snapshot the autocomplete state we need for rendering, while
	// still holding r.mu. buildDropdownLine runs after Unlock and must
	// not read mutable fields concurrently with the read loop.
	var snapCandidates []CompletionCandidate
	var snapSelected int
	if dropdownVisible {
		snapCandidates = append([]CompletionCandidate(nil), r.autocomplete.candidates...)
		snapSelected = r.autocomplete.selected
	}

	r.mu.Unlock()

	// Width-aware wrap. The footer's TerminalSize returns (cols, rows)
	// but on non-TTY fd=-1 the values are 0; fall back to 80 cols so
	// tests still produce a sensible layout.
	cols, _ := r.footer.TerminalSize()
	if cols <= 0 {
		cols = 80
	}

	// SP-078 Phase 3: build the combined dropdown + input line text.
	// Each candidate row is pre-formatted and pre-truncated to `cols`
	// visible columns so it fits on a single visual row of the steer
	// panel (no soft wrap inside a candidate). The combined text uses
	// \n as the row separator; the footer already handles multi-line
	// steer panels with `maxSteerRows` overflow.
	if dropdownVisible {
		full, cursorRow, cursorCol := r.buildDropdownLine(prefix, text, cursorByte, cols, snapCandidates, snapSelected)
		r.footer.SetSteerLineWrapped(full, cursorRow, cursorCol)
		return
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

// refreshDropdownLocked pulls fresh candidate rows from the
// configured richCompleter and updates the autocomplete state to
// reflect the current buffer + cursor. Caller MUST hold r.mu.
// Returns the post-update `visible` flag so the caller can decide
// whether to render the dropdown.
//
// Extracted from renderLine so callers without a footer (tests, or
// callers that update candidates before attaching a footer) can
// drive the same logic without paying the render cost.
func (r *SteerInputReader) refreshDropdownLocked(text string, cursorByte int) bool {
	if r.autocomplete == nil || r.richCompleter == nil {
		return false
	}
	r.autocomplete.update(text, cursorByte, nil, r.richCompleter)
	return r.autocomplete.visible
}

// buildDropdownLine composes the candidate rows + input line into a
// single multi-line text for SetSteerLineWrapped, returning the
// (cursorRow, cursorCol) within the combined visual layout.
//
// Each candidate row is rendered as its own line above the input
// line. The cursor always sits on the last (input) line at the
// correct column. Caller MUST NOT hold r.mu — but reading from the
// dropdown's candidate list is safe because renderLine's lock
// snapshot happened in the caller frame.
//
// Note: buildDropdownLine does NOT itself take r.mu. The caller
// (renderLine) reads r.autocomplete.candidates and r.cursorPos
// while holding r.mu, releases r.mu, and only then calls this
// helper. The dropdown's state is therefore consistent at the
// moment of render.
func (r *SteerInputReader) buildDropdownLine(prefix, text string, cursorByte, cols int, candidates []CompletionCandidate, selected int) (full string, cursorRow, cursorCol int) {
	n := len(candidates)
	if n > maxSteerDropdownRows {
		n = maxSteerDropdownRows
	}

	// Each candidate row is pre-truncated to `cols` visible columns so
	// the footer's WrapSteerLayout sees them as single visual rows.
	// (WrapHardLine inside WrapSteerLayout would otherwise try to soft
	// wrap a candidate across two rows when its content overflows.)
	rows := make([]string, 0, n+1)
	for i := 0; i < n; i++ {
		rows = append(rows, formatSteerDropdownRow(candidates[i], i == selected, cols))
	}

	inputLine := prefix + text
	rows = append(rows, inputLine)
	full = strings.Join(rows, "\n")

	// The cursor sits on the input line (last row in the combined
	// layout). Compute the cursor's (visualRow, visualCol) relative to
	// JUST the input line — wrappedGeometry walks byte-by-byte and
	// would miscount if we passed the candidate rows (their ANSI
	// bytes would inflate the row counter). Then add the number of
	// dropdown rows that precede the input line.
	prefixWidth := displayWidth(prefix)
	// Build the cursor byte index within the input line: prompt bytes
	// + buffer cursor byte.
	cursorByteInInput := len(prefix) + cursorByte
	_, cursorRowInInput, cursorCol, _, _ := wrappedGeometry(
		cols, prefixWidth, inputLine, cursorByteInInput,
	)
	// Adjust cursorRowInInput by the number of dropdown rows that
	// precede the input line in the combined layout.
	cursorRow = n + cursorRowInInput
	return full, cursorRow, cursorCol
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
