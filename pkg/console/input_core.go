package console

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/term"
)

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// InputEvent represents a key press or input event
type InputEvent struct {
	Type InputEventType
	Data string
}

type InputEventType int

const (
	EventChar InputEventType = iota
	EventUp
	EventDown
	EventLeft
	EventRight
	EventHome
	EventEnd
	EventBackspace
	EventDelete
	EventEnter
	EventTab
	EventInterrupt
	EventSuspend
	EventEscape
	EventPasteStart
	EventPasteEnd
	// Mouse events
	EventMouse
)

// InputReader handles interactive input with proper escape sequence handling
type InputReader struct {
	prompt          string
	line            string
	cursorPos       int
	history         []string
	historyIndex    int
	termFd          int
	oldState        *term.State
	terminalWidth   int
	lastLineLength  int
	lastVisualRows  int
	lastWrapPending bool

	// Edit tracking for history vs text navigation
	hasEditedLine bool

	// Paste detection
	pasteBuffer      strings.Builder
	pasteTimer       *time.Timer
	pasteActive      bool
	lastCharTime     time.Time
	inPasteMode      bool
	pasteStartPrompt string
	bracketedPaste   bool
	bracketedMatch   int
	bracketedSawCR   bool
	collapsedPastes  []pasteSpan

	// Raw binary buffer for image paste detection (accumulated alongside text pasteBuffer)
	rawPasteBuffer []byte

	// Track current physical line (for multi-line wrapped input)
	currentPhysicalLine int

	// Context menu for right-click handling
	contextMenu *ContextMenu

	// Mouse position tracking
	mouseRow int
	mouseCol int

	// SP-048-2a: pluggable completion provider invoked on Tab. Set by the
	// agent shell to wire slash-command completion. nil = Tab is a no-op.
	completer CompletionProvider
	// Active cycle state. Refreshed when Tab is pressed against a buffer
	// that differs from the last applied completion (i.e. the user typed
	// something between Tab presses).
	completionCycle *completionCycle

	// SP-048-4f: tracks the half-typed Ctrl-X prefix of the Ctrl-X Ctrl-E
	// editor-escape sequence. Reset on any keystroke that isn't Ctrl-E.
	pendingCtrlX bool

	// SP-048-4e: reverse search (Ctrl-R) state
	searchMode         bool
	searchQuery        string
	searchResult       string
	searchResultIndex  int
	preSearchLine      string
	preSearchCursorPos int

	// SP-055 follow-up: initial content pre-filled into the buffer
	// by the REPL loop when the steer reader had unsent text at
	// EndTurn. Cleared after first ReadLine consumption.
	initialContent string

	// groundTruth is a snapshot of the terminal's cooked-mode termios
	// captured at REPL startup. Used for pre-flight sanity checks and
	// emergency recovery if a prior mode transition left the terminal
	// stuck in raw/cbreak mode. Set once by SetGroundTruth.
	groundTruth *GroundTruthTermios
}

// CompletionProvider returns candidate completions for the current input
// state. It receives the current line and cursor position and should return
// a list of full-line replacements ordered by likelihood. An empty result
// means "no completion available."
type CompletionProvider func(line string, cursorPos int) []string

// completionCycle tracks an in-progress Tab cycle. When the user presses
// Tab repeatedly without typing other characters, we step through
// candidates in order. When the buffer changes (typing, arrow keys, etc.),
// the next Tab starts a fresh cycle automatically because lastApplied no
// longer matches the current line.
type completionCycle struct {
	candidates  []string // ordered candidate replacements
	index       int      // current candidate
	lastApplied string   // buffer after applying candidates[index]
}

type pasteSpan struct {
	start int
	end   int
}

const (
	// Heuristic paste detection should be conservative to avoid misclassifying
	// normal typing over high-latency links as paste bursts.
	minHeuristicPasteBytes = 12
	bracketedPasteEnable   = "\033[?2004h"
	bracketedPasteDisable  = "\033[?2004l"
	bracketedPasteEndSeq   = "\x1b[201~"
)

// NewInputReader creates a new input reader
func NewInputReader(prompt string) *InputReader {
	ir := &InputReader{
		prompt:          prompt,
		termFd:          int(os.Stdin.Fd()),
		history:         make([]string, 0, 100),
		historyIndex:    -1,
		collapsedPastes: make([]pasteSpan, 0, 8),
		contextMenu:     NewContextMenu(),
	}
	ir.updateTerminalWidth()
	return ir
}

// ReadLine reads a line of input with proper escape sequence handling
func (ir *InputReader) ReadLine() (string, error) {
	// Check if we're in a terminal
	if !term.IsTerminal(ir.termFd) {
		return ir.fallbackReadLine()
	}

	// Pre-flight: unconditionally force a known-good cooked termios
	// before MakeRaw saves the to-be-restored state. ICANON-on alone
	// (what EnsureSane checks) doesn't catch every way input can be
	// broken across turns — VMIN=0 leftover from steer mode, IXOFF
	// stop bit, missing OPOST — and any one of those silently latches
	// for the rest of the session because MakeRaw → defer Restore
	// faithfully restores whatever-broken-state was current at entry.
	// Defensive: also clear any leaked O_NONBLOCK from a prior session.
	if ir.groundTruth != nil {
		ir.groundTruth.EnsureCooked()
	}
	_ = setNonblock(ir.termFd, false)

	// Save terminal state and set raw mode
	oldState, err := term.MakeRaw(ir.termFd)
	if err != nil {
		return ir.fallbackReadLine()
	}
	defer term.Restore(ir.termFd, oldState)
	fmt.Print(bracketedPasteEnable)
	defer fmt.Print(bracketedPasteDisable)

	// Enable mouse tracking (SGR mode for extended coordinates)
	fmt.Print(MouseTrackingSGR)
	defer fmt.Print(MouseTrackingDisable)

	// Initialize line state
	ir.line = ""
	ir.cursorPos = 0
	ir.historyIndex = -1
	ir.hasEditedLine = false
	ir.updateTerminalWidth()
	ir.lastLineLength = 0
	ir.lastVisualRows = 1
	ir.lastWrapPending = false
	ir.currentPhysicalLine = 0
	ir.pasteBuffer.Reset()
	ir.pasteActive = false
	ir.inPasteMode = false
	ir.bracketedPaste = false
	ir.bracketedMatch = 0
	ir.bracketedSawCR = false
	ir.collapsedPastes = ir.collapsedPastes[:0]
	ir.rawPasteBuffer = nil
	ir.lastCharTime = time.Now()
	// SP-048-4e: reset search state
	ir.searchMode = false
	ir.searchQuery = ""
	ir.searchResult = ""
	ir.searchResultIndex = -1
	ir.preSearchLine = ""
	ir.preSearchCursorPos = 0
	// SP-048 follow-up: defensively clear the current line before printing
	// the prompt. Otherwise, if the cursor was left mid-line by prior
	// output (e.g. partial content under the status footer's scroll region
	// after a redraw), the prompt would render *on top of* that content
	// and produce the prompt-overlap-on-startup bug we caught in real use.
	fmt.Printf("\r\033[K%s", ir.prompt)

	// SP-055 follow-up: if the REPL carried over unsent steer text,
	// pre-fill it into the line buffer and render it so the user can
	// pick up where they left off. Consumed once then cleared.
	if ir.initialContent != "" {
		ir.line = ir.initialContent
		ir.cursorPos = len(ir.initialContent)
		ir.initialContent = ""
		ir.hasEditedLine = true
		ir.Refresh()
	}

	parser := NewEscapeParser()
	buf := make([]byte, 32)
	var resizeCh chan os.Signal
	if sig := resizeSignal(); sig != nil {
		resizeCh = make(chan os.Signal, 1)
		signal.Notify(resizeCh, sig)
		defer signal.Stop(resizeCh)
	}

	// Set stdin to non-blocking for paste detection
	nonBlocking := true
	if err := setNonblock(ir.termFd, true); err != nil {
		// Some terminals/PTYs reject non-blocking mode. Keep raw mode enabled and
		// continue with blocking reads so arrow keys/history still work.
		nonBlocking = false
	}
	if nonBlocking {
		defer func() {
			_ = setNonblock(ir.termFd, false)
		}()
	}

	for {
		if ir.processPendingResize(resizeCh, parser) {
			continue
		}

		n, err := os.Stdin.Read(buf)

		// Handle non-blocking read errors
		if err != nil {
			errStr := err.Error()
			// Check if it's just "no data available" (EAGAIN/EWOULDBLOCK)
			// Common error messages: "no data", "resource temporarily unavailable", "EAGAIN"
			isNoData := strings.Contains(errStr, "no data") ||
				strings.Contains(errStr, "temporarily unavailable") ||
				errStr == "EAGAIN" ||
				errStr == "EWOULDBLOCK"
			isInterrupted := strings.Contains(errStr, "interrupted system call") ||
				errStr == "EINTR"

			if nonBlocking && isNoData {
				if ir.processPendingResize(resizeCh, parser) {
					continue
				}
				// Check if paste timer should fire
				if ir.pasteActive && time.Since(ir.lastCharTime) > 100*time.Millisecond {
					// Paste detected - process it
					if ir.finalizePaste() {
						// Paste was finalized, continue reading
						continue
					}
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			if isInterrupted && ir.processPendingResize(resizeCh, parser) {
				continue
			}
			// Real error - return it wrapped with context
			return "", fmt.Errorf("stdin read error: %w", err)
		}

		if n == 0 {
			// In non-blocking mode, this means no data
			if nonBlocking {
				time.Sleep(10 * time.Millisecond)
			}
			continue
		}

		// Process each byte through the escape parser
		for i := 0; i < n; i++ {
			b := buf[i]
			now := time.Now()

			if ir.bracketedPaste {
				if ir.consumeBracketedPasteByte(b) {
					ir.finalizePaste()
				}
				ir.lastCharTime = now
				continue
			}

			// Detect paste: rapid character input
			timeSinceLastChar := now.Sub(ir.lastCharTime)

			// Handle Ctrl+C and Ctrl+Z directly before parsing
			if b == 3 { // Ctrl+C
				fmt.Printf("\r%s", ClearToEndOfLineSeq()) // Clear line
				fmt.Println("^C")
				return "", fmt.Errorf("interrupted")
			}

			// SP-048-4f: Ctrl-X Ctrl-E opens $EDITOR with the current buffer
			// pre-filled. Two-keystroke sequence: first byte 24 (Ctrl-X)
			// arms pendingCtrlX; the next byte either triggers the editor
			// escape (Ctrl-E = 5) or aborts the sequence and falls through
			// to normal processing.
			if ir.pendingCtrlX {
				ir.pendingCtrlX = false
				if b == 5 { // Ctrl-E
					if newState := ir.runExternalEditor(oldState, nonBlocking); newState != nil {
						oldState = newState
					}
					ir.Refresh()
					continue
				}
				// Not Ctrl-E — fall through and process `b` normally below.
			}
			if b == 24 { // Ctrl-X (start of Ctrl-X Ctrl-E sequence)
				ir.pendingCtrlX = true
				continue
			}

			if b == 26 { // Ctrl+Z
				// Re-enter cooked mode before suspension so the shell
				// state is clean while the user is away.
				term.Restore(ir.termFd, oldState)
				suspendTerminal()

				// Execution resumes here after SIGCONT (e.g. "fg").
				ignoreTerminalSignals()

				// Drain any bytes that arrived while suspended (e.g.
				// keystrokes, newline from "fg" command).  Only possible
				// when the fd is in non-blocking mode; otherwise there is
				// no safe way to poll without blocking indefinitely.
				if nonBlocking {
					time.Sleep(50 * time.Millisecond)
					discardBuf := make([]byte, 256)
					for {
						n, _ := os.Stdin.Read(discardBuf)
						if n <= 0 {
							break
						}
					}
				}

				// Re-enter raw mode.
				if newState, err := term.MakeRaw(ir.termFd); err == nil {
					oldState = newState
				}

				// Re-enable bracketed paste mode (lost when we exited raw mode).
				fmt.Print(bracketedPasteEnable)

				resetTerminalSignals()

				// Clear the current line and redisplay the prompt.
				fmt.Printf("\r%s%s", ClearLineSeq(), ir.prompt)
				ir.line = ""
				ir.cursorPos = 0
				continue
			}

			// SP-048-4e: Ctrl-R reverse search
			if b == 0x12 { // Ctrl-R
				if !ir.searchMode {
					ir.enterSearchMode()
				} else {
					ir.cycleSearchResult()
				}
				ir.renderSearchPrompt()
				continue
			}

			// SP-048-4e: Search mode byte handler
			if ir.searchMode {
				if b == 13 || b == 10 { // Enter — accept match
					ir.exitSearchMode(true)
					ir.Refresh()
					fmt.Println()
					input := ir.line
					if input != "" {
						ir.AddToHistory(input)
					}
					return input, nil
				} else if b == 27 { // Escape — cancel
					ir.exitSearchMode(false)
					ir.Refresh()
					continue
				} else if b == 3 { // Ctrl-C — cancel, return error
					ir.exitSearchMode(false)
					ir.Refresh()
					fmt.Printf("%s", ClearToEndOfLineSeq())
					fmt.Println("^C")
					return "", fmt.Errorf("interrupted")
				} else {
					ir.handleSearchByte(b)
					ir.renderSearchPrompt()
					continue
				}
			}

			// Check for escape sequences BEFORE paste detection
			// Arrow keys send escape sequences which look like rapid input
			isEscapeSeq := (b == 27) || (parser.state > 0)

			// Start paste mode only when input looks strongly like a paste burst.
			// This avoids false positives on remote/high-latency links where
			// regular keypresses may be delivered in small batches.
			if !ir.inPasteMode && !isEscapeSeq && i == 0 && shouldStartHeuristicPaste(buf[:n], timeSinceLastChar) {
				ir.inPasteMode = true
				ir.pasteActive = true
				ir.pasteStartPrompt = ir.prompt
				ir.pasteBuffer.Reset()
				ir.pasteBuffer.WriteByte(b)
				ir.lastCharTime = now
				continue
			}

			// Collect paste content
			if ir.inPasteMode {
				// Exit paste mode for control characters that indicate user intent
				if b == 27 || b == 8 || b == 127 { // ESC, Backspace, Delete
					ir.inPasteMode = false
					ir.pasteActive = false
					// For ESC, let escape parser handle it
					// For Backspace/Delete, handle them normally
					if b != 27 {
						continue
					}
				} else if timeSinceLastChar > 100*time.Millisecond || (b == 13 && ir.pasteBuffer.Len() > 0) {
					// Check if paste is ending (slow input or Enter at end)
					// Finalize paste on Enter or timeout
					if b != 13 {
						ir.pasteBuffer.WriteByte(b)
					}
					if ir.finalizePaste() {
						// Continue after paste
						continue
					}
				} else {
					// Convert \r to \n for proper multiline handling
					if b == 13 {
						ir.pasteBuffer.WriteRune('\n')
					} else if b >= 32 {
						ir.pasteBuffer.WriteByte(b)
					} else if b == 9 { // Tab
						ir.pasteBuffer.WriteByte('\t')
					}
					ir.lastCharTime = now
					continue
				}
			}

			ir.lastCharTime = now

			// Parse the byte through the escape parser
			event := parser.Parse(b)
			if event != nil {
				if event.Type == EventPasteStart {
					ir.bracketedPaste = true
					ir.bracketedMatch = 0
					ir.bracketedSawCR = false
					ir.inPasteMode = true
					ir.pasteActive = true
					ir.pasteBuffer.Reset()
					ir.rawPasteBuffer = nil
					continue
				}
				if event.Type == EventPasteEnd {
					ir.bracketedPaste = false
					ir.bracketedMatch = 0
					ir.bracketedSawCR = false
					ir.finalizePaste()
					continue
				}
				if event.Type == EventMouse {
					// Handle mouse event
					ir.handleMouseEvent(event.Data)
					continue
				}
				if event.Type == EventEnter {
					// End of input
					fmt.Println() // Move to next line
					input := ir.line
					if input != "" {
						ir.AddToHistory(input)
					}
					return input, nil
				}
				ir.HandleEvent(event)
			}
			for parser.hasPending {
				pending := parser.Parse(0)
				if pending == nil {
					break
				}
				ir.HandleEvent(pending)
			}
		}
	}
}

// fallbackReadLine provides simple input for non-terminal environments
func (ir *InputReader) fallbackReadLine() (string, error) {
	fmt.Print(ir.prompt)
	var input string
	_, err := fmt.Scanln(&input)
	return input, fmt.Errorf("failed to read fallback input: %w", err)
}

// HandleEvent processes an input event
func (ir *InputReader) HandleEvent(event *InputEvent) {
	switch event.Type {
	case EventChar:
		ir.InsertChar(event.Data)
	case EventBackspace:
		ir.Backspace()
	case EventDelete:
		ir.Delete()
	case EventLeft:
		ir.MoveCursor(-1)
	case EventRight:
		ir.MoveCursor(1)
	case EventHome:
		ir.SetCursor(0)
	case EventEnd:
		ir.SetCursor(len(ir.line))
	case EventUp:
		// If context menu is visible, navigate it
		if ir.contextMenu != nil && ir.contextMenu.Visible {
			ir.contextMenu.NavigateUp()
			ir.contextMenu.Render()
		} else {
			ir.NavigateVertically(-1)
		}
	case EventDown:
		// If context menu is visible, navigate it
		if ir.contextMenu != nil && ir.contextMenu.Visible {
			ir.contextMenu.NavigateDown()
			ir.contextMenu.Render()
		} else {
			ir.NavigateVertically(1)
		}
	case EventTab:
		// Context menu takes precedence — Tab closes it like Escape.
		if ir.contextMenu != nil && ir.contextMenu.Visible {
			ir.contextMenu.Hide()
			if ir.contextMenu.OnEscape != nil {
				ir.contextMenu.OnEscape()
			}
			return
		}
		// SP-048-2a: slash command completion.
		ir.handleTabCompletion()
	case EventEscape:
		if ir.contextMenu != nil && ir.contextMenu.Visible {
			ir.contextMenu.Hide()
			if ir.contextMenu.OnEscape != nil {
				ir.contextMenu.OnEscape()
			}
		}
	case EventEnter:
		// If context menu is visible, select current item
		if ir.contextMenu != nil && ir.contextMenu.Visible {
			item := ir.contextMenu.SelectCurrent()
			if item != nil {
				ir.contextMenu.Hide()
			}
			return
		}
		// Normal enter handling will occur after this function
	default:
		// Ignore other events
	}
}

// SetCompleter installs a completion provider that is invoked when the user
// presses Tab. Pass nil to disable completion. The provider receives the
// current buffer + cursor position and returns ordered candidate replacements.
func (ir *InputReader) SetCompleter(c CompletionProvider) {
	ir.completer = c
}

// SetPrompt updates the input reader's prompt prefix. Call this between
// ReadLine invocations (not during) to reflect state changes such as the
// active model name after a `/model` switch. SP-048-5d follow-up.
func (ir *InputReader) SetPrompt(p string) {
	ir.prompt = p
}

// SetInitialContent pre-fills the input buffer with text that should
// appear as if the user typed it. Used by the REPL loop to carry over
// unsent steer-panel text into the main prompt after a turn ends.
// The content is consumed on the next ReadLine call and then cleared.
func (ir *InputReader) SetInitialContent(content string) {
	ir.initialContent = content
}

// SetGroundTruth installs the terminal's pristine cooked-mode termios
// snapshot for pre-flight sanity checks. Call once at REPL startup,
// before the first ReadLine.
func (ir *InputReader) SetGroundTruth(gt *GroundTruthTermios) {
	ir.groundTruth = gt
}

// handleTabCompletion either starts a new completion cycle or advances the
// existing one. A cycle is "live" as long as the buffer still matches the
// last completion we applied; any other edit (typing, arrow keys) leaves
// the buffer different and the next Tab starts fresh.
func (ir *InputReader) handleTabCompletion() {
	if ir.completer == nil {
		return
	}

	// Continue an existing cycle if the buffer is exactly what we last set.
	if ir.completionCycle != nil && ir.line == ir.completionCycle.lastApplied {
		c := ir.completionCycle
		c.index = (c.index + 1) % len(c.candidates)
		ir.applyCompletion(c.candidates[c.index])
		return
	}

	// Start a fresh cycle from the current buffer.
	candidates := ir.completer(ir.line, ir.cursorPos)
	if len(candidates) == 0 {
		// No matches — clear any stale cycle and silently do nothing.
		ir.completionCycle = nil
		return
	}

	ir.completionCycle = &completionCycle{
		candidates: candidates,
		index:      0,
	}
	ir.applyCompletion(candidates[0])
}

// applyCompletion replaces the buffer with the given completion, moves the
// cursor to the end, refreshes the rendering, and records the applied value
// in the active cycle so subsequent Tabs can detect that we're still cycling.
func (ir *InputReader) applyCompletion(s string) {
	ir.line = s
	ir.cursorPos = len(s)
	ir.hasEditedLine = true
	ir.historyIndex = -1
	if ir.completionCycle != nil {
		ir.completionCycle.lastApplied = s
	}
	ir.Refresh()
}

// InsertChar inserts a character at the cursor position
func (ir *InputReader) InsertChar(char string) {
	ir.expandPasteAtCursor()

	// Mark line as edited and disconnect from history
	ir.hasEditedLine = true
	ir.historyIndex = -1

	insertAt := ir.cursorPos
	before := ir.line[:ir.cursorPos]
	after := ir.line[ir.cursorPos:]
	ir.line = before + char + after
	ir.cursorPos += len(char)
	ir.shiftPasteSpans(insertAt, len(char))

	// For typing at end of line, just output the character (more efficient)
	if ir.cursorPos == len(ir.line) && len(ir.collapsedPastes) == 0 {
		fmt.Printf("%s", char)
		// Keep refresh bookkeeping in sync even on fast-path writes.
		promptWidth := visibleRuneWidth(ir.prompt)
		lineWidth := len([]rune(ir.line))
		totalWidth := promptWidth + lineWidth
		ir.lastLineLength = totalWidth
		cursorPos := promptWidth + ir.cursorPos
		ir.currentPhysicalLine = cursorLineIndex(ir.terminalWidth, cursorPos)
		ir.lastWrapPending = isWrapPending(ir.terminalWidth, totalWidth, cursorPos, totalWidth)
	} else {
		// Inserting in middle requires full refresh
		ir.Refresh()
	}
}

// Backspace deletes the character before the cursor
func (ir *InputReader) Backspace() {
	if ir.cursorPos > 0 {
		if ir.deleteCollapsedPasteEndingAtCursor() {
			ir.Refresh()
			return
		}
		ir.expandPasteAtCursor()

		// Mark line as edited and disconnect from history
		ir.hasEditedLine = true
		ir.historyIndex = -1

		deletePos := ir.cursorPos - 1
		before := ir.line[:deletePos]
		after := ir.line[ir.cursorPos:]
		ir.line = before + after
		ir.cursorPos--
		ir.shiftPasteSpans(deletePos+1, -1)
		ir.Refresh()
	}
}

// Delete deletes the character at the cursor position
func (ir *InputReader) Delete() {
	if ir.cursorPos < len(ir.line) {
		if ir.deleteCollapsedPasteStartingAtCursor() {
			ir.Refresh()
			return
		}
		ir.expandPasteAtCursor()

		// Mark line as edited and disconnect from history
		ir.hasEditedLine = true
		ir.historyIndex = -1

		before := ir.line[:ir.cursorPos]
		after := ir.line[ir.cursorPos+1:]
		ir.line = before + after
		ir.shiftPasteSpans(ir.cursorPos+1, -1)
		ir.Refresh()
	}
}

// MoveCursor moves the cursor left or right
func (ir *InputReader) MoveCursor(delta int) {
	newPos := ir.cursorPos + delta
	if newPos >= 0 && newPos <= len(ir.line) {
		ir.cursorPos = newPos
		ir.expandPasteAtCursor()
		ir.Refresh()
	}
}

// SetCursor sets the cursor to an absolute position
func (ir *InputReader) SetCursor(pos int) {
	if pos >= 0 && pos <= len(ir.line) {
		ir.cursorPos = pos
		ir.expandPasteAtCursor()
		ir.Refresh()
	}
}

// detectCodePattern checks if the pasted content looks like code
func (ir *InputReader) detectCodePattern(content string) bool {
	// Check for common code patterns
	codeIndicators := []string{
		"function ", "def ", "class ", "func ", "var ", "let ", "const ",
		"if ", "for ", "while ", "return ", "import ", "from ", "export ",
		"//", "/*", "*/", "#", "<!--",
		"package ", "struct ", "type ", "interface ",
	}

	hasSpace := strings.Contains(content, " ")
	braceCount := strings.Count(content, "{") + strings.Count(content, "}")
	parenCount := strings.Count(content, "(") + strings.Count(content, ")")
	bracketCount := strings.Count(content, "[") + strings.Count(content, "]")

	// Check for code indicators
	isCode := false
	for _, indicator := range codeIndicators {
		if strings.Contains(content, indicator) {
			isCode = true
			break
		}
	}

	// Also check for multiple pairs of brackets (common in code)
	totalBrackets := braceCount + parenCount + bracketCount
	if totalBrackets >= 4 && hasSpace {
		return true
	}

	return isCode
}

// EscapeParser handles escape sequences using a simple state machine
type EscapeParser struct {
	state       int
	buffer      []byte
	pendingChar byte   // Stores a character that should be processed next
	hasPending  bool   // Whether there's a pending character
	mouseBuf    []byte // Buffer for mouse event data
}

// NewEscapeParser creates a new escape sequence parser
func NewEscapeParser() *EscapeParser {
	return &EscapeParser{
		state:  0,
		buffer: make([]byte, 0, 10),
	}
}

// Parse processes a byte and returns an event if complete
func (ep *EscapeParser) Parse(b byte) *InputEvent {
	// If we have a pending character, return it first
	if ep.hasPending {
		ep.hasPending = false
		return &InputEvent{Type: EventChar, Data: string([]byte{ep.pendingChar})}
	}

	switch ep.state {
	case 0: // Waiting for ESC or regular char
		if b == 27 {
			ep.state = 1
			return nil
		}
		// Handle control characters
		switch b {
		case 8, 127:
			return &InputEvent{Type: EventBackspace}
		case 13:
			return &InputEvent{Type: EventEnter}
		case 9:
			return &InputEvent{Type: EventTab}
		default:
			// Return regular printable characters as character events
			if b >= 32 && b <= 126 {
				return &InputEvent{Type: EventChar, Data: string([]byte{b})}
			}
			return nil
		}

	case 1: // Got ESC, expecting '[' or other sequence
		ep.buffer = append(ep.buffer, b)
		if b == '[' {
			ep.state = 2
			return nil
		}
		// Handle other ESC sequences (like ESC O for function keys)
		if b == 'O' {
			ep.state = 4
			return nil
		}
		// Alt+Enter (ESC + CR/LF): insert a literal newline into the
		// buffer instead of submitting. Parity with the steer panel —
		// plain Enter still submits, but Alt+Enter lets the user
		// compose multi-line prompts. Most terminals translate the
		// Alt modifier to a leading ESC byte; iTerm2 needs "Option
		// acts as Meta" enabled for this to fire.
		if b == 13 || b == 10 {
			ep.Reset()
			return &InputEvent{Type: EventChar, Data: "\n"}
		}
		// Not a CSI sequence, treat ESC as escape event
		// This character could be printable, save it for next call
		ep.Reset()
		if b >= 32 && b <= 126 {
			ep.pendingChar = b
			ep.hasPending = true
		}
		return &InputEvent{Type: EventEscape}

	case 2: // Got '[', reading sequence
		ep.buffer = append(ep.buffer, b)

		// Check for completed sequences - only look at the last character for simple cases
		switch b {
		case 'A': // Up arrow
			event := &InputEvent{Type: EventUp}
			ep.Reset()
			return event
		case 'B': // Down arrow
			event := &InputEvent{Type: EventDown}
			ep.Reset()
			return event
		case 'C': // Right arrow
			event := &InputEvent{Type: EventRight}
			ep.Reset()
			return event
		case 'D': // Left arrow
			event := &InputEvent{Type: EventLeft}
			ep.Reset()
			return event
		case 'H': // Home
			event := &InputEvent{Type: EventHome}
			ep.Reset()
			return event
		case 'F': // End
			event := &InputEvent{Type: EventEnd}
			ep.Reset()
			return event
		case '<': // Mouse event (SGR mode): ESC [ < Cb;Cx;Cy M
			ep.state = 5
			ep.mouseBuf = []byte{27, '[', '<'}
			return nil
		case 'M': // Mouse event (X10 mode): ESC [ M Cb Cx Cy
			ep.state = 6
			ep.mouseBuf = []byte{27, '[', 'M'}
			return nil
		default:
			// Handle numeric CSI params and terminated forms like ESC [ 3 ~ and ESC [ 200 ~.
			if (b >= '0' && b <= '9') || b == ';' {
				return nil
			}
			if b == '~' {
				param := ""
				if len(ep.buffer) >= 3 {
					param = string(ep.buffer[1 : len(ep.buffer)-1])
				}
				firstParam := param
				if idx := strings.IndexByte(param, ';'); idx >= 0 {
					firstParam = param[:idx]
				}
				ep.Reset()
				switch firstParam {
				case "1", "7":
					return &InputEvent{Type: EventHome}
				case "4", "8":
					return &InputEvent{Type: EventEnd}
				case "3":
					return &InputEvent{Type: EventDelete}
				case "200":
					return &InputEvent{Type: EventPasteStart}
				case "201":
					return &InputEvent{Type: EventPasteEnd}
				default:
					return &InputEvent{Type: EventEscape}
				}
			}
			// Unknown sequence - treat as standalone ESC
			// This character could be printable, save it for next call
			ep.Reset()
			if b >= 32 && b <= 126 {
				ep.pendingChar = b
				ep.hasPending = true
			}
			return &InputEvent{Type: EventEscape}
		}

	case 3: // Expecting "~" for Delete
		ep.buffer = append(ep.buffer, b)
		if b == '~' {
			event := &InputEvent{Type: EventDelete}
			ep.Reset()
			return event
		}
		// Not Delete, the 'b' could be a printable character
		ep.Reset()
		if b >= 32 && b <= 126 {
			ep.pendingChar = b
			ep.hasPending = true
		}
		return &InputEvent{Type: EventEscape}

	case 4: // ESC O sequences (function keys)
		ep.buffer = append(ep.buffer, b)
		switch b {
		case 'H': // Home
			event := &InputEvent{Type: EventHome}
			ep.Reset()
			return event
		case 'F': // End
			event := &InputEvent{Type: EventEnd}
			ep.Reset()
			return event
		default:
			// Unknown sequence, this character could be printable
			ep.Reset()
			if b >= 32 && b <= 126 {
				ep.pendingChar = b
				ep.hasPending = true
			}
			return &InputEvent{Type: EventEscape}
		}

	case 5: // Mouse event tracking (SGR mode: ESC [ < Cb;Cx;Cy M)
		ep.mouseBuf = append(ep.mouseBuf, b)
		if b == 'M' {
			// Complete mouse event
			mouseData := string(ep.mouseBuf)
			ep.Reset()
			ep.mouseBuf = nil
			return &InputEvent{Type: EventMouse, Data: mouseData}
		}
		if b == 'm' {
			// Complete mouse event (lowercase variant)
			mouseData := string(ep.mouseBuf)
			ep.Reset()
			ep.mouseBuf = nil
			return &InputEvent{Type: EventMouse, Data: mouseData}
		}
		return nil

	case 6: // Mouse event tracking (X10 mode: ESC [ M Cb Cx Cy)
		ep.mouseBuf = append(ep.mouseBuf, b)
		if len(ep.mouseBuf) == 4 {
			// Complete X10 mouse event: ESC [ M Cb Cx Cy
			mouseData := string(ep.mouseBuf)
			ep.Reset()
			ep.mouseBuf = nil
			return &InputEvent{Type: EventMouse, Data: mouseData}
		}
		return nil
	}

	return nil
}

// Reset the parser state
func (ep *EscapeParser) Reset() {
	ep.state = 0
	ep.buffer = ep.buffer[:0]
	ep.mouseBuf = nil
}

// ─── SP-048-4e: Ctrl-R reverse search methods ───────────────────────────────

// enterSearchMode starts a new reverse-history search, saving the current
// line/cursor so they can be restored on cancellation.
func (ir *InputReader) enterSearchMode() {
	ir.searchMode = true
	ir.searchQuery = ""
	ir.searchResult = ""
	ir.searchResultIndex = -1
	ir.preSearchLine = ir.line
	ir.preSearchCursorPos = ir.cursorPos
	// Show most recent history entry for empty query.
	if len(ir.history) > 0 {
		ir.searchResult = ir.history[len(ir.history)-1]
		ir.searchResultIndex = len(ir.history) - 1
	}
}

// exitSearchMode leaves reverse-search mode.  When accept is true the
// current searchResult is loaded into the line buffer; otherwise the
// pre-search state is restored.
func (ir *InputReader) exitSearchMode(accept bool) {
	if accept && ir.searchResult != "" {
		ir.line = ir.searchResult
		ir.cursorPos = len(ir.searchResult)
		ir.hasEditedLine = true
		ir.historyIndex = -1
	} else {
		ir.line = ir.preSearchLine
		ir.cursorPos = ir.preSearchCursorPos
	}
	ir.searchMode = false
	ir.searchQuery = ""
	ir.searchResult = ""
	ir.searchResultIndex = -1
	ir.preSearchLine = ""
	ir.preSearchCursorPos = 0
}

// handleSearchByte processes a single byte while in search mode.
func (ir *InputReader) handleSearchByte(b byte) {
	// TODO(SP-048-4e): This function processes typed input one byte at a time.
	// For ASCII this works fine, but multi-byte UTF-8 sequences (e.g. accented
	// characters, CJK) will be split across multiple handleSearchByte calls.
	// Each byte ≥ 128 is converted via string(rune(b)) which produces an
	// incorrect code point — e.g. 0xC3 becomes 'Ã' (U+00C3) instead of being
	// part of a multi-byte sequence like 'é'. Properly fixing this requires
	// buffering bytes until a complete UTF-8 rune is available via utf8.DecodeRune.
	switch {
	case b == 127 || b == 8: // Backspace
		if len(ir.searchQuery) > 0 {
			ir.searchQuery = ir.searchQuery[:len(ir.searchQuery)-1]
		}
		// Re-search from the end of history (find newest match).
		if ir.searchQuery == "" {
			// Empty query: show most recent entry again.
			if len(ir.history) > 0 {
				ir.searchResult = ir.history[len(ir.history)-1]
				ir.searchResultIndex = len(ir.history) - 1
			} else {
				ir.searchResult = ""
				ir.searchResultIndex = -1
			}
		} else {
			result, idx, ok := ir.searchHistory(ir.searchQuery, len(ir.history)-1)
			if ok {
				ir.searchResult = result
				ir.searchResultIndex = idx
			} else {
				ir.searchResult = ""
				ir.searchResultIndex = -1
			}
		}
	case b >= 32: // Printable character
		ir.searchQuery += string(rune(b))
		// Search from the end of history (newest first).
		result, idx, ok := ir.searchHistory(ir.searchQuery, len(ir.history)-1)
		if ok {
			ir.searchResult = result
			ir.searchResultIndex = idx
		} else {
			ir.searchResult = ""
			ir.searchResultIndex = -1
		}
	// Everything else: ignore
	}
}

// cycleSearchResult searches for the next older match with the current query.
//
// When searchResultIndex is -1 (no previous match found), searchHistory
// normalizes the negative startIndex (searchResultIndex-1 = -2) back to
// len(history)-1, effectively wrapping around to search from the end again.
// In practice this means cycling when already at "no match" is a no-op,
// which is the desired behavior.
func (ir *InputReader) cycleSearchResult() {
	if ir.searchQuery == "" {
		// No query: cycle through history in order.
		if len(ir.history) > 0 {
			idx := ir.searchResultIndex - 1
			if idx < 0 {
				idx = len(ir.history) - 1
			}
			ir.searchResult = ir.history[idx]
			ir.searchResultIndex = idx
		}
		return
	}
	result, idx, ok := ir.searchHistory(ir.searchQuery, ir.searchResultIndex-1)
	if ok {
		ir.searchResult = result
		ir.searchResultIndex = idx
	}
	// If not found: keep current result (user can keep pressing Ctrl-R but
	// it just stays on the current match).
}

// searchHistory searches ir.history backwards from startIndex for a case-insensitive
// substring match of query.  Returns the matching entry, its index, and whether
// a match was found.
func (ir *InputReader) searchHistory(query string, startIndex int) (string, int, bool) {
	if startIndex < 0 {
		startIndex = len(ir.history) - 1
	}
	if len(ir.history) == 0 {
		return "", -1, false
	}
	queryLower := strings.ToLower(query)
	for i := startIndex; i >= 0; i-- {
		if strings.Contains(strings.ToLower(ir.history[i]), queryLower) {
			return ir.history[i], i, true
		}
	}
	return "", -1, false
}

// renderSearchPrompt draws the reverse-search prompt line.
func (ir *InputReader) renderSearchPrompt() {
	// Clear the current line and go to the beginning.
	fmt.Printf("\r%s", ClearLineSeq())

	// Replace newlines with "\n" to prevent multi-line terminal rendering issues
	// when the history entry itself contains newline characters.
	display := strings.ReplaceAll(ir.searchResult, "\n", "\\n")

	if ir.searchResult != "" {
		fmt.Printf("(reverse-i-search)'%s': %s", BoldText(ir.searchQuery), display)
	} else {
		// No match found.
		fmt.Printf("(failing reverse-i-search)'%s': ", BoldText(ir.searchQuery))
	}

	// Clear any trailing content.
	fmt.Printf("%s", ClearToEndOfLineSeq())
}
