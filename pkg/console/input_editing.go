package console

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// InsertChar inserts a character string at the current cursor position.
func (ir *InputReader) InsertChar(char string) {
	ir.expandPasteAtCursor()

	// Mark line as edited and disconnect from history
	ir.hasEditedLine = true
	ir.historyIndex = -1

	insertAt := ir.cursorPos
	before := ir.line[:ir.cursorPos]
	after := ir.line[ir.cursorPos:]
	ir.line = before + char + after
	ir.cursorPos += len(char)
	ir.shiftPasteSpans(insertAt, len(char))

	// For typing at end of line, just output the character (more efficient).
	// Wrap the write in the console output lock so a status-footer Refresh
	// firing from a background event subscriber can't slide in mid-write
	// and displace the cursor — that's the path that makes typed chars
	// look "dropped" between turns.
	//
	// Slash-command input always takes the full Refresh path so the live
	// autocomplete dropdown can update alongside the input line.
	if ir.cursorPos == len(ir.line) && len(ir.collapsedPastes) == 0 && !strings.HasPrefix(ir.line, "/") {
		LockOutput()
		fmt.Printf("%s", char)
		UnlockOutput()
		// Keep refresh bookkeeping in sync even on fast-path writes.
		promptWidth := visibleRuneWidth(ir.prompt)
		lineWidth := len([]rune(ir.line))
		totalWidth := promptWidth + lineWidth
		ir.lastLineLength = totalWidth
		cursorPos := promptWidth + ir.cursorPos
		ir.currentPhysicalLine = cursorLineIndex(ir.terminalWidth, cursorPos)
		ir.lastWrapPending = isWrapPending(ir.terminalWidth, totalWidth, cursorPos, totalWidth)
	} else {
		// Inserting in middle requires full refresh
		ir.Refresh()
	}
}

// Backspace deletes the character before the cursor
func (ir *InputReader) Backspace() {
	if ir.cursorPos > 0 {
		if ir.deleteCollapsedPasteEndingAtCursor() {
			ir.Refresh()
			return
		}
		ir.expandPasteAtCursor()

		// Mark line as edited and disconnect from history
		ir.hasEditedLine = true
		ir.historyIndex = -1

		deletePos := ir.cursorPos - 1
		before := ir.line[:deletePos]
		after := ir.line[ir.cursorPos:]
		ir.line = before + after
		ir.cursorPos--
		ir.shiftPasteSpans(deletePos+1, -1)
		ir.Refresh()
	}
}

// Delete deletes the character at the cursor position
func (ir *InputReader) Delete() {
	if ir.cursorPos < len(ir.line) {
		if ir.deleteCollapsedPasteStartingAtCursor() {
			ir.Refresh()
			return
		}
		ir.expandPasteAtCursor()

		// Mark line as edited and disconnect from history
		ir.hasEditedLine = true
		ir.historyIndex = -1

		before := ir.line[:ir.cursorPos]
		after := ir.line[ir.cursorPos+1:]
		ir.line = before + after
		ir.shiftPasteSpans(ir.cursorPos+1, -1)
		ir.Refresh()
	}
}

// MoveCursor moves the cursor left or right
func (ir *InputReader) MoveCursor(delta int) {
	newPos := ir.cursorPos + delta
	if newPos >= 0 && newPos <= len(ir.line) {
		ir.cursorPos = newPos
		ir.expandPasteAtCursor()
		ir.Refresh()
	}
}

// SetCursor sets the cursor to an absolute position
func (ir *InputReader) SetCursor(pos int) {
	if pos >= 0 && pos <= len(ir.line) {
		ir.cursorPos = pos
		ir.expandPasteAtCursor()
		ir.Refresh()
	}
}

// deleteRange deletes characters in [start, end) from ir.line, adjusts
// the cursor and collapsed-paste spans, and refreshes the display.
// start/end are clamped to valid byte bounds; a no-op when start >= end.
func (ir *InputReader) deleteRange(start, end int) {
	if start < 0 {
		start = 0
	}
	if end > len(ir.line) {
		end = len(ir.line)
	}
	if start >= end {
		return
	}
	ir.expandPasteAtCursor()
	ir.hasEditedLine = true
	ir.historyIndex = -1
	removed := end - start
	ir.line = ir.line[:start] + ir.line[end:]
	if ir.cursorPos > start {
		if ir.cursorPos <= end {
			ir.cursorPos = start
		} else {
			ir.cursorPos -= removed
		}
	}
	filtered := ir.collapsedPastes[:0]
	for _, span := range ir.collapsedPastes {
		switch {
		case span.end <= start:
			filtered = append(filtered, span)
		case span.start >= end:
			span.start -= removed
			span.end -= removed
			filtered = append(filtered, span)
		}
		// spans overlapping [start,end) are dropped
	}
	ir.collapsedPastes = filtered
	ir.Refresh()
}

// MoveWord moves the cursor by one word in the given direction
// (-1 backward / Alt-B / Ctrl-Left, +1 forward / Alt-F / Ctrl-Right).
// A word is a maximal run of non-whitespace (unicode.IsSpace).
func (ir *InputReader) MoveWord(direction int) {
	line := ir.line
	pos := ir.cursorPos
	if direction < 0 {
		for pos > 0 {
			r, size := utf8.DecodeLastRuneInString(line[:pos])
			if unicode.IsSpace(r) {
				pos -= size
			} else {
				break
			}
		}
		for pos > 0 {
			r, size := utf8.DecodeLastRuneInString(line[:pos])
			if !unicode.IsSpace(r) {
				pos -= size
			} else {
				break
			}
		}
	} else {
		for pos < len(line) {
			r, size := utf8.DecodeRuneInString(line[pos:])
			if unicode.IsSpace(r) {
				pos += size
			} else {
				break
			}
		}
		for pos < len(line) {
			r, size := utf8.DecodeRuneInString(line[pos:])
			if !unicode.IsSpace(r) {
				pos += size
			} else {
				break
			}
		}
	}
	ir.SetCursor(pos)
}

// DeleteWordBackward deletes the word before the cursor (Ctrl-W /
// Meta-Backspace).
func (ir *InputReader) DeleteWordBackward() {
	if ir.cursorPos == 0 {
		return
	}
	line := ir.line
	pos := ir.cursorPos
	for pos > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:pos])
		if unicode.IsSpace(r) {
			pos -= size
		} else {
			break
		}
	}
	for pos > 0 {
		r, size := utf8.DecodeLastRuneInString(line[:pos])
		if !unicode.IsSpace(r) {
			pos -= size
		} else {
			break
		}
	}
	ir.deleteRange(pos, ir.cursorPos)
}

// KillToEndOfLine deletes from the cursor to the end of the line (Ctrl-K).
func (ir *InputReader) KillToEndOfLine() {
	ir.deleteRange(ir.cursorPos, len(ir.line))
}

// KillToStartOfLine deletes from the start of the line to the cursor (Ctrl-U).
func (ir *InputReader) KillToStartOfLine() {
	ir.deleteRange(0, ir.cursorPos)
}
