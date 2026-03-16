package console

import (
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// InputEvent represents a key press or input event
type InputEvent struct {
	Type InputEventType
	Data string
}

type InputEventType int

const (
	EventChar InputEventType = iota
	EventUp
	EventDown
	EventLeft
	EventRight
	EventHome
	EventEnd
	EventBackspace
	EventDelete
	EventEnter
	EventTab
	EventInterrupt
	EventSuspend
	EventEscape
	EventPasteStart
	EventPasteEnd
)

// InputReader handles interactive input with proper escape sequence handling
type InputReader struct {
	prompt          string
	line            string
	cursorPos       int
	history         []string
	historyIndex    int
	termFd          int
	oldState        *term.State
	terminalWidth   int
	lastLineLength  int
	lastWrapPending bool

	// Edit tracking for history vs text navigation
	hasEditedLine bool

	// Paste detection
	pasteBuffer      strings.Builder
	pasteTimer       *time.Timer
	pasteActive      bool
	lastCharTime     time.Time
	inPasteMode      bool
	pasteStartPrompt string
	bracketedPaste   bool
	bracketedMatch   int
	bracketedSawCR   bool
	collapsedPastes  []pasteSpan

	// Track current physical line (for multi-line wrapped input)
	currentPhysicalLine int
}

type pasteSpan struct {
	start int
	end   int
}

const (
	// Heuristic paste detection should be conservative to avoid misclassifying
	// normal typing over high-latency links as paste bursts.
	minHeuristicPasteBytes = 12
	bracketedPasteEnable   = "\033[?2004h"
	bracketedPasteDisable  = "\033[?2004l"
	bracketedPasteEndSeq   = "\x1b[201~"
)

// NewInputReader creates a new input reader
func NewInputReader(prompt string) *InputReader {
	ir := &InputReader{
		prompt:          prompt,
		termFd:          int(os.Stdin.Fd()),
		history:         make([]string, 0, 100),
		historyIndex:    -1,
		collapsedPastes: make([]pasteSpan, 0, 8),
	}
	ir.updateTerminalWidth()
	return ir
}

// ReadLine reads a line of input with proper escape sequence handling
func (ir *InputReader) ReadLine() (string, error) {
	// Check if we're in a terminal
	if !term.IsTerminal(ir.termFd) {
		return ir.fallbackReadLine()
	}

	// Save terminal state and set raw mode
	oldState, err := term.MakeRaw(ir.termFd)
	if err != nil {
		return ir.fallbackReadLine()
	}
	defer term.Restore(ir.termFd, oldState)
	fmt.Print(bracketedPasteEnable)
	defer fmt.Print(bracketedPasteDisable)

	// Initialize line state
	ir.line = ""
	ir.cursorPos = 0
	ir.historyIndex = -1
	ir.hasEditedLine = false
	ir.updateTerminalWidth()
	ir.lastLineLength = 0
	ir.lastWrapPending = false
	ir.currentPhysicalLine = 0
	ir.pasteBuffer.Reset()
	ir.pasteActive = false
	ir.inPasteMode = false
	ir.bracketedPaste = false
	ir.bracketedMatch = 0
	ir.bracketedSawCR = false
	ir.collapsedPastes = ir.collapsedPastes[:0]
	ir.lastCharTime = time.Now()
	fmt.Printf("%s", ir.prompt) // Simple initial prompt

	parser := NewEscapeParser()
	buf := make([]byte, 32)
	var resizeCh chan os.Signal
	if sig := resizeSignal(); sig != nil {
		resizeCh = make(chan os.Signal, 1)
		signal.Notify(resizeCh, sig)
		defer signal.Stop(resizeCh)
	}

	// Set stdin to non-blocking for paste detection
	nonBlocking := true
	if err := setNonblock(ir.termFd, true); err != nil {
		// Some terminals/PTYs reject non-blocking mode. Keep raw mode enabled and
		// continue with blocking reads so arrow keys/history still work.
		nonBlocking = false
	}
	if nonBlocking {
		defer func() {
			_ = setNonblock(ir.termFd, false)
		}()
	}

	for {
		if ir.processPendingResize(resizeCh, parser) {
			continue
		}

		n, err := os.Stdin.Read(buf)

		// Handle non-blocking read errors
		if err != nil {
			errStr := err.Error()
			// Check if it's just "no data available" (EAGAIN/EWOULDBLOCK)
			// Common error messages: "no data", "resource temporarily unavailable", "EAGAIN"
			isNoData := strings.Contains(errStr, "no data") ||
				strings.Contains(errStr, "temporarily unavailable") ||
				errStr == "EAGAIN" ||
				errStr == "EWOULDBLOCK"
			isInterrupted := strings.Contains(errStr, "interrupted system call") ||
				errStr == "EINTR"

			if nonBlocking && isNoData {
				if ir.processPendingResize(resizeCh, parser) {
					continue
				}
				// Check if paste timer should fire
				if ir.pasteActive && time.Since(ir.lastCharTime) > 100*time.Millisecond {
					// Paste detected - process it
					if ir.finalizePaste() {
						// Paste was finalized, continue reading
						continue
					}
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			if isInterrupted && ir.processPendingResize(resizeCh, parser) {
				continue
			}
			// Real error - return it wrapped with context
			return "", fmt.Errorf("stdin read error: %w", err)
		}

		if n == 0 {
			// In non-blocking mode, this means no data
			if nonBlocking {
				time.Sleep(10 * time.Millisecond)
			}
			continue
		}

		// Process each byte through the escape parser
		for i := 0; i < n; i++ {
			b := buf[i]
			now := time.Now()

			if ir.bracketedPaste {
				if ir.consumeBracketedPasteByte(b) {
					ir.finalizePaste()
				}
				ir.lastCharTime = now
				continue
			}

			// Detect paste: rapid character input
			timeSinceLastChar := now.Sub(ir.lastCharTime)

			// Handle Ctrl+C and Ctrl+Z directly before parsing
			if b == 3 { // Ctrl+C
				fmt.Printf("\r%s", ClearToEndOfLineSeq()) // Clear line
				fmt.Println("^C")
				return "", fmt.Errorf("interrupted")
			}

			if b == 26 { // Ctrl+Z
				// Restore terminal before suspension for clean shell state
				term.Restore(ir.termFd, oldState)
				suspendTerminal()

				// Wait for SIGCONT (when fg resumes the process)
				// The process continues here after resume
				// Give the terminal a moment to stabilize
				ignoreTerminalSignals()

				// Re-enter raw mode
				if newState, err := term.MakeRaw(ir.termFd); err == nil {
					oldState = newState
				}

				// Drain input buffer to clear any characters typed during suspension
				discardBuf := make([]byte, 256)
				for {
					n, _ := os.Stdin.Read(discardBuf)
					if n <= 0 {
						break
					}
				}

				resetTerminalSignals()

				// Clear the current line and redisplay the prompt
				fmt.Printf("\r%s%s", ClearLineSeq(), ir.prompt)
				ir.line = ""
				ir.cursorPos = 0
				continue
			}

			// Check for escape sequences BEFORE paste detection
			// Arrow keys send escape sequences which look like rapid input
			isEscapeSeq := (b == 27) || (parser.state > 0)

			// Start paste mode only when input looks strongly like a paste burst.
			// This avoids false positives on remote/high-latency links where
			// regular keypresses may be delivered in small batches.
			if !ir.inPasteMode && !isEscapeSeq && i == 0 && shouldStartHeuristicPaste(buf[:n], timeSinceLastChar) {
				ir.inPasteMode = true
				ir.pasteActive = true
				ir.pasteStartPrompt = ir.prompt
				ir.pasteBuffer.Reset()
				ir.pasteBuffer.WriteByte(b)
				ir.lastCharTime = now
				continue
			}

			// Collect paste content
			if ir.inPasteMode {
				// Exit paste mode for control characters that indicate user intent
				if b == 27 || b == 8 || b == 127 { // ESC, Backspace, Delete
					ir.inPasteMode = false
					ir.pasteActive = false
					// For ESC, let escape parser handle it
					// For Backspace/Delete, handle them normally
					if b != 27 {
						continue
					}
				} else if timeSinceLastChar > 100*time.Millisecond || (b == 13 && ir.pasteBuffer.Len() > 0) {
					// Check if paste is ending (slow input or Enter at end)
					// Finalize paste on Enter or timeout
					if b != 13 {
						ir.pasteBuffer.WriteByte(b)
					}
					if ir.finalizePaste() {
						// Continue after paste
						continue
					}
				} else {
					// Convert \r to \n for proper multiline handling
					if b == 13 {
						ir.pasteBuffer.WriteRune('\n')
					} else if b >= 32 {
						ir.pasteBuffer.WriteByte(b)
					} else if b == 9 { // Tab
						ir.pasteBuffer.WriteByte('\t')
					}
					ir.lastCharTime = now
					continue
				}
			}

			ir.lastCharTime = now

			// Parse the byte through the escape parser
			event := parser.Parse(b)
			if event != nil {
				if event.Type == EventPasteStart {
					ir.bracketedPaste = true
					ir.bracketedMatch = 0
					ir.bracketedSawCR = false
					ir.inPasteMode = true
					ir.pasteActive = true
					ir.pasteBuffer.Reset()
					continue
				}
				if event.Type == EventPasteEnd {
					ir.bracketedPaste = false
					ir.bracketedMatch = 0
					ir.bracketedSawCR = false
					ir.finalizePaste()
					continue
				}
				if event.Type == EventEnter {
					// End of input
					fmt.Println() // Move to next line
					input := ir.line
					if input != "" {
						ir.AddToHistory(input)
					}
					return input, nil
				}
				ir.HandleEvent(event)
			}
			for parser.hasPending {
				pending := parser.Parse(0)
				if pending == nil {
					break
				}
				ir.HandleEvent(pending)
			}
		}
	}
}

func (ir *InputReader) processPendingResize(resizeCh <-chan os.Signal, parser *EscapeParser) bool {
	if resizeCh == nil {
		return false
	}

	handled := false
	for {
		select {
		case <-resizeCh:
			parser.Reset()
			if ir.handleResize() {
				handled = true
			}
		default:
			return handled
		}
	}
}

func (ir *InputReader) handleResize() bool {
	oldWidth := ir.terminalWidth
	ir.updateTerminalWidth()
	return ir.applyTerminalWidthChange(oldWidth, ir.terminalWidth)
}

func (ir *InputReader) applyTerminalWidthChange(oldWidth, newWidth int) bool {
	if newWidth <= 0 {
		newWidth = 80
	}
	if oldWidth == newWidth {
		ir.terminalWidth = newWidth
		return false
	}

	ir.terminalWidth = newWidth
	ir.lastLineLength = 0
	ir.currentPhysicalLine = 0
	ir.lastWrapPending = false

	// After a terminal resize, the previous wrapped geometry is invalid.
	// Redraw on a fresh line rather than trying to clear using stale counts.
	fmt.Printf("\r%s\n", ClearLineSeq())
	ir.Refresh()
	return true
}

func (ir *InputReader) consumeBracketedPasteByte(b byte) bool {
	expected := bracketedPasteEndSeq[ir.bracketedMatch]
	if b == expected {
		ir.bracketedMatch++
		if ir.bracketedMatch == len(bracketedPasteEndSeq) {
			ir.bracketedPaste = false
			ir.bracketedMatch = 0
			return true
		}
		return false
	}

	if ir.bracketedMatch > 0 {
		ir.pasteBuffer.WriteString(bracketedPasteEndSeq[:ir.bracketedMatch])
		ir.bracketedMatch = 0
	}

	if b == 13 {
		ir.pasteBuffer.WriteRune('\n')
		ir.bracketedSawCR = true
		return false
	}
	if b == 10 && ir.bracketedSawCR {
		ir.bracketedSawCR = false
		return false
	}
	ir.bracketedSawCR = false

	if b == 9 || b == 10 || b >= 32 {
		ir.pasteBuffer.WriteByte(b)
	}

	return false
}

// fallbackReadLine provides simple input for non-terminal environments
func (ir *InputReader) fallbackReadLine() (string, error) {
	fmt.Print(ir.prompt)
	var input string
	_, err := fmt.Scanln(&input)
	return input, err
}

// HandleEvent processes an input event
func (ir *InputReader) HandleEvent(event *InputEvent) {
	switch event.Type {
	case EventChar:
		ir.InsertChar(event.Data)
	case EventBackspace:
		ir.Backspace()
	case EventDelete:
		ir.Delete()
	case EventLeft:
		ir.MoveCursor(-1)
	case EventRight:
		ir.MoveCursor(1)
	case EventHome:
		ir.SetCursor(0)
	case EventEnd:
		ir.SetCursor(len(ir.line))
	case EventUp:
		ir.NavigateVertically(-1)
	case EventDown:
		ir.NavigateVertically(1)
	case EventTab, EventEscape:
		// Handle as needed
	default:
		// Ignore other events
	}
}

// InsertChar inserts a character at the cursor position
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

	// For typing at end of line, just output the character (more efficient)
	if ir.cursorPos == len(ir.line) && len(ir.collapsedPastes) == 0 {
		fmt.Printf("%s", char)
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

// updateTerminalWidth gets the current terminal width
func (ir *InputReader) updateTerminalWidth() {
	if width, _, err := term.GetSize(ir.termFd); err == nil {
		ir.terminalWidth = width
	} else {
		ir.terminalWidth = 80 // Fallback to standard width
	}
}

// finalizePaste processes pasted content and inserts it literally at cursor.
func (ir *InputReader) finalizePaste() bool {
	pastedContent := ir.pasteBuffer.String()
	ir.pasteBuffer.Reset()
	ir.inPasteMode = false
	ir.pasteActive = false

	// Strip trailing newline that triggered the paste
	pastedContent = strings.TrimRight(pastedContent, "\n")
	if pastedContent == "" {
		return true
	}

	ir.hasEditedLine = true
	ir.historyIndex = -1

	// Insert at cursor position instead of always appending.
	start := ir.cursorPos
	before := ir.line[:ir.cursorPos]
	after := ir.line[ir.cursorPos:]
	ir.line = before + pastedContent + after
	ir.cursorPos += len(pastedContent)
	ir.shiftPasteSpans(start, len(pastedContent))
	ir.addCollapsedPaste(start, start+len(pastedContent))

	// Show feedback and refresh
	ir.Refresh()

	ir.lastLineLength = visibleRuneWidth(ir.prompt) + len([]rune(ir.line))
	ir.lastWrapPending = isWrapPending(ir.terminalWidth, ir.lastLineLength, visibleRuneWidth(ir.prompt)+len([]rune(ir.line)), ir.lastLineLength)

	return true
}

func (ir *InputReader) addCollapsedPaste(start, end int) {
	if start < 0 || end <= start || end > len(ir.line) {
		return
	}
	ir.collapsedPastes = append(ir.collapsedPastes, pasteSpan{start: start, end: end})
	sort.Slice(ir.collapsedPastes, func(i, j int) bool {
		return ir.collapsedPastes[i].start < ir.collapsedPastes[j].start
	})
}

func (ir *InputReader) shiftPasteSpans(pos, delta int) {
	if delta == 0 || len(ir.collapsedPastes) == 0 {
		return
	}
	filtered := ir.collapsedPastes[:0]
	for _, span := range ir.collapsedPastes {
		if span.end <= pos {
			filtered = append(filtered, span)
			continue
		}
		if span.start >= pos {
			span.start += delta
			span.end += delta
		} else {
			// Edits inside a collapsed span are ambiguous; expand it.
			continue
		}
		if span.start < 0 {
			span.start = 0
		}
		if span.end > len(ir.line) {
			span.end = len(ir.line)
		}
		if span.end > span.start {
			filtered = append(filtered, span)
		}
	}
	ir.collapsedPastes = filtered
}

func (ir *InputReader) findCollapsedPasteAtCursor() int {
	for i, span := range ir.collapsedPastes {
		if ir.cursorPos > span.start && ir.cursorPos < span.end {
			return i
		}
	}
	return -1
}

func (ir *InputReader) expandPasteAtCursor() {
	if idx := ir.findCollapsedPasteAtCursor(); idx >= 0 {
		ir.collapsedPastes = append(ir.collapsedPastes[:idx], ir.collapsedPastes[idx+1:]...)
	}
}

func (ir *InputReader) deleteCollapsedPasteEndingAtCursor() bool {
	for i, span := range ir.collapsedPastes {
		if span.end == ir.cursorPos {
			ir.line = ir.line[:span.start] + ir.line[span.end:]
			ir.cursorPos = span.start
			ir.hasEditedLine = true
			ir.historyIndex = -1
			removed := span.end - span.start
			ir.collapsedPastes = append(ir.collapsedPastes[:i], ir.collapsedPastes[i+1:]...)
			ir.shiftPasteSpans(span.end, -removed)
			return true
		}
	}
	return false
}

func (ir *InputReader) deleteCollapsedPasteStartingAtCursor() bool {
	for i, span := range ir.collapsedPastes {
		if span.start == ir.cursorPos {
			ir.line = ir.line[:span.start] + ir.line[span.end:]
			ir.hasEditedLine = true
			ir.historyIndex = -1
			removed := span.end - span.start
			ir.collapsedPastes = append(ir.collapsedPastes[:i], ir.collapsedPastes[i+1:]...)
			ir.shiftPasteSpans(span.end, -removed)
			return true
		}
	}
	return false
}

func (ir *InputReader) renderLineWithCollapsedPastes() (string, int) {
	if len(ir.collapsedPastes) == 0 {
		return ir.line, ir.cursorPos
	}
	var out strings.Builder
	rawPos := 0
	displayCursor := 0
	cursorSet := false

	for _, span := range ir.collapsedPastes {
		if span.start < rawPos || span.end > len(ir.line) || span.start >= span.end {
			continue
		}
		out.WriteString(ir.line[rawPos:span.start])
		if !cursorSet && ir.cursorPos <= span.start {
			displayCursor = out.Len() - (span.start - ir.cursorPos)
			cursorSet = true
		}

		label := fmt.Sprintf("[pasted %d chars]", utf8.RuneCountInString(ir.line[span.start:span.end]))
		if !cursorSet && ir.cursorPos > span.start && ir.cursorPos <= span.end {
			displayCursor = out.Len() + len(label)
			cursorSet = true
		}
		out.WriteString(label)
		rawPos = span.end
	}
	out.WriteString(ir.line[rawPos:])

	if !cursorSet {
		displayCursor = out.Len() - (len(ir.line) - ir.cursorPos)
	}
	if displayCursor < 0 {
		displayCursor = 0
	}
	if displayCursor > out.Len() {
		displayCursor = out.Len()
	}

	return out.String(), displayCursor
}

func runeCountAtByteIndex(s string, byteIndex int) int {
	if byteIndex <= 0 {
		return 0
	}
	if byteIndex >= len(s) {
		return utf8.RuneCountInString(s)
	}
	return utf8.RuneCountInString(s[:byteIndex])
}

func shouldStartHeuristicPaste(chunk []byte, timeSinceLastChar time.Duration) bool {
	if len(chunk) < minHeuristicPasteBytes {
		return false
	}

	printable := 0
	for _, b := range chunk {
		switch {
		case b >= 32 && b <= 126:
			printable++
		case b == 9 || b == 10 || b == 13:
			printable++
		case b == 27 || b == 8 || b == 127:
			// Explicitly exclude ESC/backspace/delete bursts.
			return false
		default:
			// Ignore unsupported control bytes for paste detection.
		}
	}

	// Require nearly all bytes to be printable paste content.
	if printable < len(chunk)-1 {
		return false
	}

	// For moderate bursts, still require rapid arrival.
	if len(chunk) < 20 && timeSinceLastChar >= 30*time.Millisecond {
		return false
	}

	return true
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

// EscapeParser handles escape sequences using a simple state machine
type EscapeParser struct {
	state       int
	buffer      []byte
	pendingChar byte // Stores a character that should be processed next
	hasPending  bool // Whether there's a pending character
}

// NewEscapeParser creates a new escape sequence parser
func NewEscapeParser() *EscapeParser {
	return &EscapeParser{
		state:  0,
		buffer: make([]byte, 0, 10),
	}
}

// Parse processes a byte and returns an event if complete
func (ep *EscapeParser) Parse(b byte) *InputEvent {
	// If we have a pending character, return it first
	if ep.hasPending {
		ep.hasPending = false
		return &InputEvent{Type: EventChar, Data: string([]byte{ep.pendingChar})}
	}

	switch ep.state {
	case 0: // Waiting for ESC or regular char
		if b == 27 {
			ep.state = 1
			return nil
		}
		// Handle control characters
		switch b {
		case 8, 127:
			return &InputEvent{Type: EventBackspace}
		case 13:
			return &InputEvent{Type: EventEnter}
		case 9:
			return &InputEvent{Type: EventTab}
		default:
			// Return regular printable characters as character events
			if b >= 32 && b <= 126 {
				return &InputEvent{Type: EventChar, Data: string([]byte{b})}
			}
			return nil
		}

	case 1: // Got ESC, expecting '[' or other sequence
		ep.buffer = append(ep.buffer, b)
		if b == '[' {
			ep.state = 2
			return nil
		}
		// Handle other ESC sequences (like ESC O for function keys)
		if b == 'O' {
			ep.state = 4
			return nil
		}
		// Not a CSI sequence, treat ESC as escape event
		// This character could be printable, save it for next call
		ep.Reset()
		if b >= 32 && b <= 126 {
			ep.pendingChar = b
			ep.hasPending = true
		}
		return &InputEvent{Type: EventEscape}

	case 2: // Got '[', reading sequence
		ep.buffer = append(ep.buffer, b)

		// Check for completed sequences - only look at the last character for simple cases
		switch b {
		case 'A': // Up arrow
			event := &InputEvent{Type: EventUp}
			ep.Reset()
			return event
		case 'B': // Down arrow
			event := &InputEvent{Type: EventDown}
			ep.Reset()
			return event
		case 'C': // Right arrow
			event := &InputEvent{Type: EventRight}
			ep.Reset()
			return event
		case 'D': // Left arrow
			event := &InputEvent{Type: EventLeft}
			ep.Reset()
			return event
		case 'H': // Home
			event := &InputEvent{Type: EventHome}
			ep.Reset()
			return event
		case 'F': // End
			event := &InputEvent{Type: EventEnd}
			ep.Reset()
			return event
		default:
			// Handle numeric CSI params and terminated forms like ESC [ 3 ~ and ESC [ 200 ~.
			if (b >= '0' && b <= '9') || b == ';' {
				return nil
			}
			if b == '~' {
				param := ""
				if len(ep.buffer) >= 3 {
					param = string(ep.buffer[1 : len(ep.buffer)-1])
				}
				firstParam := param
				if idx := strings.IndexByte(param, ';'); idx >= 0 {
					firstParam = param[:idx]
				}
				ep.Reset()
				switch firstParam {
				case "1", "7":
					return &InputEvent{Type: EventHome}
				case "4", "8":
					return &InputEvent{Type: EventEnd}
				case "3":
					return &InputEvent{Type: EventDelete}
				case "200":
					return &InputEvent{Type: EventPasteStart}
				case "201":
					return &InputEvent{Type: EventPasteEnd}
				default:
					return &InputEvent{Type: EventEscape}
				}
			}
			// Unknown sequence - treat as standalone ESC
			// This character could be printable, save it for next call
			ep.Reset()
			if b >= 32 && b <= 126 {
				ep.pendingChar = b
				ep.hasPending = true
			}
			return &InputEvent{Type: EventEscape}
		}

	case 3: // Expecting "~" for Delete
		ep.buffer = append(ep.buffer, b)
		if b == '~' {
			event := &InputEvent{Type: EventDelete}
			ep.Reset()
			return event
		}
		// Not Delete, the 'b' could be a printable character
		ep.Reset()
		if b >= 32 && b <= 126 {
			ep.pendingChar = b
			ep.hasPending = true
		}
		return &InputEvent{Type: EventEscape}

	case 4: // ESC O sequences (function keys)
		ep.buffer = append(ep.buffer, b)
		switch b {
		case 'H': // Home
			event := &InputEvent{Type: EventHome}
			ep.Reset()
			return event
		case 'F': // End
			event := &InputEvent{Type: EventEnd}
			ep.Reset()
			return event
		default:
			// Unknown sequence, this character could be printable
			ep.Reset()
			if b >= 32 && b <= 126 {
				ep.pendingChar = b
				ep.hasPending = true
			}
			return &InputEvent{Type: EventEscape}
		}
	}

	return nil
}

// Reset the parser state
func (ep *EscapeParser) Reset() {
	ep.state = 0
	ep.buffer = ep.buffer[:0]
}
