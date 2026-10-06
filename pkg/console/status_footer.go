package console

import (
	"io"
	"os"
	"sync"
	"sync/atomic"
)

// footerResetAll resets all ANSI formatting. Used by drawLocked to
// terminate each footer row so color codes don't leak into subsequent
// terminal output.
const footerResetAll = "\033[0m"

// StatusFooter renders a single pinned line at the bottom of the terminal
// showing live session state: model, context-window usage, cumulative cost,
// and working directory.
//
// Mechanism: when started, the footer sets a terminal scroll region of
// rows 1..(N-1) where N is the terminal height. Subsequent output scrolls
// within that region; row N stays put for the footer. On Stop (and on
// signal-driven shutdown) the scroll region is reset so the user's
// terminal isn't left in a broken state.
//
// Suppressed entirely on non-TTY writers — Render is a no-op, scroll
// region is never touched.
type StatusFooter struct {
	mu     sync.Mutex
	w      io.Writer
	isTTY  bool
	fd     int
	active bool
	source ContentSource

	// sizeOverride pins terminalSize for tests (no pty needed). nil in
	// production.
	sizeOverride *terminalSizeOverride
	// resizeSnapshot pins terminalSize for the duration of one Resize pass,
	// so every step of the pass works from the same geometry even if the
	// window keeps changing (a drag sends a burst of SIGWINCH).
	resizeSnapshot atomic.Pointer[terminalSizeOverride]

	// lastRows remembers the terminal height at the most recent draw so
	// that a resize handler can clear the OLD footer rows (which would
	// otherwise be orphaned mid-screen after a grow) before applying a
	// fresh scroll region for the new dimensions.
	lastRows int

	// SP-055: optional steer input line. When steerActive is true the
	// footer reserves additional pinned rows above the existing rule
	// (N-1) and content (N) — one row per visual line of the steer
	// buffer, capped at maxSteerRows. steerLine is the literal buffer
	// text (with embedded `\n` for line breaks) supplied by
	// SteerInputReader; the footer splits it into rows at draw time.
	steerActive bool
	steerLine   string
	// steerCursor is the byte offset within steerLine where the input
	// caret (▏) should be rendered. -1 (default) means "at end" for
	// backward compat with SetSteerLine. Set by SetSteerLineWithCursor.
	steerCursor int
	// lastSteerRows is the row count we drew last time. Used to detect
	// when the row count changed (user added/removed a newline) so we
	// can reapply the scroll region and blank any orphaned rows.
	lastSteerRows int
	// lastHintRows is the hint row count we drew last time (0 or 1).
	// Used by Resize/Stop to clear the old hint row when it was present.
	lastHintRows int
	// appliedReserved / appliedRows record the DECSTBM margins last sent,
	// so shiftScrollRegionLocked can tell how far the pinned block grew or
	// shrank since then.
	appliedReserved int
	appliedRows     int
	// composerMode is what the pinned input box does on Enter; it picks
	// the box's label, accent and placeholder and the hint row's wording.
	composerMode ComposerMode

	// SP-115: keyboard shortcut hint row. When showKeymapHint is true
	// the footer reserves an extra pinned row above the rule to display
	// registered keybindings (e.g. "Alt+T breakdown · Alt+V verbose").
	showKeymapHint bool
	// hintOverride, when set, replaces the hint row's wording (an open
	// picker shows its own keys there).
	hintOverride string

	// SP-078 Phase 1: steerWrappedActive selects the width-aware
	// WrapSteerLayout render path in drawLocked (instead of the legacy
	// byte-offset steerCursor + splitSteerLines path). steerCursorRow
	// and steerCursorCol record the caller-requested caret (0-based
	// row/col into the visual row array) for callers that need to
	// query it; drawLocked itself re-derives caret placement from the
	// buffer, which — because the dropdown and steer input only render
	// with the cursor at end-of-line — lands on the same cell. Set by
	// SetSteerLineWrapped; cleared by SetSteerLine / SetSteerLineWithCursor.
	steerCursorRow     int
	steerCursorCol     int
	steerWrappedActive bool

	winchStop chan struct{}
	winchDone chan struct{}

	// resizePollerStop halts the platform resize watcher (SIGWINCH on Unix,
	// poll timer on Windows). Stored so Stop can clean it up.
	resizePollerStop func()

	// lastCols is the terminal column count at the most recent draw.
	// Used by Resize to compute how many wrapped overflow rows the old
	// (wider) content occupies at the new (narrower) width, so the clear
	// can start high enough to catch them all.
	lastCols int

	// proseStreaming is set by the AssistantTurnRenderer while prose
	// chunks are actively being written. When true, Refresh() skips
	// the draw to avoid DEC save/restore (\0337/\0338) racing with
	// cursor movement in the scroll region — the saved position goes
	// stale when content scrolls between save and restore, scattering
	// prose characters across the screen.
	proseStreaming bool

	// pendingResize is set when a SIGWINCH arrives during active prose
	// streaming. Resize() defers scroll-region manipulation while
	// streaming (it would displace in-flight prose), and
	// SetProseStreaming(false) fires the deferred resize once the
	// segment is done.
	pendingResize bool

	// pendingSteerRegion is set when the steer panel's row count changes
	// while prose is streaming. The DECSTBM re-apply is deferred to
	// segment end (same rationale as pendingResize); the new row is
	// still rendered immediately by drawLocked. Applied by
	// SetProseStreaming(false) via the catch-up refresh.
	pendingSteerRegion bool

	// resizeInFlight prevents multiple deferred-resize goroutines from
	// stacking when SetProseStreaming(false) fires rapidly across
	// consecutive segments. The first goroutine to CAS from false→true
	// runs; others see true and skip. Cleared by the goroutine on exit.
	resizeInFlight atomic.Bool

	// Cost-warn thresholds (USD). Costs above warn render yellow; above
	// alert render red. Sane defaults; future config wiring possible.
	WarnCost  float64
	AlertCost float64
}

// ContentSource supplies the current values rendered in the footer. The
// footer reads from it on every Refresh; the source must be safe for
// concurrent calls.
type ContentSource interface {
	Model() string
	ContextTokens() (used, limit int)
	TotalCost() float64
	WorkingDir() string
}

// billingTypeSource is an optional addition to ContentSource for sources
// that can report the current provider's billing type. When the concrete
// source implements it AND the charged cost is zero, the footer renders
// "included" (subscription) or "free" instead of the misleading
// "$0.0000". SP-113.
type billingTypeSource interface {
	BillingType() string
}

// activeSubagentsSource is an optional addition to ContentSource for sources
// that can report how many subagents are currently running. When the
// concrete source implements it AND the count is non-zero, the footer
// renders a " · N sub" segment. SP-051-2d.
type activeSubagentsSource interface {
	ActiveSubagents() int
}

// queuedMessagesSource is an optional addition to ContentSource for
// sources that can report how many SP-055 deferred ("queued") steer
// messages are waiting for the next user-prompted turn. The footer
// renders a "⏸ N queued" badge when N > 0, otherwise the segment is
// hidden. SP-055 Phase 3b.
type queuedMessagesSource interface {
	QueuedMessages() int
}

// todoProgressSource is an optional addition to ContentSource for sources
// that can report the agent's todo list progress. When the concrete source
// implements it AND there are todos with some completed, the footer
// renders a " · 3/7 done" badge so the user can gauge turn progress at a
// glance. CLI-UX-4.
type todoProgressSource interface {
	TodoProgress() (done, total int)
}

// NewStatusFooter constructs a footer that writes to w. If w is nil
// os.Stderr is used (the same channel the spinner uses). Non-TTY writers
// produce a no-op footer.
func NewStatusFooter(w io.Writer, source ContentSource) *StatusFooter {
	if w == nil {
		w = os.Stderr
	}
	isTTY := false
	fd := -1
	if f, ok := w.(*os.File); ok {
		fd = int(f.Fd())
	}
	isTTY = SupportsCursorControl(w)
	return &StatusFooter{
		w:           w,
		isTTY:       isTTY,
		fd:          fd,
		source:      source,
		WarnCost:    1.0,
		AlertCost:   5.0,
		steerCursor: -1,
	}
}

// Start declares the scroll region, spawns a SIGWINCH watcher, and renders
// the initial footer line. Safe to call multiple times; redundant calls
// just re-render (idempotent on the watcher).
func (f *StatusFooter) Start() {
	if f == nil || !f.isTTY || f.source == nil {
		return
	}
	f.mu.Lock()
	wasActive := f.active
	f.active = true
	if !wasActive {
		f.winchStop = make(chan struct{})
		f.winchDone = make(chan struct{})
	}
	stopCh := f.winchStop
	doneCh := f.winchDone
	f.mu.Unlock()

	f.applyScrollRegion()
	f.draw()

	// Fingerprint the terminal flavor NOW, before any input reader
	// parks on stdin. The DA2 probe reads a reply from fd 0; when it
	// ran lazily on the first Resize() (the first soft-keyboard toggle
	// on Termux) an InputReader/SteerInputReader was already blocked in
	// a raw-mode Read on the same fd — the kernel woke one of the two
	// readers, the input reader won, consumed the reply, and the probe
	// timed out and cached false FOREVER. With the cache poisoned the
	// footer took the xterm union-clear path on Termux, whose
	// bottom-anchored reflow means that window holds freshly
	// pulled-down conversation lines: visible history was erased on
	// every keyboard dismiss. Probing here is safe: the terminal is
	// cooked, no reader is active, and on Termux the reply lands in
	// <10ms (early return at the terminator).
	LockOutput()
	bottomAnchoredResize()
	UnlockOutput()

	if !wasActive {
		go f.watchResize(stopCh, doneCh)
		// On platforms without SIGWINCH (Windows), watchResize is a no-op.
		// Start a polling-based resize detector that calls Resize() +
		// notifyResizeSubscribers() on a timer. The stop function is stored
		// and invoked from Stop().
		f.mu.Lock()
		f.resizePollerStop = startResizePoller(func() {
			f.Resize()
		})
		f.mu.Unlock()
	}
}

// Refresh re-reads the source and redraws the footer. Idempotent and
// cheap; safe to call from event subscribers on each ToolEnd.
//
// Skipped while prose is actively streaming (proseStreaming flag set by
// the AssistantTurnRenderer) to avoid the DEC save/restore cursor
// sequences racing with scroll-region content — the root cause of the
// "scattered characters" clobbering symptom.
//
// The proseStreaming check is performed TWICE: once before acquiring
// outputMu (fast-path bail-out) and once inside drawLocked after the
// lock is held (TOCTOU guard). The double-check closes the window where
// Refresh reads proseStreaming=false, then a concurrent WriteChunk sets
// it to true and acquires outputMu before Refresh does. Without the
// second check, Refresh would proceed to drawLocked and emit DECSC/DECRC
// sequences that race with the prose being written.
func (f *StatusFooter) Refresh() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	active := f.active
	streaming := f.proseStreaming
	f.mu.Unlock()
	if !active || streaming {
		return
	}
	f.draw()
}

// SetProseStreaming toggles the prose-streaming gate. When true,
// Refresh() is a no-op so the footer's cursor save/restore can't race
// with prose being written to the scroll region.
//
// This method MUST NOT take outputMu. It is called from the
// AssistantTurnRenderer's WriteChunk / resetSegment paths, both of
// which already hold LockOutput — and resetSegment fires from
// FinalizeAtTurnEnd, also under LockOutput. Calling Refresh() (which
// calls draw → LockOutput) here would be a re-entrant lock on a
// non-reentrant sync.Mutex, self-deadlocking the REPL goroutine at
// every turn end. That hang left the steer panel on screen and
// blocked the next ReadLine, reproducing the "can't submit
// follow-ups, must hard-close" symptom. Callers that need a catch-up
// draw call Refresh() themselves once the lock is released.
func (f *StatusFooter) SetProseStreaming(active bool) {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.proseStreaming = active
	// When prose streaming ends, fire a deferred resize if one was
	// pended during streaming. This ensures the scroll region and
	// footer rows catch up to the new terminal dimensions that
	// couldn't be applied mid-stream.
	//
	// MUST run asynchronously: SetProseStreaming is called from
	// resetSegment / OnExternalWriteRows, both of which run inside
	// LockOutput. Resize() also acquires LockOutput. Calling it
	// synchronously would re-enter the non-reentrant outputMu and
	// self-deadlock — the exact bug that
	// TestSetProseStreaming_NoDeadlockUnderOutputLock guards against.
	// The goroutine is safe because Resize acquires its own locks
	// (f.mu, LockOutput) from a clean stack.
	shouldResize := false
	shouldSteerRegion := false
	if !active {
		if f.pendingResize {
			f.pendingResize = false
			shouldResize = true
		}
		if f.pendingSteerRegion {
			f.pendingSteerRegion = false
			// A steer row-count change pends only when the resize path
			// is NOT already firing — Resize re-applies the scroll
			// region anyway, which supersedes the steer-region apply.
			shouldSteerRegion = !shouldResize
		}
	}
	f.mu.Unlock()

	if shouldResize {
		// Only fire if no deferred resize is already in-flight. The
		// CAS prevents goroutine stacking when consecutive segments
		// end in rapid succession. Resize reads the latest terminal
		// size itself, so skipping a duplicate is safe.
		if f.resizeInFlight.CompareAndSwap(false, true) {
			go func() {
				defer f.resizeInFlight.Store(false)
				f.Resize()
			}()
		}
	} else if shouldSteerRegion {
		// Deferred steer-panel row-count change: re-apply the scroll
		// region and redraw under outputMu, from a clean stack (this
		// method runs inside LockOutput). Async for the same
		// non-reentrancy reason as the resize above.
		go func() {
			LockOutput()
			defer UnlockOutput()
			f.applyScrollRegionLocked()
			f.drawLocked()
		}()
	}
}

// SetTurnActive switches the hint row between its mid-turn and idle
// wording.
func (f *StatusFooter) SetTurnActive(active bool) {
	if f == nil {
		return
	}
	mode := ComposerIdle
	if active {
		mode = ComposerSteer
	}
	if f.setComposerMode(mode) {
		f.Refresh()
	}
}

// SetComposerMode records what the input box does on Enter. The caller's
// next box update redraws it.
func (f *StatusFooter) SetComposerMode(mode ComposerMode) {
	if f == nil {
		return
	}
	f.setComposerMode(mode)
}

func (f *StatusFooter) setComposerMode(mode ComposerMode) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	changed := f.composerMode != mode
	f.composerMode = mode
	return changed
}

// SetShowKeymapHint enables/disables the keyboard shortcut hint row
// above the rule. When true, drawLocked reserves an extra row.
// SP-115.
func (f *StatusFooter) SetShowKeymapHint(show bool) {
	if f == nil {
		return
	}
	f.mu.Lock()
	changed := f.showKeymapHint != show
	f.showKeymapHint = show
	live := f.active && f.isTTY && !f.proseStreaming
	f.mu.Unlock()
	if !changed || !live {
		return
	}
	// The row count changed under a live footer: re-apply the margins now
	// rather than letting the next draw paint the hint over a transcript row.
	LockOutput()
	defer UnlockOutput()
	f.shiftScrollRegionLocked()
	f.applyScrollRegionLocked()
	f.drawLocked()
}

// Resize handles a terminal-size change (SIGWINCH). The OLD footer rows
// (tracked via lastRows) are cleared first so a grow doesn't leave the
// previous footer stranded mid-screen, then the scroll region is
// re-applied for the new height and the footer is redrawn at the new
// bottom.
//
// The entire body is wrapped in LockOutput so the scroll-region reset,
// row clearing, and re-application can't interleave with a concurrent
// SteerInputReader.renderLine → footer.draw or a PrintExternal call.
// Without the lock, two SIGWINCH handlers (footer + steer reader) race:
// the steer reader's draw renders footer rows that Resize just cleared,
// and the scroll-region manipulation in Resize displaces content the
// steer reader's draw just positioned — producing the stacked-duplicates
// symptom on every resize during an active turn.
