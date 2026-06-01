package console

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

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
// re-implemented by reading the byte directly):
//
//	Enter (CR/LF)   → submitFn(buffer); buffer cleared
//	Backspace (DEL) → remove last byte
//	Escape (alone)  → clear buffer (does not exit steer mode)
//	Ctrl+C  (0x03)  → interruptFn() — caller routes to TriggerInterrupt
//	Ctrl+D  (0x04)  → ignored (no EOF on a transient channel)
//	ESC [ ... ~/A-Z → swallow common escape-sequence (arrows, fn keys)
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

	r.renderLine()
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
	// without bracketed paste enabled).
	fmt.Fprint(os.Stderr, bracketedPasteDisable)

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
	r.historyIndex = -1
	r.pendingBuffer = nil
}

// readLoop is the input-handling goroutine. Steer mode sets VMIN=0
// and VTIME=0 on the termios (see steer_termios_*.go) so Read returns
// immediately with 0 bytes when nothing is ready — no need for an
// O_NONBLOCK file descriptor flag. The poll interval (5ms) is short
// enough that typing feels instantaneous and Stop() observes the exit
// signal within one frame.
func (r *SteerInputReader) readLoop(stopCh, doneCh chan struct{}) {
	defer close(doneCh)

	var buf [1]byte
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		n, err := os.Stdin.Read(buf[:])
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
			case <-ticker.C:
			}
			continue
		}

		b := buf[0]

		// While a bracketed paste is in flight, ALL bytes go into
		// pasteBuf verbatim — including newlines and what would
		// otherwise be control characters. The only escape is the
		// "ESC [ 201~" terminator, which we detect inline.
		r.mu.Lock()
		inPaste := r.pasteActive
		r.mu.Unlock()
		if inPaste {
			if b == 0x1B {
				// Likely the start of the paste-end sequence
				// (ESC [ 201~). Let the escape handler peek at
				// the follow-up bytes to confirm.
				r.handleEscapeOrSequence()
				continue
			}
			r.appendPasteByte(b)
			continue
		}

		switch {
		case b == 0x03: // Ctrl+C
			r.handleInterrupt()
		case b == 0x04: // Ctrl+D — ignore (no EOF on a steer channel)
		case b == 0x09: // Tab — toggle submit mode (SP-055 Phase 3b)
			r.toggleSubmitMode()
		case b == 0x0D, b == 0x0A: // Enter (CR or LF)
			r.handleSubmit()
		case b == 0x1B: // Escape — could be plain ESC or sequence prefix
			r.handleEscapeOrSequence()
		case b == 0x7F, b == 0x08: // DEL or backspace
			r.handleBackspace()
		case b >= 0x20 && b < 0x7F: // printable ASCII
			r.handlePrintable(b)
		case b >= 0xC0: // UTF-8 lead byte (multi-byte rune)
			r.handleUTF8Lead(b)
		default:
			// Lone continuation bytes (0x80..0xBF) and other control
			// bytes are dropped — they shouldn't arrive standalone in
			// a well-formed UTF-8 stream.
		}
	}
}

// handleInterrupt fires the user-provided callback (typically wired to
// chatAgent.TriggerInterrupt) and clears the buffer. Stays active —
// the user can keep typing or hit Ctrl+C again.
func (r *SteerInputReader) handleInterrupt() {
	r.mu.Lock()
	r.buffer = r.buffer[:0]
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

// handleEscapeOrSequence handles three cases:
//  1. Plain ESC (no follow-up byte) → clear the buffer (cancel typing).
//  2. ESC `[` <final-byte> (CSI sequence) → dispatch to history
//     navigation for arrow keys (`A` up, `B` down) and drain anything
//     else (left/right, function keys, etc.).
//  3. Bracketed-paste markers (ESC [ 200 ~ ... ESC [ 201 ~) — drained
//     as ordinary CSI sequences; the inner content arrives as
//     printable bytes that go through handlePrintable.
//
// Stdin is already non-blocking (set in readLoop), so a quick Read
// returns immediately. If the next byte isn't ready within a short
// poll, we treat ESC as standalone.
func (r *SteerInputReader) handleEscapeOrSequence() {
	clearBuffer := func() {
		r.mu.Lock()
		r.buffer = r.buffer[:0]
		r.historyIndex = -1
		r.pendingBuffer = nil
		r.mu.Unlock()
		r.renderLine()
	}

	// Brief wait window for the second byte of an arrow-key sequence.
	// Real arrow keys arrive in a tight ESC[<letter> burst (<1ms);
	// 20ms is comfortably above that without making plain ESC feel
	// laggy.
	deadline := time.Now().Add(20 * time.Millisecond)
	var probe [1]byte
	var got byte
	for time.Now().Before(deadline) {
		n, _ := os.Stdin.Read(probe[:])
		if n > 0 {
			got = probe[0]
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got == 0 {
		// No follow-up byte — plain ESC.
		clearBuffer()
		return
	}
	if got != '[' {
		// Not a recognized CSI sequence. Clear + drop the second byte
		// (a follow-up edge case not worth a putback queue in v1).
		clearBuffer()
		return
	}
	// ESC `[` — read bytes until a final byte (0x40..0x7E) terminates
	// the CSI sequence per ECMA-48. Cap the drain at a few bytes so
	// a malformed sequence can't hang us. The final byte determines
	// the action. We also capture parameter bytes so we can recognize
	// the bracketed-paste markers (ESC [ 200~ start, ESC [ 201~ end).
	var params []byte
	drainDeadline := time.Now().Add(20 * time.Millisecond)
	for i := 0; i < 8 && time.Now().Before(drainDeadline); i++ {
		var b [1]byte
		n, _ := os.Stdin.Read(b[:])
		if n == 0 {
			time.Sleep(1 * time.Millisecond)
			continue
		}
		if b[0] >= 0x40 && b[0] <= 0x7E {
			// Bracketed-paste markers are parameterized with 200/201
			// and terminate with '~'. Handle them inline so the normal
			// CSI dispatch (arrow keys etc.) stays focused.
			if b[0] == '~' {
				switch string(params) {
				case "200":
					r.beginPaste()
					return
				case "201":
					r.endPaste()
					return
				}
			}
			r.dispatchCSIFinal(b[0])
			return
		}
		// Parameter/intermediate bytes (0x30..0x3F, 0x20..0x2F) keep
		// the sequence going.
		if b[0] >= 0x30 && b[0] <= 0x3F {
			params = append(params, b[0])
		}
	}
}

// beginPaste enters bracketed-paste accumulation mode. All bytes that
// arrive between now and endPaste() are appended verbatim to pasteBuf.
func (r *SteerInputReader) beginPaste() {
	r.mu.Lock()
	r.pasteActive = true
	r.pasteBuf = r.pasteBuf[:0]
	r.mu.Unlock()
}

// endPaste finalizes a bracketed paste: appends the accumulated bytes
// to the live buffer and re-renders the pinned line. Pasted content
// keeps embedded newlines so multi-line code/log snippets survive — on
// submit the model receives the exact text the user pasted.
func (r *SteerInputReader) endPaste() {
	r.mu.Lock()
	paste := string(r.pasteBuf)
	r.pasteBuf = r.pasteBuf[:0]
	r.pasteActive = false
	r.buffer = append(r.buffer, paste...)
	r.hasEditedHistory()
	r.mu.Unlock()
	r.renderLine()
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

// dispatchCSIFinal acts on the terminator byte of a CSI sequence we
// just drained. Only arrow keys are wired today; other final bytes are
// swallowed silently.
func (r *SteerInputReader) dispatchCSIFinal(final byte) {
	switch final {
	case 'A': // Up arrow — recall previous history entry.
		r.recallHistory(-1)
	case 'B': // Down arrow — advance toward in-progress buffer.
		r.recallHistory(+1)
	default:
		// Left/right/Home/End/Pg... not yet wired.
	}
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

// handleBackspace removes the last RUNE (not byte) from the buffer
// so a single backspace on a multi-byte character — Greek "α", Han
// "字", emoji "🚀" — deletes the whole glyph rather than corrupting
// it. Walks backward from the end skipping UTF-8 continuation bytes
// (10xxxxxx) until it finds a lead byte (or ASCII) to drop.
//
// Also exits history navigation: editing a recalled entry treats it
// as a fresh in-progress message.
func (r *SteerInputReader) handleBackspace() {
	r.mu.Lock()
	if n := len(r.buffer); n > 0 {
		// Find the start of the last rune by walking back over
		// continuation bytes (0x80..0xBF).
		i := n - 1
		for i > 0 && r.buffer[i]&0xC0 == 0x80 {
			i--
		}
		r.buffer = r.buffer[:i]
	}
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// handlePrintable appends a printable ASCII byte to the buffer and
// redraws. Typing exits "history navigation" mode: the buffer becomes
// editable from the recalled state and the next Up arrow starts fresh.
func (r *SteerInputReader) handlePrintable(b byte) {
	r.mu.Lock()
	r.buffer = append(r.buffer, b)
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// handleUTF8Lead handles the start of a multi-byte UTF-8 sequence
// (SP-055 Phase 3c). The lead byte's high bits encode the total
// sequence length: 110xxxxx → 2 bytes, 1110xxxx → 3 bytes,
// 11110xxx → 4 bytes. We read the remaining continuation bytes
// (which all begin with 10xxxxxx) and only commit the rune to the
// buffer once it's complete — so partial reads never render a
// half-character on screen.
//
// Reads the continuation bytes via the same VMIN=0 polling stdin
// the main loop uses. If a continuation never arrives within a short
// window (malformed input, paste interrupted), the partial sequence
// is dropped.
func (r *SteerInputReader) handleUTF8Lead(lead byte) {
	var need int
	switch {
	case lead&0xE0 == 0xC0:
		need = 1 // total 2 bytes
	case lead&0xF0 == 0xE0:
		need = 2 // total 3 bytes
	case lead&0xF8 == 0xF0:
		need = 3 // total 4 bytes
	default:
		return // invalid lead byte
	}

	seq := make([]byte, 0, need+1)
	seq = append(seq, lead)

	deadline := time.Now().Add(20 * time.Millisecond)
	for len(seq) < need+1 && time.Now().Before(deadline) {
		var b [1]byte
		n, _ := os.Stdin.Read(b[:])
		if n == 0 {
			time.Sleep(1 * time.Millisecond)
			continue
		}
		// Continuation bytes must be 10xxxxxx.
		if b[0]&0xC0 != 0x80 {
			return // malformed; drop the whole sequence
		}
		seq = append(seq, b[0])
	}
	if len(seq) != need+1 {
		return // timed out
	}

	r.mu.Lock()
	r.buffer = append(r.buffer, seq...)
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// renderLine asks the footer to repaint the pinned input row with the
// current buffer and a mode-specific prefix. The prefix is included
// here (not in the footer) so the footer stays content-agnostic and
// any future modes don't require footer changes.
func (r *SteerInputReader) renderLine() {
	if r.footer == nil {
		return
	}
	r.mu.Lock()
	text := string(r.buffer)
	prefix := SteerPromptPrefix
	if r.submitMode == SteerSubmitModeQueue {
		prefix = QueuePromptPrefix
	}
	r.mu.Unlock()
	r.footer.SetSteerLine(fmt.Sprintf("%s%s", prefix, text))
}
