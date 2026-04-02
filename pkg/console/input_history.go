package console

import (
	"strings"
)

// NavigateHistory navigates through command history
func (ir *InputReader) NavigateHistory(direction int) {
	if len(ir.history) == 0 {
		return
	}

	switch direction {
	case 1: // Up arrow - older commands
		if ir.historyIndex == -1 {
			ir.historyIndex = len(ir.history) - 1
			ir.line = ir.history[ir.historyIndex]
		} else if ir.historyIndex > 0 {
			ir.historyIndex--
			ir.line = ir.history[ir.historyIndex]
		}
	case -1: // Down arrow - newer commands
		if ir.historyIndex == -1 {
			ir.line = ""
		} else if ir.historyIndex < len(ir.history)-1 {
			ir.historyIndex++
			ir.line = ir.history[ir.historyIndex]
		} else {
			ir.historyIndex = -1
			ir.line = ""
		}
	}

	// Reset edit flag when loading from history
	ir.hasEditedLine = false
	ir.collapsedPastes = ir.collapsedPastes[:0]
	ir.cursorPos = len(ir.line)
	ir.Refresh()
}

// NavigateVertically handles both history navigation and multi-line text navigation
// direction: -1 for up, 1 for down
func (ir *InputReader) NavigateVertically(direction int) {
	// Navigate history if: line is empty OR we haven't edited the current line
	if len(ir.line) == 0 || !ir.hasEditedLine {
		// Invert direction for history (up arrow = older commands)
		ir.NavigateHistory(-direction)
		return
	}

	// Otherwise, navigate within multi-line text
	ir.navigateInLine(direction)
}

// navigateInLine moves cursor up/down within multi-line text
func (ir *InputReader) navigateInLine(direction int) {
	lines := ir.splitIntoLines()
	if len(lines) == 1 {
		// Single line - no vertical movement possible
		return
	}

	// Find current line and column
	currentLineIdx, currentCol := ir.getLineAndColumn()

	// Calculate target line
	targetLineIdx := currentLineIdx + direction
	if targetLineIdx < 0 || targetLineIdx >= len(lines) {
		// Would move outside the text - stay at current position
		return
	}

	// Calculate new cursor position
	// Move to start of target line, then add column (clamped to line length)
	targetLine := lines[targetLineIdx]
	targetCol := min(currentCol, len([]rune(targetLine)))

	// Calculate cursor position: sum of all previous lines + target column
	newPos := 0
	for i := 0; i < targetLineIdx; i++ {
		newPos += len([]rune(lines[i])) + 1 // +1 for newline
	}
	newPos += targetCol

	ir.cursorPos = newPos
	ir.expandPasteAtCursor()
	ir.Refresh()
}

// splitIntoLines splits the current line into individual lines
func (ir *InputReader) splitIntoLines() []string {
	return strings.Split(ir.line, "\n")
}

// getLineAndColumn returns the current line index and column within that line
func (ir *InputReader) getLineAndColumn() (lineIdx, col int) {
	lines := ir.splitIntoLines()
	pos := ir.cursorPos

	for i, line := range lines {
		lineLen := len([]rune(line)) + 1 // +1 for newline
		if pos < lineLen {
			// We're on this line
			if i == len(lines)-1 {
				// Last line - no trailing newline in original
				return i, len([]rune(line))
			}
			return i, min(pos, len([]rune(line)))
		}
		pos -= lineLen
	}

	// Shouldn't reach here, but return last position if we do
	return len(lines) - 1, len([]rune(lines[len(lines)-1]))
}

// AddToHistory adds a command to history
func (ir *InputReader) AddToHistory(command string) {
	// Remove duplicates
	for i, cmd := range ir.history {
		if cmd == command {
			ir.history = append(ir.history[:i], ir.history[i+1:]...)
			break
		}
	}

	ir.history = append(ir.history, command)

	// Limit history size
	if len(ir.history) > 100 {
		ir.history = ir.history[1:]
	}
}

// SetHistory sets the command history
func (ir *InputReader) SetHistory(history []string) {
	ir.history = make([]string, len(history))
	copy(ir.history, history)
}

// GetHistory returns the command history
func (ir *InputReader) GetHistory() []string {
	result := make([]string, len(ir.history))
	copy(result, ir.history)
	return result
}
