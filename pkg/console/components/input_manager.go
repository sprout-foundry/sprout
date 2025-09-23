package components

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/alantheprice/ledit/pkg/console"
	"golang.org/x/term"
)

// InputManager handles concurrent input with persistent input field
type InputManager struct {
	// Terminal state
	terminalFd   int
	oldTermState *term.State
	isRawMode    bool
	termWidth    int
	termHeight   int

	// Input state
	currentLine    []rune
	cursorPos      int
	prompt         string
	inputFieldLine int // Line number where input field is shown

	// Layout integration (optional)
	layoutManager interface {
		GetRegion(name string) (console.Region, error)
	}

	// Concurrency
	inputChan     chan string
	interruptChan chan struct{}
	queuedInputs  []string
	isProcessing  bool
	mutex         sync.RWMutex

	// Callbacks
	onInput     func(string) error
	onInterrupt func()

	// Context for shutdown
	ctx    context.Context
	cancel context.CancelFunc

	// State tracking
	lastOutputLine int
	running        bool
}

// NewInputManager creates a new concurrent input manager
func NewInputManager(prompt string) *InputManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &InputManager{
		terminalFd:    int(os.Stdin.Fd()),
		prompt:        prompt,
		inputChan:     make(chan string, 10), // Buffer for queued inputs
		interruptChan: make(chan struct{}, 1),
		queuedInputs:  make([]string, 0),
		ctx:           ctx,
		cancel:        cancel,
		mutex:         sync.RWMutex{},
	}
}

// Start begins concurrent input handling
func (im *InputManager) Start() error {
	if !term.IsTerminal(im.terminalFd) {
		return fmt.Errorf("not running in a terminal")
	}

	// Switch to raw mode
	oldState, err := term.MakeRaw(im.terminalFd)
	if err != nil {
		return fmt.Errorf("failed to enter raw mode: %w", err)
	}

	im.oldTermState = oldState
	im.isRawMode = true
	im.running = true

	// Get terminal dimensions
	im.updateTerminalSize()

	// Start input reading goroutine
	go im.inputLoop()

	// Start resize monitoring
	go im.resizeLoop()

	// Show initial input field
	im.showInputField()

	return nil
}

// Stop stops the input manager and restores terminal
func (im *InputManager) Stop() {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	if !im.running {
		return
	}

	im.running = false
	im.cancel()

	if im.isRawMode && im.oldTermState != nil {
		term.Restore(im.terminalFd, im.oldTermState)
		im.isRawMode = false
	}
}

// SetCallbacks sets the callback functions
func (im *InputManager) SetCallbacks(onInput func(string) error, onInterrupt func()) {
	im.onInput = onInput
	im.onInterrupt = onInterrupt
}

// SetLayoutManager sets the layout manager for automatic positioning
func (im *InputManager) SetLayoutManager(layoutManager interface {
	GetRegion(name string) (console.Region, error)
}) {
	im.mutex.Lock()
	defer im.mutex.Unlock()
	im.layoutManager = layoutManager
	im.calculateInputPosition()
}

// calculateInputPosition calculates where the input field should be positioned
func (im *InputManager) calculateInputPosition() {
	if im.layoutManager == nil {
		panic("InputManager requires a layout manager - call SetLayoutManager() first")
	}

	region, err := im.layoutManager.GetRegion("input")
	if err != nil {
		panic(fmt.Sprintf("Failed to get input region from layout manager: %v", err))
	}

	// Convert 0-based region coordinates to 1-based terminal line number
	im.inputFieldLine = region.Y + 1
}

// SetProcessing sets the processing state
func (im *InputManager) SetProcessing(processing bool) {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	im.isProcessing = processing

	if !processing && len(im.queuedInputs) > 0 {
		// Process queued inputs
		go im.processQueuedInputs()
	}
}

// inputLoop runs the main input reading loop
func (im *InputManager) inputLoop() {
	buffer := make([]byte, 1024)

	for {
		select {
		case <-im.ctx.Done():
			return
		default:
			n, err := os.Stdin.Read(buffer)
			if err != nil {
				continue
			}

			im.processKeystrokes(buffer[:n])
		}
	}
}

// processKeystrokes handles raw keyboard input
func (im *InputManager) processKeystrokes(data []byte) {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	if !im.running {
		return
	}

	for i := 0; i < len(data); i++ {
		b := data[i]

		switch b {
		case 13: // Enter key
			im.handleEnter()
		case 3: // Ctrl+C
			im.handleCtrlC()
		case 127, 8: // Backspace/Delete
			im.handleBackspace()
		case 27: // Escape sequence
			if i+2 < len(data) && data[i+1] == '[' {
				// Arrow keys and other escape sequences
				switch data[i+2] {
				case 'A': // Up arrow
					im.handleUpArrow()
				case 'B': // Down arrow
					im.handleDownArrow()
				case 'C': // Right arrow
					im.handleRightArrow()
				case 'D': // Left arrow
					im.handleLeftArrow()
				}
				i += 2 // Skip the escape sequence
			}
		default:
			// Regular character
			if unicode.IsPrint(rune(b)) {
				im.insertChar(rune(b))
			}
		}
	}

	// Redraw input field after changes
	im.showInputField()
}

// handleEnter processes Enter key press
func (im *InputManager) handleEnter() {
	input := strings.TrimSpace(string(im.currentLine))

	if input == "" {
		return
	}

	// Clear current line
	im.currentLine = []rune{}
	im.cursorPos = 0

	// Hide input field temporarily
	im.hideInputField()

	if im.isProcessing {
		// Send input while agent is processing (will be injected into conversation)
		if im.onInput != nil {
			go func() {
				im.onInput(input)
			}()
		}
	} else {
		// Process immediately (start new conversation)
		if im.onInput != nil {
			go func() {
				im.onInput(input)
			}()
		}
	}
}

// handleCtrlC processes Ctrl+C interrupt
func (im *InputManager) handleCtrlC() {
	select {
	case im.interruptChan <- struct{}{}:
		if im.onInterrupt != nil {
			go im.onInterrupt()
		}
	default:
		// Channel full, ignore
	}
}

// handleBackspace removes character before cursor
func (im *InputManager) handleBackspace() {
	if im.cursorPos > 0 {
		im.currentLine = append(im.currentLine[:im.cursorPos-1], im.currentLine[im.cursorPos:]...)
		im.cursorPos--
	}
}

// Arrow key handlers
func (im *InputManager) handleUpArrow()   { /* TODO: History navigation */ }
func (im *InputManager) handleDownArrow() { /* TODO: History navigation */ }
func (im *InputManager) handleLeftArrow() {
	if im.cursorPos > 0 {
		im.cursorPos--
	}
}
func (im *InputManager) handleRightArrow() {
	if im.cursorPos < len(im.currentLine) {
		im.cursorPos++
	}
}

// insertChar inserts a character at cursor position
func (im *InputManager) insertChar(ch rune) {
	if im.cursorPos >= len(im.currentLine) {
		im.currentLine = append(im.currentLine, ch)
	} else {
		im.currentLine = append(im.currentLine[:im.cursorPos], append([]rune{ch}, im.currentLine[im.cursorPos:]...)...)
	}
	im.cursorPos++
}

// showInputField displays the input field above the footer
func (im *InputManager) showInputField() {
	if !im.running {
		return
	}

	// Move to input field position (above footer)
	fmt.Printf("\033[%d;1H", im.inputFieldLine)

	// Clear line and show prompt + input
	fmt.Printf("\r\033[K%s%s", im.prompt, string(im.currentLine))

	// Position cursor correctly
	// Simple prompt "> " is 2 characters
	promptDisplayWidth := len(im.prompt)
	cursorCol := promptDisplayWidth + im.cursorPos + 1
	fmt.Printf("\033[%d;%dH", im.inputFieldLine, cursorCol)
}

// hideInputField clears the input field
func (im *InputManager) hideInputField() {
	if !im.running {
		return
	}

	fmt.Printf("\033[%d;1H\033[K", im.inputFieldLine)
}

// updateTerminalSize gets current terminal dimensions
func (im *InputManager) updateTerminalSize() {
	width, height, err := term.GetSize(im.terminalFd)
	if err == nil {
		im.termWidth = width
		im.termHeight = height
		im.calculateInputPosition()
	}
}

// resizeLoop monitors for terminal resize events
func (im *InputManager) resizeLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	lastWidth, lastHeight := im.termWidth, im.termHeight

	for {
		select {
		case <-im.ctx.Done():
			return
		case <-ticker.C:
			im.updateTerminalSize()
			if im.termWidth != lastWidth || im.termHeight != lastHeight {
				im.calculateInputPosition() // Recalculate position from layout manager
				im.showInputField()         // Redraw at new position
				lastWidth, lastHeight = im.termWidth, im.termHeight
			}
		}
	}
}

// processQueuedInputs processes any queued inputs
func (im *InputManager) processQueuedInputs() {
	im.mutex.Lock()
	inputs := make([]string, len(im.queuedInputs))
	copy(inputs, im.queuedInputs)
	im.queuedInputs = im.queuedInputs[:0] // Clear queue
	im.mutex.Unlock()

	for _, input := range inputs {
		if im.onInput != nil {
			im.onInput(input)
		}
	}
}

// GetInputFieldLine returns the current input field line position (for testing)
func (im *InputManager) GetInputFieldLine() int {
	im.mutex.RLock()
	defer im.mutex.RUnlock()
	return im.inputFieldLine
}

// GetInterruptChannel returns the interrupt channel
func (im *InputManager) GetInterruptChannel() <-chan struct{} {
	return im.interruptChan
}

// ScrollOutput scrolls content up to make room for input field
func (im *InputManager) ScrollOutput() {
	// The scroll region should already be set up by the agent console
	// to account for both input field and footer
	// Just ensure input field is visible
	im.showInputField()
}
