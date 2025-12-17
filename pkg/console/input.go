package console

import (
	"fmt"
	"os"
	"strings"
	"syscall"

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
	prompt       string
	line         string
	cursorPos    int
	history      []string
	historyIndex int
	termFd       int
	oldState     *term.State
}

// NewInputReader creates a new input reader
func NewInputReader(prompt string) *InputReader {
	return &InputReader{
		prompt:       prompt,
		termFd:       int(os.Stdin.Fd()),
		history:      make([]string, 0, 100),
		historyIndex: -1,
	}
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
	fmt.Printf("%s", ir.prompt) // Simple initial prompt

	buf := make([]byte, 32) // Larger buffer for escape sequences
	escapeParser := NewEscapeParser()

	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return "", err
		}

		if n == 0 {
			continue
		}

		// Process each byte
		for i := 0; i < n; i++ {
			b := buf[i]

			// Handle control characters directly
			switch b {
			case 3: // Ctrl+C
				fmt.Printf("\r%s", ClearToEndOfLineSeq()) // Clear line
				fmt.Println("^C")
				return "", fmt.Errorf("interrupted")

			case 26: // Ctrl+Z
				// Restore terminal for suspension
				term.Restore(ir.termFd, oldState)
				syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)
				// When resumed, set raw mode again
				if newState, err := term.MakeRaw(ir.termFd); err == nil {
					oldState = newState
				}
				ir.Refresh()
				continue

			case 13: // Enter
				fmt.Println() // Move to next line
				input := strings.TrimSpace(ir.line)
				if input != "" {
					ir.AddToHistory(input)
				}
				return input, nil

			case 8, 127: // Backspace
				ir.Backspace()

			case 9: // Tab
				// Tab completion could be implemented here
				continue

			default:
				// Try to parse as escape sequence first
				if event := escapeParser.Parse(b); event != nil {
					ir.HandleEvent(event)
				}
				// Escape sequence parser now handles regular characters internally
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
	// For regular typing, just output the character directly
	// and update internal state without calling Refresh
	fmt.Printf("%s", char)

	before := ir.line[:ir.cursorPos]
	after := ir.line[ir.cursorPos:]
	ir.line = before + char + after
	ir.cursorPos += len(char)
}

// Backspace deletes the character before the cursor
func (ir *InputReader) Backspace() {
	if ir.cursorPos > 0 {
		// If we're at the end of the line, just move back and clear
		if ir.cursorPos == len(ir.line) {
			fmt.Printf("\b%s", ClearToEndOfLineSeq())
			before := ir.line[:ir.cursorPos-1]
			ir.line = before
			ir.cursorPos--
		} else {
			// Complex case: character in the middle, need full refresh
			before := ir.line[:ir.cursorPos-1]
			after := ir.line[ir.cursorPos:]
			ir.line = before + after
			ir.cursorPos--
			ir.Refresh()
		}
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
	// Simple and robust approach - just clear to end and redraw
	fmt.Printf("\r%s%s%s", ir.prompt, ir.line, ClearToEndOfLineSeq())

	// Position cursor correctly
	totalCursorPos := len(ir.prompt) + ir.cursorPos
	fmt.Printf("%s", MoveCursorToColumnSeq(totalCursorPos+1))
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

// EscapeParser handles escape sequences using a simple state machine
type EscapeParser struct {
	state  int
	buffer []byte
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
			// Unknown sequence, reset and ignore
			ep.Reset()
			return nil
		}

	case 3: // Expecting "~" for Delete
		ep.buffer = append(ep.buffer, b)
		if b == '~' {
			event := &InputEvent{Type: EventDelete}
			ep.Reset()
			return event
		}
		// Not Delete, reset and ignore
		ep.Reset()
		return nil

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
			// Unknown sequence, reset and ignore
			ep.Reset()
			return nil
		}

	case 5: // ESC [ 1 sequence (Home)
		ep.buffer = append(ep.buffer, b)
		if b == '~' {
			event := &InputEvent{Type: EventHome}
			ep.Reset()
			return event
		}
		// Not Home, reset and ignore
		ep.Reset()
		return nil

	case 6: // ESC [ 4 sequence (End)
		ep.buffer = append(ep.buffer, b)
		if b == '~' {
			event := &InputEvent{Type: EventEnd}
			ep.Reset()
			return event
		}
		// Not End, reset and ignore
		ep.Reset()
		return nil
	}

	return nil
}

// Reset the parser state
func (ep *EscapeParser) Reset() {
	ep.state = 0
	ep.buffer = ep.buffer[:0]
}