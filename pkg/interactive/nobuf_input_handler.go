package interactive

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// NobufInputHandler provides input handling with absolutely no buffering
type NobufInputHandler struct {
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
}

// NewNobufInputHandler creates a new no-buffer input handler
func NewNobufInputHandler(prompt string) *NobufInputHandler {
	// Disable all buffering on stdout
	os.Stdout = os.NewFile(uintptr(1), "/dev/stdout")

	// Get terminal width
	termWidth := 80 // default
	if width, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		termWidth = width
	}

	return &NobufInputHandler{
		history:        make([]string, 0, 100),
		historyIndex:   -1,
		currentLine:    make([]rune, 0, 256),
		cursorPos:      0,
		prompt:         prompt,
		pasteThreshold: 5 * time.Millisecond,
		termWidth:      termWidth,
	}
}

// ReadLine reads a line with no buffering
func (h *NobufInputHandler) ReadLine() (string, bool, error) {
	// Check if stdin is a terminal
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Fallback to simple line reading
		fmt.Print(h.prompt)
		var line string
		_, err := fmt.Scanln(&line)
		if err != nil {
			return "", false, err
		}
		return line, false, nil
	}

	// Switch to raw mode
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", false, err
	}
	h.oldState = oldState
	defer h.restore()

	// Display prompt
	fmt.Print(h.prompt)

	// Reset state
	h.currentLine = h.currentLine[:0]
	h.cursorPos = 0
	h.isPasting = false
	h.pasteBuffer.Reset()

	var pasteLines []string
	buf := make([]byte, 1)

	for {
		// Read one byte at a time
		n, err := os.Stdin.Read(buf)
		if err != nil || n != 1 {
			if err != nil {
				return "", false, err
			}
			continue
		}

		b := buf[0]

		// Detect paste by timing
		if b >= 32 || b == '\t' || b == 13 || b == 10 {
			now := time.Now()
			if !h.lastInputTime.IsZero() && now.Sub(h.lastInputTime) < h.pasteThreshold {
				if !h.isPasting {
					h.isPasting = true
					if len(h.currentLine) > 0 {
						h.pasteBuffer.WriteString(string(h.currentLine))
					}
				}
			}
			h.lastInputTime = now
		}

		switch b {
		case 3: // Ctrl+C
			fmt.Print("^C\n")
			return "", false, fmt.Errorf("interrupted")

		case 4: // Ctrl+D
			if len(h.currentLine) == 0 {
				return "", false, fmt.Errorf("EOF")
			}

		case 13, 10: // Enter
			if h.isPasting {
				h.pasteBuffer.WriteString(string(h.currentLine))
				h.pasteBuffer.WriteByte('\n')
				pasteLines = append(pasteLines, string(h.currentLine))
				h.currentLine = h.currentLine[:0]
				h.cursorPos = 0
				fmt.Print("\r\n")
				continue
			} else {
				line := string(h.currentLine)
				fmt.Print("\n")
				if len(strings.TrimSpace(line)) > 0 {
					h.addToHistory(line)
				}
				return line, false, nil
			}

		case 27: // ESC
			if h.isPasting && len(pasteLines) > 0 {
				fmt.Print("\n")
				combined := strings.Join(pasteLines, "\n")
				if len(h.currentLine) > 0 {
					combined += "\n" + string(h.currentLine)
				}
				return combined, true, nil
			}

			// Handle arrow keys
			seq := make([]byte, 2)
			os.Stdin.Read(seq)
			if seq[0] == '[' {
				switch seq[1] {
				case 'A': // Up
					h.historyUp()
					h.redrawLine()
				case 'B': // Down
					h.historyDown()
					h.redrawLine()
				case 'C': // Right
					if h.cursorPos < len(h.currentLine) {
						h.cursorPos++
						fmt.Print("\033[1C")
					}
				case 'D': // Left
					if h.cursorPos > 0 {
						h.cursorPos--
						fmt.Print("\033[1D")
					}
				}
			}

		case 127, 8: // Backspace
			if h.cursorPos > 0 {
				h.currentLine = append(h.currentLine[:h.cursorPos-1], h.currentLine[h.cursorPos:]...)
				h.cursorPos--
				h.redrawLine()
			}

		case 9: // Tab
			if len(h.currentLine) > 0 && h.currentLine[0] == '/' {
				h.handleTabCompletion()
			}

		default:
			// Regular character
			if b >= 32 || b == '\t' {
				var r rune
				if b < utf8.RuneSelf {
					r = rune(b)
				} else {
					// Handle multi-byte UTF-8
					_, size := h.decodeRune(b)
					if size > 1 {
						fullBytes := make([]byte, size)
						fullBytes[0] = b
						for i := 1; i < size; i++ {
							os.Stdin.Read(fullBytes[i : i+1])
						}
						r, _ = utf8.DecodeRune(fullBytes)
					} else {
						r = utf8.RuneError
					}
				}

				if h.isPasting {
					h.pasteBuffer.WriteRune(r)
				}

				// Insert character
				if h.cursorPos == len(h.currentLine) {
					h.currentLine = append(h.currentLine, r)
					h.cursorPos++
					if !h.isPasting {
						// Direct write with no buffering
						fmt.Printf("%c", r)
					}
				} else {
					h.currentLine = append(h.currentLine, 0)
					copy(h.currentLine[h.cursorPos+1:], h.currentLine[h.cursorPos:])
					h.currentLine[h.cursorPos] = r
					h.cursorPos++
					if !h.isPasting {
						h.redrawLine()
					}
				}
			}
		}

		// Check paste timeout
		if h.isPasting && time.Since(h.lastInputTime) > 50*time.Millisecond {
			allContent := h.pasteBuffer.String() + string(h.currentLine)
			lines := strings.Split(allContent, "\n")
			if len(lines) > 1 {
				fmt.Print("\n")
				h.isPasting = false
				h.pasteBuffer.Reset()
				return allContent, true, nil
			}
			h.isPasting = false
			h.pasteBuffer.Reset()
		}
	}
}

// redrawLine redraws the current line
func (h *NobufInputHandler) redrawLine() {
	fmt.Print("\r\033[K")
	fmt.Print(h.prompt)
	fmt.Print(string(h.currentLine))
	if h.cursorPos < len(h.currentLine) {
		moveBack := len(h.currentLine) - h.cursorPos
		fmt.Printf("\033[%dD", moveBack)
	}
}

// historyUp moves up in history
func (h *NobufInputHandler) historyUp() {
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
func (h *NobufInputHandler) historyDown() {
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
func (h *NobufInputHandler) addToHistory(line string) {
	if len(h.history) > 0 && h.history[len(h.history)-1] == line {
		return
	}
	h.history = append(h.history, line)
	if len(h.history) > 1000 {
		h.history = h.history[len(h.history)-1000:]
	}
	h.historyIndex = -1
}

// handleTabCompletion handles tab completion
func (h *NobufInputHandler) handleTabCompletion() {
	current := string(h.currentLine)
	commands := []string{
		"/help", "/quit", "/exit", "/paste", "/models", "/provider",
		"/shell", "/exec", "/info", "/commit", "/changes", "/status",
		"/log", "/rollback", "/mcp",
	}

	var matches []string
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, current) {
			matches = append(matches, cmd)
		}
	}

	if len(matches) == 1 {
		h.currentLine = []rune(matches[0])
		h.cursorPos = len(h.currentLine)
		h.redrawLine()
	} else if len(matches) > 1 {
		fmt.Print("\n")
		for _, match := range matches {
			fmt.Printf("  %s\n", match)
		}
		fmt.Print(h.prompt)
		fmt.Print(string(h.currentLine))
	}
}

// decodeRune decodes a UTF-8 rune
func (h *NobufInputHandler) decodeRune(b byte) (rune, int) {
	if b < utf8.RuneSelf {
		return rune(b), 1
	}
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

// restore restores terminal state
func (h *NobufInputHandler) restore() {
	if h.oldState != nil {
		term.Restore(int(os.Stdin.Fd()), h.oldState)
		h.oldState = nil
	}
}

// SetPrompt sets the prompt
func (h *NobufInputHandler) SetPrompt(prompt string) {
	h.prompt = prompt
}

// LoadHistory loads history from file
func (h *NobufInputHandler) LoadHistory(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line != "" {
			h.history = append(h.history, line)
		}
	}
	if len(h.history) > 1000 {
		h.history = h.history[len(h.history)-1000:]
	}
	return nil
}

// SaveHistory saves history to file
func (h *NobufInputHandler) SaveHistory(filename string) error {
	data := strings.Join(h.history, "\n")
	return os.WriteFile(filename, []byte(data), 0600)
}
