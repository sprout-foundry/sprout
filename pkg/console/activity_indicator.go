package console

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
)

// spinnerFrames is the braille animation cycle used by ActivityIndicator.
// Ten frames at 80ms cadence gives a smooth 12.5 Hz rotation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerCadence = 80 * time.Millisecond

// ActivityIndicator renders a transient single-line spinner that updates on
// a timer. It's designed for the "agent is doing something" gap between user
// submit and visible output, and for showing per-tool progress during tool
// execution.
//
// All output goes to a Writer (default os.Stderr). When the writer is not a
// TTY, all methods are no-ops so piped or redirected output stays clean.
//
// The zero value is unusable — construct via NewActivityIndicator.
type ActivityIndicator struct {
	mu        sync.Mutex
	w         io.Writer
	isTTY     bool
	active    bool
	msg       string
	startedAt time.Time
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewActivityIndicator constructs an indicator that writes to w. If w is
// nil, os.Stderr is used. TTY detection runs against the underlying file
// descriptor; when w is not an *os.File it is treated as not-a-TTY.
func NewActivityIndicator(w io.Writer) *ActivityIndicator {
	if w == nil {
		w = os.Stderr
	}
	isTTY := false
	if f, ok := w.(*os.File); ok {
		isTTY = term.IsTerminal(int(f.Fd()))
	}
	return &ActivityIndicator{
		w:     w,
		isTTY: isTTY,
	}
}

// Start begins rendering the spinner with the given message. If the
// indicator is already active, the message is updated in place and the
// spinner continues from its current frame.
//
// msg should be a single line; embedded newlines and carriage returns are
// stripped to keep the render loop on one row.
func (a *ActivityIndicator) Start(msg string) {
	if a == nil || !a.isTTY {
		return
	}
	a.mu.Lock()
	msg = sanitizeLine(msg)
	if a.active {
		a.msg = msg
		a.mu.Unlock()
		return
	}
	a.active = true
	a.msg = msg
	a.startedAt = time.Now()
	a.stopCh = make(chan struct{})
	a.doneCh = make(chan struct{})
	a.mu.Unlock()

	go a.run()
}

// Update changes the spinner's message without restarting it. No-op if the
// indicator is not currently active.
func (a *ActivityIndicator) Update(msg string) {
	if a == nil || !a.isTTY {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.active {
		return
	}
	a.msg = sanitizeLine(msg)
}

// Stop halts the ticker and erases the spinner line. Idempotent — safe to
// call when the indicator is already stopped.
func (a *ActivityIndicator) Stop() {
	if a == nil || !a.isTTY {
		return
	}
	a.mu.Lock()
	if !a.active {
		a.mu.Unlock()
		return
	}
	a.active = false
	stopCh := a.stopCh
	doneCh := a.doneCh
	a.mu.Unlock()

	close(stopCh)
	<-doneCh

	// \r returns the cursor to column 0; \033[K clears to end-of-line.
	fmt.Fprint(a.w, "\r\033[K")
}

// Replace atomically stops the spinner and prints line in its place,
// terminated with a newline. Use this when a transient spinner should
// resolve into a permanent result line (e.g. ✓ tool · 0.3s).
//
// On a non-TTY writer, line is still printed (so non-interactive logs still
// see the resolved result).
func (a *ActivityIndicator) Replace(line string) {
	if a == nil {
		return
	}
	if a.isTTY {
		a.Stop()
	}
	fmt.Fprintln(a.w, line)
}

// ReplaceLast is shorthand for ReplaceLastN(line, 1).
func (a *ActivityIndicator) ReplaceLast(line string) {
	a.ReplaceLastN(line, 1)
}

// ReplaceLastN stops the spinner and OVERWRITES the previous N rows
// before printing line. Used by the tool-collapse subscriber to merge
// a series of identical tool-end lines (separated by spinner-frame
// blank rows) into a single "✓ read_file × N (foo.go, bar.go, …)" line
// updated in place.
//
// Caller is responsible for knowing N matches the actual row layout:
//   - n=1: overwrites the immediately preceding row (e.g. a spinner)
//   - n=2: overwrites the prev row + the blank line above it (the
//     pattern emitted between consecutive tool spinners in the CLI's
//     ToolStart path)
//
// Only safe to call when the caller knows those rows belong to this
// indicator — no streaming text or unrelated chrome has written there
// since. On a non-TTY writer this degenerates to a regular Fprintln so
// logs still show each iteration (slightly noisier but never corrupted).
func (a *ActivityIndicator) ReplaceLastN(line string, n int) {
	if a == nil {
		return
	}
	if !a.isTTY {
		fmt.Fprintln(a.w, line)
		return
	}
	a.Stop()
	if n < 1 {
		n = 1
	}
	// \033[F moves cursor to start of previous line; \033[K clears from
	// cursor to end of line. Repeat n times to walk up and erase.
	for i := 0; i < n; i++ {
		fmt.Fprint(a.w, "\033[F\033[K")
	}
	fmt.Fprintln(a.w, line)
}

// Elapsed returns how long the current spinner has been running. Returns
// zero if the indicator is not active.
func (a *ActivityIndicator) Elapsed() time.Duration {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.active {
		return 0
	}
	return time.Since(a.startedAt)
}

// IsActive reports whether the spinner is currently rendering.
func (a *ActivityIndicator) IsActive() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active
}

func (a *ActivityIndicator) run() {
	ticker := time.NewTicker(spinnerCadence)
	defer ticker.Stop()
	defer close(a.doneCh)

	// Render the first frame immediately so the user sees something within
	// 0ms rather than waiting for the first tick.
	a.render(0)
	frame := 1

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.render(frame)
			frame = (frame + 1) % len(spinnerFrames)
		}
	}
}

func (a *ActivityIndicator) render(frame int) {
	a.mu.Lock()
	if !a.active {
		a.mu.Unlock()
		return
	}
	msg := a.msg
	elapsed := time.Since(a.startedAt)
	a.mu.Unlock()
	fmt.Fprintf(a.w, "\r\033[K%s %s (%.1fs)", spinnerFrames[frame], msg, elapsed.Seconds())
}

// sanitizeLine strips newlines and carriage returns so the spinner always
// renders on a single row.
func sanitizeLine(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// Global indicator registration so far-flung CLI prompt sites can suspend
// the active spinner without taking a direct dependency on whatever owns
// the indicator. Use RegisterGlobalIndicator from the CLI entry point.
// Code that cannot import pkg/console (e.g. pkg/utils, pkg/agent_tools)
// suspends via the leaf-only bridge in pkg/clihooks.
var (
	globalIndicator   *ActivityIndicator
	globalIndicatorMu sync.RWMutex
)

// RegisterGlobalIndicator installs ind as the process-wide indicator that
// SuspendIndicator and clihooks.SuspendIndicator both target. Pass nil to
// clear. Safe to call multiple times.
func RegisterGlobalIndicator(ind *ActivityIndicator) {
	globalIndicatorMu.Lock()
	defer globalIndicatorMu.Unlock()
	globalIndicator = ind
	if ind != nil {
		clihooks.SetSuspendIndicator(ind.Stop)
	} else {
		clihooks.SetSuspendIndicator(nil)
	}
}

// SuspendIndicator stops the registered global activity indicator if one is
// active. Safe to call when no indicator is registered (no-op) or when the
// indicator is already stopped (idempotent). Use this immediately before
// rendering an interactive CLI prompt to keep the spinner from overwriting
// the prompt text. Mirrored by clihooks.SuspendIndicator for callers that
// can't import pkg/console.
func SuspendIndicator() {
	globalIndicatorMu.RLock()
	a := globalIndicator
	globalIndicatorMu.RUnlock()
	a.Stop()
}
