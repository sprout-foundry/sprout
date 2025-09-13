package interactive

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// UnbufferedInputHandler provides truly unbuffered input handling
type UnbufferedInputHandler struct {
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

	// File descriptors for direct I/O
	stdinFd  int
	stdoutFd int
}

// NewUnbufferedInputHandler creates a new unbuffered input handler
func NewUnbufferedInputHandler(prompt string) *UnbufferedInputHandler {
	// Try to open /dev/tty directly for output to ensure we're writing to the terminal
	ttyFile, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	var stdoutFd int
	if err == nil {
		stdoutFd = int(ttyFile.Fd())
	} else {
		// Fallback to stdout
		stdoutFd = int(os.Stdout.Fd())
	}

	// Get terminal width
	termWidth := 80 // default
	if width, _, err := term.GetSize(stdoutFd); err == nil {
		termWidth = width
	}

	return &UnbufferedInputHandler{
		history:        make([]string, 0, 100),
		historyIndex:   -1,
		currentLine:    make([]rune, 0, 256),
		cursorPos:      0,
		prompt:         prompt,
		pasteThreshold: 5 * time.Millisecond,
		termWidth:      termWidth,
		stdinFd:        int(os.Stdin.Fd()),
		stdoutFd:       stdoutFd,
	}
}

// writeString writes a string directly to stdout using syscall
func (h *UnbufferedInputHandler) writeString(s string) {
	syscall.Write(h.stdoutFd, []byte(s))
	// Also try os.Stdout.Sync()
	os.Stdout.Sync()
}

// writeRune writes a rune directly to stdout
func (h *UnbufferedInputHandler) writeRune(r rune) {
	buf := make([]byte, utf8.UTFMax)
	n := utf8.EncodeRune(buf, r)

	// Write the character
	os.Stdout.Write(buf[:n])

	// Force cursor forward one position to ensure visibility
	// This might help with terminal state issues
	os.Stdout.WriteString("\033[1C\033[1D") // Move right then left

	// Explicit flush
	os.Stdout.Sync()
}

// ReadLine reads a line with direct syscalls for immediate response
func (h *UnbufferedInputHandler) ReadLine() (string, bool, error) {
	// Check if stdin is a terminal
	if !term.IsTerminal(h.stdinFd) {
		// Fallback to simple line reading
		fmt.Print(h.prompt)
		var line string
		_, err := fmt.Scanln(&line)
		if err != nil {
			return "", false, err
		}
		return line, false, nil
	}

	// Ensure clean state before starting
	// This helps recover from commands that mess with the terminal
	h.restore() // Ensure any previous state is cleaned up

	// Switch to raw mode
	oldState, err := term.MakeRaw(h.stdinFd)
	if err != nil {
		return "", false, err
	}
	h.oldState = oldState
	defer h.restore()

	// Display prompt - write directly
	h.writeString(h.prompt)

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
	buf := make([]byte, 1)

	for {
		// Read a single byte directly from stdin
		n, err := syscall.Read(h.stdinFd, buf)
		if err != nil || n != 1 {
			if err != nil {
				return "", false, err
			}
			continue
		}

		b := buf[0]

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
			h.writeString("^C\n")
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
				h.writeString("\r\n")
				continue
			} else {
				// Normal enter - return the line
				line := string(h.currentLine)
				h.writeString("\n")

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
				h.writeString("\n")
				combined := strings.Join(pasteLines, "\n")
				if len(h.currentLine) > 0 {
					combined += "\n" + string(h.currentLine)
				}
				return combined, true, nil
			}

			// Read additional bytes for escape sequences
			seq := make([]byte, 2)
			// Try to read with a short timeout
			syscall.Read(h.stdinFd, seq[:1])
			if seq[0] == '[' {
				syscall.Read(h.stdinFd, seq[1:2])

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
						h.writeString("\033[1C")
					}
				case 'D': // Left arrow
					if h.cursorPos > 0 {
						h.cursorPos--
						h.writeString("\033[1D")
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
							syscall.Read(h.stdinFd, fullBytes[i:i+1])
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

					// For append at end, just echo the character immediately
					if !h.isPasting {
						// WORKAROUND: Some terminals don't display characters immediately in raw mode
						// Do a minimal redraw to ensure the character is visible
						// This is inefficient but ensures characters always appear
						h.redrawLine()
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
				h.writeString("\n")
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

// redrawLine redraws the current line
func (h *UnbufferedInputHandler) redrawLine() {
	// Move to start of line and clear
	h.writeString("\r\033[K")
	h.writeString(h.prompt)
	h.writeString(string(h.currentLine))

	// Move cursor to correct position if needed
	if h.cursorPos < len(h.currentLine) {
		moveBack := len(h.currentLine) - h.cursorPos
		h.writeString(fmt.Sprintf("\033[%dD", moveBack))
	}
}

// historyUp moves up in history
func (h *UnbufferedInputHandler) historyUp() {
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
func (h *UnbufferedInputHandler) historyDown() {
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
func (h *UnbufferedInputHandler) addToHistory(line string) {
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
func (h *UnbufferedInputHandler) handleTabCompletion() {
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
		h.writeString("\n")
		for _, match := range matches {
			h.writeString(fmt.Sprintf("  %s\n", match))
		}
		// Redraw current line
		h.writeString(h.prompt)
		h.writeString(string(h.currentLine))
	}
}

// decodeRune attempts to decode a UTF-8 rune starting with the given byte
func (h *UnbufferedInputHandler) decodeRune(b byte) (rune, int) {
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
func (h *UnbufferedInputHandler) restore() {
	if h.oldState != nil {
		term.Restore(h.stdinFd, h.oldState)
		h.oldState = nil
	}
}

// EnsureCleanTerminal ensures the terminal is in a clean state
// This is useful after running external commands that might mess with the terminal
func (h *UnbufferedInputHandler) EnsureCleanTerminal() {
	// First restore if we have a saved state
	h.restore()

	// Clear any pending input
	term.Restore(h.stdinFd, nil)

	// Ensure echo is on and terminal is in cooked mode
	// This uses stty to force the terminal into a known good state
	if term.IsTerminal(h.stdinFd) {
		// Try to reset terminal using stty
		cmd := exec.Command("stty", "sane")
		cmd.Stdin = os.Stdin
		cmd.Run()
	}
}

// SetPrompt changes the prompt
func (h *UnbufferedInputHandler) SetPrompt(prompt string) {
	h.prompt = prompt
}

// LoadHistory loads history from a file
func (h *UnbufferedInputHandler) LoadHistory(filename string) error {
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
func (h *UnbufferedInputHandler) SaveHistory(filename string) error {
	data := strings.Join(h.history, "\n")
	return os.WriteFile(filename, []byte(data), 0600)
}
