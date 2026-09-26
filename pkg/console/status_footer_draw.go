package console

import (
	"fmt"
	"strings"
	"sync"

	"golang.org/x/term"
)

// status_footer_draw.go — the draw path + global registry + terminal
// size probing for StatusFooter, split out of status_footer.go. Pure move.

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
	reserved := f.reservedRowsLocked()
	f.mu.Unlock()
	if !steerActive || steerRows == 0 {
		return
	}
	cols, rows := f.terminalSize()
	if rows < reserved+1 {
		return
	}
	lines, cursorLineIdx, cursorByteCol := f.steerVisualLines(steerLine, steerCursor, steerRows, cols, steerWrapped)
	// Wrap the absolute-positioned writes in DECSC/DECRC. Without the
	// save/restore, every mid-stream steer echo relocates the cursor to
	// the steer row and leaves it there. Concurrent consumers that erase
	// their previous frame with a RELATIVE walk-back (SelectList's
	// "\r\033[K\033[A", refreshInputLine's MoveCursorUpSeq) then start
	// from the wrong row, miss their own rows, and stack duplicate
	// frames on every keystroke.
	fmt.Fprint(f.w, "\0337")
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
	fmt.Fprint(f.w, "\0338")
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
	// Snapshot steer/hint state (and the row budget) under f.mu in ONE
	// critical section: steerRowCount reads steerActive/steerWrappedActive,
	// which ClearSteerLine and the keystroke handlers mutate under the same
	// mutex. Reading them in a separate, earlier unlocked pass let the row
	// budget race a concurrent ClearSteerLine.
	f.mu.Lock()
	reserved := f.reservedRowsLocked()
	steerActive := f.steerActive
	steerLine := f.steerLine
	steerCursor := f.steerCursor
	steerWrapped := f.steerWrappedActive
	steerRows := f.steerRowCount()
	hintRows := f.hintRowCount()
	f.mu.Unlock()
	if rows < reserved+1 {
		return
	}
	line := f.composeLine(cols)
	rule := strings.Repeat("─", cols)

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
