package console

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// InputReader handles interactive input with arrow key navigation
type InputReader struct {
	history       []string
	historyIndex  int
	currentInput  string
	cursorPos     int
	prompt        string
	previousInput string // Track the previous input for proper line clearing
}

// NewInputReader creates a new input reader
func NewInputReader(prompt string) *InputReader {
	return &InputReader{
		history:      []string{},
		historyIndex:  -1,
		currentInput:  "",
		cursorPos:     0,
		prompt:        prompt,
	}
}

// ReadLine reads a line of input with arrow key navigation support
func (ir *InputReader) ReadLine() (string, error) {
	// Set terminal to raw mode for reading individual characters
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		// Fallback to simple input if raw mode fails
		fmt.Print(ir.prompt)
		var input string
		_, err := fmt.Scanln(&input)
		return input, err
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	ir.currentInput = ""
	ir.cursorPos = 0
	ir.historyIndex = -1

	// Show the initial prompt
	ir.refreshLine()

	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return "", err
		}

		if n == 0 {
			continue
		}

		// Handle escape sequences (arrow keys)
		if buf[0] == 27 {
			// Read the rest of the escape sequence
			sequence, err := ir.readEscapeSequence()
			if err != nil {
				continue
			}

			switch sequence {
			case "[A": // Up arrow
				ir.navigateHistory(1)
			case "[B": // Down arrow
				ir.navigateHistory(-1)
			case "[C": // Right arrow
				ir.moveCursor(1)
			case "[D": // Left arrow
				ir.moveCursor(-1)
			case "[H": // Home
				ir.setCursorPos(0)
			case "[F": // End
				ir.setCursorPos(len(ir.currentInput))
			case "[3~": // Delete
				ir.deleteAtCursor()
			}
			continue
		}

		switch buf[0] {
		case 3: // Ctrl+C
			// Clear the current line before showing interrupt
			fmt.Printf("\r\033[K")
			fmt.Println("^C")
			return "", fmt.Errorf("interrupted")
		case 13: // Enter/Return
			// Move to next line and clear any remaining content
			fmt.Println()
			input := strings.TrimSpace(ir.currentInput)
			if input != "" {
				ir.addToHistory(input)
			}
			return input, nil
		case 8, 127: // Backspace/Delete
			ir.backspace()
		case 9: // Tab - ignore for now
			// Could implement tab completion here
		default:
			// Regular character
			if buf[0] >= 32 && buf[0] <= 126 {
				ir.insertChar(rune(buf[0]))
			}
		}
	}
}

// readEscapeSequence reads the rest of an escape sequence
func (ir *InputReader) readEscapeSequence() (string, error) {
	buf := make([]byte, 2)
	n, err := os.Stdin.Read(buf)
	if err != nil {
		return "", err
	}
	if n < 2 {
		return "", fmt.Errorf("incomplete escape sequence")
	}

	sequence := string(buf)
	
	// Handle extended sequences (like Delete which is [3~)
	if sequence == "[3" {
		// Read the final ~
		finalBuf := make([]byte, 1)
		n, err := os.Stdin.Read(finalBuf)
		if err != nil || n == 0 || finalBuf[0] != '~' {
			return "", fmt.Errorf("incomplete escape sequence")
		}
		sequence += "~"
	}
	
	return sequence, nil
}

// navigateHistory navigates through command history
func (ir *InputReader) navigateHistory(direction int) {
	if len(ir.history) == 0 {
		return
	}

	// Store current input before navigation for proper line clearing
	ir.previousInput = ir.currentInput

	switch direction {
	case 1: // Up arrow - older commands
		if ir.historyIndex == -1 {
			// Save current input and go to last command
			ir.historyIndex = len(ir.history) - 1
			ir.currentInput = ir.history[ir.historyIndex]
		} else if ir.historyIndex > 0 {
			// Go to older command
			ir.historyIndex--
			ir.currentInput = ir.history[ir.historyIndex]
		}
	case -1: // Down arrow - newer commands
		if ir.historyIndex == -1 {
			// Already at newest, clear input
			ir.currentInput = ""
		} else if ir.historyIndex < len(ir.history)-1 {
			// Go to newer command
			ir.historyIndex++
			ir.currentInput = ir.history[ir.historyIndex]
		} else {
			// At the newest command, clear input
			ir.historyIndex = -1
			ir.currentInput = ""
		}
	}

	ir.cursorPos = len(ir.currentInput)
	ir.refreshLine()
}

// moveCursor moves the cursor left or right
func (ir *InputReader) moveCursor(direction int) {
	newPos := ir.cursorPos + direction
	if newPos >= 0 && newPos <= len(ir.currentInput) {
		ir.cursorPos = newPos
		ir.refreshLine()
	}
}

// setCursorPos sets the cursor to an absolute position
func (ir *InputReader) setCursorPos(pos int) {
	if pos >= 0 && pos <= len(ir.currentInput) {
		ir.cursorPos = pos
		ir.refreshLine()
	}
}

// insertChar inserts a character at the cursor position
func (ir *InputReader) insertChar(char rune) {
	// Store current input before modification
	ir.previousInput = ir.currentInput
	
	before := ir.currentInput[:ir.cursorPos]
	after := ir.currentInput[ir.cursorPos:]
	ir.currentInput = before + string(char) + after
	ir.cursorPos++
	ir.refreshLine()
}

// backspace deletes the character before the cursor
func (ir *InputReader) backspace() {
	if ir.cursorPos > 0 {
		// Store current input before modification
		ir.previousInput = ir.currentInput
		
		before := ir.currentInput[:ir.cursorPos-1]
		after := ir.currentInput[ir.cursorPos:]
		ir.currentInput = before + after
		ir.cursorPos--
		ir.refreshLine()
	}
}

// deleteAtCursor deletes the character at the cursor position
func (ir *InputReader) deleteAtCursor() {
	if ir.cursorPos < len(ir.currentInput) {
		// Store current input before modification
		ir.previousInput = ir.currentInput
		
		before := ir.currentInput[:ir.cursorPos]
		after := ir.currentInput[ir.cursorPos+1:]
		ir.currentInput = before + after
		ir.refreshLine()
	}
}

// refreshLine redraws the current input line
func (ir *InputReader) refreshLine() {
	// Get terminal width to handle line wrapping
	width, _, err := term.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		width = 80 // fallback width
	}
	
	// Calculate how many lines the previous input might have occupied (including prompt)
	previousLength := len(ir.prompt) + len(ir.previousInput)
	previousLines := 1
	if previousLength > 0 {
		previousLines = (previousLength + width - 1) / width
	}
	
	// Calculate how many lines the current input will occupy (including prompt)
	currentLength := len(ir.prompt) + len(ir.currentInput)
	currentLines := 1
	if currentLength > 0 {
		currentLines = (currentLength + width - 1) / width
	}
	
	// We need to clear the maximum of previous and current lines to ensure no leftover content
	linesToClear := previousLines
	if currentLines > previousLines {
		linesToClear = currentLines
	}
	
	// Move cursor to beginning of line
	fmt.Printf("\r")
	
	// Clear all lines that might have contained previous content
	for i := 0; i < linesToClear; i++ {
		if i > 0 {
			fmt.Printf("\033[B") // Move down
		}
		fmt.Printf("\033[K") // Clear current line
	}
	
	// Move back to the first line
	for i := 1; i < linesToClear; i++ {
		fmt.Printf("\033[A")
	}
	
	// Move cursor to beginning of line again
	fmt.Printf("\r")
	
	// Print prompt and current input
	fmt.Printf("%s%s", ir.prompt, ir.currentInput)
	
	// Calculate cursor position
	cursorTotalPos := len(ir.prompt) + ir.cursorPos
	
	// Move cursor to correct position
	if currentLength > 0 {
		// Calculate which line the cursor should be on (0-based)
		cursorLine := cursorTotalPos / width
		// Calculate which column the cursor should be on (0-based)
		cursorCol := cursorTotalPos % width
		
		// Calculate which line the cursor is currently on (after printing)
		// Fix: Use currentLines - 1 for the last line index
		currentLine := currentLines - 1
		
		// Move up to the correct line if needed
		linesUp := currentLine - cursorLine
		if linesUp > 0 {
			fmt.Printf("\033[%dA", linesUp)
		}
		
		// Move to correct column (ANSI uses 1-based positioning)
		fmt.Printf("\033[%dG", cursorCol+1)
	}
	
	// Update previousInput for next refresh
	ir.previousInput = ir.currentInput
}

// addToHistory adds a command to history
func (ir *InputReader) addToHistory(command string) {
	// Don't add duplicates of the last command
	if len(ir.history) > 0 && ir.history[len(ir.history)-1] == command {
		return
	}
	
	// Remove from history if it already exists (to avoid duplicates)
	for i, cmd := range ir.history {
		if cmd == command {
			ir.history = append(ir.history[:i], ir.history[i+1:]...)
			break
		}
	}
	
	// Add to history
	ir.history = append(ir.history, command)
	
	// Limit history size
	if len(ir.history) > 100 {
		ir.history = ir.history[1:]
	}
}

// SetHistory sets the command history
func (ir *InputReader) SetHistory(history []string) {
	ir.history = history
}

// GetHistory returns the command history
func (ir *InputReader) GetHistory() []string {
	return ir.history
}