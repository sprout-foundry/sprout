package interactive

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// RawInputHandler provides a custom input handler with full control
type RawInputHandler struct {
	history      []string
	historyIndex int
	currentLine  []rune
	cursorPos    int
	oldState     *term.State
	prompt       string

	// Paste detection
	buffer         []byte
	lastInputTime  time.Time
	pasteThreshold time.Duration
	isPasting      bool
	pasteBuffer    strings.Builder

	// Performance optimization
	needsRedraw    bool
	lastRedrawTime time.Time

	// Terminal info
	termWidth int

	// Output buffering
	outputBuffer []byte
}

// NewRawInputHandler creates a new raw input handler
func NewRawInputHandler(prompt string) *RawInputHandler {
	// Get terminal width
	termWidth := 80 // default
	if width, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		termWidth = width
	}

	return &RawInputHandler{
		history:        make([]string, 0, 100),
		historyIndex:   -1,
		currentLine:    make([]rune, 0, 256),
		cursorPos:      0,
		prompt:         prompt,
		buffer:         make([]byte, 0, 1024),
		pasteThreshold: 5 * time.Millisecond, // Tighter threshold to avoid false paste detection
		termWidth:      termWidth,
		outputBuffer:   make([]byte, 0, 256),
	}
}

// ReadLine reads a line with full paste detection support
func (h *RawInputHandler) ReadLine() (string, bool, error) {
	// Ensure we're not already in raw mode
	if h.oldState != nil {
		// Already in raw mode, restore first
		h.restore()
	}

	// Switch to raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return "", false, err
	}
	h.oldState = oldState
	defer h.restore()

	// Display prompt
	fmt.Print(h.prompt)

	// Reset state - keep capacity to avoid reallocation
	h.currentLine = h.currentLine[:0]
	h.cursorPos = 0
	h.isPasting = false
	h.pasteBuffer.Reset()

	// Ensure we have reasonable initial capacity
	if cap(h.currentLine) < 256 {
		h.currentLine = make([]rune, 0, 256)
	}

	buf := make([]byte, 1)
	var pasteLines []string

	// Debug: track loop iterations
	loopCount := 0

	for {
		// Time the read operation
		readStart := time.Now()
		n, err := os.Stdin.Read(buf)
		readTime := time.Since(readStart)

		if err != nil {
			return "", false, err
		}
		if n == 0 {
			continue
		}

		loopCount++

		// Check if read is slow
		if readTime > 10*time.Millisecond && buf[0] >= 32 {
			fmt.Fprintf(os.Stderr, "\r\n[SLOW READ] Loop %d, line len=%d: read took %v\r\n",
				loopCount, len(h.currentLine), readTime)
		}

		// Detect paste by timing - only for printable characters
		if buf[0] >= 32 || buf[0] == '\t' || buf[0] == 13 || buf[0] == 10 {
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

		switch buf[0] {
		case 3: // Ctrl+C
			fmt.Println("^C")
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
				fmt.Print("\r\n")
				continue
			} else {
				// Normal enter - return the line
				line := string(h.currentLine)
				fmt.Println()

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
				fmt.Println()
				combined := strings.Join(pasteLines, "\n")
				if len(h.currentLine) > 0 {
					combined += "\n" + string(h.currentLine)
				}
				return combined, true, nil
			}

			// Read additional bytes for escape sequences with timeout
			seq := make([]byte, 2)
			// Set a short timeout for escape sequence reads
			if err := os.Stdin.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
				// If we can't set deadline, just continue
				continue
			}
			n, readErr := os.Stdin.Read(seq)
			// IMPORTANT: Clear deadline immediately to avoid affecting subsequent reads
			os.Stdin.SetReadDeadline(time.Time{})

			// Check if the read error was due to timeout (expected) or something else
			if readErr != nil && !os.IsTimeout(readErr) {
				// Real error, not just timeout
				continue
			}
			if n == 2 && seq[0] == '[' {
				switch seq[1] {
				case 'A': // Up arrow
					h.historyUp()
					h.redrawLine()
					h.lastRedrawTime = time.Now()
				case 'B': // Down arrow
					h.historyDown()
					h.redrawLine()
					h.lastRedrawTime = time.Now()
				case 'C': // Right arrow
					if h.cursorPos < len(h.currentLine) {
						h.cursorPos++
						fmt.Print("\033[1C")
					}
				case 'D': // Left arrow
					if h.cursorPos > 0 {
						h.cursorPos--
						fmt.Print("\033[1D")
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
			if buf[0] >= 32 || buf[0] == '\t' {
				// Debug: time the character handling
				_ = time.Now() // charStart

				// Handle UTF-8 input
				var r rune
				if buf[0] < utf8.RuneSelf {
					// ASCII fast path - most common case
					r = rune(buf[0])
				} else {
					// Multi-byte UTF-8
					_, size := h.decodeRune(buf[0])
					if size > 1 {
						// Read additional bytes for multi-byte character
						fullBytes := make([]byte, size)
						fullBytes[0] = buf[0]
						os.Stdin.Read(fullBytes[1:])
						r, _ = utf8.DecodeRune(fullBytes)
					} else {
						r = utf8.RuneError
					}
				}

				// Insert character at cursor position
				if h.isPasting {
					h.pasteBuffer.WriteRune(r)
				}

				// More efficient insertion - append if at end, otherwise make room
				if h.cursorPos == len(h.currentLine) {
					// Simple append at end - this is the common case
					h.currentLine = append(h.currentLine, r)
					h.cursorPos++

					// For append at end, just echo the character instead of full redraw
					if !h.isPasting {
						// Time critical path
						outputStart := time.Now()
						fmt.Printf("%c", r)
						outputTime := time.Since(outputStart)

						// Check if output is slow
						if outputTime > 5*time.Millisecond || len(h.currentLine) == 90 {
							fmt.Fprintf(os.Stderr, "\r\n[DEBUG] Char %d: output took %v\r\n",
								len(h.currentLine), outputTime)
							// Also check terminal state
							fmt.Fprintf(os.Stderr, "[DEBUG] Terminal width: %d, Total length: %d\r\n",
								h.termWidth, len(h.prompt)+len(h.currentLine))
						}

						h.needsRedraw = false
					} else {
						h.needsRedraw = true
					}
				} else {
					// Insert in middle - still need to copy but try to reuse capacity
					h.currentLine = append(h.currentLine, 0)
					copy(h.currentLine[h.cursorPos+1:], h.currentLine[h.cursorPos:])
					h.currentLine[h.cursorPos] = r
					h.cursorPos++
					h.needsRedraw = true

					// For insert in middle, we need to redraw from cursor position
					if !h.isPasting {
						now := time.Now()
						minRedrawInterval := 16 * time.Millisecond // ~60fps max

						// Increase interval for longer lines to avoid terminal slowdown
						if len(h.currentLine) > 100 {
							minRedrawInterval = 50 * time.Millisecond // ~20fps for long lines
						}
						if len(h.currentLine) > 200 {
							minRedrawInterval = 100 * time.Millisecond // ~10fps for very long lines
						}

						if now.Sub(h.lastRedrawTime) > minRedrawInterval {
							h.redrawLine()
							h.lastRedrawTime = now
							h.needsRedraw = false
						}
					}
				}

				// Debug: check if character handling is slow
				// Character timing debug removed
			}
		}

		// Check if paste ended (timeout)
		if h.isPasting && time.Since(h.lastInputTime) > 50*time.Millisecond {
			// Paste ended
			allContent := h.pasteBuffer.String() + string(h.currentLine)
			lines := strings.Split(allContent, "\n")

			if len(lines) > 1 {
				// Multi-line paste detected
				fmt.Println()
				h.isPasting = false
				h.pasteBuffer.Reset()
				return allContent, true, nil
			}

			// Single line paste - treat as normal input
			h.isPasting = false
			h.pasteBuffer.Reset()
			// Continue processing normally
		}

		// Perform any pending redraws
		if h.needsRedraw && !h.isPasting {
			now := time.Now()
			if now.Sub(h.lastRedrawTime) > 16*time.Millisecond {
				h.redrawLine()
				h.lastRedrawTime = now
				h.needsRedraw = false
			}
		}
	}
}

// redrawLine redraws the current line
func (h *RawInputHandler) redrawLine() {
	// Calculate if we've wrapped
	totalLength := len(h.prompt) + len(h.currentLine)
	hasWrapped := totalLength > h.termWidth

	if hasWrapped {
		// For wrapped lines, we need to be more careful
		// Move to beginning of current line (not just column 0)
		fmt.Print("\r")

		// Clear from cursor to end of display (handles multi-line clear)
		fmt.Print("\033[J")

		// Redraw prompt and line
		fmt.Print(h.prompt)
		fmt.Print(string(h.currentLine))

		// Don't try to reposition cursor on wrapped lines - too complex
	} else {
		// Simple case - single line
		fmt.Print("\r\033[K") // Move to start and clear line
		fmt.Print(h.prompt)
		fmt.Print(string(h.currentLine))

		// Move cursor to correct position if needed
		if h.cursorPos < len(h.currentLine) {
			moveBack := len(h.currentLine) - h.cursorPos
			fmt.Printf("\033[%dD", moveBack)
		}
	}
}

// historyUp moves up in history
func (h *RawInputHandler) historyUp() {
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
func (h *RawInputHandler) historyDown() {
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
func (h *RawInputHandler) addToHistory(line string) {
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
func (h *RawInputHandler) handleTabCompletion() {
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
		fmt.Println()
		for _, match := range matches {
			fmt.Printf("  %s\n", match)
		}
		// Redraw current line
		fmt.Print(h.prompt)
		fmt.Print(string(h.currentLine))
	}
}

// decodeRune attempts to decode a UTF-8 rune starting with the given byte
func (h *RawInputHandler) decodeRune(b byte) (rune, int) {
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
func (h *RawInputHandler) restore() {
	if h.oldState != nil {
		term.Restore(int(os.Stdin.Fd()), h.oldState)
		h.oldState = nil // Clear to prevent double restore
	}
}

// SetPrompt changes the prompt
func (h *RawInputHandler) SetPrompt(prompt string) {
	h.prompt = prompt
}

// LoadHistory loads history from a file
func (h *RawInputHandler) LoadHistory(filename string) error {
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
func (h *RawInputHandler) SaveHistory(filename string) error {
	data := strings.Join(h.history, "\n")
	return os.WriteFile(filename, []byte(data), 0600)
}
