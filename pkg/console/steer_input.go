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

	// oldState is the pre-MakeRaw termios state, restored on Stop.
	oldState *term.State
}

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

	st, err := term.MakeRaw(r.fd)
	if err != nil {
		// Couldn't enter raw mode — degrade to no-op rather than fight
		// the terminal. Caller's session continues to work, just without
		// the steer affordance.
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

// Stop restores cooked mode, clears the pinned line, and signals the
// read goroutine to exit. Idempotent. MUST be called on every exit
// path (including signal-driven shutdown) or the terminal will be left
// in raw mode.
//
// Note: we do NOT wait for the read goroutine to exit. The polling
// loop checks stopCh on every iteration with a short tick (5ms), so it
// exits within a frame of the signal. Waiting on doneCh would
// deadlock when the goroutine is blocked on Read — which is the
// common case at Stop time.
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
	oldState := r.oldState
	r.oldState = nil
	r.mu.Unlock()

	close(stopCh)
	if oldState != nil {
		_ = term.Restore(r.fd, oldState)
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

// readLoop is the input-handling goroutine. Polls stdin in
// non-blocking mode so it can observe stopCh between reads. The poll
// interval is short enough (5ms) that typing feels instantaneous.
func (r *SteerInputReader) readLoop(stopCh, doneCh chan struct{}) {
	defer close(doneCh)

	// Put stdin in non-blocking mode for the reader's lifetime. The
	// defer restores blocking-mode so the primary InputReader's
	// MakeRaw cycle starts from a clean state.
	if err := setNonblock(r.fd, true); err != nil {
		// Fall back to blocking reads — Stop()'s terminal-restore will
		// still eventually wake us, just with one byte of latency.
	} else {
		defer setNonblock(r.fd, false)
	}

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
	if cb != nil {
		cb(text)
	}
}

// handleEscapeOrSequence handles both plain ESC (clear buffer) and ESC
// `[` sequences (arrow keys, function keys). Plain ESC is the v1 "clear
// what I'm typing" affordance; arrow-key sequences are swallowed so they
// don't insert literal `[A` etc. into the buffer.
//
// Stdin is already non-blocking (set in readLoop), so a quick Read
// returns immediately. If the next byte isn't ready within a short
// poll, we treat ESC as standalone.
func (r *SteerInputReader) handleEscapeOrSequence() {
	clearBuffer := func() {
		r.mu.Lock()
		r.buffer = r.buffer[:0]
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
	// ESC `[` — drain until a final byte (0x40..0x7E) which terminates
	// CSI sequences per ECMA-48. Cap the drain at a few bytes so a
	// malformed sequence can't hang us.
	drainDeadline := time.Now().Add(20 * time.Millisecond)
	for i := 0; i < 8 && time.Now().Before(drainDeadline); i++ {
		var b [1]byte
		n, _ := os.Stdin.Read(b[:])
		if n == 0 {
			time.Sleep(1 * time.Millisecond)
			continue
		}
		if b[0] >= 0x40 && b[0] <= 0x7E {
			return // terminator byte
		}
	}
}

// handleBackspace removes the last byte from the buffer if any, then
// redraws the line.
func (r *SteerInputReader) handleBackspace() {
	r.mu.Lock()
	if n := len(r.buffer); n > 0 {
		r.buffer = r.buffer[:n-1]
	}
	r.mu.Unlock()
	r.renderLine()
}

// handlePrintable appends a printable ASCII byte to the buffer and
// redraws.
func (r *SteerInputReader) handlePrintable(b byte) {
	r.mu.Lock()
	r.buffer = append(r.buffer, b)
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
