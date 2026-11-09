package console

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"
)

// status_footer_resize.go — the terminal-resize / scroll-region / Stop
// half of StatusFooter, split out of status_footer.go. Pure move.

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

	// An idle prompt is cleared where it stands, measured from the cursor
	// that still sits on it, before anything below moves the cursor. It is
	// redrawn on the region's last row at the end.
	cols, rows := f.terminalSize()
	f.resizeSnapshot.Store(&terminalSizeOverride{cols: cols, rows: rows})
	defer f.resizeSnapshot.Store(nil)
	// Rows crossed the boundary between the screen and the terminal's scrollback
	// without a transcript write to account for them: a shrink pushes the oldest
	// rows off the top. Sync the mirror now so a later replay cannot re-emit what
	// scrollback is already holding.
	trimTranscriptMirrorToAreaLocked()

	idle := activeInputReader
	picker := activeSelectList
	switch {
	case idle != nil && idle.footer == f:
		idle.clearInlineInputForResizeLocked(cols)
		picker = nil
	case picker != nil && picker.isFooterDriven():
		idle = nil
		picker.clearForResizeLocked(cols)
	default:
		idle, picker = nil, nil
	}

	// Reset the scroll region so both paths below can address every row.
	replay := false
	overflow := 0
	// clearedFrom is the first row this pass erased, 0 when it erased nothing.
	// The gap-close below needs it: only rows at or below it are known to be
	// blank, and those are the only rows a scroll may push out of the region.
	clearedFrom := 0
	f.mu.Lock()
	lastCols := f.lastCols
	f.mu.Unlock()
	if oldRows > 1 {
		fmt.Fprint(f.w, "\033[r")
		// Redraw the conversation area from the transcript mirror instead of
		// guessing where the terminal's reflow left the stale footer rows.
		//
		// Only a GROW qualifies. A SHRINK pushes the rows it cannot show off the
		// top into scrollback, so re-emitting the mirrored lines would print them
		// under the copy the terminal filed away. A bottom-anchored terminal
		// (Termux) is excluded even on a grow: it pulls rows DOWN out of its
		// scrollback to fill the new space, and those rows were never the
		// console's to write — clearing the area would destroy output it cannot
		// put back. There the old-geometry clear below stays, narrower and
		// therefore better than a certain loss.
		//
		// A picker is excluded because its frame lives inside the region and
		// renderAnchoredLocked redraws it; a replay would write the transcript
		// through it.
		if picker == nil && !bottomAnchoredResize() && rows > oldRows {
			replay = f.replayTranscriptLocked(oldRows, lastCols, cols)
		}
	}
	if !replay && oldRows > 1 {
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
		overflow = f.computeOverflowRows(oldCols, newCols, reserved)
		newTop := newRows - reserved - overflow + 1

		// The stale footer rows sit at OLD-geometry positions (they were
		// drawn for the old height). On a GROW rows keep their absolute
		// positions: the old footer block (hint+steer+rule+content =
		// `reserved` rows) starts at oldRows-reserved+1, and any
		// width-change wrap extends it DOWNWARD into the fresh rows. On a
		// SHRINK the region reset reflows content and the wrapped old
		// footer settles at the bottom, spanning
		// newRows-(reserved+overflow)+1 .. newRows.
		//
		// Both block tops are exact: the row immediately above each is
		// conversation content and must NOT be erased. Starting one row
		// higher (the old rows-reserved-overflow formula) wiped a live
		// output line on every resize — on Termux the soft keyboard
		// fires a resize per message, so blank lines accumulated in the
		// visible history. Union both tops: clear from the HIGHER of the
		// two, downward to the end of the screen. The union still wipes
		// the stranded old rows on a grow, which a new-geometry-only
		// window misses (the pre-fix macOS symptom).
		//
		// BOTTOM-ANCHORED terminals break the grow half of that union.
		// Termux's TerminalBuffer.resize anchors content to the bottom and
		// pulls rows down out of the transcript on a grow, so the rows at
		// oldTop hold freshly pulled-down conversation lines, not the
		// stranded footer — clearing them eats visible history on every
		// soft-keyboard resize. There the stale footer is always inside
		// the new-geometry window, so clear only newTop. The DA2 probe
		// fingerprints Termux natively and through ssh.
		clearTop := newTop
		if !bottomAnchoredResize() && rows > oldRows {
			// Only on a GROW does the old absolute position still describe
			// where the stale footer sits: rows keep their screen positions,
			// so the block the old geometry drew is stranded at oldTop. On a
			// SHRINK the terminal reflowed or dropped rows above, and the old
			// position points at live conversation.
			oldTop := oldRows - reserved + 1
			if oldTop < clearTop {
				clearTop = oldTop
			}
		}
		if clearTop < 1 {
			clearTop = 1
		}
		clearedFrom = clearTop
		fmt.Fprintf(f.w, "\033[%d;1H\033[J", clearTop)
	}

	f.applyScrollRegionLocked()
	// Record the new geometry before redrawing: drawFullLocked declines to
	// draw at a size Resize has not handled yet.
	f.mu.Lock()
	f.lastRows, f.lastCols = rows, cols
	f.mu.Unlock()
	f.drawLocked()
	// Close the gap whoever owns the screen. It used to run only with an
	// idle prompt or a picker up; a resize landing in between — the
	// moment a turn ends, before the prompt is back — left the region's
	// content at the top with the cursor parked on its new last row, so
	// the next output started below a band of blank rows and the band
	// pushed the latest transcript into scrollback.
	if !replay {
		f.closeGrowGapLocked(oldRows, overflow, clearedFrom)
	}
	if idle != nil {
		idle.redrawAnchoredLocked(cols)
	}
	if picker != nil {
		picker.renderAnchoredLocked(rows-f.reservedRows(), cols)
	}
}

// replayTranscriptLocked redraws the conversation area from the transcript
// mirror: wipe the rows the transcript is known to occupy, then re-emit them
// ending just above the pinned composer box. Caller MUST hold outputMu and MUST
// have reset the scroll region.
//
// This replaces the geometry arithmetic the fallback path uses to locate the
// stale footer rows. That arithmetic predicts what the terminal's own reflow did
// to rows it already drew, and a wrong prediction erases conversation lines.
// Here the rows come from the lines themselves: the block's OLD top is where the
// clear begins, and everything below it — the stale footer rows a grow strands
// mid-screen included — is rewritten.
//
// The clear deliberately stops there. Rows above the block were not written by
// the transcript writer, and a terminal that anchors content to the bottom
// repopulates exactly those rows from its scrollback on a grow.
//
// Returns false when the lines cannot be replayed, leaving the caller's fallback
// to run without anything having been erased.
func (f *StatusFooter) replayTranscriptLocked(oldRows, oldCols, newCols int) bool {
	lines, partial, rows, ok := TranscriptRepaint()
	if !ok {
		return false
	}
	bottom := f.TranscriptBottomRow()
	limit := bottom - pinnedComposerRows(newCols)
	if limit < 1 || rows > limit {
		return false
	}
	oldRowRows, known := TranscriptRowsAt(oldCols)
	newRowRows, knownNew := TranscriptRowsAt(newCols)
	if !known || !knownNew {
		return false
	}
	// The row count at the NARROWER width is both the one the old screen can
	// have shown (a narrower reflow split lines onto rows the old height had
	// no room for, and the terminal scrolled those up out of the old viewport)
	// and the one the block needs now.
	rowRows := oldRowRows
	if newRowRows > rowRows {
		rowRows = newRowRows
	}
	f.mu.Lock()
	oldReserved := 2 + f.lastSteerRows + f.lastHintRows
	f.mu.Unlock()
	if rowRows+oldReserved > oldRows {
		// The reflow spilled rows out of the old viewport's top; the block's
		// old position is no longer derivable from the old geometry alone,
		// and a clear computed from it would strand or double rows.
		return false
	}
	oldBottom := oldRows - oldReserved

	// Where the block sat before the resize, in the document the reflow
	// produced: a newline-terminated write ended it one row above the area's
	// last row, which is the blank the writer parks on.
	top := oldBottom - rowRows + 1
	if partial == "" {
		top--
	}
	if top < 1 {
		top = 1
	}
	if top > limit {
		return false
	}
	_, _ = fmt.Fprintf(f.w, "\033[%d;1H\033[J", top)

	// And where it goes now: the same lines, ending above the composer box at
	// the new height.
	start := limit - rows + 1
	if partial == "" {
		start--
	}
	if start < 1 {
		start = 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\033[%d;1H", start)
	for _, line := range lines {
		b.WriteString(strings.TrimRight(line, "\r\n"))
		b.WriteString("\r\n")
	}
	if partial != "" {
		// The line in progress keeps its row, unterminated: the cursor belongs
		// inside it, where the writer left it.
		b.WriteString(partial)
	}
	_, _ = f.w.Write([]byte(b.String()))
	return true
}

// pinnedComposerRows is how many of the region's last rows the idle input
// reader's composer box occupies at cols, or 0 while a turn owns the screen
// (no idle reader) or the input is not pinned.
//
// It measures the line at the given width rather than trusting lastVisualRows:
// Resize clears the inline prompt before calling this, and the clear zeroes the
// row count the box had at the old width. A box shorter than it thinks leaves
// the replay one row too low, and the box redrawn at the region bottom then
// overwrites the transcript's last row.
func pinnedComposerRows(cols int) int {
	ir := activeInputReader
	if ir == nil || !ir.IsPinned() {
		return 0
	}
	if cols <= 0 {
		return max(ir.lastVisualRows, 1)
	}
	displayLine, _ := ir.renderLineWithCollapsedPastes()
	totalRows, _, _, _, _ := wrappedGeometry(cols, visibleRuneWidth(ir.prompt), displayLine, ir.cursorPos)
	if totalRows < 1 {
		return 1
	}
	return totalRows
}

// closeGrowGapLocked keeps the conversation against the input box after a
// resize opened blank rows between them. Two causes, each scrolled away by
// pulling the region's content down:
//
//   - growth: most terminals add the new rows at the bottom and leave the
//     content in place while the box is redrawn on the new last rows;
//   - width shrink: the footer's padded rows rewrap onto overflow extra
//     rows, pushing the content up, and the clear above that erases them
//     leaves exactly that many blank rows behind.
//
// The scroll is capped at the rows this pass actually erased (clearedFrom). That
// is the whole of what the scroll is allowed to consume: everything from
// clearedFrom down is blank by construction, and the worst case above it is
// content ending on the row directly over the erased window. Scroll past that
// and the bottom-most conversation row is pushed out of the region's bottom edge
// and overwritten by the footer — which is how growing the terminal deleted
// output. An underestimate leaves a band of blank rows, which is the older,
// survivable failure. Bottom-anchored terminals (Termux) pull content down
// themselves. Caller must hold outputMu.
func (f *StatusFooter) closeGrowGapLocked(oldRows, overflow, clearedFrom int) {
	if bottomAnchoredResize() {
		return
	}
	_, rows := f.terminalSize()
	shift := max(overflow, 0)
	if oldRows > 1 {
		shift += max(rows-oldRows, 0)
	}
	bottom := rows - f.reservedRows()
	if clearedFrom < 1 {
		// Nothing was erased, so nothing below the content is known to be blank.
		shift = 0
	} else if room := bottom - clearedFrom + 1; shift > room {
		shift = max(room, 0)
	}
	if shift == 0 || bottom <= shift {
		return
	}
	// Back to the region's last row afterwards: output continues there.
	_, _ = fmt.Fprintf(f.w, "\033[1;1H%s\033[%d;1H", strings.Repeat("\033M", shift), bottom)
}

// computeOverflowRows calculates how many extra physical rows the footer's
// old content occupies after a terminal width change. Each footer row padded
// to the old width wraps across ceil(oldCols/newCols) physical rows at the
// new width. On a SHRINK the reflowed block settles at the screen bottom, so
// the extra rows appear ABOVE the footer's known row positions; on a GROW
// the rows keep their positions and the extra rows extend DOWNWARD into the
// fresh rows. Either way they must fall inside the cleared window to avoid
// stale duplicates.
//
// Returns the number of additional rows the wrapped block adds beyond one
// physical row per footer line.
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
		// +1: the pinned block starts at rows-reserved+1; the row
		// above is content and the wipe must not eat it (same exact-top
		// rule as Resize).
		topRow := rows - reserved + 1

		newCols, _ := f.terminalSize()
		// NOTE: this overflow math assumes a reflow at the current width
		// already happened (via a prior Resize for the width change). Stop
		// itself does not reflow — if the last width change was deferred
		// (pendingResize during prose streaming) and the process exits
		// before the deferred resize runs, the window may be off by a row
		// and a stale fragment can survive on exit. Harmless: the session
		// is ending.
		overflow := f.computeOverflowRows(oldColsSnap, newCols, reserved)
		topRow -= overflow
		if topRow < 1 {
			topRow = 1
		}
		fmt.Fprintf(f.w, "\033[%d;1H\033[J", topRow)
	}
	// The input box hides the terminal cursor while it is pinned; restore it
	// here too, since force-quit paths stop the footer without unwinding the
	// input reader.
	_, _ = fmt.Fprint(f.w, "\033[r"+ShowCursorSeq())
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

// resizeSettle is how long the watcher waits after a SIGWINCH before repainting.
// A drag-resize fires one signal per step; each repaint rewrites the whole
// conversation area, so the burst is collapsed to its final size instead of
// strobing through every intermediate one.
const resizeSettle = 60 * time.Millisecond

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
	settle := time.NewTimer(time.Hour)
	if !settle.Stop() {
		<-settle.C
	}
	defer settle.Stop()
	pending := false
	for {
		// No timer armed while a signal is outstanding: arming one per signal
		// would push the repaint back forever during a long drag.
		var after <-chan time.Time
		if pending {
			after = settle.C
		}
		select {
		case <-stopCh:
			return
		case <-ch:
			pending = true
			if !settle.Stop() {
				select {
				case <-settle.C:
				default:
				}
			}
			settle.Reset(resizeSettle)
		case <-after:
			pending = false
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

// scheduleResize runs Resize on its own goroutine unless one is already in
// flight. Callers hold outputMu, which Resize takes, hence the goroutine.
func (f *StatusFooter) scheduleResize() {
	if f.resizeInFlight.CompareAndSwap(false, true) {
		go func() {
			defer f.resizeInFlight.Store(false)
			f.Resize()
		}()
	}
}
