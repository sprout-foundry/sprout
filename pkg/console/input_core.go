package console

import (
	"bufio"
	"errors"
	"fmt"
	"io"
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
	EventWordLeft
	EventWordRight
	EventDeleteWordBackward
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
	pasteBuffer     strings.Builder
	pasteTimer      *time.Timer
	pasteActive     bool
	lastCharTime    time.Time
	bracketedPaste  bool
	bracketedMatch  int
	bracketedSawCR  bool
	collapsedPastes []pasteSpan

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
	// something between Tab presses). SP-078: type aliased to the shared
	// CompletionCycle so the same cycle state machine is reusable from
	// SteerInputReader.
	completionCycle *CompletionCycle

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

	// searchBuf accumulates bytes for a multi-byte UTF-8 rune while
	// typing in Ctrl-R reverse-search mode. handleSearchByte processes
	// one byte at a time from raw input, so without buffering, bytes
	// >= 128 (continuation bytes) would each be mis-converted via
	// string(rune(b)).
	searchBuf []byte

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
	bracketedPasteEnable  = "\033[?2004h"
	bracketedPasteDisable = "\033[?2004l"
	bracketedPasteEndSeq  = "\x1b[201~"
	// modifyOtherKeysEnable asks xterm-protocol-compatible terminals
	// (Windows Terminal, kitty, alacritty, foot, iTerm2 w/ CSI u, etc.)
	// to report modified keystrokes — most importantly Shift+Enter as
	// `ESC [ 13 ; 2 u` instead of indistinguishably as plain `\r`.
	// Level 1 covers Shift/Ctrl/Alt+Enter without disturbing arrows.
	// Restore (level 0) on exit so we don't leak the mode to whatever
	// runs after us.
	modifyOtherKeysEnable  = "\033[>4;1m"
	modifyOtherKeysDisable = "\033[>4;0m"

	// pastePollInterval is the idle spin sleep in the non-blocking
	// read loop; tuned empirically to keep typing responsive.
	pastePollInterval = 10 * time.Millisecond
	suspendDrainDelay = 50 * time.Millisecond // wait for in-flight bytes after SIGCONT

	// maxHistoryEntries caps the in-memory prompt history. Older entries
	// are dropped FIFO once the cap is exceeded.
	maxHistoryEntries = 100
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

	// Save terminal state and set raw mode. setupInputTerm (called
	// further down, after the line-state reset and prompt render)
	// installs the bracketed paste + SGR mouse + modifyOtherKeys modes,
	// registers this reader as the active one, sets up the SIGWINCH
	// channel, and flips the fd into non-blocking for paste detection.
	// teardownInputTerm (deferred inside setupInputTerm's call site)
	// undoes those modes in LIFO order: clear the reader slot, disable
	// the SGR sequences, then flip non-blocking off. The final cooked
	// termios restore is this outer `defer term.Restore` which runs last.
	oldState, err := term.MakeRaw(ir.termFd)
	if err != nil {
		return ir.fallbackReadLine()
	}
	defer term.Restore(ir.termFd, oldState)

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
	// setupInputTerm does (in order):
	//   - enable bracketed paste, SGR mouse tracking, modifyOtherKeys
	//   - register this reader as the active one (for PrintExternal)
	//   - create the SIGWINCH channel (nil on platforms without resize)
	//   - flip the fd to non-blocking (false on platforms that reject it)
	// The matching teardownInputTerm (deferred below) undoes the SGR
	// sequences and clears the active reader slot; the final
	// `defer term.Restore` reverts cooked termios.
	resizeCh, nonBlocking := ir.setupInputTerm()
	defer signal.Stop(resizeCh)
	// teardownInputTerm undoes the terminal modes that setupInputTerm
	// enabled (bracketed paste, SGR mouse, modifyOtherKeys, active
	// reader). It must run while the fd is still in raw mode, so it
	// is the first defer installed and therefore the first to fire.
	// The terminal is restored to cooked mode by the outer
	// `defer term.Restore` below.
	defer ir.teardownInputTerm()
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
			continueLoop, err := ir.handleReadError(err, nonBlocking, resizeCh, parser)
			if err != nil {
				return "", err
			}
			if continueLoop {
				continue
			}
		}

		if n == 0 {
			// In non-blocking mode, this means no data
			if nonBlocking {
				time.Sleep(pastePollInterval)
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

			// Emacs/readline-style control characters. These match the
			// bindings users expect from bash/zsh/python REPLs. Skipped
			// while in Ctrl-R reverse-search mode — there, only
			// Enter/Esc/Ctrl-C/Ctrl-R/Backspace/printable bytes affect
			// the search query (handled further below). Without this
			// guard, Ctrl-D on an empty prompt during search would
			// terminate the session, and Ctrl-A/E/U/K/W would corrupt
			// the underlying line buffer instead of the query.
			if !ir.searchMode {
				switch b {
				case 1: // Ctrl-A — move to start of line
					ir.SetCursor(0)
					continue
				case 4: // Ctrl-D — EOF on empty line, else forward-delete
					if len(ir.line) == 0 {
						fmt.Println()
						return "", io.EOF
					}
					ir.Delete()
					continue
				case 5: // Ctrl-E — move to end of line (standalone; the
					// two-keystroke Ctrl-X Ctrl-E editor escape is handled
					// above via pendingCtrlX)
					ir.SetCursor(len(ir.line))
					continue
				case 2: // Ctrl-B — move back one char
					ir.MoveCursor(-1)
					continue
				case 6: // Ctrl-F — move forward one char
					ir.MoveCursor(1)
					continue
				case 11: // Ctrl-K — kill to end of line
					ir.KillToEndOfLine()
					continue
				case 21: // Ctrl-U — kill to start of line
					ir.KillToStartOfLine()
					continue
				case 23: // Ctrl-W — delete previous word
					ir.DeleteWordBackward()
					continue
				}
			}

			if b == 26 { // Ctrl+Z
				if newState := ir.handleSuspend(oldState, nonBlocking); newState != nil {
					oldState = newState
				}
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
				result, input := ir.handleSearchModeByte(b)
				switch result {
				case searchContinue:
					continue
				case searchReturn:
					return input, nil
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
	case EventWordLeft:
		ir.MoveWord(-1)
	case EventWordRight:
		ir.MoveWord(1)
	case EventDeleteWordBackward:
		ir.DeleteWordBackward()
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

// handleReadError classifies a stdin read error and decides whether the
// read loop should continue (EAGAIN/EINTR) or abort. Returns (continueLoop bool, err error).
// When continueLoop is true, err is nil and the caller should `continue` the read loop.
// When err is non-nil, the caller should return it.
func (ir *InputReader) handleReadError(err error, nonBlocking bool, resizeCh chan os.Signal, parser *EscapeParser) (bool, error) {
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
			return true, nil
		}
		time.Sleep(pastePollInterval)
		return true, nil
	}
	if isInterrupted && ir.processPendingResize(resizeCh, parser) {
		return true, nil
	}
	// Real error — return it wrapped with context.
	// When stdin EOF arrives in the raw read loop (not the Ctrl-D path
	// at line ~381), log a diagnostic to help distinguish an attached-TTY
	// EOF (unexpected but possible) from a TTY that was revoked underneath
	// the process (pane closed, SSH timeout, parent exited).
	if errors.Is(err, io.EOF) {
		if term.IsTerminal(ir.termFd) {
			fmt.Fprintf(os.Stderr, "[console] stdin EOF received on attached terminal (fd=%d); exiting REPL\n", ir.termFd)
		} else {
			fmt.Fprintf(os.Stderr, "[console] stdin EOF: terminal no longer attached (fd=%d); this typically means the controlling TTY was closed (terminal pane closed, SSH timeout, parent process exited). Exiting REPL.\n", ir.termFd)
		}
	}
	return false, fmt.Errorf("stdin read error: %w", err)
}

// handleSuspend processes Ctrl-Z: suspends the process (SIGTSTP), waits for
// resume (SIGCONT), drains stale input, re-enters raw mode, and restores the
// prompt. Returns the new raw-mode *term.State (or nil if re-entering failed).
func (ir *InputReader) handleSuspend(oldState *term.State, nonBlocking bool) *term.State {
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
		time.Sleep(suspendDrainDelay)
		discardBuf := make([]byte, 256)
		for {
			n, _ := os.Stdin.Read(discardBuf)
			if n <= 0 {
				break
			}
		}
	}

	// Re-enter raw mode.
	newState, err := term.MakeRaw(ir.termFd)
	if err != nil {
		return nil
	}

	// Re-enable bracketed paste mode (lost when we exited raw mode).
	fmt.Print(bracketedPasteEnable)

	resetTerminalSignals()

	// Redisplay the prompt and the preserved line content.
	// Unlike a fresh prompt, we keep whatever the user had
	// typed before suspending (mirrors runExternalEditor).
	fmt.Printf("\r%s%s", ClearLineSeq(), ir.prompt)
	ir.Refresh()
	return newState
}

// searchByteResult communicates what the ReadLine loop should do after
// handleSearchModeByte processes a byte.
type searchByteResult int

const (
	searchContinue searchByteResult = iota // continue the read loop
	searchReturn                           // return (input, nil) — accepted match
)

// handleSearchModeByte dispatches a byte while in Ctrl-R reverse-search mode.
// The `input` out-param is set only when result is searchReturn.
//
// Note: Ctrl-C (byte 3) never reaches here — it is intercepted earlier in
// the ReadLine loop and aborts the whole read. We therefore do not handle
// it here; doing so would be dead code.
func (ir *InputReader) handleSearchModeByte(b byte) (result searchByteResult, input string) {
	if b == 13 || b == 10 { // Enter — accept match
		ir.exitSearchMode(true)
		ir.Refresh()
		fmt.Println()
		input := ir.line
		if input != "" {
			ir.AddToHistory(input)
		}
		return searchReturn, input
	} else if b == 27 { // Escape — cancel
		ir.exitSearchMode(false)
		ir.Refresh()
		return searchContinue, ""
	} else {
		ir.handleSearchByte(b)
		ir.renderSearchPrompt()
		return searchContinue, ""
	}
}
