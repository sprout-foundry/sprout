package console

// input_rows.go — row-wise caret movement at the idle prompt: Up/Down
// through a wrapped or multi-line input, Page Up/Page Down by a screenful.
// Rows are the ones on screen — the inline prompt wraps where the terminal
// does, with collapsed pastes shown as their placeholders — so a long single
// line can be walked row by row, not only one with hard line breaks.

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
