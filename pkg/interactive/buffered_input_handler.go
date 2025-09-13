package interactive

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// BufferedInputHandler provides a custom input handler with buffered reads for better performance
type BufferedInputHandler struct {
	history      []string
	historyIndex int
	currentLine  []rune
	cursorPos    int
	oldState     *term.State
	prompt       string

	// Paste detection
	lastInputTime  time.Time
	pasteThreshold time.Duration
	isPasting      bool
	pasteBuffer    strings.Builder

	// Terminal info
	termWidth int

	// Buffered input
	reader      *bufio.Reader
	inputBuffer []byte
	bufferMu    sync.Mutex

	// Output writer for flushing
	output *bufio.Writer
}

// NewBufferedInputHandler creates a new buffered input handler
func NewBufferedInputHandler(prompt string) *BufferedInputHandler {
	// Get terminal width
	termWidth := 80 // default
	if width, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		termWidth = width
	}

	return &BufferedInputHandler{
		history:        make([]string, 0, 100),
		historyIndex:   -1,
		currentLine:    make([]rune, 0, 256),
		cursorPos:      0,
		prompt:         prompt,
		pasteThreshold: 5 * time.Millisecond,
		termWidth:      termWidth,
		reader:         bufio.NewReaderSize(os.Stdin, 4096),
		inputBuffer:    make([]byte, 0, 1024),
		output:         bufio.NewWriter(os.Stdout),
	}
}

// ReadLine reads a line with buffered input for better performance
func (h *BufferedInputHandler) ReadLine() (string, bool, error) {
	// Check if stdin is a terminal
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Fallback to simple line reading
		reader := bufio.NewReader(os.Stdin)
		writer := bufio.NewWriter(os.Stdout)
		writer.WriteString(h.prompt)
		writer.Flush()
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", false, err
		}
		return strings.TrimSuffix(line, "\n"), false, nil
	}

	// Switch to raw mode
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", false, err
	}
	h.oldState = oldState
	defer h.restore()

	// Display prompt
	h.output.WriteString(h.prompt)
	h.output.Flush()

	// Reset state
	h.currentLine = h.currentLine[:0]
	h.cursorPos = 0
	h.isPasting = false
	h.pasteBuffer.Reset()

	// Ensure we have reasonable initial capacity
	if cap(h.currentLine) < 256 {
		h.currentLine = make([]rune, 0, 256)
	}

	var pasteLines []string

	for {
		// Read a byte with buffering
		b, err := h.reader.ReadByte()
		if err != nil {
			if err == io.EOF {
				return "", false, fmt.Errorf("EOF")
			}
			return "", false, err
		}

		// Detect paste by timing - only for printable characters
		if b >= 32 || b == '\t' || b == 13 || b == 10 {
			now := time.Now()
			if !h.lastInputTime.IsZero() && now.Sub(h.lastInputTime) < h.pasteThreshold {
				if !h.isPasting {
					h.isPasting = true
					// Add what we've collected so far to paste buffer
					if len(h.currentLine) > 0 {
						h.pasteBuffer.WriteString(string(h.currentLine))
					}
				}
			}
			h.lastInputTime = now
		}

		switch b {
		case 3: // Ctrl+C
			h.output.WriteString("^C\n")
			h.output.Flush()
			return "", false, fmt.Errorf("interrupted")

		case 4: // Ctrl+D
			if len(h.currentLine) == 0 {
				return "", false, fmt.Errorf("EOF")
			}

		case 13, 10: // Enter/Return
			if h.isPasting {
				// Add current line to paste buffer
				h.pasteBuffer.WriteString(string(h.currentLine))
				h.pasteBuffer.WriteByte('\n')
				pasteLines = append(pasteLines, string(h.currentLine))

				// Clear current line for next paste line
				h.currentLine = h.currentLine[:0]
				h.cursorPos = 0

				// Don't print newline in paste mode, just move cursor
				h.output.WriteString("\r\n")
				h.output.Flush()
				continue
			} else {
				// Normal enter - return the line
				line := string(h.currentLine)
				h.output.WriteString("\n")
				h.output.Flush()

				// Add to history if not empty
				if len(strings.TrimSpace(line)) > 0 {
					h.addToHistory(line)
				}

				return line, false, nil
			}

		case 27: // ESC or arrow keys
			// Check if we were pasting
			if h.isPasting && len(pasteLines) > 0 {
				// ESC during paste - return collected paste
				h.output.WriteString("\n")
				h.output.Flush()
				combined := strings.Join(pasteLines, "\n")
				if len(h.currentLine) > 0 {
					combined += "\n" + string(h.currentLine)
				}
				return combined, true, nil
			}

			// Read additional bytes for escape sequences
			seq := make([]byte, 2)
			for i := 0; i < 2; i++ {
				if h.reader.Buffered() > 0 || waitForInput(h.reader, 50*time.Millisecond) {
					seq[i], _ = h.reader.ReadByte()
				} else {
					break
				}
			}

			if seq[0] == '[' {
				switch seq[1] {
				case 'A': // Up arrow
					h.historyUp()
					h.redrawLine()
				case 'B': // Down arrow
					h.historyDown()
					h.redrawLine()
				case 'C': // Right arrow
					if h.cursorPos < len(h.currentLine) {
						h.cursorPos++
						h.output.WriteString("\033[1C")
						h.output.Flush()
					}
				case 'D': // Left arrow
					if h.cursorPos > 0 {
						h.cursorPos--
						h.output.WriteString("\033[1D")
						h.output.Flush()
					}
				case 'H': // Home
					h.cursorPos = 0
					h.redrawLine()
				case 'F': // End
					h.cursorPos = len(h.currentLine)
					h.redrawLine()
				}
			}

		case 127, 8: // Backspace
			if h.cursorPos > 0 {
				// Remove character before cursor
				h.currentLine = append(h.currentLine[:h.cursorPos-1], h.currentLine[h.cursorPos:]...)
				h.cursorPos--
				h.redrawLine()
			}

		case 9: // Tab
			// Simple tab completion for /commands
			if len(h.currentLine) > 0 && h.currentLine[0] == '/' {
				h.handleTabCompletion()
			}

		default:
			// Regular character
			if b >= 32 || b == '\t' {
				// Handle UTF-8 input
				var r rune
				if b < utf8.RuneSelf {
					// ASCII fast path
					r = rune(b)
				} else {
					// Multi-byte UTF-8
					_, size := h.decodeRune(b)
					if size > 1 {
						// Read additional bytes for multi-byte character
						fullBytes := make([]byte, size)
						fullBytes[0] = b
						for i := 1; i < size; i++ {
							fullBytes[i], _ = h.reader.ReadByte()
						}
						r, _ = utf8.DecodeRune(fullBytes)
					} else {
						r = utf8.RuneError
					}
				}

				// Insert character at cursor position
				if h.isPasting {
					h.pasteBuffer.WriteRune(r)
				}

				// More efficient insertion
				if h.cursorPos == len(h.currentLine) {
					// Simple append at end
					h.currentLine = append(h.currentLine, r)
					h.cursorPos++

					// For append at end, just echo the character
					if !h.isPasting {
						h.output.WriteRune(r)
						h.output.Flush() // Force flush to ensure character appears immediately
					}
				} else {
					// Insert in middle
					h.currentLine = append(h.currentLine, 0)
					copy(h.currentLine[h.cursorPos+1:], h.currentLine[h.cursorPos:])
					h.currentLine[h.cursorPos] = r
					h.cursorPos++

					// Redraw for insert in middle
					if !h.isPasting {
						h.redrawLine()
					}
				}
			}
		}

		// Check if paste ended (timeout)
		if h.isPasting && time.Since(h.lastInputTime) > 50*time.Millisecond {
			// Paste ended
			allContent := h.pasteBuffer.String() + string(h.currentLine)
			lines := strings.Split(allContent, "\n")

			if len(lines) > 1 {
				// Multi-line paste detected
				h.output.WriteString("\n")
				h.output.Flush()
				h.isPasting = false
				h.pasteBuffer.Reset()
				return allContent, true, nil
			}

			// Single line paste - treat as normal input
			h.isPasting = false
			h.pasteBuffer.Reset()
		}
	}
}

// waitForInput waits for input to be available with a timeout
func waitForInput(reader *bufio.Reader, timeout time.Duration) bool {
	// Check if data is already buffered
	if reader.Buffered() > 0 {
		return true
	}

	// Since we can't do a proper select on stdin in Go,
	// we'll just return false after timeout
	// This is a simplified approach
	return false
}

// redrawLine redraws the current line
func (h *BufferedInputHandler) redrawLine() {
	// Move to start of line and clear
	h.output.WriteString("\r\033[K")
	h.output.WriteString(h.prompt)
	h.output.WriteString(string(h.currentLine))

	// Move cursor to correct position if needed
	if h.cursorPos < len(h.currentLine) {
		moveBack := len(h.currentLine) - h.cursorPos
		fmt.Fprintf(h.output, "\033[%dD", moveBack)
	}

	h.output.Flush()
}

// historyUp moves up in history
func (h *BufferedInputHandler) historyUp() {
	if len(h.history) == 0 {
		return
	}

	if h.historyIndex == -1 {
		h.historyIndex = len(h.history) - 1
	} else if h.historyIndex > 0 {
		h.historyIndex--
	}

	if h.historyIndex >= 0 && h.historyIndex < len(h.history) {
		h.currentLine = []rune(h.history[h.historyIndex])
		h.cursorPos = len(h.currentLine)
	}
}

// historyDown moves down in history
func (h *BufferedInputHandler) historyDown() {
	if h.historyIndex == -1 {
		return
	}

	h.historyIndex++
	if h.historyIndex >= len(h.history) {
		h.historyIndex = -1
		h.currentLine = h.currentLine[:0]
		h.cursorPos = 0
	} else {
		h.currentLine = []rune(h.history[h.historyIndex])
		h.cursorPos = len(h.currentLine)
	}
}

// addToHistory adds a line to history
func (h *BufferedInputHandler) addToHistory(line string) {
	// Don't add duplicates
	if len(h.history) > 0 && h.history[len(h.history)-1] == line {
		return
	}

	h.history = append(h.history, line)

	// Limit history size
	if len(h.history) > 1000 {
		h.history = h.history[len(h.history)-1000:]
	}

	h.historyIndex = -1
}

// handleTabCompletion handles basic tab completion for commands
func (h *BufferedInputHandler) handleTabCompletion() {
	current := string(h.currentLine)

	// List of commands to complete
	commands := []string{
		"/help", "/quit", "/exit", "/paste", "/models", "/provider",
		"/shell", "/exec", "/info", "/commit", "/changes", "/status",
		"/log", "/rollback", "/mcp",
	}

	// Find matches
	var matches []string
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, current) {
			matches = append(matches, cmd)
		}
	}

	if len(matches) == 1 {
		// Single match - complete it
		h.currentLine = []rune(matches[0])
		h.cursorPos = len(h.currentLine)
		h.redrawLine()
	} else if len(matches) > 1 {
		// Multiple matches - show them
		h.output.WriteString("\n")
		for _, match := range matches {
			fmt.Fprintf(h.output, "  %s\n", match)
		}
		// Redraw current line
		h.output.WriteString(h.prompt)
		h.output.WriteString(string(h.currentLine))
		h.output.Flush()
	}
}

// decodeRune attempts to decode a UTF-8 rune starting with the given byte
func (h *BufferedInputHandler) decodeRune(b byte) (rune, int) {
	if b < utf8.RuneSelf {
		return rune(b), 1
	}

	// Determine the size of the UTF-8 sequence
	size := 0
	if b&0xE0 == 0xC0 {
		size = 2
	} else if b&0xF0 == 0xE0 {
		size = 3
	} else if b&0xF8 == 0xF0 {
		size = 4
	} else {
		return utf8.RuneError, 1
	}

	return utf8.RuneError, size
}

// restore restores the terminal state
func (h *BufferedInputHandler) restore() {
	if h.oldState != nil {
		term.Restore(int(os.Stdin.Fd()), h.oldState)
		h.oldState = nil
	}
}

// SetPrompt changes the prompt
func (h *BufferedInputHandler) SetPrompt(prompt string) {
	h.prompt = prompt
}

// LoadHistory loads history from a file
func (h *BufferedInputHandler) LoadHistory(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No history yet
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line != "" {
			h.history = append(h.history, line)
		}
	}

	// Limit history size
	if len(h.history) > 1000 {
		h.history = h.history[len(h.history)-1000:]
	}

	return nil
}

// SaveHistory saves history to a file
func (h *BufferedInputHandler) SaveHistory(filename string) error {
	data := strings.Join(h.history, "\n")
	return os.WriteFile(filename, []byte(data), 0600)
}
