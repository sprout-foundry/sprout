package console

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/term"
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

	// SP-115: keyboard shortcut hint row. When showKeymapHint is true
	// the footer reserves an extra pinned row above the rule to display
	// registered keybindings (e.g. "Alt+T breakdown · Alt+V verbose").
	showKeymapHint bool

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
		isTTY = term.IsTerminal(fd)
	}
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

// SetShowKeymapHint enables/disables the keyboard shortcut hint row
// above the rule. When true, drawLocked reserves an extra row.
// SP-115.
func (f *StatusFooter) SetShowKeymapHint(show bool) {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.showKeymapHint = show
	f.mu.Unlock()
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
func (f *StatusFooter) Resize() {
	if f == nil || !f.isTTY {
		return
	}

	LockOutput()
	defer UnlockOutput()

	f.mu.Lock()
	active := f.active
	streaming := f.proseStreaming
	oldRows := f.lastRows
	f.mu.Unlock()
	if !active {
		return
	}

	// Defer resize while prose is actively streaming. Scroll-region
	// manipulation during streaming displaces in-flight prose content,
	// garbling the output. Set pendingResize so
	// SetProseStreaming(false) fires the deferred resize once the
	// segment ends.
	if streaming {
		f.mu.Lock()
		f.pendingResize = true
		f.mu.Unlock()
		return
	}

	// Reset the scroll region, clear stale footer content, then
	// re-apply the scroll region and redraw.
	//
	// Footer content is padded to the terminal width at draw time. When
	// the terminal shrinks, those padded rows wrap across multiple
	// physical rows. The terminal reflows everything, making precise
	// row calculations unreliable. Instead, we clear a generous region
	// at the bottom of the screen (footer rows × max wrap ratio + slack)
	// to guarantee all stale copies are wiped before redrawing.
	if oldRows > 1 {
		// Reset the scroll region first so we can address the full screen.
		fmt.Fprint(f.w, "\033[r")
		f.mu.Lock()
		lastHint := f.lastHintRows
		lastSteer := f.lastSteerRows
		oldCols := f.lastCols
		f.mu.Unlock()

		newCols, newRows := f.terminalSize()
		reserved := 2 + lastSteer + lastHint
		if newRows < reserved+1 {
			newRows = reserved + 1
		}

		// Compute wrapped overflow: each padded footer row wraps to
		// ceil(oldCols/newCols) rows at the new width.
		overflow := f.computeOverflowRows(oldCols, newCols, reserved)

		// Clear from (newRows - reserved - overflow) to end of screen.
		// This catches the footer's current rows AND any wrapped overflow
		// from the old wider content that the terminal reflowed upward.
		clearTop := newRows - reserved - overflow
		if clearTop < 1 {
			clearTop = 1
		}
		fmt.Fprintf(f.w, "\033[%d;1H\033[J", clearTop)
	}

	f.applyScrollRegionLocked()
	f.drawLocked()
}

// computeOverflowRows calculates how many extra physical rows the footer's
// old content occupies after a terminal width change. When the terminal
// shrinks, each footer row that was padded to the old width wraps across
// ceil(oldCols/newCols) physical rows. The extra rows (beyond the 1 row
// per footer line) appear ABOVE the footer's known row positions and must
// be cleared to avoid stale duplicates.
//
// Returns the number of additional rows to clear above the topmost footer row.
func (f *StatusFooter) computeOverflowRows(oldCols, newCols, footerRows int) int {
	if oldCols <= 0 || newCols <= 0 || oldCols <= newCols {
		return 0
	}
	// Each footer row was padded to oldCols. At newCols it wraps to
	// ceil(oldCols / newCols) rows. The overflow per row is that minus 1.
	rowsPerLine := (oldCols-1)/newCols + 1
	overflowPerLine := rowsPerLine - 1
	total := overflowPerLine * footerRows
	// Cap at a sane maximum to avoid clearing the entire screen on
	// extreme shrinks (e.g. 1000→20). The terminal's own scrollback
	// handles content above this range.
	if total > 30 {
		total = 30
	}
	return total
}

// ApplyPendingResizeStreamingLocked re-applies the scroll region for the
// current terminal geometry while prose is streaming, WITHOUT consuming
// pendingResize and WITHOUT the stale-row clearing or footer redraw that
// Resize() performs — both would wipe in-flight prose. Safe only when the
// caller knows the cursor sits at column 0 of a fresh row (a completed-line
// boundary): the DECSTBM re-apply is wrapped in DECSC/DECRC (\0337/\0338)
// so the streaming writer's cursor is preserved. Caller MUST hold outputMu.
func (f *StatusFooter) ApplyPendingResizeStreamingLocked() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	snapshot := f.active && f.pendingResize
	f.mu.Unlock()
	if !snapshot {
		return
	}
	// Wrap the DECSTBM re-apply in a cursor save/restore so the
	// streaming writer's cursor is preserved. applyScrollRegionLocked
	// skips when the terminal is too short for the reserved rows.
	fmt.Fprint(f.w, "\0337")
	f.applyScrollRegionLocked()
	fmt.Fprint(f.w, "\0338")
}

// Stop resets the scroll region to full-screen, clears the footer row, and
// halts the SIGWINCH watcher. MUST be called on every exit path (including
// signal-driven shutdown) or the user's terminal is left with a broken
// scroll region. Idempotent — safe to call when already stopped.
func (f *StatusFooter) Stop() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	if !f.active {
		f.mu.Unlock()
		return
	}
	f.active = false
	stopCh := f.winchStop
	doneCh := f.winchDone
	f.winchStop = nil
	f.winchDone = nil
	pollerStop := f.resizePollerStop
	f.resizePollerStop = nil
	f.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
		// Bounded wait: the SIGWINCH watcher may be blocked acquiring
		// outputMu behind a wedged PTY write. Waiting forever here (this
		// runs from signal handlers via StopGlobalStatusFooter right
		// before os.Exit) deadlocks shutdown; the watcher exits on its
		// own once unblocked and the region reset below is idempotent.
		select {
		case <-doneCh:
		case <-time.After(500 * time.Millisecond):
		}
	}
	if pollerStop != nil {
		pollerStop()
	}

	_, rows := f.terminalSize()
	// Snapshot every field the teardown reads once, under f.mu; the
	// write sequence below runs under outputMu so it cannot interleave
	// with a concurrent Refresh/Resize, and reading f.lastHintRows
	// directly inside it raced with drawLocked's bookkeeping.
	f.mu.Lock()
	lastSteerSnap := f.lastSteerRows
	lastHintSnap := f.lastHintRows
	oldColsSnap := f.lastCols
	steerActiveSnap := f.steerActive
	f.mu.Unlock()

	LockOutput()
	if rows > 1 {
		reserved := 2 + lastSteerSnap + lastHintSnap
		topRow := rows - reserved

		newCols, _ := f.terminalSize()
		overflow := f.computeOverflowRows(oldColsSnap, newCols, reserved)
		topRow -= overflow
		if topRow < 1 {
			topRow = 1
		}
		fmt.Fprintf(f.w, "\033[%d;1H\033[J", topRow)
	}
	fmt.Fprint(f.w, "\033[r")
	if rows > 1 {
		topPinned := rows - 1
		if steerActiveSnap && lastSteerSnap > 0 {
			topPinned = steerRowFor(rows, lastSteerSnap, lastHintSnap, 0)
		} else if lastHintSnap > 0 {
			topPinned = rows - 2
		}
		fmt.Fprintf(f.w, "\033[%d;1H", topPinned)
	}
	UnlockOutput()

	f.mu.Lock()
	f.steerActive = false
	f.steerLine = ""
	f.mu.Unlock()
}

// watchResize listens for SIGWINCH (or the platform equivalent) and
// re-applies the scroll region + redraws the footer. Exits when stopCh
// is closed. On platforms without SIGWINCH (Windows, js/wasm) the goroutine
// just waits for stopCh and never fires Resize.
//
// After its own Resize, it calls notifyResizeSubscribers so the active
// turn renderer and any other width-dependent consumers update their
// width snapshots.
func (f *StatusFooter) watchResize(stopCh, doneCh chan struct{}) {
	defer close(doneCh)
	sig := resizeSignal()
	if sig == nil {
		<-stopCh
		return
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig)
	defer signal.Stop(ch)
	for {
		select {
		case <-stopCh:
			return
		case <-ch:
			f.Resize()
			notifyResizeSubscribers()
		}
	}
}

// maxSteerRows caps how tall the steer panel can grow.Multi-line steer
// input gets one row per `\n`-separated line up to this cap; beyond
// that the panel scrolls internally (truncation in the topmost rendered
// row). Picked to leave enough scroll region for the conversation even
// on small terminals while comfortably handling typical multi-line
// pastes / messages.
const maxSteerRows = 6

// applySteerRegionOrDefer handles the activation/row-count-change path
// shared by the steer setters: when prose is streaming, the DECSTBM
// re-apply is deferred (it would displace in-flight prose) and only
// the steer rows render; otherwise orphaned rows are cleared, the
// region is re-applied, and a full draw happens — atomically under
// outputMu, so the sequence can't interleave with prose writes.
// Returns true when the caller's follow-up draw() should be skipped
// (the full draw already happened here).
func (f *StatusFooter) applySteerRegionOrDefer(wasActive bool, prevRows, newRows int) bool {
	f.mu.Lock()
	streaming := f.proseStreaming
	f.mu.Unlock()
	if streaming {
		f.mu.Lock()
		f.pendingSteerRegion = true
		f.mu.Unlock()
		return false
	}
	LockOutput()
	defer UnlockOutput()
	if wasActive && newRows < prevRows {
		f.clearOrphanedSteerRows(prevRows, newRows)
	}
	f.applyScrollRegionLocked()
	f.drawLocked()
	return true
}

// SetSteerLine reserves one or more pinned rows above the rule and
// renders the supplied text there. Newlines (`\n`) in `text` produce
// additional rows up to maxSteerRows. Called by SteerInputReader as
// the user types — each keystroke replaces the prior content. Safe to
// call repeatedly; the scroll region is re-applied only when the row
// count changes. SP-055.
//
// SP-078: also clears steerWrappedActive so a subsequent legacy
// SetSteerLine after SetSteerLineWrapped reverts to the byte-offset
// render path.
func (f *StatusFooter) SetSteerLine(text string) {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = true
	f.steerLine = text
	f.steerCursor = -1
	f.steerWrappedActive = false
	f.steerCursorRow = -1
	f.steerCursorCol = 0
	active := f.active
	newRows := f.steerRowCount()
	f.mu.Unlock()
	if !active {
		return
	}
	if !wasActive || newRows != prevRows {
		// Activation OR row-count change: blank any orphaned rows from
		// the previous size before reapplying the region. Without this,
		// shrinking from 3 rows to 1 would leave the top two rows
		// stranded above the new scroll region.
		if f.applySteerRegionOrDefer(wasActive, prevRows, newRows) {
			return
		}
	}
	f.draw()
}

// SetSteerLineWithCursor is like SetSteerLine but also specifies the
// byte offset within text where the input caret (▏) should appear.
// Used by SteerInputReader to render a mid-buffer cursor for readline
// cursor movement (Ctrl-A/E/B/F, Alt-B/F, etc.). An offset of -1
// falls back to caret-at-end (legacy) behavior.
func (f *StatusFooter) SetSteerLineWithCursor(text string, cursorByteOffset int) {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = true
	f.steerLine = text
	f.steerCursor = cursorByteOffset
	f.steerWrappedActive = false
	f.steerCursorRow = -1
	f.steerCursorCol = 0
	active := f.active
	newRows := f.steerRowCount()
	f.mu.Unlock()
	if !active {
		return
	}
	if !wasActive || newRows != prevRows {
		if f.applySteerRegionOrDefer(wasActive, prevRows, newRows) {
			return
		}
	}
	f.draw()
}

// SetSteerLineWrapped is the SP-078 width-aware variant. text is the
// full steer buffer (already prefixed). cursorRow and cursorCol are
// 0-based indices into the VISUAL row array the footer will render
// after hard-break (\n) split + soft wrap to the terminal width.
//
// Use this when the buffer can exceed the panel width; the legacy
// SetSteerLineWithCursor path splits on \n only and overflows
// horizontally on over-wide lines. cursorRow < 0 is treated as
// "caret at end of last visible row."
//
// The footer reserves enough scroll-region rows for the visual row
// count (capped at maxSteerRows) and shifts the caret row back into
// the visible window when truncation occurs.
func (f *StatusFooter) SetSteerLineWrapped(text string, cursorRow, cursorCol int) {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = true
	f.steerLine = text
	f.steerCursor = -1
	f.steerWrappedActive = true
	f.steerCursorRow = cursorRow
	f.steerCursorCol = cursorCol
	active := f.active
	newRows := f.steerRowCount()
	f.mu.Unlock()
	if !active {
		return
	}
	if !wasActive || newRows != prevRows {
		// Defer the scroll-region change to segment end while prose
		// streams: DECSTBM homes the cursor and re-clamps the region,
		// which races with in-flight prose writes inside the region.
		// The extra row is rendered by drawLocked regardless, and
		// SetProseStreaming(false) re-applies the region once prose
		// is done.
		if f.applySteerRegionOrDefer(wasActive, prevRows, newRows) {
			return
		}
	}
	f.draw()
}

// SetSteerLineWrappedLocked is the lock-free variant of
// SetSteerLineWrapped for callers that already hold outputMu (e.g.
// InputReader.refreshLocked runs under LockOutput). It records the
// same wrapped-mode state and re-renders via applyScrollRegionLocked +
// drawLocked instead of applyScrollRegion + draw, which would
// re-acquire the non-reentrant outputMu and deadlock.
func (f *StatusFooter) SetSteerLineWrappedLocked(text string, cursorRow, cursorCol int) {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = true
	f.steerLine = text
	f.steerCursor = -1
	f.steerWrappedActive = true
	f.steerCursorRow = cursorRow
	f.steerCursorCol = cursorCol
	active := f.active
	newRows := f.steerRowCount()
	f.mu.Unlock()
	if !active {
		return
	}
	if !wasActive || newRows != prevRows {
		if wasActive && newRows < prevRows {
			f.clearOrphanedSteerRows(prevRows, newRows)
		}
		// Defer the DECSTBM re-apply while prose streams — same
		// rationale as applySteerRegionOrDefer. This Locked variant's
		// caller already holds outputMu, so the catch-up runs via the
		// pendingSteerRegion goroutine at SetProseStreaming(false).
		f.mu.Lock()
		streaming := f.proseStreaming
		if streaming {
			f.pendingSteerRegion = true
		}
		f.mu.Unlock()
		if !streaming {
			f.applyScrollRegionLocked()
		}
	}
	f.drawLocked()
}

// clearOrphanedSteerRows blanks rows that USED to belong to the steer
// panel but won't be rendered this frame because the panel shrank.
// Without this, deleting a `\n` would leave the previous row's text
// stranded above the now-smaller panel. Called with the mutex NOT
// held; it does its own short ANSI write.
func (f *StatusFooter) clearOrphanedSteerRows(prevRows, newRows int) {
	_, rows := f.terminalSize()
	if rows < 3 {
		return
	}
	// SP-115: hint row pushes steer panel up by hintRows.
	f.mu.Lock()
	hintRows := f.hintRowCount()
	f.mu.Unlock()
	// Steer panel occupies rows (rows-1-hintRows-prevRows) .. (rows-2-hintRows).
	// After shrinking, it occupies (rows-1-hintRows-newRows) .. (rows-2-hintRows).
	// Blank the rows in the top of the old panel that the new one doesn't cover.
	fmt.Fprint(f.w, "\0337")
	// Temporarily drop the region so we can address the soon-to-be-
	// scrollable rows directly; applyScrollRegion will re-clamp it.
	fmt.Fprint(f.w, "\033[r")
	for i := 0; i < prevRows-newRows; i++ {
		row := rows - 1 - hintRows - prevRows + i
		if row < 1 {
			continue
		}
		fmt.Fprintf(f.w, "\033[%d;1H\033[K", row)
	}
	fmt.Fprint(f.w, "\0338")
}

// ClearSteerLine drops the steer panel, blanks the rows it occupied,
// and contracts the scroll region back to 2 reserved rows. Called when
// the SteerInputReader stops (e.g. ProcessQuery returned). SP-055.
func (f *StatusFooter) ClearSteerLine() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = false
	f.steerLine = ""
	f.steerCursor = -1
	f.steerWrappedActive = false
	f.steerCursorRow = -1
	f.steerCursorCol = 0
	f.lastSteerRows = 0
	active := f.active
	f.mu.Unlock()
	if !active || !wasActive {
		return
	}
	// Reset region, blank each previously-occupied steer row, then
	// re-apply with no steer reservation. Order: reset region first so
	// we can address the previously-reserved rows by absolute number.
	// The whole sequence runs under outputMu so it cannot interleave
	// with a concurrent Refresh/Resize mid-sequence.
	LockOutput()
	_, rows := f.terminalSize()
	if rows > 2 && prevRows > 0 {
		fmt.Fprint(f.w, "\033[r")
		fmt.Fprint(f.w, "\0337")
		// SP-115: hint row pushes steer panel up by hintRows.
		f.mu.Lock()
		hintRows := f.lastHintRows
		f.mu.Unlock()
		for i := 0; i < prevRows; i++ {
			// Match steerRowFor(rows, prevRows, hintRows, i): the steer
			// panel is drawn at `rows-1-hintRows-steerRows+i`, so we
			// blank that same row. A prior version used `+1` here, which
			// cleared the rule row (repainted immediately by draw())
			// instead of the steer text row — leaving stale steer text
			// on screen after EndTurn (visible above the next idle prompt).
			row := rows - 1 - hintRows - prevRows + i
			if row < 1 {
				continue
			}
			fmt.Fprintf(f.w, "\033[%d;1H\033[K", row)
		}
		fmt.Fprint(f.w, "\0338")
	}
	f.applyScrollRegionLocked()
	f.drawLocked()
	UnlockOutput()
}

// ClearSteerLineLocked is the lock-free variant of ClearSteerLine for
// callers that already hold outputMu. Mirrors ClearSteerLine but uses
// applyScrollRegionLocked + drawLocked so it can run from
// InputReader.refreshLocked without re-acquiring the non-reentrant
// outputMu.
func (f *StatusFooter) ClearSteerLineLocked() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = false
	f.steerLine = ""
	f.steerCursor = -1
	f.steerWrappedActive = false
	f.steerCursorRow = -1
	f.steerCursorCol = 0
	f.lastSteerRows = 0
	active := f.active
	f.mu.Unlock()
	if !active || !wasActive {
		return
	}
	// Reset region, blank each previously-occupied steer row, then
	// re-apply with no steer reservation. Order: reset region first so
	// we can address the previously-reserved rows by absolute number.
	_, rows := f.terminalSize()
	if rows > 2 && prevRows > 0 {
		fmt.Fprint(f.w, "\033[r")
		fmt.Fprint(f.w, "\0337")
		f.mu.Lock()
		hintRows := f.lastHintRows
		f.mu.Unlock()
		for i := 0; i < prevRows; i++ {
			row := rows - 1 - hintRows - prevRows + i
			if row < 1 {
				continue
			}
			fmt.Fprintf(f.w, "\033[%d;1H\033[K", row)
		}
		fmt.Fprint(f.w, "\0338")
	}
	f.applyScrollRegionLocked()
	f.drawLocked()
}

// draw renders the pinned footer rows. Always: row N-1 horizontal rule,
// row N content. When a steer line is active: row N-2 steer input,
// row N-1 rule, row N content. Uses save/restore cursor (DEC private mode
// \0337/\0338) so any in-flight prompt rendering above the footer is
// not perturbed.
func (f *StatusFooter) draw() {
	// Serialize against InputReader render and other console chrome so
	// the multi-step save-cursor / move / clear / restore sequence can't
	// interleave with a keystroke render. Without this, typing between
	// turns with background event subscribers firing Refresh looks like
	// characters are dropped (they're in the line buffer, but the cursor
	// has been displaced mid-render).
	LockOutput()
	defer UnlockOutput()
	f.drawLocked()
}

// drawLocked is the lock-free inner body of draw. Caller MUST hold
// outputMu. Extracted so printExternalLocked (which already holds
// outputMu from PrintExternal) can re-render the footer without
// re-acquiring the non-reentrant mutex and deadlocking.
//
// Performs a final proseStreaming check under outputMu to close the
// TOCTOU window in Refresh: between reading proseStreaming=false and
// acquiring outputMu, a concurrent WriteChunk could have set it to true
// and started writing prose. Re-checking here prevents DECSC/DECRC
// from racing with in-flight prose.
func (f *StatusFooter) drawLocked() {
	f.mu.Lock()
	streaming := f.proseStreaming
	f.mu.Unlock()
	if streaming {
		// Prose is streaming into the scroll region. Full chrome draws
		// are suppressed (the DECSC/DECRC + rule/content rows are what
		// historically raced with scrolling prose) — but the STEER rows
		// must still render: they live in the reserved area BELOW the
		// scroll region, and suppressing them is why typed characters
		// went invisible mid-stream and "caught up" only at segment
		// boundaries. Steering while the model talks is the whole point
		// of the steer panel.
		//
		// This is safe now that every prose write (WriteChunk and
		// friends) holds outputMu: the steer-row render below cannot
		// interleave with a partial prose write. It writes no \n and
		// does not touch the scroll region, so it cannot displace the
		// cursor the streaming writer is using inside the region.
		f.drawSteerRowsLocked()
		return
	}
	f.drawFullLocked()
}

// drawSteerRowsLocked renders only the pinned steer input rows (no rule,
// no content line, no hint row, no scroll-region mutation). Caller must
// hold outputMu. Used while prose is streaming: keeps keystroke echo
// live without reintroducing the mid-stream chrome race.
func (f *StatusFooter) drawSteerRowsLocked() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	steerActive := f.steerActive
	steerLine := f.steerLine
	steerCursor := f.steerCursor
	steerWrapped := f.steerWrappedActive
	steerRows := f.steerRowCount()
	hintRows := f.hintRowCount()
	f.mu.Unlock()
	if !steerActive || steerRows == 0 {
		return
	}
	cols, rows := f.terminalSize()
	if rows < f.reservedRows()+1 {
		return
	}
	lines, cursorLineIdx, cursorByteCol := f.steerVisualLines(steerLine, steerCursor, steerRows, cols, steerWrapped)
	for i, lineText := range lines {
		withCursor := false
		col := -1
		if steerCursor >= 0 || steerWrapped {
			if i == cursorLineIdx {
				withCursor = true
				col = cursorByteCol
			}
		} else {
			withCursor = i == len(lines)-1
		}
		rendered := steerRowTextWithCursor(lineText, cols, withCursor, col)
		fmt.Fprintf(f.w, "\033[%d;1H\033[K%s%s%s", steerRowFor(rows, steerRows, hintRows, i), steerColor, rendered, footerResetAll)
	}
}

// steerVisualLines computes the visual steer rows and cursor placement
// for the current steer state at the given width. steerWrapped and
// steerRows are snapshotted under f.mu by the caller.
func (f *StatusFooter) steerVisualLines(steerLine string, steerCursor, steerRows, cols int, steerWrapped bool) (lines []string, cursorLineIdx, cursorByteCol int) {
	if steerWrapped {
		return WrapSteerLayout(steerLine, steerCursor, cols, maxSteerRows)
	}
	lines = splitSteerLines(steerLine, steerRows)
	cursorLineIdx = len(lines) - 1
	cursorByteCol = -1
	if steerCursor >= 0 {
		offset := 0
		for i, lineText := range lines {
			lineEnd := offset + len(lineText)
			if steerCursor <= lineEnd || i == len(lines)-1 {
				cursorLineIdx = i
				rawByteCol := steerCursor - offset
				if rawByteCol < 0 {
					rawByteCol = 0
				}
				if rawByteCol > len(lineText) {
					rawByteCol = len(lineText)
				}
				cursorByteCol = visibleRuneWidth(lineText[:rawByteCol])
				break
			}
			offset = lineEnd + 1
		}
	}
	return lines, cursorLineIdx, cursorByteCol
}

// drawFullLocked is the pre-streaming-gate body of drawLocked: the full
// chrome (steer rows + optional hint row + rule + content line).
func (f *StatusFooter) drawFullLocked() {
	cols, rows := f.terminalSize()
	if rows < f.reservedRows()+1 {
		return
	}
	line := f.composeLine(cols)
	rule := strings.Repeat("─", cols)

	f.mu.Lock()
	steerActive := f.steerActive
	steerLine := f.steerLine
	steerCursor := f.steerCursor
	steerWrapped := f.steerWrappedActive
	steerRows := f.steerRowCount()
	hintRows := f.hintRowCount()
	f.mu.Unlock()

	// \0337 save cursor; draw chrome rows from top-to-bottom; \0338
	// restore. Color codes wrap each row so the chrome reads as "system
	// UI" without leaking color into surrounding output.
	fmt.Fprint(f.w, "\0337")
	if steerActive && steerRows > 0 {
		// SP-078 Phase 1: two render paths (wrapped vs legacy \n split),
		// shared with drawSteerRowsLocked via steerVisualLines.
		lines, cursorLineIdx, cursorByteCol := f.steerVisualLines(steerLine, steerCursor, steerRows, cols, steerWrapped)

		for i, lineText := range lines {
			withCursor := false
			col := -1
			if steerCursor >= 0 || steerWrapped {
				// Cursor-aware path: caret only on the line the cursor
				// actually falls on, at the computed column.
				if i == cursorLineIdx {
					withCursor = true
					col = cursorByteCol
				}
			} else {
				// Legacy path: caret at the end of the last line.
				withCursor = i == len(lines)-1
			}
			rendered := steerRowTextWithCursor(lineText, cols, withCursor, col)
			fmt.Fprintf(f.w, "\033[%d;1H\033[K%s%s%s", steerRowFor(rows, steerRows, hintRows, i), steerColor, rendered, footerResetAll)
		}
	}
	// SP-115: keyboard shortcut hint row. Sits at rows-2 when hintRows=1
	// (above the rule at rows-1, below the steer panel when active).
	if hintRows > 0 {
		hintLine := KeymapHintRow()
		if hintLine != "" {
			hintRow := rows - 1 - hintRows // hintRows is always 1 → rows-2
			rendered := padToWidth(truncateToWidth(hintLine, cols, "…"), cols)
			fmt.Fprintf(f.w, "\033[%d;1H\033[K%s%s%s", hintRow, footerBaseColor, rendered, footerResetAll)
		}
	}
	fmt.Fprintf(f.w, "\033[%d;1H\033[K%s%s%s", rows-1, footerBaseColor, rule, footerResetAll)
	fmt.Fprintf(f.w, "\033[%d;1H\033[K%s%s%s\0338", rows, footerBaseColor, line, footerResetAll)

	// Re-assert the scroll region at the END of the draw, wrapped in its
	// own DECSC/DECRC so the outer \0338 above still restores the pre-draw
	// cursor. Child processes (editors, pagers, anything emitting \033[r)
	// can drop the DECSTBM margins, after which prose and tool output
	// scroll the FULL screen — writing over the pinned footer rows.
	// Re-applying here makes every full draw a self-healing checkpoint
	// for region damage the footer didn't cause.
	fmt.Fprint(f.w, "\0337")
	f.applyScrollRegionLocked()
	fmt.Fprint(f.w, "\0338")

	// Track the row count so the next Resize knows which OLD rows to
	// clear before re-applying a region for the new size, and so the
	// next SetSteerLine can detect row-count changes.
	f.mu.Lock()
	f.lastRows = rows
	f.lastCols = cols
	f.lastSteerRows = steerRows
	f.lastHintRows = hintRows
	f.mu.Unlock()
}

// Global registration so signal handlers (which don't have a footer// reference) can stop the footer before force-quitting via os.Exit, which
// otherwise skips deferred cleanup and leaves the terminal with a broken
// scroll region.
var (
	globalFooter   *StatusFooter
	globalFooterMu sync.RWMutex
)

// RegisterGlobalStatusFooter installs f as the process-wide footer that
// StopGlobalStatusFooter targets. Pass nil to clear. Safe to call
// multiple times. Mirrors RegisterGlobalIndicator.
func RegisterGlobalStatusFooter(f *StatusFooter) {
	globalFooterMu.Lock()
	defer globalFooterMu.Unlock()
	globalFooter = f
}

// GetGlobalStatusFooter returns the process-wide footer, or nil if none
// is registered. Used by the AssistantTurnRenderer to suppress footer
// refresh during active prose streaming.
func GetGlobalStatusFooter() *StatusFooter {
	globalFooterMu.RLock()
	defer globalFooterMu.RUnlock()
	return globalFooter
}

// StopGlobalStatusFooter resets the registered global footer's scroll
// region and clears its row. Safe to call when no footer is registered or
// when it's already stopped (no-op). Use from signal handlers immediately
// before os.Exit so the user's terminal isn't left in a weird state.
func StopGlobalStatusFooter() {
	globalFooterMu.RLock()
	f := globalFooter
	globalFooterMu.RUnlock()
	f.Stop()
}

func (f *StatusFooter) terminalSize() (cols, rows int) {
	if f.sizeOverride != nil {
		return f.sizeOverride.cols, f.sizeOverride.rows
	}
	if f.fd < 0 {
		return 0, 0
	}
	c, r, err := term.GetSize(f.fd)
	if err != nil {
		return 0, 0
	}
	return c, r
}

// terminalSizeOverride lets tests pin the terminal geometry without a
// pty; terminalSize consults it first.
type terminalSizeOverride struct {
	cols, rows int
}

// TerminalSize is the exported alias of terminalSize, for callers
// outside the console package (e.g. SteerInputReader's width-aware
// render path). Returns (cols, rows). Both are 0 when the footer is
// not attached to a real TTY (fd < 0 or GetSize errored).
func (f *StatusFooter) TerminalSize() (cols, rows int) {
	return f.terminalSize()
}

// proseStreamingActive reports whether assistant prose is currently
// streaming into the scroll region. Read under f.mu only (never
// outputMu) so callers like the indicator's resume closure can check
// it from any stack without risking the lock-ordering contract.
func (f *StatusFooter) proseStreamingActive() bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.proseStreaming
}
