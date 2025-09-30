package components

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/alantheprice/ledit/pkg/console"
	"golang.org/x/term"
)

// InputManager handles concurrent input with persistent input field
type InputManager struct {
	// Terminal state
	terminalFd                  int
	isRawMode                   bool
	termWidth                   int
	termHeight                  int
	controller                  *console.TerminalController
	rawModeRelease              func()
	wasRunningBeforePassthrough bool

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
	onScrollUp     func(int) // Callback for scroll up
	onScrollDown   func(int) // Callback for scroll down

	// Context for shutdown
	ctx    context.Context
	cancel context.CancelFunc

	// State tracking
	running   bool
	paused    bool
	redrawing bool // Prevent concurrent redraw operations
	// Tracks last drawn input field region
	lastRenderHeight int
	lastRenderY      int

	// Focus provider (returns "input" or "output")
	focusProvider func() string
	onToggleFocus func()

	// Scrolling state provider
	isScrollingProvider func() bool

	// Vim sequence state
	pendingG bool

	// Help overlay toggle (for output focus)
	onToggleHelp func()
}

// NewInputManager creates a new concurrent input manager
func NewInputManager(prompt string) *InputManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &InputManager{
		terminalFd:       int(os.Stdin.Fd()),
		prompt:           prompt,
		inputChan:        make(chan string, 10), // Buffer for queued inputs
		interruptChan:    make(chan struct{}, 1),
		queuedInputs:     make([]string, 0),
		ctx:              ctx,
		cancel:           cancel,
		mutex:            sync.RWMutex{},
		inputHeight:      1, // Start with single line
		lastRenderHeight: 0, // Nothing drawn yet
		lastRenderY:      0,
		historyIndex:     -1,       // Not in history mode initially
		tempInput:        []rune{}, // Empty temp input
	}
}

const rawModeOwnerInputManager = "input-manager"

// SetController wires the terminal controller for coordinated raw-mode management.
func (im *InputManager) SetController(controller *console.TerminalController) {
	im.mutex.Lock()
	im.controller = controller
	if im.running {
		im.ensureRawModeLocked(rawModeOwnerInputManager)
	}
	im.mutex.Unlock()
}

func (im *InputManager) ensureRawModeLocked(reason string) {
	if im.controller == nil || im.rawModeRelease != nil {
		return
	}

	release, err := im.controller.AcquireRawMode(reason)
	if err != nil {
		if console.DebugEnabled() {
			fmt.Fprintf(os.Stderr, "[DEBUG] InputManager failed to acquire raw mode: %v\n", err)
		}
		return
	}

	im.rawModeRelease = release
	im.isRawMode = true
}

func (im *InputManager) releaseRawModeLocked(reason string) {
	if im.rawModeRelease != nil {
		im.rawModeRelease()
		im.rawModeRelease = nil
	}

	if im.controller != nil {
		im.isRawMode = im.controller.IsRawMode()
	} else {
		im.isRawMode = false
	}
}

func (im *InputManager) rawModeActive() bool {
	if im.controller != nil {
		return im.controller.IsRawMode()
	}
	return im.isRawMode
}

// SetFocusProvider sets a callback to get current focus mode ("input" or "output")
func (im *InputManager) SetFocusProvider(provider func() string) {
	im.mutex.Lock()
	defer im.mutex.Unlock()
	im.focusProvider = provider
}

// SetScrollingProvider sets a callback to know if output is currently in scrolling mode
func (im *InputManager) SetScrollingProvider(provider func() bool) {
	im.mutex.Lock()
	defer im.mutex.Unlock()
	im.isScrollingProvider = provider
}

// SetFocusToggle sets a callback to toggle focus manually (e.g., Tab)
func (im *InputManager) SetFocusToggle(cb func()) {
	im.mutex.Lock()
	defer im.mutex.Unlock()
	im.onToggleFocus = cb
}

// SetHelpToggle sets a callback to toggle output help overlay
func (im *InputManager) SetHelpToggle(cb func()) {
	im.mutex.Lock()
	defer im.mutex.Unlock()
	im.onToggleHelp = cb
}

// Start begins concurrent input handling
func (im *InputManager) Start() error {
	if !term.IsTerminal(im.terminalFd) {
		return fmt.Errorf("not running in a terminal")
	}

	im.mutex.Lock()
	im.ensureRawModeLocked(rawModeOwnerInputManager)
	im.running = true
	im.paused = false
	im.mutex.Unlock()

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
	im.releaseRawModeLocked(rawModeOwnerInputManager)
	im.wasRunningBeforePassthrough = false
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

// SetScrollCallbacks sets the scroll up/down callbacks
func (im *InputManager) SetScrollCallbacks(onScrollUp, onScrollDown func(int)) {
	im.mutex.Lock()
	defer im.mutex.Unlock()
	im.onScrollUp = onScrollUp
	im.onScrollDown = onScrollDown
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
		// Default to last line if no layout manager
		if im.termHeight <= 0 {
			im.inputFieldLine = 1
		} else {
			im.inputFieldLine = im.termHeight
		}
		return
	}

	region, err := im.layoutManager.GetRegion("input")
	if err != nil {
		// Fallback: place input at last terminal line when region is not defined
		if im.termHeight <= 0 {
			im.inputFieldLine = 1
		} else {
			im.inputFieldLine = im.termHeight
		}
		return
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
		case 16: // Ctrl+P (history up)
			im.handleUpArrow()
		case 14: // Ctrl+N (history down)
			im.handleDownArrow()
		case 2: // Ctrl+B (vim-style page up - scroll up full page)
			if im.onScrollUp != nil {
				im.onScrollUp(im.termHeight - 1) // Full page scroll up
			}
		case 6: // Ctrl+F (vim-style page down - scroll down full page)
			if im.onScrollDown != nil {
				im.onScrollDown(im.termHeight - 1) // Full page scroll down
			}
		case 9: // Tab - manual focus toggle
			if im.onToggleFocus != nil {
				go im.onToggleFocus()
			}
		case 10: // Ctrl+J (vim-style scroll up - half page)
			if im.onScrollUp != nil {
				im.onScrollUp(im.termHeight / 2) // Half page scroll up
			}
		case 11: // Ctrl+K (vim-style scroll down - half page)
			if im.onScrollDown != nil {
				im.onScrollDown(im.termHeight / 2) // Half page scroll down
			}
		case 127, 8: // Backspace/Delete
			im.handleBackspace()
		case 27: // Escape key or escape sequence
			// Handle CSI sequences (ESC [ ...)
			if i+1 < len(data) && data[i+1] == '[' {
				// CSI sequences (ESC [)
				if i+2 < len(data) {
					switch data[i+2] {
					case 'A': // Up arrow
						if im.focusProvider != nil && im.focusProvider() == "output" {
							if im.onScrollUp != nil {
								im.onScrollUp(1)
							}
						} else {
							im.handleUpArrow()
						}
						i += 2
					case 'B': // Down arrow
						if im.focusProvider != nil && im.focusProvider() == "output" {
							if im.onScrollDown != nil {
								im.onScrollDown(1)
							}
						} else {
							im.handleDownArrow()
						}
						i += 2
					case 'C': // Right arrow
						im.handleRightArrow()
						i += 2
					case 'D': // Left arrow
						im.handleLeftArrow()
						i += 2
					case '5': // Page Up (ESC [ 5 ~)
						if i+3 < len(data) && data[i+3] == '~' {
							if im.onScrollUp != nil {
								im.onScrollUp(10) // Scroll up 10 lines
							}
							i += 3 // Skip the full sequence
						}
					case '6': // Page Down (ESC [ 6 ~)
						if i+3 < len(data) && data[i+3] == '~' {
							if im.onScrollDown != nil {
								im.onScrollDown(10) // Scroll down 10 lines
							}
							i += 3 // Skip the full sequence
						}
					case 'M': // Legacy X10 mouse events (ESC [ M button x y)
						if i+5 < len(data) {
							button := int(data[i+3] - 32)
							im.handleMouseButton(button)
							i += 5
						} else {
							i = len(data) - 1
						}
						continue
					case '<': // SGR mouse events (ESC [ < button;x;y M/m)
						if button, endIdx, ok := im.parseSGRMouse(data, i); ok {
							im.handleMouseButton(button)
							i = endIdx
							continue
						}
						// Fallback: skip to end of sequence if parsing fails
						for j := i + 3; j < len(data); j++ {
							if data[j] == 'M' || data[j] == 'm' {
								i = j
								break
							}
						}
						continue
					default:
						// For any other CSI sequences, skip them
						// Skip at least 2 bytes (ESC [), and assume sequences are up to 10 bytes
						if i+10 < len(data) {
							i += 9 // Skip up to 10 bytes total (ESC + 9 more)
						} else {
							i = len(data) - 1 // Skip rest of buffer
						}
					}
				}
			} else {
				// Plain ESC (not followed by '[') -> treat as interrupt
				im.handleCtrlC()
				// Consume only ESC
			}
			// end ESC handling
		default:
			// Regular character
			if im.focusProvider != nil && im.focusProvider() == "output" {
				// Vim-style navigation in output focus
				switch b {
				case 'j':
					if im.onScrollDown != nil {
						im.onScrollDown(1)
					}
				case 'k':
					if im.onScrollUp != nil {
						im.onScrollUp(1)
					}
				case '?':
					if im.onToggleHelp != nil {
						im.onToggleHelp()
					}
					// do not affect pendingG
				case 'g':
					if im.pendingG {
						if im.onScrollUp != nil {
							im.onScrollUp(im.termHeight * 100)
						}
						im.pendingG = false
					} else {
						im.pendingG = true
					}
				case 'G':
					if im.onScrollDown != nil {
						im.onScrollDown(im.termHeight * 100)
					}
					im.pendingG = false
				default:
					// Ignore others in output focus; reset pendingG
					im.pendingG = false
				}
			} else {
				im.pendingG = false
				if unicode.IsPrint(rune(b)) {
					im.insertChar(rune(b))
				}
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
	if im.rawModeActive() && strings.Contains(text, "\n") {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	fmt.Print(text)
}

// write outputs text directly, applying raw mode line ending normalization
func (im *InputManager) write(text string) {
	if im.rawModeActive() && strings.Contains(text, "\n") {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	fmt.Print(text)
}

// calculateInputDimensions calculates how many lines the input takes and cursor position
func (im *InputManager) calculateInputDimensions() (lines int, cursorLine int, cursorCol int) {
	if im.termWidth <= 0 {
		im.updateTerminalSize()
	}

	// Calculate effective width using the same gutter-aware width as rendering
	effectiveWidth := im.getEffectiveWidth()

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
	// Only show debug messages if LEDIT_DEBUG is set
	if os.Getenv("LEDIT_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[DEBUG] InputManager.showInputField called\n")
	}
	if !im.running || im.redrawing {
		return
	}
	im.redrawing = true
	defer func() { im.redrawing = false }()

	// Calculate input dimensions based on current focus
	lines, cursorLine, cursorCol := im.calculateInputDimensions()
	showingHint := false
	hintText := ""
	if im.focusProvider != nil && im.focusProvider() == "output" {
		// Only show hint when actively scrolling to reduce noise
		scrolling := false
		if im.isScrollingProvider != nil {
			scrolling = im.isScrollingProvider()
		}
		if !scrolling {
			// do not show hint when not scrolling
		} else {
			// When output is focused, show a concise scrolling hint instead of the prompt/input
			// Colors: light grey (info) and muted blue (keys)
			greyColor := "\033[38;2;170;170;170m"
			blueColor := "\033[38;2;110;160;230m"
			resetColor := "\033[0m"
			// Keep it short to avoid wrapping; use simple keys that work across terminals
			// Example: "Scrolling: ↑/↓, PgUp/PgDn, g g top, G bottom, ? help"
			hintText = greyColor + "Scrolling: " + blueColor + "↑/↓" + greyColor + ", " + blueColor + "PgUp/PgDn " + resetColor

			// Force single-line rendering for hint to avoid escape sequence slicing
			lines = 1
			cursorLine = 0
			cursorCol = 1
			showingHint = true
		}
	}

	// If height grew, notify early so layout can expand before we draw
	if lines > im.inputHeight {
		im.notifyLayoutOfInputHeight(lines)
	}

	// Determine left margin based on focus (reserve column 1 for focus bar when input is focused)
	// Reserve 3 columns for gutter (2-bar + padding)

	// First, clear the previously rendered region at its original Y (in case Y changed due to layout)
	if im.lastRenderHeight > 0 && im.lastRenderY > 0 {
		for i := 0; i < im.lastRenderHeight; i++ {
			im.write(console.MoveCursorSeq(1, im.lastRenderY+i) + console.ClearLineSeq())
		}
	}

	// Then clear the current target region that we're about to draw into
	for i := 0; i < lines; i++ {
		im.write(console.MoveCursorSeq(1, im.inputFieldLine+i) + console.ClearLineSeq())
	}

	// Update height to match actual content needs (handle shrink or any change)
	if lines != im.inputHeight {
		im.notifyLayoutOfInputHeight(lines)
	}

	// Display the input text or hint with proper wrapping
	fullText := im.prompt + string(im.currentLine)
	if showingHint {
		fullText = hintText
	}

	// Calculate effective width for wrapping
	effectiveWidth := im.getEffectiveWidth()

	// Render content and place cursor
	im.renderInputContent(fullText, effectiveWidth, cursorLine, cursorCol)

	// Remember how many lines and where we actually drew
	im.lastRenderHeight = lines
	im.lastRenderY = im.inputFieldLine
}

// getEffectiveWidth returns the wrapping width with safe fallbacks
func (im *InputManager) getEffectiveWidth() int {
	// Reserve 2 columns for gutter consistently
	effectiveWidth := im.termWidth - 2
	if effectiveWidth <= 0 {
		effectiveWidth = 80
	}
	return effectiveWidth
}

// renderInputContent writes the input prompt+text wrapped to the terminal and positions cursor
func (im *InputManager) renderInputContent(fullText string, effectiveWidth, cursorLine, cursorCol int) {
	// account for left margin when computing cursor column (gutter=2)
	leftMargin := 3
	continuationPrefix := "> "
	continuationWidth := len(continuationPrefix)

	// Split text into lines based on terminal width
	currentLine := 0
	for i := 0; i < len(fullText); i += effectiveWidth {
		end := i + effectiveWidth
		if end > len(fullText) {
			end = len(fullText)
		}

		segment := fullText[i:end]
		y := im.inputFieldLine + currentLine
		// Draw continuation prompt on wrapped lines for visual alignment
		if currentLine == 0 {
			im.write(console.MoveCursorSeq(leftMargin, y) + segment)
		} else {
			// Write continuation marker then the segment
			im.write(console.MoveCursorSeq(leftMargin, y) + continuationPrefix)
			im.write(console.MoveCursorSeq(leftMargin+continuationWidth, y) + segment)
		}
		currentLine++
	}
	// Position cursor correctly on the right line and column
	actualCursorLine := im.inputFieldLine + cursorLine
	cursorX := leftMargin + cursorCol - 1
	if cursorLine > 0 {
		cursorX += continuationWidth
	}
	im.write(console.MoveCursorSeq(cursorX, actualCursorLine))
}

// calculateLinesForWidth calculates how many lines the current input would use for a given width.
// This helper is retained for tests that verify wrapping behavior.
func (im *InputManager) calculateLinesForWidth(width int) int {
	if width <= 0 {
		return 1
	}

	// Reserve the gutter to match renderInputContent behavior
	effectiveWidth := width - 2
	if effectiveWidth <= 0 {
		effectiveWidth = 80
	}

	fullText := im.prompt + string(im.currentLine)
	if len(fullText) == 0 {
		return 1
	}

	lines := (len(fullText) + effectiveWidth - 1) / effectiveWidth
	if lines < 1 {
		lines = 1
	}
	return lines
}

// hideInputField clears the input field (all lines it uses)
func (im *InputManager) hideInputField() {
	if !im.running {
		return
	}

	// Clear previously rendered region first (in case Y changed)
	if im.lastRenderHeight > 0 && im.lastRenderY > 0 {
		for i := 0; i < im.lastRenderHeight; i++ {
			im.printf("%s", console.MoveCursorSeq(1, im.lastRenderY+i)+console.ClearLineSeq())
		}
	}

	// Also clear current input field region to be safe
	for i := 0; i < im.inputHeight; i++ {
		im.printf("%s", console.MoveCursorSeq(1, im.inputFieldLine+i)+console.ClearLineSeq())
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

func (im *InputManager) parseSGRMouse(data []byte, start int) (button int, end int, ok bool) {
	if start+3 >= len(data) {
		return 0, 0, false
	}

	j := start + 3
	btnStart := j
	for j < len(data) && data[j] != ';' && data[j] != 'M' && data[j] != 'm' {
		j++
	}
	if j >= len(data) || data[j] != ';' {
		return 0, 0, false
	}

	buttonVal, err := strconv.Atoi(string(data[btnStart:j]))
	if err != nil {
		return 0, 0, false
	}

	for j < len(data) && data[j] != 'M' && data[j] != 'm' {
		j++
	}
	if j >= len(data) {
		return 0, 0, false
	}

	return buttonVal, j, true
}

func (im *InputManager) handleMouseButton(button int) {
	im.pendingG = false

	if button&0x40 == 0 {
		return
	}

	switch button & 0x3 {
	case 0:
		im.handleMouseScroll(-1)
	case 1:
		im.handleMouseScroll(1)
	default:
		// Ignore horizontal wheel or release events
	}
}

func (im *InputManager) handleMouseScroll(direction int) {
	if direction == 0 {
		return
	}

	if im.focusProvider == nil || im.focusProvider() != "output" {
		return
	}

	lines := im.mouseScrollLines()
	if lines <= 0 {
		lines = 1
	}

	if direction < 0 {
		if im.onScrollUp != nil {
			im.onScrollUp(lines)
		}
	} else {
		if im.onScrollDown != nil {
			im.onScrollDown(lines)
		}
	}
}

func (im *InputManager) mouseScrollLines() int {
	if im.termHeight <= 0 {
		return 3
	}

	lines := im.termHeight / 6
	if lines < 1 {
		lines = 1
	}
	if lines > 10 {
		lines = 10
	}
	return lines
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

		im.wasRunningBeforePassthrough = true

		// Debug output (stderr only, and only when LEDIT_DEBUG is set)
		if os.Getenv("LEDIT_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "\r\n[DEBUG] Enabling passthrough mode for interactive command...\r\n")
		}

		// Hide input field
		im.hideInputField()

		// Release our raw-mode hold so passthrough commands can manage the terminal
		im.releaseRawModeLocked(rawModeOwnerInputManager)

		// Reset scroll region using the controller when available to ensure a clean primary screen
		if im.controller != nil {
			im.updateTerminalSize()
			if err := im.controller.ResetScrollRegion(); err != nil && console.DebugEnabled() {
				fmt.Fprintf(os.Stderr, "[DEBUG] InputManager failed to reset scroll region: %v\n", err)
			}
			if im.termHeight > 0 {
				if err := im.controller.MoveCursor(1, im.termHeight); err != nil && console.DebugEnabled() {
					fmt.Fprintf(os.Stderr, "[DEBUG] InputManager failed to move cursor during passthrough: %v\n", err)
				}
			}
			im.controller.Flush()
		} else {
			im.printf("\033[r")
		}

		// Stop the input manager completely
		im.running = false
		im.cancel()

		// Note: Terminal state is managed by console app's terminal manager
		// We don't manipulate raw mode independently

		if os.Getenv("LEDIT_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[DEBUG] Passthrough mode enabled - terminal state managed by console app\r\n")
		}
	} else {
		if im.running {
			return // Already running
		}

		if !im.wasRunningBeforePassthrough {
			return
		}

		if os.Getenv("LEDIT_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "\r\n[DEBUG] Disabling passthrough mode - restoring console input...\r\n")
		}

		// Create new context
		ctx, cancel := context.WithCancel(context.Background())
		im.ctx = ctx
		im.cancel = cancel

		im.running = true
		im.paused = false

		// Reacquire raw mode for interactive input
		im.ensureRawModeLocked(rawModeOwnerInputManager)

		// Restart input reading goroutine
		go im.inputLoop()

		im.wasRunningBeforePassthrough = false

		// Note: Resize monitoring handled by AgentConsole.OnResize()
		// go im.resizeLoop()

		// Get terminal dimensions
		im.updateTerminalSize()

		// Show input field
		im.showInputField()

		if os.Getenv("LEDIT_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[DEBUG] Console input restored - ready for commands\r\n")
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

// GetHistoryState returns the current history navigation state
func (im *InputManager) GetHistoryState() (int, string) {
	im.mutex.RLock()
	defer im.mutex.RUnlock()

	tempInput := ""
	if im.tempInput != nil {
		tempInput = string(im.tempInput)
	}

	return im.historyIndex, tempInput
}

// SetHistoryState sets the history navigation state
func (im *InputManager) SetHistoryState(historyIndex int, tempInput string) {
	im.mutex.Lock()
	defer im.mutex.Unlock()

	im.historyIndex = historyIndex
	if tempInput != "" {
		im.tempInput = []rune(tempInput)
	} else {
		im.tempInput = []rune{}
	}
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
			im.write(console.MoveCursorSeq(1, im.inputFieldLine+i) + console.ClearToEndOfLineSeq())
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
