// Package console: core types and constants for InputReader (split from input_core.go)

package console

import (
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
	// EventAltLetter is fired for Alt-modified letters not already
	// claimed by a more specific event (Alt+B / Alt+F / Alt+Backspace).
	// The letter is in .Data as a single byte (e.g. "T" for Alt+T).
	// CLI-D uses this to drive the status-footer tooltip toggle.
	EventAltLetter
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
	// richCompleter provides structured candidates (text + description)
	// for the live autocomplete dropdown. When set, the dropdown
	// prefers this over the plain completer to render descriptions.
	richCompleter RichCompletionProvider
	// Active cycle state. Refreshed when Tab is pressed against a buffer
	// that differs from the last applied completion (i.e. the user typed
	// something between Tab presses). SP-078: type aliased to the shared
	// CompletionCycle so the same cycle state machine is reusable from
	// SteerInputReader.
	completionCycle *CompletionCycle

	// Live inline autocomplete dropdown for slash commands. Activated
	// automatically when the input line starts with "/".
	autocomplete *inlineAutocomplete

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

	// footerTooltip, when non-nil, is shown/hidden by Alt+T.
	// Initialized to a default that writes to os.Stderr; tests can
	// override via SetFooterTooltip.
	footerTooltip *FooterTooltip
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
