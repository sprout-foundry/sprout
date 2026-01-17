package console

import (
	"fmt"
	"os"
	"strings"
	"time"

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
)

// InputReader handles interactive input with proper escape sequence handling
type InputReader struct {
	prompt         string
	line           string
	cursorPos      int
	history        []string
	historyIndex   int
	termFd         int
	oldState       *term.State
	terminalWidth  int
	lastLineLength int

	// Paste detection
	pasteBuffer      strings.Builder
	pasteTimer       *time.Timer
	pasteActive      bool
	lastCharTime     time.Time
	inPasteMode      bool
	pasteStartPrompt string
}

// NewInputReader creates a new input reader
func NewInputReader(prompt string) *InputReader {
	ir := &InputReader{
		prompt:       prompt,
		termFd:       int(os.Stdin.Fd()),
		history:      make([]string, 0, 100),
		historyIndex: -1,
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

	// Initialize line state
	ir.line = ""
	ir.cursorPos = 0
	ir.historyIndex = -1
	ir.updateTerminalWidth()
	ir.lastLineLength = 0
	ir.pasteBuffer.Reset()
	ir.pasteActive = false
	ir.inPasteMode = false
	ir.lastCharTime = time.Now()
	fmt.Printf("%s", ir.prompt) // Simple initial prompt

	parser := NewEscapeParser()
	buf := make([]byte, 32)

	// Set stdin to non-blocking for paste detection
	if err := setNonblock(ir.termFd, true); err != nil {
		// Fall back to blocking mode if non-blocking fails
		term.Restore(ir.termFd, oldState)
		return ir.fallbackReadLine()
	}
	defer func() {
		_ = setNonblock(ir.termFd, false)
	}()

	for {
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

			if isNoData {
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
			// Real error - return it wrapped with context
			return "", fmt.Errorf("stdin read error: %w", err)
		}

		if n == 0 {
			// In non-blocking mode, this means no data
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Process each byte through the escape parser
		for i := 0; i < n; i++ {
			b := buf[i]
			now := time.Now()

			// Detect paste: rapid character input
			timeSinceLastChar := now.Sub(ir.lastCharTime)
			isRapidInput := timeSinceLastChar < 30*time.Millisecond && n > 1

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

			// Start paste mode on rapid input (but not for escape sequences)
			if !ir.inPasteMode && !isEscapeSeq && isRapidInput && ir.line == "" {
				ir.inPasteMode = true
				ir.pasteActive = true
				ir.pasteStartPrompt = ir.prompt
				ir.pasteBuffer.Reset()
				ir.pasteBuffer.WriteRune(rune(b))
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
						ir.pasteBuffer.WriteRune(rune(b))
					}
					if ir.finalizePaste() {
						// Continue after paste
						continue
					}
				} else {
					// Convert \r to \n for proper multiline handling
					if b == 13 {
						ir.pasteBuffer.WriteRune('\n')
					} else if b >= 32 && b <= 126 {
						ir.pasteBuffer.WriteRune(rune(b))
					} else if b == 9 { // Tab
						ir.pasteBuffer.WriteRune('\t')
					}
					ir.lastCharTime = now
					continue
				}
			}

			ir.lastCharTime = now

			// Parse the byte through the escape parser
			event := parser.Parse(b)
			if event != nil {
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
		}
	}
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
		ir.NavigateHistory(1)
	case EventDown:
		ir.NavigateHistory(-1)
	case EventTab, EventEscape:
		// Handle as needed
	default:
		// Ignore other events
	}
}

// InsertChar inserts a character at the cursor position
func (ir *InputReader) InsertChar(char string) {
	before := ir.line[:ir.cursorPos]
	after := ir.line[ir.cursorPos:]
	ir.line = before + char + after
	ir.cursorPos += len(char)

	// For typing at end of line, just output the character (more efficient)
	if ir.cursorPos == len(ir.line) {
		fmt.Printf("%s", char)
	} else {
		// Inserting in middle requires full refresh
		ir.Refresh()
	}
}

// Backspace deletes the character before the cursor
func (ir *InputReader) Backspace() {
	if ir.cursorPos > 0 {
		before := ir.line[:ir.cursorPos-1]
		after := ir.line[ir.cursorPos:]
		ir.line = before + after
		ir.cursorPos--
		ir.Refresh()
	}
}

// Delete deletes the character at the cursor position
func (ir *InputReader) Delete() {
	if ir.cursorPos < len(ir.line) {
		before := ir.line[:ir.cursorPos]
		after := ir.line[ir.cursorPos+1:]
		ir.line = before + after
		ir.Refresh()
	}
}

// MoveCursor moves the cursor left or right
func (ir *InputReader) MoveCursor(delta int) {
	newPos := ir.cursorPos + delta
	if newPos >= 0 && newPos <= len(ir.line) {
		ir.cursorPos = newPos
		ir.Refresh()
	}
}

// SetCursor sets the cursor to an absolute position
func (ir *InputReader) SetCursor(pos int) {
	if pos >= 0 && pos <= len(ir.line) {
		ir.cursorPos = pos
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

	ir.cursorPos = len(ir.line)
	ir.Refresh()
}

// Refresh redraws the current input line
func (ir *InputReader) Refresh() {
	// Calculate how many physical lines the content takes up
	totalLength := len(ir.prompt) + len(ir.line)
	currentLineCount := (totalLength + ir.terminalWidth - 1) / ir.terminalWidth
	previousLineCount := (ir.lastLineLength + ir.terminalWidth - 1) / ir.terminalWidth

	// Move cursor up to the start of the first line if we had wrapped content
	if previousLineCount > 1 {
		fmt.Printf("%s", MoveCursorUpSeq(previousLineCount-1))
	}

	// Move to start of line
	fmt.Printf("\r")

	// Clear all old lines FIRST, before drawing new content
	// This prevents visual artifacts when content shrinks
	for i := 0; i < previousLineCount; i++ {
		fmt.Printf("%s%s", ClearLineSeq(), MoveCursorDownSeq(1))
	}

	// Move back up to the first line
	// After the loop above, we're at line `previousLineCount`
	// We need to go back to line 0, so move up `previousLineCount` lines
	fmt.Printf("%s", MoveCursorUpSeq(previousLineCount))
	fmt.Printf("\r") // Back to start of first line

	// Redraw the prompt and line content
	fmt.Printf("%s%s", ir.prompt, ir.line)

	// Clear any trailing content if current line is shorter than terminal width
	if totalLength % ir.terminalWidth != 0 {
		fmt.Printf("%s", ClearToEndOfLineSeq())
	}

	// Update tracked length AFTER drawing
	ir.lastLineLength = totalLength

	// Position cursor correctly
	if ir.cursorPos < len(ir.line) {
		// Cursor is in the middle - need to move it
		cursorPos := len(ir.prompt) + ir.cursorPos
		cursorLine := cursorPos / ir.terminalWidth
		cursorCol := cursorPos % ir.terminalWidth

		// After printing, cursor is at end of content (line 'currentLineCount - 1')
		// We need to move to line 'cursorLine'
		endLine := currentLineCount - 1
		if endLine > cursorLine {
			// Move up to the cursor line
			fmt.Printf("%s", MoveCursorUpSeq(endLine-cursorLine))
		} else if endLine < cursorLine {
			// Move down to the cursor line
			fmt.Printf("%s", MoveCursorDownSeq(cursorLine-endLine))
		}

		// Now position cursor at correct column (1-based)
		fmt.Printf("\r%s", MoveCursorToColumnSeq(cursorCol+1))
	}
	// If cursor is at end, it's already in the right position after printing
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

// finalizePaste processes the pasted content and formats it appropriately
func (ir *InputReader) finalizePaste() bool {
	pastedContent := ir.pasteBuffer.String()
	ir.pasteBuffer.Reset()
	ir.inPasteMode = false
	ir.pasteActive = false

	// Strip trailing newline that triggered the paste
	pastedContent = strings.TrimRight(pastedContent, "\n")

	// Check if content is multiline
	lineCount := strings.Count(pastedContent, "\n") + 1

	// Check if content looks like code or structured data
	looksLikeCode := ir.detectCodePattern(pastedContent)

	// Format the pasted content
	var formatted string
	if looksLikeCode && lineCount > 1 {
		// Wrap in triple backticks for code blocks
		formatted = fmt.Sprintf("```\n%s\n```", pastedContent)
	} else if lineCount > 1 {
		// Multiline but not code - use quotes
		formatted = fmt.Sprintf("\"\"\"\n%s\n\"\"\"", pastedContent)
	} else {
		// Single line - insert as-is
		formatted = pastedContent
	}

	// Insert the formatted content
	ir.line = ir.line + formatted
	ir.cursorPos = len(ir.line)

	// Show feedback and refresh
	ir.Refresh()

	ir.lastLineLength = len(ir.prompt) + len(ir.line)

	return true
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
		if b >= 32 && b <= 126 {
			ep.pendingChar = b
			ep.hasPending = true
		}
		ep.Reset()
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
		case '3': // Start of Delete sequence
			ep.state = 3
			return nil
		case '1': // Start of Home/End with numeric prefix
			ep.state = 5
			return nil
		case '4': // Start of End with numeric prefix
			ep.state = 6
			return nil
		default:
			// Handle longer sequences or numeric parameters
			if b >= '0' && b <= '9' || b == ';' {
				// Part of a longer sequence (like page up/down)
				return nil
			}
			// Unknown sequence - treat as standalone ESC
			// This character could be printable, save it for next call
			if b >= 32 && b <= 126 {
				ep.pendingChar = b
				ep.hasPending = true
			}
			ep.Reset()
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
		if b >= 32 && b <= 126 {
			ep.pendingChar = b
			ep.hasPending = true
		}
		ep.Reset()
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
			if b >= 32 && b <= 126 {
				ep.pendingChar = b
				ep.hasPending = true
			}
			ep.Reset()
			return &InputEvent{Type: EventEscape}
		}

	case 5: // ESC [ 1 sequence (Home)
		ep.buffer = append(ep.buffer, b)
		if b == '~' {
			event := &InputEvent{Type: EventHome}
			ep.Reset()
			return event
		}
		// Not Home, this character could be printable
		if b >= 32 && b <= 126 {
			ep.pendingChar = b
			ep.hasPending = true
		}
		ep.Reset()
		return &InputEvent{Type: EventEscape}

	case 6: // ESC [ 4 sequence (End)
		ep.buffer = append(ep.buffer, b)
		if b == '~' {
			event := &InputEvent{Type: EventEnd}
			ep.Reset()
			return event
		}
		// Not End, this character could be printable
		if b >= 32 && b <= 126 {
			ep.pendingChar = b
			ep.hasPending = true
		}
		ep.Reset()
		return &InputEvent{Type: EventEscape}
	}

	return nil
}

// Reset the parser state
func (ep *EscapeParser) Reset() {
	ep.state = 0
	ep.buffer = ep.buffer[:0]
	ep.hasPending = false
	ep.pendingChar = 0
}
