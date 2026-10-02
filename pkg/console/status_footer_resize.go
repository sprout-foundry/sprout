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

	// Reset the scroll region, clear stale footer content, then
	// re-apply the scroll region and redraw.
	//
	// Footer content is padded to the terminal width at draw time. When
	// the terminal shrinks, those padded rows wrap across multiple
	// physical rows. The stale footer block is exactly reserved+overflow
	// rows tall, so the clear starts at its computed top (see the
	// oldTop/newTop derivation below) — no slack row, so no live
	// content row is ever erased.
	overflow := 0
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
		if !bottomAnchoredResize() {
			oldTop := oldRows - reserved + 1
			if oldTop < clearTop {
				clearTop = oldTop
			}
		}
		if clearTop < 1 {
			clearTop = 1
		}
		fmt.Fprintf(f.w, "\033[%d;1H\033[J", clearTop)
	}

	f.applyScrollRegionLocked()
	f.drawLocked()
	if idle != nil {
		f.closeGrowGapLocked(oldRows, overflow)
		idle.redrawAnchoredLocked(cols)
	}
	if picker != nil {
		f.closeGrowGapLocked(oldRows, overflow)
		picker.renderAnchoredLocked(rows-f.reservedRows(), cols)
	}
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
// The rows that fall off the bottom are the ones the resize just cleared.
// Bottom-anchored terminals (Termux) pull content down themselves. Caller
// must hold outputMu.
func (f *StatusFooter) closeGrowGapLocked(oldRows, overflow int) {
	if bottomAnchoredResize() {
		return
	}
	_, rows := f.terminalSize()
	shift := max(overflow, 0)
	if oldRows > 1 {
		shift += max(rows-oldRows, 0)
	}
	bottom := rows - f.reservedRows()
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
