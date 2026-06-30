package computer_use

import (
	"errors"
	"os"
	"sync"
	"time"
)

// ErrPanicKeyHalted is returned by all ComputerBackend methods when the panic
// key has been triggered and computer-use actions are blocked.
var ErrPanicKeyHalted = errors.New("computer-use halted by panic key")

// ErrAlreadyHalted is returned by Halt() when the panic key is triggered a
// second time while already halted.
var ErrAlreadyHalted = errors.New("computer-use already halted")

// processStartedHook / processFinishedHook are mutex-protected function
// pointers used by PanicableBackend to track the in-flight subprocess for
// kill-on-halt. They are set by NewPanicableBackend and cleared when the
// decorator is replaced. Read/write under hooksMu.
var (
	hooksMu             sync.Mutex
	processStartedHook  func(*os.Process)
	processFinishedHook func()
)

// globalPanicableBackend is the singleton PanicableBackend used by external
// callers (CLI chord handler, future signal handler) to trigger the panic key
// after RegisterComputerUseTools has set up the decorator chain. Nil when
// computer use is disabled or not yet registered.
var globalPanicableBackend *PanicableBackend

// TriggerPanicKey fires the panic key on the registered backend. It is a
// no-op when no PanicableBackend has been registered (e.g. computer use is
// disabled). Returns nil on first trigger, ErrAlreadyHalted on subsequent
// triggers. The reason is recorded to the audit log.
func TriggerPanicKey(reason string) error {
	p := GlobalPanicable()
	if p == nil {
		return nil
	}
	return p.Halt(reason)
}

// GlobalPanicable returns the registered PanicableBackend, or nil if none has
// been registered. Exported for tests and any future signal handler that needs
// direct access.
func GlobalPanicable() *PanicableBackend {
	return globalPanicableBackend
}

// PanicableBackend wraps a ComputerBackend so that in-flight subprocess
// actions can be killed when the panic key is pressed. It sits between the
// subprocess backend and the rate-limit decorator:
//
//	real → panicable → rateLimited → auditing
type PanicableBackend struct {
	inner ComputerBackend

	mu         sync.Mutex
	halted     bool
	haltReason string
	haltedAt   time.Time
	now        func() time.Time

	// currentProcess tracks the in-flight subprocess for kill-on-halt.
	currentProcess *os.Process
}

// NewPanicableBackend wraps inner with panic-key support. It installs
// package-level hooks so that the subprocess backend's runWithCtx can
// report the in-flight process to this decorator.
func NewPanicableBackend(inner ComputerBackend) *PanicableBackend {
	// Clear the old backend's hooks so a discarded PanicableBackend doesn't
	// leak its callbacks (SHOULD_FIX #1).
	if prev := globalPanicableBackend; prev != nil {
		hooksMu.Lock()
		processStartedHook = nil
		processFinishedHook = nil
		hooksMu.Unlock()
	}
	p := &PanicableBackend{
		inner: inner,
		now:   time.Now,
	}
	// Install the hooks so subprocessBackend.runWithCtx notifies us.
	hooksMu.Lock()
	processStartedHook = p.trackProcessStarted
	processFinishedHook = p.trackProcessFinished
	hooksMu.Unlock()
	// Expose this backend to external callers (CLI chord handler, etc.).
	// Last-registered-wins — re-registration replaces the handle.
	globalPanicableBackend = p
	return p
}

func (p *PanicableBackend) trackProcessStarted(proc *os.Process) {
	p.mu.Lock()
	p.currentProcess = proc
	p.mu.Unlock()
}

func (p *PanicableBackend) trackProcessFinished() {
	p.mu.Lock()
	p.currentProcess = nil
	p.mu.Unlock()
}

// Halt signals the panic key was pressed. It kills any in-flight subprocess
// and records the halt state. Subsequent actions return ErrPanicKeyHalted.
// Calling Halt() while already halted returns ErrAlreadyHalted and records
// a "panic_key_duplicate" audit event.
func (p *PanicableBackend) Halt(reason string) error {
	p.mu.Lock()
	if p.halted {
		originalHaltAt := p.haltedAt
		p.mu.Unlock()

		RecordSafetyEvent("panic_key_duplicate", map[string]any{
			"reason":           reason,
			"original_halt_at": originalHaltAt.UTC().Format(time.RFC3339),
		})
		return ErrAlreadyHalted
	}

	p.halted = true
	p.haltReason = reason
	p.haltedAt = p.now()

	// Snapshot the process reference under lock, then clear it.
	proc := p.currentProcess
	p.currentProcess = nil
	p.mu.Unlock()

	// Kill the in-flight subprocess group outside the lock.
	if proc != nil {
		_ = KillProcessGroup(proc)
	}

	RecordSafetyEvent("panic_key_triggered", map[string]any{
		"reason": reason,
	})
	return nil
}

// IsHalted reports whether the panic key has been triggered.
func (p *PanicableBackend) IsHalted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.halted
}

// HaltReason returns the reason string passed to the most recent Halt() call.
// Safe to call concurrently.
func (p *PanicableBackend) HaltReason() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.haltReason
}

// HaltedAt returns the time when Halt() was most recently called.
// Safe to call concurrently.
func (p *PanicableBackend) HaltedAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.haltedAt
}

// Reset clears the halted state after the user acknowledges the halt.
// Records a "panic_key_reset" audit event. No-op (with no audit event) when
// the backend is not currently halted.
func (p *PanicableBackend) Reset() {
	p.mu.Lock()
	if !p.halted {
		p.mu.Unlock()
		return
	}
	haltDuration := time.Since(p.haltedAt).Milliseconds()
	reason := p.haltReason
	p.halted = false
	p.haltReason = ""
	p.haltedAt = time.Time{}
	p.currentProcess = nil
	p.mu.Unlock()

	RecordSafetyEvent("panic_key_reset", map[string]any{
		"halt_duration_ms": haltDuration,
		"halt_reason":      reason,
	})
}

// --- ComputerBackend delegation ---

func (p *PanicableBackend) Screenshot(region *Rect) ([]byte, Size, error) {
	if p.IsHalted() {
		return nil, Size{}, ErrPanicKeyHalted
	}
	return p.inner.Screenshot(region)
}

func (p *PanicableBackend) MouseClick(x, y int, button MouseButton, double bool) error {
	if p.IsHalted() {
		return ErrPanicKeyHalted
	}
	return p.inner.MouseClick(x, y, button, double)
}

func (p *PanicableBackend) MouseDrag(from, to Point, button MouseButton) error {
	if p.IsHalted() {
		return ErrPanicKeyHalted
	}
	return p.inner.MouseDrag(from, to, button)
}

func (p *PanicableBackend) MoveTo(x, y int) error {
	if p.IsHalted() {
		return ErrPanicKeyHalted
	}
	return p.inner.MoveTo(x, y)
}

func (p *PanicableBackend) KeyboardType(text string) error {
	if p.IsHalted() {
		return ErrPanicKeyHalted
	}
	return p.inner.KeyboardType(text)
}

func (p *PanicableBackend) KeyboardPress(key string) error {
	if p.IsHalted() {
		return ErrPanicKeyHalted
	}
	return p.inner.KeyboardPress(key)
}

func (p *PanicableBackend) Scroll(dir ScrollDir, amount int, at *Point) error {
	if p.IsHalted() {
		return ErrPanicKeyHalted
	}
	return p.inner.Scroll(dir, amount, at)
}
