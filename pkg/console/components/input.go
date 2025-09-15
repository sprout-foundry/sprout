package components

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/console"
	"golang.org/x/term"
)

// InputComponent handles user input with history, line editing, and command callbacks
type InputComponent struct {
	*console.BaseComponent

	// Configuration
	prompt         string
	multiline      bool
	echoEnabled    bool
	historyEnabled bool
	maxHistory     int

	// State
	history      []string
	historyIndex int
	currentLine  []rune
	cursorPos    int
	isRawMode    bool
	oldTermState *term.State
	tempLine     string // Store current line when navigating history

	// Terminal dimensions for multi-line handling
	termWidth     int
	prevLineCount int // Track how many lines the previous render used

	// Callbacks
	onSubmit func(string) error
	onCancel func()
	onTab    func(string, int) []string // For autocomplete
}

// NewInputComponent creates a new input component
func NewInputComponent(id, prompt string) *InputComponent {
	base := console.NewBaseComponent(id, "input")

	return &InputComponent{
		BaseComponent:  base,
		prompt:         prompt,
		echoEnabled:    true,
		historyEnabled: true,
		maxHistory:     100,
		history:        make([]string, 0, 100),
		historyIndex:   0,
		currentLine:    make([]rune, 0, 256),
		cursorPos:      0,
	}
}

// OnResize handles terminal resize events
func (c *InputComponent) OnResize(width, height int) {
	// Only redraw if we're in raw mode (actively reading input)
	if c.isRawMode && c.echoEnabled {
		c.redrawLine()
	}
}

// Configuration methods

func (c *InputComponent) SetPrompt(prompt string) *InputComponent {
	c.prompt = prompt
	return c
}

func (c *InputComponent) SetMultiline(enabled bool) *InputComponent {
	c.multiline = enabled
	return c
}

func (c *InputComponent) SetEcho(enabled bool) *InputComponent {
	c.echoEnabled = enabled
	return c
}

func (c *InputComponent) SetHistory(enabled bool) *InputComponent {
	c.historyEnabled = enabled
	return c
}

func (c *InputComponent) SetOnSubmit(fn func(string) error) *InputComponent {
	c.onSubmit = fn
	return c
}

func (c *InputComponent) SetOnCancel(fn func()) *InputComponent {
	c.onCancel = fn
	return c
}

func (c *InputComponent) SetOnTab(fn func(string, int) []string) *InputComponent {
	c.onTab = fn
	return c
}

// InputHandler interface implementation

func (c *InputComponent) ReadLine() (string, bool, error) {
	fd := int(os.Stdin.Fd())

	// Non-terminal fallback
	if !term.IsTerminal(fd) {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print(c.prompt)
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
	c.oldTermState = oldState
	c.isRawMode = true
	defer c.restore()

	// Clear line state
	c.currentLine = c.currentLine[:0]
	c.cursorPos = 0
	c.historyIndex = len(c.history)
	c.tempLine = ""
	c.prevLineCount = 1

	// Get terminal width
	c.termWidth = 80 // Default
	if w, _, err := term.GetSize(fd); err == nil {
		c.termWidth = w
	}

	// Ensure we're on a new line before displaying prompt
	// This fixes the issue where prompt appears at the end of previous output
	fmt.Print("\r\033[K") // Clear any leftover on current line
	fmt.Print(c.prompt)

	// Read input
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return "", false, err
		}

		for i := 0; i < n; i++ {
			// Check for Ctrl+C first
			if buf[i] == 3 {
				// Process as a regular keypress so we handle it consistently
				wasMultiline, done := c.processKeypress(buf[i])
				if done {
					line := string(c.currentLine)
					if c.historyEnabled && line != "" {
						c.addToHistory(line)
					}
					return line, wasMultiline, nil
				}
				continue
			}

			// Check if this is an escape sequence
			if buf[i] == 27 && i+2 < n && buf[i+1] == '[' {
				// Handle escape sequences inline
				if buf[i+2] == 'A' { // Up arrow
					if c.historyEnabled && len(c.history) > 0 {
						// Save current line if we're at the end of history
						if c.historyIndex >= len(c.history) {
							c.tempLine = string(c.currentLine)
							c.historyIndex = len(c.history) - 1
						} else if c.historyIndex > 0 {
							c.historyIndex--
						} else {
							// Already at the beginning
							i += 2
							continue
						}

						// Update the current line from history
						if c.historyIndex < len(c.history) {
							c.currentLine = []rune(c.history[c.historyIndex])
							c.cursorPos = len(c.currentLine)
							c.redrawLine()
						}
					}
					i += 2 // Skip the [ and A
					continue
				} else if buf[i+2] == 'B' { // Down arrow
					if c.historyEnabled {
						// Move down in history
						if c.historyIndex < len(c.history) {
							c.historyIndex++

							if c.historyIndex == len(c.history) {
								// Restore the temp line
								c.currentLine = []rune(c.tempLine)
							} else {
								c.currentLine = []rune(c.history[c.historyIndex])
							}

							c.cursorPos = len(c.currentLine)
							c.redrawLine()
						}
					}
					i += 2 // Skip the [ and B
					continue
				} else if buf[i+2] == 'C' { // Right arrow
					if c.cursorPos < len(c.currentLine) {
						c.cursorPos++
						c.redrawLine()
					}
					i += 2
					continue
				} else if buf[i+2] == 'D' { // Left arrow
					if c.cursorPos > 0 {
						c.cursorPos--
						c.redrawLine()
					}
					i += 2
					continue
				}
			}

			// Process single byte normally
			wasMultiline, done := c.processKeypress(buf[i])
			if done {
				line := string(c.currentLine)
				if c.historyEnabled && line != "" {
					c.addToHistory(line)
				}
				return line, wasMultiline, nil
			}
		}
	}
}

func (c *InputComponent) processKeypress(key byte) (multiline, done bool) {
	switch key {
	case 3: // Ctrl+C
		// When in raw mode, we need to handle Ctrl+C ourselves
		fmt.Print("\r\033[K") // Clear line
		if c.onCancel != nil {
			c.onCancel()
		}
		// Clear current line
		c.currentLine = c.currentLine[:0]
		c.cursorPos = 0
		c.historyIndex = len(c.history)
		c.tempLine = ""
		// Don't return done, just continue
		return false, false

	case 1: // Ctrl+A - move to beginning of line
		c.cursorPos = 0
		c.redrawLine()

	case 4: // Ctrl+D
		if len(c.currentLine) == 0 {
			return false, true
		}

	case 5: // Ctrl+E - move to end of line
		c.cursorPos = len(c.currentLine)
		c.redrawLine()

	case 11: // Ctrl+K - delete to end of line
		if c.cursorPos < len(c.currentLine) {
			c.currentLine = c.currentLine[:c.cursorPos]
			c.redrawLine()
		}

	case 21: // Ctrl+U - clear line
		c.currentLine = c.currentLine[:0]
		c.cursorPos = 0
		c.redrawLine()

	case 9: // Tab
		if c.onTab != nil {
			suggestions := c.onTab(string(c.currentLine), c.cursorPos)
			if len(suggestions) > 0 {
				// Simple completion - just use first suggestion
				c.currentLine = []rune(suggestions[0])
				c.cursorPos = len(c.currentLine)
				c.redrawLine()
			}
		}

	case 10, 13: // Enter
		fmt.Print("\r\033[K")                                 // Clear the line first
		fmt.Printf("%s%s\n", c.prompt, string(c.currentLine)) // Reprint the full command
		return false, true

	case 127: // Backspace
		if c.cursorPos > 0 {
			c.currentLine = append(c.currentLine[:c.cursorPos-1], c.currentLine[c.cursorPos:]...)
			c.cursorPos--
			c.redrawLine()
		}

	case 27: // Escape sequence
		// Just ESC by itself - ignore for now
		return false, false

	default:
		// Regular character
		if key >= 32 && key < 127 {
			r := rune(key)
			c.currentLine = append(c.currentLine[:c.cursorPos],
				append([]rune{r}, c.currentLine[c.cursorPos:]...)...)
			c.cursorPos++

			// Always redraw to handle line wrapping
			if c.echoEnabled {
				c.redrawLine()
			}
		} else if key >= 0xC0 {
			// Handle UTF-8
			// This is simplified - in production you'd want proper UTF-8 handling
			c.currentLine = append(c.currentLine[:c.cursorPos],
				append([]rune{rune(key)}, c.currentLine[c.cursorPos:]...)...)
			c.cursorPos++
			if c.echoEnabled {
				c.redrawLine()
			}
		}
	}

	return false, false
}

func (c *InputComponent) redrawLine() {
	// Get terminal width
	if c.termWidth == 0 {
		c.termWidth = 80 // Default
		if c.Terminal() != nil {
			if w, _, err := c.Terminal().GetSize(); err == nil {
				c.termWidth = w
			}
		}
	}

	// Calculate total length including prompt
	promptLen := len(c.prompt)
	totalLen := promptLen + len(c.currentLine)

	// Calculate how many lines this will take
	newLineCount := 1
	if c.termWidth > 0 {
		newLineCount = (totalLen + c.termWidth - 1) / c.termWidth
		if newLineCount == 0 {
			newLineCount = 1
		}
	}

	// If we previously rendered multiple lines, we need to clear them all
	if c.prevLineCount > 1 {
		// Move to the beginning of the first line
		for i := 1; i < c.prevLineCount; i++ {
			fmt.Print("\033[A") // Move up
		}
	}

	// Clear all lines that we're going to use
	fmt.Print("\r\033[K") // Clear current line
	for i := 1; i < newLineCount; i++ {
		fmt.Print("\n\033[K") // New line and clear
	}

	// Move back to the beginning
	if newLineCount > 1 {
		for i := 1; i < newLineCount; i++ {
			fmt.Print("\033[A") // Move up
		}
	}
	fmt.Print("\r")

	// Print prompt and content
	fmt.Print(c.prompt)
	if c.echoEnabled {
		fmt.Print(string(c.currentLine))
	} else {
		// Password mode
		for range c.currentLine {
			fmt.Print("*")
		}
	}

	// Position cursor correctly
	if c.cursorPos < len(c.currentLine) {
		// Calculate absolute position
		absolutePos := promptLen + c.cursorPos
		targetRow := absolutePos / c.termWidth
		targetCol := absolutePos % c.termWidth

		// Current position is at the end
		currentPos := totalLen
		currentRow := (currentPos - 1) / c.termWidth
		currentCol := (currentPos - 1) % c.termWidth

		// Move cursor to target position
		if targetRow < currentRow {
			fmt.Printf("\033[%dA", currentRow-targetRow) // Move up
		}
		if targetCol < currentCol {
			fmt.Printf("\033[%dD", currentCol-targetCol) // Move left
		} else if targetCol > currentCol {
			fmt.Printf("\033[%dC", targetCol-currentCol) // Move right
		}
	}

	// Remember how many lines we used
	c.prevLineCount = newLineCount
}

func (c *InputComponent) addToHistory(line string) {
	// Don't add duplicates
	if len(c.history) > 0 && c.history[len(c.history)-1] == line {
		return
	}

	c.history = append(c.history, line)

	// Trim history if needed
	if len(c.history) > c.maxHistory {
		c.history = c.history[len(c.history)-c.maxHistory:]
	}
}

func (c *InputComponent) restore() {
	if c.isRawMode && c.oldTermState != nil {
		term.Restore(int(os.Stdin.Fd()), c.oldTermState)
		c.isRawMode = false
		c.oldTermState = nil
	}
}

// Component interface implementation

func (c *InputComponent) Render() error {
	// Input component renders inline during ReadLine
	return nil
}

func (c *InputComponent) NeedsRedraw() bool {
	return false // Input manages its own drawing
}

func (c *InputComponent) HandleInput(input []byte) (handled bool, err error) {
	// Input component uses ReadLine instead
	return false, nil
}

func (c *InputComponent) CanHandleInput() bool {
	return false // Uses ReadLine API instead
}

// Additional utility methods

func (c *InputComponent) GetHistory() []string {
	return append([]string{}, c.history...)
}

func (c *InputComponent) ClearHistory() {
	c.history = c.history[:0]
	c.historyIndex = 0
}

func (c *InputComponent) AddToHistory(line string) {
	c.addToHistory(line)
}

// Compatibility wrapper for legacy code
type LegacyInputWrapper struct {
	*InputComponent
}

func NewLegacyInputWrapper(prompt string) *LegacyInputWrapper {
	// Create a standalone input component
	input := NewInputComponent("legacy-input", prompt)
	return &LegacyInputWrapper{InputComponent: input}
}

func (w *LegacyInputWrapper) Close() error {
	return w.Cleanup()
}

// LoadHistory loads history from a file
func (c *InputComponent) LoadHistory(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No history file yet
		}
		return err
	}

	lines := strings.Split(string(data), "\n")
	c.history = c.history[:0] // Clear existing
	for _, line := range lines {
		line = strings.TrimRight(line, "\r\n") // Remove line endings but preserve spaces
		if line != "" {
			c.history = append(c.history, line)
		}
	}
	c.historyIndex = len(c.history)
	return nil
}

// SaveHistory saves history to a file
func (c *InputComponent) SaveHistory(filename string) error {
	data := strings.Join(c.history, "\n")
	return os.WriteFile(filename, []byte(data), 0600)
}
