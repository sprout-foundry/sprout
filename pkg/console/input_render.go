package console

import (
	"fmt"
	"strings"
)

// Refresh redraws the current input line
func (ir *InputReader) Refresh() {
	// Calculate display width (accounting for multibyte characters)
	promptRunes := []rune(stripANSIEscapeCodes(ir.prompt))
	displayLine, displayCursorByte := ir.renderLineWithCollapsedPastes()
	lineRunes := []rune(displayLine)
	promptWidth := len(promptRunes)
	lineWidth := len(lineRunes)
	totalWidth := promptWidth + lineWidth

	currentLineCount := visualLineCount(ir.terminalWidth, totalWidth)
	previousLineCount := visualLineCount(ir.terminalWidth, ir.lastLineLength)
	previousCursorLine := ir.currentPhysicalLine
	previousWrapPending := ir.lastWrapPending

	// Calculate current cursor visual position.
	displayCursorRunes := runeCountAtByteIndex(displayLine, displayCursorByte)
	cursorPos := promptWidth + displayCursorRunes
	cursorLine := cursorLineIndex(ir.terminalWidth, cursorPos)
	cursorCol := cursorColumnOffset(ir.terminalWidth, cursorPos)

	// Maximum number of wrapped lines we need to clear
	// Always clear at least as many as we have now, plus what we had before
	maxLines := currentLineCount
	if previousLineCount > maxLines {
		maxLines = previousLineCount
	}

	// Move to start of current physical line
	if previousWrapPending {
		// Terminals in autowrap-pending state can treat the next redraw
		// relative to the wrapped line. Step left once to normalize.
		fmt.Printf("%s", MoveCursorLeftSeq(1))
	}
	fmt.Printf("\r")

	// Move up from previous rendered cursor line to the top wrapped line.
	if previousCursorLine > 0 {
		// Move up to the top wrapped line
		fmt.Printf("%s", MoveCursorUpSeq(previousCursorLine))
	}

	// Clear all wrapped lines from top to bottom
	for i := 0; i < maxLines; i++ {
		fmt.Printf("%s", ClearLineSeq())
		if i < maxLines-1 {
			// Move down to next line
			fmt.Printf("%s", MoveCursorDownSeq(1))
		}
	}

	// Move back to the top line to redraw
	if maxLines > 1 {
		fmt.Printf("%s", MoveCursorUpSeq(maxLines-1))
	}

	// Redraw the prompt and line content
	fmt.Printf("%s%s", ir.prompt, displayLine)

	// Clear any trailing content on the last line (in case new content is shorter than old)
	fmt.Printf("%s", ClearToEndOfLineSeq())

	// Update tracked length AFTER drawing (use display width, not byte length)
	ir.lastLineLength = totalWidth

	// Position cursor correctly.
	// After printing, cursor is at end of content (on line 'currentLineCount - 1').
	endLine := currentLineCount - 1
	if endLine > cursorLine {
		fmt.Printf("%s", MoveCursorUpSeq(endLine-cursorLine))
	} else if endLine < cursorLine {
		fmt.Printf("%s", MoveCursorDownSeq(cursorLine-endLine))
	}

	// Move to target column on that line.
	if cursorCol > 0 {
		fmt.Printf("\r\033[%dC", cursorCol)
	} else {
		fmt.Printf("\r")
	}

	// Track current rendered cursor line (0-based wrapped line index).
	ir.currentPhysicalLine = cursorLine
	ir.lastWrapPending = isWrapPending(ir.terminalWidth, totalWidth, cursorPos, promptWidth+lineWidth)
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

// visibleRuneWidth returns the printable rune width of a string after removing
// ANSI control sequences.
func visibleRuneWidth(s string) int {
	return len([]rune(stripANSIEscapeCodes(s)))
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
