package console

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

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
		fd: int(os.Stdin.Fd()),
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
	fmt.Fprint(os.Stderr, modifyOtherKeysEnable)

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
	fmt.Fprint(os.Stderr, modifyOtherKeysDisable)

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

// readLoop is the input-handling goroutine. Steer mode sets VMIN=0
// and VTIME=0 on the termios (see steer_termios_*.go) so Read returns
// immediately with 0 bytes when nothing is ready — no need for an
// O_NONBLOCK file descriptor flag. The poll interval (10ms) is short
// enough that typing feels instantaneous and Stop() observes the exit
// signal within one frame.
//
// Input is read in multi-byte chunks (up to 64 bytes per Read) and fed
// through the shared EscapeParser — the same parser InputReader uses —
// instead of hand-rolling escape-sequence detection and polling loops
// here. Emacs-style control keys are dispatched directly before the
// parser so they aren't shadowed by the parser's own handling.
func (r *SteerInputReader) readLoop(stopCh, doneCh chan struct{}) {
	defer close(doneCh)

	parser := NewEscapeParser()
	buf := make([]byte, 64)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	// Subscribe to terminal resize events so the steer panel
	// re-renders with correct dimensions after a SIGWINCH. The footer's
	// own resize watcher handles the scroll region; we just need to
	// refresh our pinned line content.
	var resizeCh chan os.Signal
	if sig := resizeSignal(); sig != nil {
		resizeCh = make(chan os.Signal, 1)
		signal.Notify(resizeCh, sig)
		defer signal.Stop(resizeCh)
	}

	// pasteMatch tracks how many bytes of the bracketed-paste end
	// sequence (ESC [ 201~) have been seen consecutively while a
	// paste is in flight. A local rather than a struct field because
	// readLoop is the sole consumer and bracketed-paste handling is
	// strictly sequential (single goroutine, no overlap).
	pasteMatch := 0

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		n, err := os.Stdin.Read(buf)
		if n == 0 {
			// No byte ready (or EOF). Sleep briefly via the ticker
			// instead of busy-spinning, then re-check stopCh.
			if err != nil && !isEAGAIN(err) {
				// Real error (stdin closed, etc.) — exit.
				return
			}
			select {
			case <-stopCh:
				return
			case <-resizeCh:
				// Terminal resized — re-render the steer line so the
				// caret and content adapt to the new width.
				r.renderLine()
			case <-ticker.C:
			}
			continue
		}

		for i := 0; i < n; i++ {
			b := buf[i]

			// While a bracketed paste is in flight, ALL bytes go into
			// pasteBuf verbatim — including newlines and what would
			// otherwise be control characters. The only escape is the
			// "ESC [ 201~" terminator, which we detect inline by
			// matching the byte stream against bracketedPasteEndSeq.
			r.mu.Lock()
			inPaste := r.pasteActive
			r.mu.Unlock()
			if inPaste {
				if b == bracketedPasteEndSeq[pasteMatch] {
					pasteMatch++
					if pasteMatch == len(bracketedPasteEndSeq) {
						pasteMatch = 0
						r.endPaste()
					}
					continue
				}
				// Mismatch: flush any partially-matched prefix as
				// literal bytes, then handle the current byte.
				if pasteMatch > 0 {
					for _, pb := range []byte(bracketedPasteEndSeq[:pasteMatch]) {
						r.appendPasteByte(pb)
					}
					pasteMatch = 0
				}
				r.appendPasteByte(b)
				continue
			}

			// Ctrl-X Ctrl-E editor escape (SP-048-4f parity). The first
			// Ctrl-X sets pendingCtrlX; if the next byte is Ctrl-E we
			// launch the external editor, otherwise we fall through to
			// normal processing.
			if r.pendingCtrlX {
				r.pendingCtrlX = false
				if b == 0x05 { // Ctrl-E
					r.runExternalEditor()
					continue
				}
				// Not Ctrl-E — fall through to normal processing.
			}

			// While in Ctrl-R reverse-search mode, route keystrokes to
			// the search handler instead of the normal buffer/edit
			// dispatch. Enter accepts the match, Esc cancels, Ctrl-R
			// cycles to the next older match, Backspace trims the
			// query, and printable/UTF-8 bytes extend the query.
			if r.searchMode {
				switch {
				case b == 0x0D: // Enter — accept match
					r.exitSearchMode(true)
					r.renderLine()
					continue
				case b == 0x1B: // Esc — cancel
					r.exitSearchMode(false)
					r.renderLine()
					continue
				case b == 0x7F || b == 0x08: // Backspace
					r.handleSearchBackspace()
					r.renderLine()
					continue
				case b >= 0x20 && b < 0x7F: // Printable ASCII
					r.searchQuery += string(rune(b))
					r.refreshSearchForQuery()
					r.renderLine()
					continue
				case b >= 0x80: // UTF-8 — buffer until full rune
					r.searchBuf = append(r.searchBuf, b)
					if utf8.FullRune(r.searchBuf) {
						r.searchQuery += string(r.searchBuf)
						r.searchBuf = r.searchBuf[:0]
						r.refreshSearchForQuery()
						r.renderLine()
					}
					continue
				}
				// Other control chars in search mode: ignore.
				continue
			}

			// Pre-handle control characters that the EscapeParser
			// doesn't produce events for (emacs/readline-style
			// editing). The parser handles backspace (8/127), enter
			// (13), bare newline (10), tab (9), escape sequences (27),
			// and printable / UTF-8 characters.
			switch b {
			case 0x01: // Ctrl+A — move to start
				r.moveCursorStart()
				continue
			case 0x02: // Ctrl+B — move back one rune
				r.moveCursorBackward()
				continue
			case 0x03: // Ctrl+C — interrupt
				r.handleInterrupt()
				continue
			case 0x04: // Ctrl+D — forward-delete rune at cursor
				r.deleteForward()
				continue
			case 0x05: // Ctrl+E — move to end
				r.moveCursorEnd()
				continue
			case 0x06: // Ctrl+F — move forward one rune
				r.moveCursorForward()
				continue
			case 0x0B: // Ctrl+K — kill to end
				r.killToEnd()
				continue
			case 0x12: // Ctrl+R — reverse search
				if r.searchMode {
					r.cycleSearchResult()
				} else {
					r.enterSearchMode()
				}
				r.renderLine()
				continue
			case 0x15: // Ctrl+U — kill to start
				r.killToStart()
				continue
			case 0x17: // Ctrl+W — delete previous word
				r.deleteWordBackward()
				continue
			case 0x18: // Ctrl+X — start of Ctrl-X Ctrl-E sequence
				r.pendingCtrlX = true
				continue
			}

			// Feed everything else through the shared EscapeParser.
			event := parser.Parse(b)
			if event == nil {
				continue
			}
			r.handleEvent(event)
			// Drain any pending events the parser queued (e.g. a
			// printable byte carried over after an escape sequence).
			for parser.hasPending {
				pending := parser.Parse(0)
				if pending == nil {
					break
				}
				r.handleEvent(pending)
			}
		}
	}
}

// handleEvent dispatches a parsed InputEvent (produced by the shared
// EscapeParser) to the appropriate steer reader action. This replaces
// the former hand-rolled escape-sequence / UTF-8 / CSI parsing that
// lived in this file.
func (r *SteerInputReader) handleEvent(event *InputEvent) {
	switch event.Type {
	case EventChar:
		r.insertAtCursor([]byte(event.Data))
	case EventBackspace:
		r.handleBackspace()
	case EventDelete:
		r.deleteForward()
	case EventEnter:
		r.handleSubmit()
	case EventTab:
		r.toggleSubmitMode()
	case EventUp:
		r.recallHistory(-1)
	case EventDown:
		r.recallHistory(1)
	case EventLeft:
		r.moveCursorBackward()
	case EventRight:
		r.moveCursorForward()
	case EventHome:
		r.moveCursorStart()
	case EventEnd:
		r.moveCursorEnd()
	case EventWordLeft:
		r.moveWord(-1)
	case EventWordRight:
		r.moveWord(1)
	case EventDeleteWordBackward:
		r.deleteWordBackward()
	case EventEscape:
		r.clearBuffer()
	case EventInterrupt:
		r.handleInterrupt()
	case EventPasteStart:
		r.beginPaste()
	case EventPasteEnd:
		r.endPaste()
	case EventMouse:
		// Mouse events are not supported in steer mode — swallow.
	}
}

// clearBuffer clears the steer buffer (plain ESC key).
func (r *SteerInputReader) clearBuffer() {
	r.mu.Lock()
	r.buffer = r.buffer[:0]
	r.cursorPos = 0
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// handleInterrupt fires the user-provided callback (typically wired to
// chatAgent.TriggerInterrupt) and clears the buffer. Stays active —
// the user can keep typing or hit Ctrl+C again.
func (r *SteerInputReader) handleInterrupt() {
	r.mu.Lock()
	r.buffer = r.buffer[:0]
	r.cursorPos = 0
	r.historyIndex = -1
	r.pendingBuffer = nil
	cb := r.interruptFn
	r.mu.Unlock()
	r.renderLine()
	if cb != nil {
		cb()
	}
}

// handleSubmit fires the appropriate callback for the current submit
// mode and clears the line. Empty submissions are dropped (no-op) so
// users can hit Enter on an empty line without sending noise to the
// agent. Non-empty submissions are appended to the steer history ring
// regardless of mode so up-arrow recall works across both.
func (r *SteerInputReader) handleSubmit() {
	r.mu.Lock()
	text := string(r.buffer)
	r.buffer = r.buffer[:0]
	r.cursorPos = 0
	mode := r.submitMode
	submit := r.submitFn
	queue := r.queueFn
	r.mu.Unlock()
	r.renderLine()
	if text == "" {
		return
	}
	r.appendHistory(text)
	if mode == SteerSubmitModeQueue && queue != nil {
		queue(text)
		return
	}
	if submit != nil {
		submit(text)
	}
}

// toggleSubmitMode flips between STEER and QUEUE modes (Tab key).
// No-op when no queueFn is wired — the reader is built without queue
// support in that case and the user shouldn't see a Tab affordance.
func (r *SteerInputReader) toggleSubmitMode() {
	r.mu.Lock()
	if r.queueFn == nil {
		r.mu.Unlock()
		return
	}
	if r.submitMode == SteerSubmitModeNow {
		r.submitMode = SteerSubmitModeQueue
	} else {
		r.submitMode = SteerSubmitModeNow
	}
	r.mu.Unlock()
	r.renderLine()
}

// SubmitMode reports the current Enter-binding. Exposed for tests.
func (r *SteerInputReader) SubmitMode() SteerSubmitMode {
	if r == nil {
		return SteerSubmitModeNow
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.submitMode
}

// beginPaste enters bracketed-paste accumulation mode. All bytes that
// arrive between now and endPaste() are appended verbatim to pasteBuf.
func (r *SteerInputReader) beginPaste() {
	r.mu.Lock()
	r.pasteActive = true
	r.pasteBuf = r.pasteBuf[:0]
	r.mu.Unlock()
}

// endPaste finalizes a bracketed paste: checks for image data,
// applies smart-save for large text pastes, or appends inline.
func (r *SteerInputReader) endPaste() {
	r.mu.Lock()
	paste := r.pasteBuf
	r.pasteBuf = r.pasteBuf[:0]
	r.pasteActive = false
	r.mu.Unlock()

	if len(paste) == 0 {
		return
	}

	// Check for binary image data
	if len(paste) > 4 && len(paste) <= MaxPastedImageSize {
		if ext, mimeType := DetectImageMagic(paste); ext != "" {
			fmt.Fprintln(os.Stderr)
			GlyphAction.Fprintf(os.Stderr, "Image paste detected (%s, %d bytes)", mimeType, len(paste))
			savedPath, err := SavePastedImage(paste, "")
			if err != nil {
				GlyphError.Fprintf(os.Stderr, "Failed to save pasted image: %v", err)
			} else {
				GlyphSuccess.Fprintf(os.Stderr, "Saved to %s", savedPath)
				placeholder := fmt.Sprintf("Pasted image saved to disk: %s ", savedPath)
				r.insertAtCursor([]byte(placeholder))
				return
			}
		}
	}

	// Convert to string for text processing
	content := string(paste)

	// Smart paste: large text auto-saved as file reference
	if ShouldSmartSavePaste(content) {
		if savedPath, err := SavePastedText(content, ""); err == nil {
			lineCount := strings.Count(content, "\n") + 1
			fmt.Fprintln(os.Stderr)
			GlyphAction.Fprintf(os.Stderr, "%d lines · %d bytes saved to %s",
				lineCount, len(content), savedPath)
			placeholder := "@" + savedPath + " "
			r.insertAtCursor([]byte(placeholder))
			return
		} else {
			GlyphError.Fprintf(os.Stderr, "smart-paste save failed: %v (inserting inline)", err)
		}
	}

	// Default: insert inline
	r.insertAtCursor(paste)
}

// appendPasteByte adds one byte to the in-flight paste buffer. Called
// from readLoop while pasteActive is true.
func (r *SteerInputReader) appendPasteByte(b byte) {
	r.mu.Lock()
	r.pasteBuf = append(r.pasteBuf, b)
	r.mu.Unlock()
}

// hasEditedHistory resets the history navigation cursor after a buffer
// mutation so subsequent up-arrow recall starts fresh. Must be called
// with r.mu held.
func (r *SteerInputReader) hasEditedHistory() {
	r.historyIndex = -1
	r.pendingBuffer = nil
}

// recallHistory walks the steer-history index by delta. Negative steps
// backward (toward older), positive steps forward (toward newer / the
// live buffer). At the live-buffer boundary it restores the pending
// buffer the user was typing when they started navigating.
func (r *SteerInputReader) recallHistory(delta int) {
	r.mu.Lock()
	if len(r.history) == 0 {
		r.mu.Unlock()
		return
	}

	if r.historyIndex == -1 && delta < 0 {
		// First Up while at live buffer — snapshot current text so we
		// can return to it on later Down.
		snap := make([]byte, len(r.buffer))
		copy(snap, r.buffer)
		r.pendingBuffer = snap
	}

	newIdx := r.historyIndex + delta
	switch {
	case newIdx < 0:
		// Already at oldest — clamp.
		newIdx = len(r.history) - 1
		if r.historyIndex < 0 {
			// First Up press from the live buffer.
			newIdx = 0
		}
	case newIdx >= len(r.history):
		// Walked past the newest entry → back to the live buffer.
		newIdx = -1
	}

	if newIdx == -1 {
		// Restore the pending buffer the user was typing.
		if r.pendingBuffer != nil {
			r.buffer = append(r.buffer[:0], r.pendingBuffer...)
		} else {
			r.buffer = r.buffer[:0]
		}
	} else {
		// history is ordered oldest→newest. UI walks newest-first, so
		// index `i` maps to history[len-1-i].
		entry := r.history[len(r.history)-1-newIdx]
		r.buffer = append(r.buffer[:0], entry...)
	}
	r.cursorPos = len(r.buffer)
	r.historyIndex = newIdx
	r.mu.Unlock()
	r.renderLine()
}

// appendHistory pushes a submitted message onto the history ring,
// deduplicating consecutive repeats and capping at SteerHistoryCap.
func (r *SteerInputReader) appendHistory(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if text == "" {
		return
	}
	if n := len(r.history); n > 0 && r.history[n-1] == text {
		return // consecutive dup
	}
	r.history = append(r.history, text)
	if over := len(r.history) - SteerHistoryCap; over > 0 {
		r.history = r.history[over:]
	}
	r.historyIndex = -1
	r.pendingBuffer = nil
}

// handleBackspace removes the RUNE (not byte) immediately before the
// cursor so a single backspace on a multi-byte character — Greek "α",
// Han "字", emoji "🚀" — deletes the whole glyph rather than corrupting
// it. Walks backward from the cursor skipping UTF-8 continuation bytes
// (10xxxxxx) until it finds a lead byte (or ASCII) to drop. A no-op
// when the cursor is at position 0.
//
// Also exits history navigation: editing a recalled entry treats it
// as a fresh in-progress message.
func (r *SteerInputReader) handleBackspace() {
	r.mu.Lock()
	if r.cursorPos > 0 {
		// Find the start of the rune just before the cursor by walking
		// back over continuation bytes (0x80..0xBF).
		i := r.cursorPos - 1
		for i > 0 && r.buffer[i]&0xC0 == 0x80 {
			i--
		}
		r.buffer = slices.Delete(r.buffer, i, r.cursorPos)
		r.cursorPos = i
	}
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// insertAtCursor inserts a byte sequence at the cursor position and
// advances the cursor. Edits exit history navigation. Caller must NOT
// hold r.mu.
func (r *SteerInputReader) insertAtCursor(data []byte) {
	r.mu.Lock()
	r.buffer = slices.Insert(r.buffer, r.cursorPos, data...)
	r.cursorPos += len(data)
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// moveCursorStart moves the cursor to byte 0 (Ctrl+A / Home).
func (r *SteerInputReader) moveCursorStart() {
	r.mu.Lock()
	r.cursorPos = 0
	r.mu.Unlock()
	r.renderLine()
}

// moveCursorEnd moves the cursor to the end of the buffer (Ctrl+E / End).
func (r *SteerInputReader) moveCursorEnd() {
	r.mu.Lock()
	r.cursorPos = len(r.buffer)
	r.mu.Unlock()
	r.renderLine()
}

// moveCursorBackward moves the cursor back one rune (Ctrl+B / Left).
// A no-op when the cursor is already at the start.
func (r *SteerInputReader) moveCursorBackward() {
	r.mu.Lock()
	if r.cursorPos > 0 {
		_, sz := utf8.DecodeLastRune(r.buffer[:r.cursorPos])
		r.cursorPos -= sz
	}
	r.mu.Unlock()
	r.renderLine()
}

// moveCursorForward moves the cursor forward one rune (Ctrl+F / Right).
// A no-op when the cursor is already at the end.
func (r *SteerInputReader) moveCursorForward() {
	r.mu.Lock()
	if r.cursorPos < len(r.buffer) {
		_, sz := utf8.DecodeRune(r.buffer[r.cursorPos:])
		r.cursorPos += sz
	}
	r.mu.Unlock()
	r.renderLine()
}

// moveWord moves the cursor by one word (delta -1 = backward, +1 =
// forward). A word is a maximal run of non-whitespace (unicode.IsSpace),
// matching the main InputReader's MoveWord semantics.
func (r *SteerInputReader) moveWord(delta int) {
	r.mu.Lock()
	pos := r.cursorPos
	buf := r.buffer
	if delta < 0 {
		// Skip whitespace backward.
		for pos > 0 {
			rr, sz := utf8.DecodeLastRune(buf[:pos])
			if unicode.IsSpace(rr) {
				pos -= sz
			} else {
				break
			}
		}
		// Skip non-whitespace backward.
		for pos > 0 {
			rr, sz := utf8.DecodeLastRune(buf[:pos])
			if !unicode.IsSpace(rr) {
				pos -= sz
			} else {
				break
			}
		}
	} else {
		// Skip whitespace forward.
		for pos < len(buf) {
			rr, sz := utf8.DecodeRune(buf[pos:])
			if unicode.IsSpace(rr) {
				pos += sz
			} else {
				break
			}
		}
		// Skip non-whitespace forward.
		for pos < len(buf) {
			rr, sz := utf8.DecodeRune(buf[pos:])
			if !unicode.IsSpace(rr) {
				pos += sz
			} else {
				break
			}
		}
	}
	r.cursorPos = pos
	r.mu.Unlock()
	r.renderLine()
}

// deleteWordBackward deletes the word before the cursor (Ctrl-W /
// Alt-Backspace). A no-op when the cursor is at the start or only
// whitespace precedes it.
func (r *SteerInputReader) deleteWordBackward() {
	r.mu.Lock()
	if r.cursorPos == 0 {
		r.mu.Unlock()
		return
	}
	pos := r.cursorPos
	buf := r.buffer
	// Skip whitespace backward.
	for pos > 0 {
		rr, sz := utf8.DecodeLastRune(buf[:pos])
		if unicode.IsSpace(rr) {
			pos -= sz
		} else {
			break
		}
	}
	// Skip non-whitespace backward.
	for pos > 0 {
		rr, sz := utf8.DecodeLastRune(buf[:pos])
		if !unicode.IsSpace(rr) {
			pos -= sz
		} else {
			break
		}
	}
	r.buffer = slices.Delete(r.buffer, pos, r.cursorPos)
	r.cursorPos = pos
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// killToEnd deletes from the cursor to the end of the buffer (Ctrl-K).
// A no-op when the cursor is already at the end.
func (r *SteerInputReader) killToEnd() {
	r.mu.Lock()
	if r.cursorPos >= len(r.buffer) {
		r.mu.Unlock()
		return
	}
	r.buffer = r.buffer[:r.cursorPos]
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// killToStart deletes from the start of the buffer to the cursor
// (Ctrl-U). A no-op when the cursor is already at the start.
func (r *SteerInputReader) killToStart() {
	r.mu.Lock()
	if r.cursorPos == 0 {
		r.mu.Unlock()
		return
	}
	r.buffer = slices.Delete(r.buffer, 0, r.cursorPos)
	r.cursorPos = 0
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// deleteForward deletes the rune at the cursor (Ctrl-D on non-empty).
// A no-op when the cursor is at the end.
func (r *SteerInputReader) deleteForward() {
	r.mu.Lock()
	if r.cursorPos >= len(r.buffer) {
		r.mu.Unlock()
		return
	}
	_, sz := utf8.DecodeRune(r.buffer[r.cursorPos:])
	r.buffer = slices.Delete(r.buffer, r.cursorPos, r.cursorPos+sz)
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// renderLine asks the footer to repaint the pinned input row with the
// current buffer and a mode-specific prefix. The prefix is included
// here (not in the footer) so the footer stays content-agnostic and
// any future modes don't require footer changes. The cursor byte
// offset is forwarded so the footer can render a caret at the correct
// position for mid-buffer editing.
//
// When Ctrl-R reverse-search is active, the line instead shows the
// search prompt (query + best match) so the user sees what they're
// searching for and what will be loaded on Enter.
func (r *SteerInputReader) renderLine() {
	if r.footer == nil {
		return
	}
	r.mu.Lock()

	if r.searchMode {
		// Render the reverse-search prompt in the steer line. The
		// caret sits at the end so the user sees where the next query
		// keystroke lands.
		var text string
		if r.searchResult != "" {
			display := strings.ReplaceAll(r.searchResult, "\n", "\\n")
			text = fmt.Sprintf("(search)'%s': %s", r.searchQuery, display)
		} else {
			text = fmt.Sprintf("(search)'%s': ", r.searchQuery)
		}
		prefix := SteerPromptPrefix
		if r.submitMode == SteerSubmitModeQueue {
			prefix = QueuePromptPrefix
		}
		r.mu.Unlock()
		r.footer.SetSteerLineWithCursor(fmt.Sprintf("%s%s", prefix, text), len(prefix)+len(text))
		return
	}

	text := string(r.buffer)
	cursor := r.cursorPos
	prefix := SteerPromptPrefix
	if r.submitMode == SteerSubmitModeQueue {
		prefix = QueuePromptPrefix
	}
	r.mu.Unlock()
	// cursor is a byte offset into the buffer; add the prefix byte
	// length to get the offset within the full rendered string.
	r.footer.SetSteerLineWithCursor(fmt.Sprintf("%s%s", prefix, text), len(prefix)+cursor)
}

// printExternalLocked prints a message in the scrollable area without
// disturbing the steer panel's pinned rows. Caller MUST hold outputMu.
//
// The scroll region is ALREADY set to exclude the pinned rows (footer +
// steer panel) by applyScrollRegionLocked when the steer reader
// started. Writing at the last row of that region and letting \n scroll
// keeps the pinned rows stationary — that's the whole point of scroll
// regions. We must NOT reset the region to full screen (\033[r) here:
// doing so makes the pinned rows part of the scrollable area, so the
// message's trailing \n scrolls the steer panel and footer up off the
// screen, destroying the terminal layout ("breaking the CLI").
//
// We bypass r.renderLine() here because it routes through
// footer.SetSteerLineWithCursor → footer.draw(), which re-acquires
// outputMu. We already hold outputMu (from PrintExternal), and
// sync.Mutex is non-reentrant, so the re-acquire would deadlock.
// Instead we set the footer's steer state directly and call
// drawLocked(), the lock-free variant.
func (r *SteerInputReader) printExternalLocked(msg string) {
	if r.footer == nil {
		fmt.Print(msg)
		return
	}
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	// Position the cursor at the bottom of the existing scrollable
	// area (just above the reserved rows). Do NOT reset the scroll
	// region — the region already protects the pinned rows, and
	// resetting it lets the message's \n scroll them away.
	_, rows := r.footer.terminalSize()
	reserved := r.footer.reservedRows()
	if rows > reserved+1 {
		fmt.Fprintf(r.footer.w, "\033[%d;1H", rows-reserved)
	}
	// Write the message. The trailing \n scrolls within the scroll
	// region, pushing prior conversation content up while the pinned
	// steer panel and footer stay fixed.
	fmt.Fprint(r.footer.w, msg)
	// Position the cursor at the bottom of the scrollable area again
	// (the \n above left it on the first pinned row).
	if rows > reserved+1 {
		fmt.Fprintf(r.footer.w, "\033[%d;1H", rows-reserved)
	}
	// Re-render the footer's pinned rows so the user sees their typed
	// buffer in the right place. Set the footer's steer state directly
	// (the same mutation renderLine → SetSteerLineWithCursor would do)
	// and call drawLocked, the lock-free variant — we hold outputMu and
	// footer.draw() would try to re-acquire it, deadlocking.
	r.mu.Lock()
	text := string(r.buffer)
	cursor := r.cursorPos
	prefix := SteerPromptPrefix
	if r.submitMode == SteerSubmitModeQueue {
		prefix = QueuePromptPrefix
	}
	r.mu.Unlock()
	r.footer.mu.Lock()
	r.footer.steerActive = true
	r.footer.steerLine = fmt.Sprintf("%s%s", prefix, text)
	r.footer.steerCursor = len(prefix) + cursor
	r.footer.mu.Unlock()
	r.footer.drawLocked()
}

// runExternalEditor opens $EDITOR (or VISUAL) with the current steer
// buffer pre-filled, lets the user edit it, and reads the result back
// into the buffer. Called from the readLoop goroutine — blocks until
// the editor exits. While blocked, the goroutine is not reading stdin,
// which is correct because the editor owns stdin during its run.
//
// Terminal lifecycle: we temporarily restore cooked mode (exitSteerMode
// or groundTruth) and disable bracketed-paste / modifyOtherKeys so the
// editor has a clean terminal. On return we re-enter steer mode and
// re-enable our modes so the readLoop resumes cleanly.
func (r *SteerInputReader) runExternalEditor() {
	editor := chooseExternalEditor()
	if editor == "" {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: no $VISUAL or $EDITOR set and no fallback available")
		return
	}

	// Snapshot the buffer for the temp file.
	r.mu.Lock()
	content := string(r.buffer)
	r.mu.Unlock()

	tmpPath, err := writeBufferToTempFile(content)
	if err != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: failed to stage buffer: %v", err)
		return
	}
	defer os.Remove(tmpPath)

	// Exit steer mode so the editor has a clean terminal (cooked mode,
	// no bracketed paste / modifyOtherKeys reporting).
	fmt.Fprint(os.Stderr, bracketedPasteDisable)
	fmt.Fprint(os.Stderr, modifyOtherKeysDisable)
	r.mu.Lock()
	oldState := r.oldState
	gt := r.groundTruth
	r.mu.Unlock()
	if gt != nil {
		_ = gt.Restore()
	} else if oldState != nil {
		_ = exitSteerMode(r.fd, oldState)
	}
	fmt.Fprintln(os.Stderr)

	// Run the editor. Blocks until the editor exits.
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()

	// Re-enter steer mode regardless of the editor's exit status so
	// the readLoop can resume reading keystrokes.
	st, enterErr := enterSteerMode(r.fd)
	if enterErr != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: failed to re-enter steer mode: %v", enterErr)
		return
	}
	r.mu.Lock()
	r.oldState = st
	r.mu.Unlock()
	fmt.Fprint(os.Stderr, bracketedPasteEnable)
	fmt.Fprint(os.Stderr, modifyOtherKeysEnable)

	if runErr != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: %s exited: %v", editor, runErr)
		r.renderLine()
		return
	}

	// Read back the edited content.
	fileContent, readErr := os.ReadFile(tmpPath)
	if readErr != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: failed to read back buffer: %v", readErr)
		r.renderLine()
		return
	}

	// Strip the trailing newline most editors append so the buffer
	// looks like the user typed exactly what they see.
	newContent := strings.TrimRight(string(fileContent), "\n")
	r.mu.Lock()
	r.buffer = []byte(newContent)
	r.cursorPos = len(r.buffer)
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// ─── Ctrl-R reverse-search methods (SP-048-4e parity) ──────────────────

// enterSearchMode starts a new reverse-history search, saving the current
// buffer/cursor so they can be restored on cancellation.
func (r *SteerInputReader) enterSearchMode() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.searchMode = true
	r.searchQuery = ""
	r.searchResult = ""
	r.searchResultIndex = -1
	r.searchBuf = r.searchBuf[:0]
	// Snapshot current buffer so Esc restores it.
	snap := make([]byte, len(r.buffer))
	copy(snap, r.buffer)
	r.preSearchBuffer = snap
	r.preSearchCursorPos = r.cursorPos
	// Show most recent history entry for empty query.
	if len(r.history) > 0 {
		r.searchResult = r.history[len(r.history)-1]
		r.searchResultIndex = len(r.history) - 1
	}
}

// exitSearchMode leaves reverse-search mode. When accept is true the
// current searchResult is loaded into the buffer; otherwise the
// pre-search state is restored.
func (r *SteerInputReader) exitSearchMode(accept bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if accept && r.searchResult != "" {
		r.buffer = []byte(r.searchResult)
		r.cursorPos = len(r.buffer)
		r.historyIndex = -1
		r.pendingBuffer = nil
	} else {
		r.buffer = r.preSearchBuffer
		r.cursorPos = r.preSearchCursorPos
	}
	r.searchMode = false
	r.searchQuery = ""
	r.searchResult = ""
	r.searchResultIndex = -1
	r.searchBuf = r.searchBuf[:0]
	r.preSearchBuffer = nil
	r.preSearchCursorPos = 0
}

// handleSearchBackspace trims one rune from the search query and
// re-runs the search.
func (r *SteerInputReader) handleSearchBackspace() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.searchBuf = r.searchBuf[:0]
	if len(r.searchQuery) > 0 {
		_, size := utf8.DecodeLastRuneInString(r.searchQuery)
		r.searchQuery = r.searchQuery[:len(r.searchQuery)-size]
	}
	r.refreshSearchForQueryLocked()
}

// refreshSearchForQuery re-runs the history search for the current query
// and updates searchResult / searchResultIndex. Must NOT hold r.mu.
func (r *SteerInputReader) refreshSearchForQuery() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshSearchForQueryLocked()
}

// refreshSearchForQueryLocked is the lock-free inner search. Caller
// must hold r.mu.
func (r *SteerInputReader) refreshSearchForQueryLocked() {
	if r.searchQuery == "" {
		if len(r.history) > 0 {
			r.searchResult = r.history[len(r.history)-1]
			r.searchResultIndex = len(r.history) - 1
		} else {
			r.searchResult = ""
			r.searchResultIndex = -1
		}
		return
	}
	// Search backwards through history for a case-insensitive match,
	// starting from the most recent entry.
	queryLower := strings.ToLower(r.searchQuery)
	startIdx := len(r.history) - 1
	for i := startIdx; i >= 0; i-- {
		if strings.Contains(strings.ToLower(r.history[i]), queryLower) {
			r.searchResult = r.history[i]
			r.searchResultIndex = i
			return
		}
	}
	r.searchResult = ""
	r.searchResultIndex = -1
}

// cycleSearchResult searches for the next older match with the current
// query. Called when the user presses Ctrl-R again while already in
// search mode.
func (r *SteerInputReader) cycleSearchResult() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.searchQuery == "" {
		// No query: cycle through history in order.
		if len(r.history) > 0 {
			idx := r.searchResultIndex - 1
			if idx < 0 {
				idx = len(r.history) - 1
			}
			r.searchResult = r.history[idx]
			r.searchResultIndex = idx
		}
		return
	}
	queryLower := strings.ToLower(r.searchQuery)
	startIdx := r.searchResultIndex - 1
	for i := startIdx; i >= 0; i-- {
		if strings.Contains(strings.ToLower(r.history[i]), queryLower) {
			r.searchResult = r.history[i]
			r.searchResultIndex = i
			return
		}
	}
	// No older match found — keep current result.
}
