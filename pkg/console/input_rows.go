package console

// input_rows.go — row-wise caret movement at the idle prompt: Up/Down
// through a wrapped or multi-line input, Page Up/Page Down by a screenful.
// Rows are the ones on screen — the inline prompt wraps where the terminal
// does, with collapsed pastes shown as their placeholders — so a long single
// line can be walked row by row, not only one with hard line breaks.

import (
	"fmt"
	"os"
)

// moveRows moves the caret delta screen rows. Reports whether it moved;
// without clamp it stays put on the edge row in the direction of travel.
func (ir *InputReader) moveRows(delta int, clamp bool) bool {
	display, displayCursor := ir.renderLineWithCollapsedPastes()
	positions := inlineCaretPositions(display, ir.terminalWidth, visibleRuneWidth(ir.prompt))
	b, ok := caretRowMove(positions, displayCursor, delta, clamp)
	if !ok {
		return false
	}
	ir.cursorPos = ir.rawOffsetForDisplay(b)
	ir.expandPasteAtCursor()
	return true
}

// rawOffsetForDisplay maps a byte offset in the displayed line (collapsed
// pastes shown as placeholders) back to the buffer. An offset inside a
// placeholder lands at the end of its paste.
func (ir *InputReader) rawOffsetForDisplay(displayByte int) int {
	rawPos, dispPos := 0, 0
	for _, span := range ir.collapsedPastes {
		if span.start < rawPos || span.end > len(ir.line) || span.start >= span.end {
			continue
		}
		plain := span.start - rawPos
		if displayByte <= dispPos+plain {
			return rawPos + (displayByte - dispPos)
		}
		dispPos += plain
		label := pastePlaceholder(ir.line[span.start:span.end])
		if displayByte <= dispPos+len(label) {
			return span.end
		}
		dispPos += len(label)
		rawPos = span.end
	}
	return min(rawPos+(displayByte-dispPos), len(ir.line))
}

// pageRows is Page Up/Page Down: a page of rows, or to the start or end of
// the input from its first or last row. Never history.
func (ir *InputReader) pageRows(dir int) {
	page := 10
	if ir.footer != nil {
		if _, rows := ir.footer.TerminalSize(); rows > 0 {
			page = max(rows-ir.footer.reservedRows()-2, 1)
		}
	}
	if !ir.moveRows(dir*page, true) {
		if dir < 0 {
			ir.cursorPos = 0
		} else {
			ir.cursorPos = len(ir.line)
		}
	}
	ir.Refresh()
}

// clickAt handles a left click at the idle prompt (only reported when
// mouse_click_to_position is on). The inline prompt's screen row is not
// tracked — it sits wherever the transcript left off — so the click is held
// and the terminal is asked where the cursor is (DSR 6); the report, handled
// by cursorReport, places the caret.
func (ir *InputReader) clickAt(row, col int) {
	if ir.autocomplete != nil && ir.autocomplete.visible {
		return // the pinned dropdown owns the rows; keyboard selects there
	}
	ir.pendingClick = &screenCell{row: row, col: col}
	_, _ = os.Stdout.WriteString("\x1b[6n")
}

// cursorReport resolves a held click against the cursor position the
// terminal reported ("row;col"): the cursor sits currentPhysicalLine rows
// below the prompt's first row, which locates the click within the prompt.
func (ir *InputReader) cursorReport(data string) {
	click := ir.pendingClick
	ir.pendingClick = nil
	if click == nil {
		return
	}
	var curRow, curCol int
	if _, err := fmt.Sscanf(data, "%d;%d", &curRow, &curCol); err != nil {
		return
	}
	layoutRow := click.row - (curRow - ir.currentPhysicalLine)
	display, _ := ir.renderLineWithCollapsedPastes()
	positions := inlineCaretPositions(display, ir.terminalWidth, visibleRuneWidth(ir.prompt))
	b, ok := caretAt(positions, layoutRow, click.col-1)
	if !ok {
		return
	}
	ir.cursorPos = ir.rawOffsetForDisplay(b)
	ir.expandPasteAtCursor()
	ir.Refresh()
}

// screenCell is a 1-based terminal cell.
type screenCell struct{ row, col int }
