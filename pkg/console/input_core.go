package console

import (
	"bufio"
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
	// modifyOtherKeysEnable asks xterm-protocol-compatible terminals
	// (Windows Terminal, kitty, alacritty, foot, iTerm2 w/ CSI u, etc.)
	// to report modified keystrokes — most importantly Shift+Enter as
	// `ESC [ 13 ; 2 u` instead of indistinguishably as plain `\r`.
	// Level 1 covers Shift/Ctrl/Alt+Enter without disturbing arrows.
	// Restore (level 0) on exit so we don't leak the mode to whatever
	// runs after us.
	modifyOtherKeysEnable  = "\033[>4;1m"
	modifyOtherKeysDisable = "\033[>4;0m"
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

	// Ask the terminal to report modified keystrokes (Shift+Enter etc.)
	// as CSI u sequences. Terminals that don't recognize this just
	// ignore the SGR; the new parser branch is a no-op when the
	// sequence never arrives.
	fmt.Print(modifyOtherKeysEnable)
	defer fmt.Print(modifyOtherKeysDisable)

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
// (piped stdin). Uses bufio.Reader so multi-word input isn't truncated at
// whitespace (the old fmt.Scanln did that) and returns the error directly
// so EOF on a closed pipe is reported faithfully rather than always wrapped
// as "failed to read fallback input: EOF" (the old fmt.Errorf("%w", err)
// produced a non-nil error even on success).
func (ir *InputReader) fallbackReadLine() (string, error) {
	fmt.Print(ir.prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
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

