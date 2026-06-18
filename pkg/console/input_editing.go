package console

import (
	"fmt"
	"strings"
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
	if ir.cursorPos == len(ir.line) && len(ir.collapsedPastes) == 0 {
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

// detectCodePattern checks if the pasted content looks like code
func (ir *InputReader) detectCodePattern(content string) bool {
	// Check for common code patterns
	codeIndicators := []string{
		"function ", "def ", "class ", "func ", "var ", "let ", "const ",
		"if ", "for ", "while ", "return ", "import ", "from ", "export ",
		"//", "/*", "*/", "#", "<!--",
		"package ", "struct ", "type ", "interface ",
	}

	hasSpace := strings.Contains(content, " ")
	braceCount := strings.Count(content, "{") + strings.Count(content, "}")
	parenCount := strings.Count(content, "(") + strings.Count(content, ")")
	bracketCount := strings.Count(content, "[") + strings.Count(content, "]")

	// Check for code indicators
	isCode := false
	for _, indicator := range codeIndicators {
		if strings.Contains(content, indicator) {
			isCode = true
			break
		}
	}

	// Also check for multiple pairs of brackets (common in code)
	totalBrackets := braceCount + parenCount + bracketCount
	if totalBrackets >= 4 && hasSpace {
		return true
	}

	return isCode
}
