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
	inputFieldLine int // Line number where input field starts
	inputHeight    int // Number of lines currently used by input field

	// Layout integration (optional)
	layoutManager interface {
		GetRegion(name string) (console.Region, error)
	}

	// History integration (optional)
	historyManager interface {
		GetHistory() []string
		AddEntry(string)
	}

	// History navigation state
	historyIndex int    // Current position in history (-1 = not in history mode)
	tempInput    []rune // Temporary storage for current input when browsing history

	// Input routing
	inputRouter *console.InputRouter

	// Concurrency
	inputChan     chan string
	interruptChan chan struct{}
	queuedInputs  []string
	isProcessing  bool
	mutex         sync.RWMutex

	// Callbacks
	onInput        func(string) error
	onInterrupt    func()
	onHeightChange func(int) // Callback when input height changes

	// Context for shutdown
	ctx    context.Context
	cancel context.CancelFunc

	// State tracking
	lastOutputLine int
	running        bool
	paused         bool
	redrawing      bool // Prevent concurrent redraw operations
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
		inputHeight:   1,        // Start with single line
		historyIndex:  -1,       // Not in history mode initially
		tempInput:     []rune{}, // Empty temp input
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

	// Note: Resize monitoring is now handled by AgentConsole.OnResize()
	// to coordinate full-screen redraws including buffer and footer
	// go im.resizeLoop()

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

// SetHeightChangeCallback sets the callback for when input height changes
func (im *InputManager) SetHeightChangeCallback(callback func(int)) {
	im.mutex.Lock()
	defer im.mutex.Unlock()
	im.onHeightChange = callback
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

// SetHistoryManager sets the history manager for arrow key navigation
func (im *InputManager) SetHistoryManager(historyManager interface {
	GetHistory() []string
	AddEntry(string)
}) {
	im.mutex.Lock()
	defer im.mutex.Unlock()
	im.historyManager = historyManager
}

// SetInputRouter sets the input router for event routing
func (im *InputManager) SetInputRouter(router *console.InputRouter) {
	im.mutex.Lock()
	defer im.mutex.Unlock()
	im.inputRouter = router
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

			// Route input through the input router if available
			if im.inputRouter != nil {
				im.inputRouter.SendKeystroke(buffer[:n])
			} else {
				// Fallback to direct processing
				im.processKeystrokes(buffer[:n])
			}
		}
	}
}

// processKeystrokes handles raw keyboard input
func (im *InputManager) processKeystrokes(data []byte) {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	if !im.running || im.paused {
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

	// Clear current line and reset to single line height
	im.currentLine = []rune{}
	im.cursorPos = 0

	// Reset history navigation state
	im.historyIndex = -1
	im.tempInput = []rune{}

	// Reset input height to single line after submission (this handles its own locking)
	im.resetInputHeight()

	// Hide input field temporarily (this will now only clear the single line)
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
		// Reset history navigation when user modifies input
		if im.historyIndex != -1 {
			im.historyIndex = -1
			im.tempInput = []rune{}
		}

		im.currentLine = append(im.currentLine[:im.cursorPos-1], im.currentLine[im.cursorPos:]...)
		im.cursorPos--
	}
}

// Arrow key handlers
func (im *InputManager) handleUpArrow() {
	if im.historyManager == nil {
		return
	}

	history := im.historyManager.GetHistory()
	if len(history) == 0 {
		return
	}

	// If we're not in history mode, save current input and start from most recent
	if im.historyIndex == -1 {
		im.tempInput = make([]rune, len(im.currentLine))
		copy(im.tempInput, im.currentLine)
		im.historyIndex = len(history) - 1
	} else if im.historyIndex > 0 {
		// Move to older entry
		im.historyIndex--
	} else {
		// Already at oldest entry
		return
	}

	// Load history entry
	im.currentLine = []rune(history[im.historyIndex])
	im.cursorPos = len(im.currentLine)

	// Recalculate input height since history entry might be different length
	im.updateInputHeightFromContent()
}

func (im *InputManager) handleDownArrow() {
	if im.historyManager == nil || im.historyIndex == -1 {
		return
	}

	history := im.historyManager.GetHistory()
	if len(history) == 0 {
		return
	}

	if im.historyIndex < len(history)-1 {
		// Move to newer entry
		im.historyIndex++
		im.currentLine = []rune(history[im.historyIndex])
		im.cursorPos = len(im.currentLine)
	} else {
		// Return to original input
		im.historyIndex = -1
		im.currentLine = make([]rune, len(im.tempInput))
		copy(im.currentLine, im.tempInput)
		im.cursorPos = len(im.currentLine)
		im.tempInput = []rune{} // Clear temp storage
	}

	// Recalculate input height
	im.updateInputHeightFromContent()
}

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
	// Reset history navigation when user starts typing
	if im.historyIndex != -1 {
		im.historyIndex = -1
		im.tempInput = []rune{}
	}

	if im.cursorPos >= len(im.currentLine) {
		im.currentLine = append(im.currentLine, ch)
	} else {
		im.currentLine = append(im.currentLine[:im.cursorPos], append([]rune{ch}, im.currentLine[im.cursorPos:]...)...)
	}
	im.cursorPos++
}

// printf is a helper that handles raw mode line endings
func (im *InputManager) printf(format string, args ...interface{}) {
	text := fmt.Sprintf(format, args...)
	if im.isRawMode && strings.Contains(text, "\n") {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	fmt.Print(text)
}

// calculateInputDimensions calculates how many lines the input takes and cursor position
func (im *InputManager) calculateInputDimensions() (lines int, cursorLine int, cursorCol int) {
	if im.termWidth <= 0 {
		im.updateTerminalSize()
	}

	// Calculate effective width (terminal width minus 1 to avoid edge wrapping issues)
	effectiveWidth := im.termWidth - 1
	if effectiveWidth <= 0 {
		effectiveWidth = 80 // Fallback
	}

	// Total text is prompt + input
	promptWidth := len(im.prompt)
	fullText := im.prompt + string(im.currentLine)

	// Calculate total lines needed
	totalChars := len(fullText)
	lines = (totalChars + effectiveWidth - 1) / effectiveWidth // Ceiling division
	if lines == 0 {
		lines = 1
	}

	// Calculate cursor position
	cursorTextPos := promptWidth + im.cursorPos
	cursorLine = cursorTextPos / effectiveWidth
	cursorCol = (cursorTextPos % effectiveWidth) + 1

	return lines, cursorLine, cursorCol
}

// notifyLayoutOfInputHeight tells the layout manager about input height changes
func (im *InputManager) notifyLayoutOfInputHeight(newHeight int) {
	if newHeight != im.inputHeight {
		im.inputHeight = newHeight

		// Notify agent console of height change so it can update the layout
		if im.onHeightChange != nil {
			im.onHeightChange(newHeight)
		}
	}
}

// showInputField displays the input field above the footer with proper multi-line handling
func (im *InputManager) showInputField() {
	if !im.running || im.redrawing {
		return
	}
	im.redrawing = true
	defer func() { im.redrawing = false }()

	// Calculate input dimensions
	lines, cursorLine, cursorCol := im.calculateInputDimensions()

	// Store the previous height before any updates
	previousHeight := im.inputHeight

	// Check if we need more lines than currently allocated
	if lines > im.inputHeight {
		im.notifyLayoutOfInputHeight(lines)
		im.inputHeight = lines
		// Note: This might require layout recalculation, but we'll continue with current positioning for now
	}

	// Clear all lines that might contain input - use the MAXIMUM of previous and current height
	// This ensures we clear artifacts when transitioning from multi-line to single-line
	linesToClear := previousHeight
	if lines > linesToClear {
		linesToClear = lines
	}

	for i := 0; i < linesToClear; i++ {
		im.printf("\033[%d;1H\033[2K", im.inputFieldLine+i)
	}

	// Update height to match actual content needs
	im.inputHeight = lines
	if lines != previousHeight {
		im.notifyLayoutOfInputHeight(lines)
	}

	// Display the input text with proper wrapping
	fullText := im.prompt + string(im.currentLine)

	// Calculate effective width for wrapping
	effectiveWidth := im.termWidth - 1
	if effectiveWidth <= 0 {
		effectiveWidth = 80
	}

	// Split text into lines based on terminal width
	currentLine := 0
	for i := 0; i < len(fullText); i += effectiveWidth {
		end := i + effectiveWidth
		if end > len(fullText) {
			end = len(fullText)
		}

		segment := fullText[i:end]
		// Always position cursor explicitly for each line to avoid artifacts
		im.printf("\033[%d;1H%s", im.inputFieldLine+currentLine, segment)
		currentLine++
	}

	// Position cursor correctly on the right line and column
	actualCursorLine := im.inputFieldLine + cursorLine
	im.printf("\033[%d;%dH", actualCursorLine, cursorCol)
}

// showInputFieldAfterResize handles input field display after terminal resize
// This method clears potential artifacts from the old terminal dimensions
func (im *InputManager) showInputFieldAfterResize(oldWidth, oldHeight int) {
	if !im.running || im.redrawing {
		return
	}
	im.redrawing = true
	defer func() { im.redrawing = false }()

	// Calculate how many lines the same content would have used at the old width
	oldLines := im.calculateLinesForWidth(oldWidth)

	// Calculate how many lines we need at the new width
	newLines, cursorLine, cursorCol := im.calculateInputDimensions()

	// Store previous height
	previousHeight := im.inputHeight

	// Clear more lines to handle resize artifacts - use the maximum of:
	// 1. Previous height (from before resize)
	// 2. Lines that would be used at old width
	// 3. Lines that will be used at new width
	// 4. Add extra buffer lines to catch edge cases
	linesToClear := previousHeight
	if oldLines > linesToClear {
		linesToClear = oldLines
	}
	if newLines > linesToClear {
		linesToClear = newLines
	}

	// Add buffer lines for edge cases (up to 3 extra lines)
	linesToClear += 3

	// Ensure we don't go beyond reasonable bounds (max 20 lines to avoid clearing too much)
	if linesToClear > 20 {
		linesToClear = 20
	}

	// Clear all potential lines that might contain artifacts
	// Use aggressive clearing for resize scenarios to handle horizontal artifacts
	for i := 0; i < linesToClear; i++ {
		// Move to beginning of line and clear entire line (including beyond terminal width)
		im.printf("\033[%d;1H\033[2K", im.inputFieldLine+i)
	}

	// Update height to match new terminal width needs
	im.inputHeight = newLines
	if newLines != previousHeight {
		im.notifyLayoutOfInputHeight(newLines)
	}

	// Display the input text with proper wrapping for new width
	fullText := im.prompt + string(im.currentLine)

	// Calculate effective width for wrapping
	effectiveWidth := im.termWidth - 1
	if effectiveWidth <= 0 {
		effectiveWidth = 80
	}

	// Split text into lines based on NEW terminal width
	currentLine := 0
	for i := 0; i < len(fullText); i += effectiveWidth {
		end := i + effectiveWidth
		if end > len(fullText) {
			end = len(fullText)
		}

		segment := fullText[i:end]
		// Always position cursor explicitly for each line to avoid artifacts
		im.printf("\033[%d;1H%s", im.inputFieldLine+currentLine, segment)
		currentLine++
	}

	// Position cursor correctly on the right line and column
	actualCursorLine := im.inputFieldLine + cursorLine
	im.printf("\033[%d;%dH", actualCursorLine, cursorCol)
}

// calculateLinesForWidth calculates how many lines the current input would use for a given width
func (im *InputManager) calculateLinesForWidth(width int) int {
	if width <= 0 {
		return 1
	}

	// Calculate effective width
	effectiveWidth := width - 1
	if effectiveWidth <= 0 {
		effectiveWidth = 80 // Fallback
	}

	// Total text is prompt + input
	fullText := im.prompt + string(im.currentLine)
	totalChars := len(fullText)

	// Calculate lines needed
	lines := (totalChars + effectiveWidth - 1) / effectiveWidth // Ceiling division
	if lines == 0 {
		lines = 1
	}

	return lines
}

// hideInputField clears the input field (all lines it uses)
func (im *InputManager) hideInputField() {
	if !im.running {
		return
	}

	// Clear all lines used by the input field
	for i := 0; i < im.inputHeight; i++ {
		im.printf("\033[%d;1H\033[2K", im.inputFieldLine+i)
	}
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
				im.calculateInputPosition()                         // Recalculate position from layout manager
				im.showInputFieldAfterResize(lastWidth, lastHeight) // Clear artifacts and redraw
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

// InputHandler interface implementation
func (im *InputManager) GetHandlerID() string {
	return "main_console"
}

// HandleInput processes input events from the router
func (im *InputManager) HandleInput(event console.InputEvent) bool {
	if event.Type == console.KeystrokeEvent {
		if data, ok := event.Data.(console.KeystrokeData); ok {
			im.processKeystrokes(data.Bytes)
			return true
		}
	} else if event.Type == console.InterruptEvent {
		im.handleCtrlC()
		return true
	}
	return false
}

// SetPassthroughMode completely stops/starts input processing for interactive commands
func (im *InputManager) SetPassthroughMode(enabled bool) {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	if enabled {
		if !im.running {
			return // Already stopped
		}

		// Debug output
		if term.IsTerminal(int(os.Stdout.Fd())) {
			fmt.Printf("\r\n🔄 Enabling passthrough mode for interactive command...\r\n")
		}

		// Hide input field
		im.hideInputField()

		// Reset scroll region
		im.printf("\033[r")

		// Stop the input manager completely
		im.running = false
		im.cancel()

		// Restore terminal from raw mode
		if im.isRawMode && im.oldTermState != nil {
			term.Restore(im.terminalFd, im.oldTermState)
			im.isRawMode = false
		}

		if term.IsTerminal(int(os.Stdout.Fd())) {
			fmt.Printf("✅ Passthrough mode enabled - terminal restored to normal mode\r\n")
		}
	} else {
		if im.running {
			return // Already running
		}

		if term.IsTerminal(int(os.Stdout.Fd())) {
			fmt.Printf("\r\n🔄 Disabling passthrough mode - restoring console input...\r\n")
		}

		// Create new context
		ctx, cancel := context.WithCancel(context.Background())
		im.ctx = ctx
		im.cancel = cancel

		// Re-enter raw mode
		oldState, err := term.MakeRaw(im.terminalFd)
		if err != nil {
			if term.IsTerminal(int(os.Stdout.Fd())) {
				fmt.Printf("❌ Failed to re-enter raw mode: %v\r\n", err)
			}
			return
		}

		im.oldTermState = oldState
		im.isRawMode = true
		im.running = true
		im.paused = false

		// Restart input reading goroutine
		go im.inputLoop()

		// Note: Resize monitoring handled by AgentConsole.OnResize()
		// go im.resizeLoop()

		// Get terminal dimensions
		im.updateTerminalSize()

		// Show input field
		im.showInputField()

		if term.IsTerminal(int(os.Stdout.Fd())) {
			fmt.Printf("✅ Console input restored - ready for commands\r\n")
		}
	}
}

// GetInputFieldLine returns the current input field line (for layout restoration)
func (im *InputManager) GetCurrentInputFieldLine() int {
	im.mutex.RLock()
	defer im.mutex.RUnlock()
	return im.inputFieldLine
}

// GetInputHeight returns the current number of lines used by the input field
func (im *InputManager) GetInputHeight() int {
	im.mutex.RLock()
	defer im.mutex.RUnlock()
	return im.inputHeight
}

// UpdateInputHeight forces a recalculation of input dimensions and returns new height
func (im *InputManager) UpdateInputHeight() int {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	lines, _, _ := im.calculateInputDimensions()
	if lines != im.inputHeight {
		im.inputHeight = lines
		return lines
	}
	return im.inputHeight
}

// updateInputHeightFromContent updates height based on current content (called from within mutex)
func (im *InputManager) updateInputHeightFromContent() {
	lines, _, _ := im.calculateInputDimensions()
	if lines != im.inputHeight {
		im.notifyLayoutOfInputHeight(lines)
	}
}

// resetInputHeight resets the input field to single line height and notifies layout
func (im *InputManager) resetInputHeight() {
	// Note: This method is called from within processKeystrokes which already holds the mutex
	// So we don't need additional locking here

	if im.inputHeight > 1 {
		// Clear all lines that were previously used
		for i := 0; i < im.inputHeight; i++ {
			im.printf("\033[%d;1H\033[K", im.inputFieldLine+i)
		}

		// Reset to single line
		im.inputHeight = 1

		// Notify layout of height change (this callback should be thread-safe)
		if im.onHeightChange != nil {
			// Call the callback without holding the mutex to avoid deadlocks
			go func(newHeight int) {
				im.onHeightChange(newHeight)
			}(1)
		}
	}
}
