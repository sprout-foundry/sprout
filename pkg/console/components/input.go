package components

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

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
		historyIndex:   -1,
		currentLine:    make([]rune, 0, 256),
		cursorPos:      0,
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

	// Display prompt
	fmt.Print(c.prompt)

	// Read input
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return "", false, err
		}

		for i := 0; i < n; i++ {
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
		fmt.Println("^C")
		if c.onCancel != nil {
			c.onCancel()
		}
		return false, true

	case 4: // Ctrl+D
		if len(c.currentLine) == 0 {
			return false, true
		}

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
		fmt.Println()
		return false, true

	case 127: // Backspace
		if c.cursorPos > 0 {
			c.currentLine = append(c.currentLine[:c.cursorPos-1], c.currentLine[c.cursorPos:]...)
			c.cursorPos--
			c.redrawLine()
		}

	case 27: // Escape sequence
		// Read the rest of the escape sequence
		seq := make([]byte, 2)
		os.Stdin.Read(seq)

		if seq[0] == '[' {
			switch seq[1] {
			case 'A': // Up arrow
				if c.historyEnabled && c.historyIndex > 0 {
					if c.historyIndex == len(c.history) {
						// Save current line
						c.history = append(c.history, string(c.currentLine))
					}
					c.historyIndex--
					c.currentLine = []rune(c.history[c.historyIndex])
					c.cursorPos = len(c.currentLine)
					c.redrawLine()
				}

			case 'B': // Down arrow
				if c.historyEnabled && c.historyIndex < len(c.history)-1 {
					c.historyIndex++
					if c.historyIndex == len(c.history)-1 {
						// Restore saved line
						c.currentLine = []rune(c.history[c.historyIndex])
						c.history = c.history[:len(c.history)-1]
					} else {
						c.currentLine = []rune(c.history[c.historyIndex])
					}
					c.cursorPos = len(c.currentLine)
					c.redrawLine()
				}

			case 'C': // Right arrow
				if c.cursorPos < len(c.currentLine) {
					c.cursorPos++
					fmt.Print("\033[C")
				}

			case 'D': // Left arrow
				if c.cursorPos > 0 {
					c.cursorPos--
					fmt.Print("\033[D")
				}

			case 'H': // Home
				c.cursorPos = 0
				c.redrawLine()

			case 'F': // End
				c.cursorPos = len(c.currentLine)
				c.redrawLine()
			}
		}

	default:
		// Regular character
		if key >= 32 && key < 127 {
			r := rune(key)
			c.currentLine = append(c.currentLine[:c.cursorPos],
				append([]rune{r}, c.currentLine[c.cursorPos:]...)...)
			c.cursorPos++

			if c.echoEnabled {
				if c.cursorPos == len(c.currentLine) {
					// At end of line, just print the character
					fmt.Printf("%c", r)
				} else {
					// In middle of line, redraw
					c.redrawLine()
				}
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
	// Clear current line
	fmt.Print("\r\033[K")

	// Redraw prompt and line
	fmt.Print(c.prompt)
	if c.echoEnabled {
		fmt.Print(string(c.currentLine))
	} else {
		// Password mode - show asterisks
		for range c.currentLine {
			fmt.Print("*")
		}
	}

	// Move cursor to correct position
	if c.cursorPos < len(c.currentLine) {
		moveBack := utf8.RuneCountInString(string(c.currentLine[c.cursorPos:]))
		if moveBack > 0 {
			fmt.Printf("\033[%dD", moveBack)
		}
	}
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
	c.historyIndex = -1
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
