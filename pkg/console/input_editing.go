package console

import (
	"fmt"
	"strings"
)

// InsertChar inserts a character string at the current cursor position.
func (ir *InputReader) InsertChar(char string) {
	ir.expandPasteAtCursor()
	ir.markEdited()
	ir.insertText(char)

	// For typing at end of line, just output the character (more efficient).
	// Wrap the write in the console output lock so a status-footer Refresh
	// firing from a background event subscriber can't slide in mid-write
	// and displace the cursor — that's the path that makes typed chars
	// look "dropped" between turns.
	//
	// Slash-command and @path input always take the full Refresh path so
	// the live autocomplete dropdown can update alongside the input line.
	_, atPath := atFileToken(ir.line)
	if ir.cursorPos == len(ir.line) && len(ir.collapsedPastes) == 0 && !strings.HasPrefix(ir.line, "/") && !atPath && !strings.Contains(char, "\n") && !ir.composerPinned() {
		LockOutput()
		fmt.Printf("%s", char)
		UnlockOutput()
		ir.syncRenderBookkeeping()
	} else {
		// Inserting in middle requires full refresh
		ir.Refresh()
	}
}

// markEdited detaches the line from history navigation and any completion
// cycle: once edited, it is the user's own text.
func (ir *InputReader) markEdited() {
	ir.hasEditedLine = true
	ir.historyIndex = -1
	ir.resetCompletionCycle()
}

// syncRenderBookkeeping records where the line and cursor sit on screen
// after text was written straight to the terminal rather than through
// refreshInputLine, so the next redraw clears exactly the rows in use.
// Measured with wrappedGeometry: byte offsets and rune counts both drift
// from screen columns once the line holds multi-byte or double-width text.
func (ir *InputReader) syncRenderBookkeeping() {
	promptWidth := visibleRuneWidth(ir.prompt)
	_, cursorRow, cursorCol, _, endCol := wrappedGeometry(ir.terminalWidth, promptWidth, ir.line, ir.cursorPos)
	ir.lastLineLength = promptWidth + displayWidth(ir.line)
	ir.currentPhysicalLine = cursorRow
	ir.lastWrapPending = ir.terminalWidth > 0 && cursorCol == ir.terminalWidth && cursorCol == endCol
}

// Backspace deletes the character before the cursor
func (ir *InputReader) Backspace() {
	if ir.cursorPos == 0 {
		return
	}
	if !ir.deleteCollapsedPasteEndingAtCursor() {
		ir.expandPasteAtCursor()
		ir.markEdited()
		ir.deleteRuneBefore()
	}
	ir.Refresh()
}

// Delete deletes the character at the cursor position
func (ir *InputReader) Delete() {
	if ir.cursorPos >= len(ir.line) {
		return
	}
	if !ir.deleteCollapsedPasteStartingAtCursor() {
		ir.expandPasteAtCursor()
		ir.markEdited()
		ir.deleteRuneAt()
	}
	ir.Refresh()
}

// MoveCursor moves the cursor delta characters left (negative) or right.
func (ir *InputReader) MoveCursor(delta int) {
	if ir.moveRunes(delta) {
		ir.expandPasteAtCursor()
		ir.Refresh()
	}
}

// SetCursor sets the cursor to an absolute position
func (ir *InputReader) SetCursor(pos int) {
	if pos >= 0 && pos <= len(ir.line) {
		ir.moveTo(pos)
		ir.expandPasteAtCursor()
		ir.Refresh()
	}
}

// MoveWord moves the cursor by one word in the given direction
// (-1 backward / Alt-B / Ctrl-Left, +1 forward / Alt-F / Ctrl-Right).
func (ir *InputReader) MoveWord(direction int) {
	ir.SetCursor(wordBoundary(ir.line, ir.cursorPos, direction))
}

// applyKill runs a kill command and redraws when it removed anything.
func (ir *InputReader) applyKill(kill func() bool) {
	ir.expandPasteAtCursor()
	if kill() {
		ir.markEdited()
		ir.Refresh()
	}
}

// DeleteWordBackward kills the word before the cursor (Ctrl-W /
// Meta-Backspace).
func (ir *InputReader) DeleteWordBackward() { ir.applyKill(ir.killWordBefore) }

// DeleteWordForward kills the word after the cursor (Alt-D).
func (ir *InputReader) DeleteWordForward() { ir.applyKill(ir.killWordAfter) }

// KillToEndOfLine kills from the cursor to the end of the line (Ctrl-K).
func (ir *InputReader) KillToEndOfLine() { ir.applyKill(ir.killToEnd) }

// KillToStartOfLine kills from the start of the line to the cursor (Ctrl-U).
func (ir *InputReader) KillToStartOfLine() { ir.applyKill(ir.killToStart) }

// Yank inserts the most recently killed text at the cursor (Ctrl-Y).
func (ir *InputReader) Yank() {
	if ir.killBuffer != "" {
		ir.InsertChar(ir.killBuffer)
	}
}

// Undo reverts the last edit step (Ctrl-_ / Ctrl-/).
func (ir *InputReader) Undo() {
	if ir.undo() {
		ir.markEdited()
		ir.Refresh()
	}
}

// abandonLine handles Ctrl-C over typed text the way shells do: the text
// stays on screen marked ^C, a fresh prompt starts below it, and the text
// goes to history so Up brings it back. Only Ctrl-C on an empty prompt
// starts the press-again-to-exit sequence.
func (ir *InputReader) abandonLine() {
	ir.AddToHistory(ir.line)
	LockOutput()
	defer UnlockOutput()
	if ir.composerPinned() {
		ir.resetLineLocked()
		ir.refreshLocked()
		return
	}
	ir.cursorPos = len(ir.line)
	ir.refreshLocked()
	fmt.Print("^C\r\n")
	ir.resetEdit()
	ir.historyIndex = -1
	ir.hasEditedLine = false
	ir.lastVisualRows = 0
	ir.currentPhysicalLine = 0
	ir.lastWrapPending = false
	ir.refreshLocked()
}

// composerPinned reports whether the input draws in the footer's pinned box
// (any time a live footer is attached) rather than inline in the scroll
// region, the fallback without one.
func (ir *InputReader) composerPinned() bool {
	return ir.IsPinned()
}

// IsPinned reports whether the input box is pinned above the footer, so
// submitted text has to be echoed into the conversation by the caller.
func (ir *InputReader) IsPinned() bool {
	return ir.footer != nil && ir.footer.canPinInput()
}

// finishSubmit records the submitted line in history and returns it. The
// pinned box empties back to its placeholder; inline, the cursor moves to
// a fresh row (in raw mode a bare LF would keep the column).
func (ir *InputReader) finishSubmit() string {
	input := ir.line
	if input != "" {
		ir.AddToHistory(input)
	}
	if ir.composerPinned() {
		LockOutput()
		ir.resetLineLocked()
		ir.refreshLocked()
		UnlockOutput()
		return input
	}
	fmt.Print("\r\n")
	return input
}

func (ir *InputReader) resetLineLocked() {
	ir.resetEdit()
	ir.historyIndex = -1
	ir.hasEditedLine = false
}
