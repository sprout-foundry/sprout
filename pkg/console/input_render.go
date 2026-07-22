package console

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Refresh redraws the current input line
func (ir *InputReader) Refresh() {
	LockOutput()
	defer UnlockOutput()
	ir.refreshLocked()
}

// refreshLocked redraws the current input line WITHOUT acquiring the output
// lock. Callers must already hold LockOutput(). This separation allows
// PrintExternal to clear the line, print an external message, and redraw
// the input in a single atomic lock-held sequence.
func (ir *InputReader) refreshLocked() {
	// Clear any existing autocomplete dropdown before redrawing the input
	// line, so stale rows don't persist.
	if ir.autocomplete != nil {
		ir.autocomplete.clear()
	}
	ir.refreshInputLine()
	// After the input line is drawn, update and render the autocomplete
	// dropdown if the line starts with "/" and the user has manually
	// edited the buffer. History-recalled lines (hasEditedLine == false)
	// don't get a dropdown — arrow keys navigate history instead of the
	// dropdown, and showing one the user can't interact with is confusing.
	if ir.autocomplete != nil && ir.hasEditedLine {
		ir.autocomplete.update(ir.line, ir.cursorPos, ir.completer, ir.richCompleter)
		ir.autocomplete.render()
	}
}

// refreshInputLine is the original refreshLocked body — draws the
// prompt + input buffer and positions the cursor. Called by
// refreshLocked after clearing the autocomplete dropdown.
func (ir *InputReader) refreshInputLine() {
	promptRunes := []rune(stripANSIEscapeCodes(ir.prompt))
	displayLine, displayCursorByte := ir.renderLineWithCollapsedPastes()
	promptWidth := len(promptRunes)

	// Compute multi-line-aware geometry. The display line may contain
	// literal `\n` (from pastes or Alt+Enter) — each acts as a hard
	// break that resets the visual column to 0 on the next row.
	currentLineCount, cursorLine, cursorCol, endLine, endCol := wrappedGeometry(
		ir.terminalWidth, promptWidth, displayLine, displayCursorByte,
	)
	previousLineCount := ir.lastVisualRows
	if previousLineCount < 1 {
		previousLineCount = 1
	}
	previousCursorLine := ir.currentPhysicalLine
	previousWrapPending := ir.lastWrapPending

	// Maximum number of wrapped lines we need to clear.
	maxLines := currentLineCount
	if previousLineCount > maxLines {
		maxLines = previousLineCount
	}

	// Move to start of current physical line.
	if previousWrapPending {
		// Terminals in autowrap-pending state treat the next redraw
		// relative to the wrapped line. Step left once to normalize.
		fmt.Printf("%s", MoveCursorLeftSeq(1))
	}
	fmt.Printf("\r")

	// Move up from previous rendered cursor line to the top wrapped line.
	if previousCursorLine > 0 {
		fmt.Printf("%s", MoveCursorUpSeq(previousCursorLine))
	}

	// Clear all wrapped lines from top to bottom.
	for i := 0; i < maxLines; i++ {
		fmt.Printf("%s", ClearLineSeq())
		if i < maxLines-1 {
			fmt.Printf("%s", MoveCursorDownSeq(1))
		}
	}

	// Move back to the top line to redraw.
	if maxLines > 1 {
		fmt.Printf("%s", MoveCursorUpSeq(maxLines-1))
	}

	// Redraw the prompt then the line content, converting `\n` to
	// `\033[K\r\n` so each new logical line starts at column 0 with a
	// cleared row (raw mode swallows OPOST, so a bare LF would leave
	// the cursor at the same column on the next row — the "staircase"
	// bug). Clear-to-EOL before the CRLF prevents leftover content from
	// the old top-row clear from re-appearing if the new content's
	// first row is shorter.
	fmt.Printf("%s", ir.prompt)
	writeWithHardBreaks(displayLine)

	// Clear any trailing content on the (last) line — handles the case
	// where the new content's final row is shorter than what was there.
	fmt.Printf("%s", ClearToEndOfLineSeq())

	ir.lastLineLength = promptWidth + utf8.RuneCountInString(displayLine)
	ir.lastVisualRows = currentLineCount

	// Position cursor: we're currently at (endLine, endCol). Move to
	// (cursorLine, cursorCol). A col value equal to terminalWidth means
	// the position is "one past the last visible column" — the terminal
	// renders that as autowrap-pending at col terminalWidth-1. Clamp
	// for the move sequence so we don't emit a CHA past EOL.
	displayCursorCol := cursorCol
	if ir.terminalWidth > 0 && displayCursorCol >= ir.terminalWidth {
		displayCursorCol = ir.terminalWidth - 1
	}
	if endLine > cursorLine {
		fmt.Printf("%s", MoveCursorUpSeq(endLine-cursorLine))
	} else if endLine < cursorLine {
		fmt.Printf("%s", MoveCursorDownSeq(cursorLine-endLine))
	}
	if displayCursorCol > 0 {
		fmt.Printf("\r\033[%dC", displayCursorCol)
	} else {
		fmt.Printf("\r")
	}

	ir.currentPhysicalLine = cursorLine
	// Autowrap-pending only matters when the cursor sits at the
	// "one-past-last-column" position of the LAST visual row — internal
	// `\n` breaks always reset to column 0 so they can't leave the
	// terminal autowrap-pending.
	ir.lastWrapPending = cursorLine == endLine &&
		cursorCol == endCol &&
		ir.terminalWidth > 0 &&
		cursorCol == ir.terminalWidth
}

// writeWithHardBreaks prints `content` to stdout, replacing each `\n`
// with `\033[K\r\n` so the terminal moves to column 0 of a freshly
// cleared next row. Without this, raw-mode LF would leave the cursor
// at the previous column and overprint subsequent content there.
func writeWithHardBreaks(content string) {
	if !strings.ContainsRune(content, '\n') {
		fmt.Print(content)
		return
	}
	segments := strings.Split(content, "\n")
	for i, seg := range segments {
		fmt.Print(seg)
		if i < len(segments)-1 {
			fmt.Print(ClearToEndOfLineSeq())
			fmt.Print("\r\n")
		}
	}
}

// wrappedGeometry walks `prompt + content` and returns:
//   - totalRows: total visual rows the rendered string occupies
//   - cursorRow, cursorCol: the (0-based row, 0-based col) of the
//     byte position `cursorByte` within `content`
//   - endRow, endCol: where the cursor lands after writing all content
//
// Each `\n` in `content` is a hard line break (next row, col 0).
// Soft wraps happen when col reaches `cols` (next row, col 0).
func wrappedGeometry(cols, promptWidth int, content string, cursorByte int) (totalRows, cursorRow, cursorCol, endRow, endCol int) {
	if cols <= 0 {
		// Pathological terminal width — render as one row.
		runesBefore := runeCountAtByteIndex(content, cursorByte)
		return 1, 0, promptWidth + runesBefore, 0, promptWidth + len([]rune(content))
	}

	row := 0
	col := promptWidth
	cursorRow = -1
	byteIdx := 0
	for byteIdx < len(content) {
		if cursorRow == -1 && byteIdx == cursorByte {
			cursorRow, cursorCol = row, col
		}
		r, size := utf8.DecodeRuneInString(content[byteIdx:])
		if r == '\n' {
			row++
			col = 0
		} else {
			// Advance by the rune's terminal display width (1 for ASCII, 2 for
			// wide/CJK, 0 for combining). A wide rune that won't fit in the
			// remaining columns wraps to the next row — matching how the
			// terminal renders it — so the cursor column stays accurate.
			w := runewidth.RuneWidth(r)
			if col+w > cols {
				row++
				col = 0
			}
			col += w
		}
		byteIdx += size
	}
	if cursorRow == -1 {
		cursorRow, cursorCol = row, col
	}
	endRow, endCol = row, col
	totalRows = row + 1
	return
}

// visualLineCount calculates how many terminal lines are occupied for a given
// rendered character width. Exact-width boundaries consume an additional line
// because terminals wrap to column 0 on the next line.
func visualLineCount(terminalWidth, renderedWidth int) int {
	if terminalWidth <= 0 {
		return 1
	}
	if renderedWidth <= 0 {
		return 1
	}
	return (renderedWidth-1)/terminalWidth + 1
}

// cursorLineIndex calculates the 0-based wrapped line index for a cursor
// position measured in rendered columns. Exact-width boundaries are treated as
// the previous visual line to avoid over-shooting when redrawing.
func cursorLineIndex(terminalWidth, cursorPos int) int {
	if terminalWidth <= 0 || cursorPos <= 0 {
		return 0
	}
	return (cursorPos - 1) / terminalWidth
}

func cursorColumnOffset(terminalWidth, cursorPos int) int {
	if terminalWidth <= 0 || cursorPos <= 0 {
		return 0
	}
	offset := cursorPos % terminalWidth
	if offset == 0 {
		return terminalWidth - 1
	}
	return offset
}

func isWrapPending(terminalWidth, cursorPos, renderedCursorPos, renderedWidth int) bool {
	if terminalWidth <= 0 || cursorPos <= 0 || renderedWidth <= 0 {
		return false
	}
	if renderedCursorPos != renderedWidth {
		return false
	}
	return cursorPos%terminalWidth == 0
}

// visibleRuneWidth returns the terminal display width of a string after
// removing ANSI control sequences (wide/CJK runes count as 2, combining as 0).
func visibleRuneWidth(s string) int {
	return displayWidth(s)
}

// stripANSIEscapeCodes removes ANSI CSI escape sequences like \x1b[31m.
func stripANSIEscapeCodes(text string) string {
	var result strings.Builder
	inEscape := false

	for i := 0; i < len(text); i++ {
		if text[i] == '\033' && i+1 < len(text) && text[i+1] == '[' {
			inEscape = true
			i++ // skip '['
			continue
		}
		if inEscape {
			if (text[i] >= 'A' && text[i] <= 'Z') || (text[i] >= 'a' && text[i] <= 'z') {
				inEscape = false
			}
			continue
		}
		result.WriteByte(text[i])
	}

	return result.String()
}
