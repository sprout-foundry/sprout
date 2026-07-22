package console

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"

	"golang.org/x/term"
)

// isEAGAIN reports whether err is the "no data ready" return from a
// non-blocking read. Both syscall.EAGAIN and io.EOF can show up
// depending on the platform / runtime; we treat both as "wait and
// retry" rather than a hard error.
func isEAGAIN(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
		return true
	}
	if errors.Is(err, io.EOF) {
		// On Linux a non-blocking read with nothing pending sometimes
		// surfaces as io.EOF instead of EAGAIN. Treat it as transient.
		return true
	}
	return false
}

// SteerInputReader captures keystrokes during an active model turn and
// renders them into a pinned line above the status footer (SP-055). It
// is distinct from InputReader, which owns stdin only during the REPL
// prompt; SteerInputReader takes over stdin between ReadLine() calls,
// while the model is processing.
//
// Lifecycle:
//
//	r := NewSteerInputReader(footer, submitFn, interruptFn)
//	r.Start()   // puts terminal in raw mode, starts read loop
//	... ProcessQuery runs ...
//	r.Stop()    // restores cooked mode, clears the pinned line
//
// Key handling (raw mode — ICANON / ISIG disabled, so signals must be
// re-implemented by reading the byte directly). Control keys below are
// dispatched directly in readLoop; arrow keys, function keys, escape
// sequences, Alt+key combos, bracketed-paste markers, and multi-byte
// UTF-8 runes go through the shared EscapeParser (the same parser
// InputReader uses in input_escape_parser.go) and are routed via
// handleEvent. Tab toggles the submit mode (STEER ↔ QUEUE).
//
//	Enter (CR/LF)   → submitFn(buffer); buffer cleared
//	Tab             → toggle STEER/QUEUE submit mode
//	Backspace (DEL) → remove rune before cursor
//	Escape (alone)  → clear buffer (does not exit steer mode)
//	Ctrl+C  (0x03)  → interruptFn() — caller routes to TriggerInterrupt
//	Ctrl+A/E         → move cursor to start / end
//	Ctrl+B/F         → move cursor back / forward one rune
//	Ctrl+D           → forward-delete rune at cursor
//	Ctrl+K/U         → kill from cursor to end / start of buffer
//	Ctrl+W           → delete word before cursor
//	Alt+B/F          → move cursor back / forward one word
//	Ctrl+Left/Right  → move cursor back / forward one word
//	Left/Right       → move cursor back / forward one rune
//	Up/Down          → recall steer history
//	Alt+Enter/Shift+Enter → insert a literal newline (multi-line compose)
//
// Submission UX: when Enter is pressed, submitFn receives the buffer.
// Caller is expected to forward to Agent.InjectInputContext (or
// equivalent). The buffer is then cleared and the pinned line shows the
// prompt prefix again, ready for the next steer.
//
// Suppressed entirely on non-TTY stdin (Start is a no-op), matching the
// behavior of StatusFooter / ActivityIndicator. Callers can construct
// the reader unconditionally; the gating happens here.
type SteerInputReader struct {
	mu sync.Mutex

	footer      *StatusFooter
	submitFn    func(string) // STEER mode: mid-turn injection
	queueFn     func(string) // QUEUE mode: deferred to next user turn
	interruptFn func()

	// submitMode controls how Enter is interpreted. Tab toggles. The
	// rendered prefix changes accordingly so the user always sees
	// which mode will fire on submit.
	submitMode SteerSubmitMode

	isTTY  bool
	fd     int
	active bool
	stopCh chan struct{}
	doneCh chan struct{}

	// buffer accumulates the in-progress steer message.
	buffer []byte

	// cursorPos is the byte offset into buffer where the next edit
	// lands. The footer renders a caret marker at this position so
	// the user sees where typed characters will be inserted.
	cursorPos int

	// oldState is the pre-steer-mode termios snapshot, restored on Stop.
	// Use steerTermiosState (NOT term.State) because we run a "cbreak"-
	// like mode that keeps OPOST intact — otherwise streaming output
	// staircases. See pkg/console/steer_termios_*.go.
	oldState *steerTermiosState

	// history is the ring of previously-submitted steer messages
	// (SP-055 Phase 3). Up arrow recalls the most recent; subsequent
	// Ups walk further back; Down walks forward toward the in-progress
	// buffer. Capped at SteerHistoryCap to stay bounded; oldest entries
	// drop off the front.
	history []string

	// historyIndex tracks the cursor into history during recall. -1
	// means "at the live buffer" (not navigating). 0..len-1 means
	// "showing history[len-1-i]". Reset to -1 on submit or text-edit.
	historyIndex int

	// pendingBuffer is the live buffer snapshotted when the user
	// starts navigating history so they can Down-arrow back to what
	// they were typing. Nil when not navigating.
	pendingBuffer []byte

	// pasteActive is true while we are between a bracketed-paste
	// start (ESC [ 200~) and end (ESC [ 201~). All bytes in that
	// window go straight into pasteBuf, bypassing the normal
	// keystroke dispatch — Enter inside a paste shouldn't submit,
	// for example. SP-057 follow-up: rich paste in steer.
	pasteActive bool
	pasteBuf    []byte

	// groundTruth is a snapshot of the terminal's cooked-mode termios
	// captured at REPL startup. When Stop() restores the terminal, it
	// uses this as the restoration target instead of the per-enter
	// oldState, preventing termios-state descent across Pause/Resume
	// cycles where intermediate code may have altered termios.
	groundTruth *GroundTruthTermios

	// pendingCtrlX is set when the user presses Ctrl-X, the first half
	// of the Ctrl-X Ctrl-E editor escape sequence. The next byte is
	// checked against Ctrl-E; if it matches, the external editor is
	// launched with the current buffer. Any other byte clears the
	// pending state and is processed normally.
	pendingCtrlX bool

	// searchMode is true during Ctrl-R reverse-search through steer
	// history. While active, typed characters build the query instead
	// of the buffer; Enter accepts the match; Esc cancels.
	searchMode         bool
	searchQuery        string
	searchResult       string
	searchResultIndex  int
	preSearchBuffer    []byte
	preSearchCursorPos int
	searchBuf          []byte // multi-byte UTF-8 accumulator

	// SP-078 Phase 2: optional completion provider for the steer
	// panel. Set via SetCompleter. nil = Ctrl-] (and any future
	// completion binding) is a silent no-op. The provider receives
	// the current buffer + cursorPos and returns ordered candidate
	// replacements, same shape as the InputReader's completer.
	//
	// completionCycle tracks the active multi-press cycle. Allocated
	// lazily on first apply; cycle.Reset() is called on every buffer
	// edit so the next press starts fresh.
	completer       CompletionProvider
	completionCycle *CompletionCycle

	// autocomplete is the live dropdown that mirrors the InputReader's
	// (slash command) affordance on the steer panel. SP-078 Phase 3.
	// Initialized in NewSteerInputReader so callers can drive it
	// without nil checks. The dropdown renders above the input line
	// as additional rows in the steer panel whenever the buffer
	// starts with "/" and richCompleter returns candidates.
	autocomplete *inlineAutocomplete

	// richCompleter is the structured provider (command + description)
	// the dropdown draws from. Optional — when nil the dropdown is
	// disabled even with a plain completer installed. Matches
	// InputReader.SetRichCompleter.
	richCompleter RichCompletionProvider
}

// SteerHistoryCap bounds the in-memory steer history to a sensible
// session-level value. Exposed for testing / config.
const SteerHistoryCap = 50

// SteerSubmitMode controls what happens on Enter (SP-055 Phase 3b).
// STEER (default) injects mid-turn via the submit callback (typically
// Agent.InjectInputContext → seed.InjectInput). QUEUE buffers the
// message into the agent's deferred queue, which the REPL drains and
// prepends to the next user-typed prompt.
type SteerSubmitMode int

const (
	SteerSubmitModeNow   SteerSubmitMode = iota // mid-turn injection (default)
	SteerSubmitModeQueue                        // hold until next turn
)

// SteerPromptPrefix is the visible glyph + space rendered at the start
// of the pinned input line in STEER mode. QueuePromptPrefix is the
// alternative shown after the user toggles via Tab. Both exposed for
// testing / theming.
const (
	SteerPromptPrefix = "⇄ steer › "
	QueuePromptPrefix = "⏸ queue › "
)

// NewSteerInputReader builds a reader that draws into the given footer
// and reports submitted/interrupt events via the callbacks. The
// callbacks fire on the reader's read goroutine — keep them quick or
// dispatch to another goroutine to avoid blocking the input loop.
//
// queueFn is optional: when nil the user has no way to switch into
// queue mode (Tab becomes a no-op). When non-nil, Tab toggles between
// STEER and QUEUE submit modes; pressing Enter in QUEUE mode calls
// queueFn(text) instead of submitFn(text).
func NewSteerInputReader(footer *StatusFooter, submitFn, queueFn, interruptFn func(string)) *SteerInputReader {
	r := &SteerInputReader{
		footer:   footer,
		submitFn: submitFn,
		queueFn:  queueFn,
		interruptFn: func() {
			if interruptFn != nil {
				interruptFn("")
			}
		},
		fd:           int(os.Stdin.Fd()),
		autocomplete: newInlineAutocomplete(),
	}
	r.isTTY = term.IsTerminal(r.fd)
	return r
}

// Start puts the terminal in raw mode and spawns the read goroutine.
// Idempotent. No-op on non-TTY. The pinned line is rendered immediately
// so the user sees the empty prompt as soon as a turn begins.
func (r *SteerInputReader) Start() {
	if r == nil || !r.isTTY {
		return
	}
	r.mu.Lock()
	if r.active {
		r.mu.Unlock()
		return
	}
	r.active = true
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})
	// NOTE: buffer is NOT cleared here. Start() is called for both
	// fresh turns (StartTurn) and resume after pause (ResumeSteer).
	// Buffer clearing is the coordinator's responsibility, done in
	// StartTurn before calling Start(). This preserves in-progress
	// text across PauseSteer/ResumeSteer cycles.
	stopCh := r.stopCh
	doneCh := r.doneCh
	r.mu.Unlock()

	st, err := enterSteerMode(r.fd)
	if err != nil {
		// Couldn't enter steer mode — degrade to no-op rather than
		// fight the terminal. Caller's session continues to work,
		// just without the steer affordance.
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
		close(doneCh)
		return
	}
	r.oldState = st

	// Enable bracketed paste so multi-line / large pastes arrive as
	// a single ESC [ 200~ ... ESC [ 201~ wrapped chunk that we can
	// accumulate verbatim instead of dispatching each byte through
	// the normal keystroke handlers. Stop() emits the matching
	// disable sequence.
	fmt.Fprint(os.Stderr, bracketedPasteEnable)
	// Ask the terminal to report modified keystrokes (Shift+Enter
	// etc.) as CSI u sequences so the steer panel can distinguish
	// "submit" from "insert newline" instead of both arriving as
	// indistinguishable CR.
	//
	// Skipped in the sprout webui terminal: see the matching comment
	// in pkg/console/input_core.go writeModifyOtherKeysEnable.
	writeModifyOtherKeysEnable(os.Stderr)

	r.renderLine()

	// Register as the active steer reader so background goroutines
	// (async output worker, tool handlers) can print messages via
	// PrintExternal without corrupting the steer panel. Cleared on Stop.
	LockOutput()
	setActiveSteerReader(r)
	UnlockOutput()

	go r.readLoop(stopCh, doneCh)
}

// Stop restores cooked mode, clears the pinned line, and waits for the
// read goroutine to exit. Idempotent. MUST be called on every exit
// path (including signal-driven shutdown) or the terminal will be left
// in steer mode.
//
// Ordering matters: we wait for the goroutine to exit BEFORE calling
// exitSteerMode. In steer mode VMIN=0/VTIME=0 makes Read return
// immediately with 0 bytes, so the goroutine's poll loop observes
// stopCh within one tick (5ms). If we restored cooked mode first, the
// goroutine's next Read would block forever (cooked VMIN=1) and leak.
func (r *SteerInputReader) Stop() {
	if r == nil || !r.isTTY {
		return
	}
	r.mu.Lock()
	if !r.active {
		r.mu.Unlock()
		return
	}
	r.active = false
	stopCh := r.stopCh
	doneCh := r.doneCh
	oldState := r.oldState
	r.oldState = nil
	r.mu.Unlock()

	close(stopCh)
	if doneCh != nil {
		<-doneCh
	}
	// Disable bracketed paste before restoring termios so the
	// terminal returns to its prior paste mode (some apps run
	// without bracketed paste enabled). Same for the modifyOtherKeys
	// reporting we enabled in Start.
	fmt.Fprint(os.Stderr, bracketedPasteDisable)
	writeModifyOtherKeysDisable(os.Stderr)

	// Restore terminal to ground-truth state if available (preferred
	// over oldState). Ground truth is the REPL's original cooked-mode
	// snapshot, immune to termios-state descent from PauseSteer /
	// ResumeSteer cycles where intermediate code may have altered the
	// termios between our enter and exit.
	r.mu.Lock()
	gt := r.groundTruth
	r.mu.Unlock()
	if gt != nil {
		_ = gt.Restore()
	} else if oldState != nil {
		_ = exitSteerMode(r.fd, oldState)
	}
	// Clear the active steer reader slot so PrintExternal no longer
	// routes through this reader after Stop. Must happen before we
	// clear the steer line so any final mid-stop messages still go
	// through the correct path.
	LockOutput()
	setActiveSteerReader(nil)
	UnlockOutput()

	// Clear the pinned line via the footer.
	if r.footer != nil {
		r.footer.ClearSteerLine()
	}
}

// IsActive reports whether the reader is currently capturing input.
// Used by callers that need to coordinate (e.g. signal handlers).
func (r *SteerInputReader) IsActive() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// SetGroundTruth installs the REPL's pristine cooked-mode termios
// snapshot. Stop() uses this instead of per-enter oldState for
// restoration, preventing termios descent across PauseSteer /
// ResumeSteer cycles.
func (r *SteerInputReader) SetGroundTruth(gt *GroundTruthTermios) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.groundTruth = gt
}

// DrainUnsentBuffer returns any text the user typed into the steer
// panel but did not submit (no Enter pressed). The caller (typically
// the REPL loop via SteerCoordinator) can carry this into the next
// ReadLine call so the text is not silently discarded when a turn ends.
// The buffer is left intact; call ResetBuffer afterwards to clear it.
func (r *SteerInputReader) DrainUnsentBuffer() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buffer)
}

// ResetBuffer clears the in-progress steer buffer. Called by the
// coordinator after draining the unsent text into the InputReader,
// so the next Start() begins with a clean slate.
func (r *SteerInputReader) ResetBuffer() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buffer = r.buffer[:0]
	r.cursorPos = 0
	r.historyIndex = -1
	r.pendingBuffer = nil
}
