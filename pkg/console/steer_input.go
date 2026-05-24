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
	submitFn    func(string)
	interruptFn func()

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
}

// SteerHistoryCap bounds the in-memory steer history to a sensible
// session-level value. Exposed for testing / config.
const SteerHistoryCap = 50

// SteerPromptPrefix is the visible glyph + space rendered at the start
// of the pinned input line. Exposed for testing / theming.
const SteerPromptPrefix = "⇄ steer › "

// NewSteerInputReader builds a reader that draws into the given footer
// and reports submitted/interrupt events via the callbacks. The
// callbacks fire on the reader's read goroutine — keep them quick or
// dispatch to another goroutine to avoid blocking the input loop.
func NewSteerInputReader(footer *StatusFooter, submitFn, interruptFn func(string)) *SteerInputReader {
	// submitFn is the natural callback; interruptFn takes no string but
	// uses the same signature so callers can use a single closure shape.
	r := &SteerInputReader{
		footer:   footer,
		submitFn: submitFn,
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
	r.buffer = r.buffer[:0]
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
	if oldState != nil {
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
		switch {
		case b == 0x03: // Ctrl+C
			r.handleInterrupt()
		case b == 0x04: // Ctrl+D — ignore (no EOF on a steer channel)
		case b == 0x0D, b == 0x0A: // Enter (CR or LF)
			r.handleSubmit()
		case b == 0x1B: // Escape — could be plain ESC or sequence prefix
			r.handleEscapeOrSequence()
		case b == 0x7F, b == 0x08: // DEL or backspace
			r.handleBackspace()
		case b >= 0x20 && b < 0x7F: // printable ASCII
			r.handlePrintable(b)
		default:
			// Other control bytes: ignore. UTF-8 multi-byte sequences
			// are not yet supported in v1 — printable ASCII covers the
			// common case (English typing). Future polish.
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

// handleSubmit fires the submit callback with the current buffer and
// clears the line. Empty submissions are dropped (no-op) so users can
// hit Enter on an empty line without sending noise to the agent.
// Non-empty submissions are appended to the steer history ring so
// up-arrow recall works.
func (r *SteerInputReader) handleSubmit() {
	r.mu.Lock()
	text := string(r.buffer)
	r.buffer = r.buffer[:0]
	cb := r.submitFn
	r.mu.Unlock()
	r.renderLine()
	if text == "" {
		return
	}
	r.appendHistory(text)
	if cb != nil {
		cb(text)
	}
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
	// the action.
	drainDeadline := time.Now().Add(20 * time.Millisecond)
	for i := 0; i < 8 && time.Now().Before(drainDeadline); i++ {
		var b [1]byte
		n, _ := os.Stdin.Read(b[:])
		if n == 0 {
			time.Sleep(1 * time.Millisecond)
			continue
		}
		if b[0] >= 0x40 && b[0] <= 0x7E {
			r.dispatchCSIFinal(b[0])
			return
		}
		// Parameter/intermediate bytes (0x30..0x3F, 0x20..0x2F) keep
		// the sequence going; we don't care about the parameters for
		// the limited set of keys we react to.
	}
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

// handleBackspace removes the last byte from the buffer if any, then
// redraws the line. Also exits history navigation: editing a recalled
// entry treats it as a fresh in-progress message.
func (r *SteerInputReader) handleBackspace() {
	r.mu.Lock()
	if n := len(r.buffer); n > 0 {
		r.buffer = r.buffer[:n-1]
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

// renderLine asks the footer to repaint the pinned input row with the
// current buffer. The prefix glyph is included here so the footer
// stays content-agnostic.
func (r *SteerInputReader) renderLine() {
	if r.footer == nil {
		return
	}
	r.mu.Lock()
	text := string(r.buffer)
	r.mu.Unlock()
	r.footer.SetSteerLine(fmt.Sprintf("%s%s", SteerPromptPrefix, text))
}
