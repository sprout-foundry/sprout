package console

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

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
	// lastSteerRows is the row count we drew last time. Used to detect
	// when the row count changed (user added/removed a newline) so we
	// can reapply the scroll region and blank any orphaned rows.
	lastSteerRows int

	winchStop chan struct{}
	winchDone chan struct{}

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
		w:         w,
		isTTY:     isTTY,
		fd:        fd,
		source:    source,
		WarnCost:  1.0,
		AlertCost: 5.0,
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
	}
}

// Refresh re-reads the source and redraws the footer. Idempotent and
// cheap; safe to call from event subscribers on each ToolEnd.
func (f *StatusFooter) Refresh() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	active := f.active
	f.mu.Unlock()
	if !active {
		return
	}
	f.draw()
}

// Resize handles a terminal-size change (SIGWINCH). The OLD footer rows
// (tracked via lastRows) are cleared first so a grow doesn't leave the
// previous footer stranded mid-screen, then the scroll region is
// re-applied for the new height and the footer is redrawn at the new
// bottom.
func (f *StatusFooter) Resize() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	active := f.active
	oldRows := f.lastRows
	f.mu.Unlock()
	if !active {
		return
	}

	// Reset the scroll region temporarily so we can address rows by
	// absolute number without the terminal clamping us inside the OLD
	// (now-stale) scroll area. Then clear the previous footer rows —
	// 2 rows by default (rule + content), 3 when steer was active.
	if oldRows > 1 {
		fmt.Fprint(f.w, "\033[r")
		fmt.Fprint(f.w, "\0337")
		fmt.Fprintf(f.w, "\033[%d;1H\033[K", oldRows)
		fmt.Fprintf(f.w, "\033[%d;1H\033[K", oldRows-1)
		f.mu.Lock()
		steerWasActive := f.steerActive
		f.mu.Unlock()
		if steerWasActive && oldRows > 2 {
			fmt.Fprintf(f.w, "\033[%d;1H\033[K", oldRows-2)
		}
		fmt.Fprint(f.w, "\0338")
	}

	f.applyScrollRegion()
	f.draw()
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
	f.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
		<-doneCh
	}

	_, rows := f.terminalSize()
	if rows > 1 {
		// Clear all pinned rows (N + N-1, plus N-2 if steer was active)
		// before resetting the scroll region so we don't leave residual
		// chrome in the scrollback. Order matters: bottom-up so the
		// cursor ends near the top of where the footer was, which is
		// more useful for the outgoing rendering context than the
		// absolute bottom.
		fmt.Fprintf(f.w, "\033[%d;1H\033[K", rows)
		fmt.Fprintf(f.w, "\033[%d;1H\033[K", rows-1)
		if f.steerActive && rows > 2 {
			fmt.Fprintf(f.w, "\033[%d;1H\033[K", rows-2)
		}
	}
	// Reset scroll region to full screen.
	fmt.Fprint(f.w, "\033[r")
	// Restore cursor to a sensible position (where the topmost pinned
	// row used to be) so subsequent output lands somewhere sensible.
	if rows > 1 {
		topPinned := rows - 1
		if f.steerActive {
			topPinned = rows - 2
		}
		fmt.Fprintf(f.w, "\033[%d;1H", topPinned)
	}
	f.steerActive = false
	f.steerLine = ""
}

// watchResize listens for SIGWINCH (or the platform equivalent) and
// re-applies the scroll region + redraws the footer. Exits when stopCh
// is closed. On platforms without SIGWINCH (Windows, js/wasm) the goroutine
// just waits for stopCh and never fires Resize.
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
		}
	}
}

// footerBaseColor is the ANSI escape used to colorize the rule row and
// the separator characters between badges. Cyan reads as "informational"
// in most terminal themes and stays legible on both dark and light
// backgrounds. The "39" code in footerResetFgKeepBase resets just the
// foreground while preserving the colored context — used between
// segments so the `·` separators stay cyan even after a colored badge.
//
// Per-segment colors (badgeColor*) replace the previous "all cyan"
// rendering: each piece of footer state carries its own semantic color
// so a glance tells you *what* is hot, not just *that* something is.
const (
	footerBaseColor       = "\033[36m" // cyan — rule + separators
	footerResetAll        = "\033[0m"
	footerResetFgKeepBase = "\033[39m" + footerBaseColor // pop fg back to cyan

	// Per-badge palette. Bright variants (9X) read as foreground on
	// most terminal themes; the cyan/yellow/red threshold colors
	// already used for cost are preserved unchanged.
	badgeColorModel    = "\033[1;96m" // bold bright-cyan — brand identity
	badgeColorCtxOK    = "\033[32m"   // green — context <50%
	badgeColorCtxWarn  = "\033[33m"   // yellow — context 50–80%
	badgeColorCtxAlert = "\033[31m"   // red — context >80%
	badgeColorCwd      = "\033[2;36m" // dim cyan — ambient, low priority
	badgeColorSubagent = "\033[95m"   // bright magenta — persona-coded
	badgeColorQueue    = "\033[33m"   // yellow — needs attention soon
)

// maxSteerRows caps how tall the steer panel can grow. Multi-line steer
// input gets one row per `\n`-separated line up to this cap; beyond
// that the panel scrolls internally (truncation in the topmost rendered
// row). Picked to leave enough scroll region for the conversation even
// on small terminals while comfortably handling typical multi-line
// pastes / messages.
const maxSteerRows = 6

// steerRowCount returns how many footer rows the current steer buffer
// needs. 0 when steer is inactive, otherwise 1 + line-break count
// (clamped to [1, maxSteerRows]).
func (f *StatusFooter) steerRowCount() int {
	if !f.steerActive {
		return 0
	}
	lines := strings.Count(f.steerLine, "\n") + 1
	if lines < 1 {
		lines = 1
	}
	if lines > maxSteerRows {
		lines = maxSteerRows
	}
	return lines
}

// reservedRows returns the number of bottom-pinned rows the footer is
// holding. Always at least 2 (rule + content). When the steer input is
// active, additional rows are reserved above the rule — one row per
// visual line of the steer buffer.
func (f *StatusFooter) reservedRows() int {
	return 2 + f.steerRowCount()
}

// applyScrollRegion sets the scroll region to rows 1..(rows-reserved) so the
// bottom pinned rows are excluded. Reserves 2 rows by default (rule + content),
// 3 rows when a steer input is active (steer + rule + content). No-op when
// the terminal is too short for both the footer and any usable scroll area.
func (f *StatusFooter) applyScrollRegion() {
	_, rows := f.terminalSize()
	reserved := f.reservedRows()
	if rows < reserved+1 {
		return
	}
	// DECSTBM: set scroll region. After setting, cursor moves to the
	// home position of the new region (row 1, col 1) per VT100 spec.
	// We then move it just above the footer so subsequent prints land
	// where the user expects (at the bottom of the active scroll area).
	fmt.Fprintf(f.w, "\033[1;%dr", rows-reserved)
	fmt.Fprintf(f.w, "\033[%d;1H", rows-reserved)
}

// SetSteerLine reserves one or more pinned rows above the rule and
// renders the supplied text there. Newlines (`\n`) in `text` produce
// additional rows up to maxSteerRows. Called by SteerInputReader as
// the user types — each keystroke replaces the prior content. Safe to
// call repeatedly; the scroll region is re-applied only when the row
// count changes. SP-055.
func (f *StatusFooter) SetSteerLine(text string) {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = true
	f.steerLine = text
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
		if wasActive && newRows < prevRows {
			f.clearOrphanedSteerRows(prevRows, newRows)
		}
		f.applyScrollRegion()
	}
	f.draw()
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
	// Steer panel occupies rows (rows-1-prevRows) .. (rows-2). After
	// shrinking, it occupies (rows-1-newRows) .. (rows-2). Blank the
	// rows in the top of the old panel that the new one doesn't cover.
	fmt.Fprint(f.w, "\0337")
	// Temporarily drop the region so we can address the soon-to-be-
	// scrollable rows directly; applyScrollRegion will re-clamp it.
	fmt.Fprint(f.w, "\033[r")
	for i := 0; i < prevRows-newRows; i++ {
		row := rows - 1 - prevRows + i
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
		for i := 0; i < prevRows; i++ {
			row := rows - 1 - prevRows + i + 1
			if row < 1 {
				continue
			}
			fmt.Fprintf(f.w, "\033[%d;1H\033[K", row)
		}
		fmt.Fprint(f.w, "\0338")
	}
	f.applyScrollRegion()
	f.draw()
}

// draw renders the pinned footer rows. Always: row N-1 horizontal rule,
// row N content. When a steer line is active: row N-2 steer input,
// row N-1 rule, row N content. Uses save/restore cursor (DEC private mode
// \0337/\0338) so any in-flight prompt rendering above the footer is
// not perturbed.
func (f *StatusFooter) draw() {
	cols, rows := f.terminalSize()
	if rows < f.reservedRows()+1 {
		return
	}
	// Serialize against InputReader render and other console chrome so
	// the multi-step save-cursor / move / clear / restore sequence can't
	// interleave with a keystroke render. Without this, typing between
	// turns with background event subscribers firing Refresh looks like
	// characters are dropped (they're in the line buffer, but the cursor
	// has been displaced mid-render).
	LockOutput()
	defer UnlockOutput()
	line := f.composeLine(cols)
	rule := strings.Repeat("─", cols)

	f.mu.Lock()
	steerActive := f.steerActive
	steerLine := f.steerLine
	steerRows := f.steerRowCount()
	f.mu.Unlock()

	// \0337 save cursor; draw chrome rows from top-to-bottom; \0338
	// restore. Color codes wrap each row so the chrome reads as "system
	// UI" without leaking color into surrounding output.
	fmt.Fprint(f.w, "\0337")
	if steerActive && steerRows > 0 {
		// Render the steer panel above the rule, one terminal row per
		// `\n`-separated line of the buffer. The caret marker goes on
		// the LAST rendered row so the user always sees where new
		// keystrokes will land. The steer rows use a brighter color
		// (bold bright-cyan) so they read as "active input" vs the
		// muted status footer chrome below.
		lines := splitSteerLines(steerLine, steerRows)
		for i, lineText := range lines {
			withCursor := i == len(lines)-1
			rendered := steerRowText(lineText, cols, withCursor)
			fmt.Fprintf(f.w, "\033[%d;1H\033[K%s%s%s", steerRowFor(rows, steerRows, i), steerColor, rendered, footerResetAll)
		}
	}
	fmt.Fprintf(f.w, "\033[%d;1H\033[K%s%s%s", rows-1, footerBaseColor, rule, footerResetAll)
	fmt.Fprintf(f.w, "\033[%d;1H\033[K%s%s%s\0338", rows, footerBaseColor, line, footerResetAll)

	// Track the row count so the next Resize knows which OLD rows to
	// clear before re-applying a region for the new size, and so the
	// next SetSteerLine can detect row-count changes.
	f.mu.Lock()
	f.lastRows = rows
	f.lastSteerRows = steerRows
	f.mu.Unlock()
}

// steerRowFor returns the absolute terminal row (1-based) where the
// i-th rendered steer line should be drawn. The rule sits at `rows-1`
// and the footer at `rows`; steer lines stack above the rule, so with
// steerRows=1 a single line lands at `rows-2`, with steerRows=2 the
// pair lands at `rows-3` and `rows-2`, etc.
//
// A previous version of this calculation wrote to `rows-1-steerRows+i+1`
// (one row lower), placing the steer panel on the rule's row. The rule
// repainted on the same draw call and the panel vanished entirely from
// the terminal. SP-055.
func steerRowFor(rows, steerRows, i int) int {
	return rows - 1 - steerRows + i
}

// splitSteerLines breaks the steer buffer into at most `cap` lines.
// When the buffer contains more lines than the cap, the topmost line
// shown gets a leading `…` so the user sees that earlier content is
// scrolled off — the last lines (including the caret) are always
// visible so typing never goes off-screen.
func splitSteerLines(text string, cap int) []string {
	if cap <= 0 {
		return nil
	}
	all := strings.Split(text, "\n")
	if len(all) <= cap {
		return all
	}
	overflow := all[len(all)-cap:]
	overflow[0] = "… " + overflow[0]
	return overflow
}

// steerColor is the ANSI prefix for the active steer input row —
// brighter than the cyan footer chrome so the user can tell at a
// glance that this row is interactive.
const steerColor = "\033[1;96m" // bold bright-cyan

// steerRowText pads a single steer-panel row to the terminal width.
// When withCursor is true, a visible cursor caret is appended after
// the text — used only on the LAST row of a multi-row steer panel so
// the user always sees where the next keystroke will land, regardless
// of where the terminal's blinking cursor was parked by the most
// recent save/restore. Earlier rows omit the caret to stay visually
// quiet.
func steerRowText(text string, cols int, withCursor bool) string {
	const caret = "▏"
	body := text
	if withCursor {
		caretLen := visibleLen(caret)
		if visibleLen(body)+caretLen >= cols {
			body = truncWithEllipsis(body, cols-caretLen-1)
		}
		body = body + caret
	} else if visibleLen(body) >= cols {
		body = truncWithEllipsis(body, cols-1)
	}
	pad := cols - visibleLen(body)
	if pad < 0 {
		pad = 0
	}
	return body + strings.Repeat(" ", pad)
}

// composeLine builds the content row of the footer, padded/truncated to
// cols width. Each badge applies its own semantic color and resets back
// to the footer base (cyan) so the `·` separators stay visually
// consistent. The pattern is:
//
//	<badgeColor> <text> <footerResetFgKeepBase>
//
// Any badge can change without affecting its neighbors. Cost thresholds
// (existing behavior) are preserved.
func (f *StatusFooter) composeLine(cols int) string {
	if f.source == nil {
		return ""
	}
	model := truncTo(f.source.Model(), 30)
	used, limit := f.source.ContextTokens()
	cost := f.source.TotalCost()
	cwd := shortPath(f.source.WorkingDir())
	branch := gitBranchOf(cwd)

	parts := []string{
		styleSegment(badgeColorModel, model),
		styleSegment(styleCtxColor(used, limit), formatCtx(used, limit)),
		f.styleCost(cost, formatCost(cost)),
		styleSegment(badgeColorCwd, cwdSegment(cwd, branch)),
	}
	// SP-051-2d: append " · N sub" when subagents are active. Optional
	// interface — sources that don't implement it (e.g. WebUI) get the
	// baseline footer with no change.
	if asc, ok := f.source.(activeSubagentsSource); ok {
		if n := asc.ActiveSubagents(); n > 0 {
			parts = append(parts, styleSegment(badgeColorSubagent, fmt.Sprintf("%d sub", n)))
		}
	}
	// SP-055 Phase 3b: append "⏸ N queued" when deferred steer messages
	// are waiting for the next user turn. Tells the user at a glance
	// that they'll see queued-from-prior-turn content on their next
	// prompt.
	if qms, ok := f.source.(queuedMessagesSource); ok {
		if n := qms.QueuedMessages(); n > 0 {
			parts = append(parts, styleSegment(badgeColorQueue, fmt.Sprintf("⏸ %d queued", n)))
		}
	}
	body := " " + strings.Join(parts, " · ") + " "
	if visibleLen(body) >= cols {
		return truncWithEllipsis(body, cols)
	}
	// Pad with spaces — the top hr already provides visual framing, so
	// the content row stays light. \033[K isn't enough here because the
	// surrounding color codes need to extend through the padding too.
	return body + strings.Repeat(" ", cols-visibleLen(body))
}

// styleSegment wraps a badge body with its color prefix and a reset
// back to the footer base color so the next separator stays cyan.
// Centralized here so adding new badges is a one-liner at the callsite.
func styleSegment(color, text string) string {
	return color + text + footerResetFgKeepBase
}

// styleCtxColor picks a threshold color for the context badge based on
// how full the context window is. Thresholds: <50% green, 50–80%
// yellow, >80% red. Unknown limits (limit ≤ 0) render in the base
// footer color so we don't lie about pressure.
func styleCtxColor(used, limit int) string {
	if limit <= 0 {
		return footerBaseColor
	}
	pct := float64(used) / float64(limit)
	switch {
	case pct >= 0.80:
		return badgeColorCtxAlert
	case pct >= 0.50:
		return badgeColorCtxWarn
	default:
		return badgeColorCtxOK
	}
}

// styleCost colorizes a cost string against the threshold fields. The
// closing escape pops the foreground back to footerBaseColor (cyan)
// rather than to the terminal default, so the rest of the footer line
// stays cyan after the highlighted span. SP-048-3d.
func (f *StatusFooter) styleCost(cost float64, text string) string {
	switch {
	case cost >= f.AlertCost:
		return "\033[31m" + text + footerResetFgKeepBase // red, then back to cyan
	case cost >= f.WarnCost:
		return "\033[33m" + text + footerResetFgKeepBase // yellow, then back to cyan
	default:
		return text
	}
}

// Global registration so signal handlers (which don't have a footer
// reference) can stop the footer before force-quitting via os.Exit, which
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
	if f.fd < 0 {
		return 0, 0
	}
	c, r, err := term.GetSize(f.fd)
	if err != nil {
		return 0, 0
	}
	return c, r
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func formatCtx(used, limit int) string {
	if limit <= 0 {
		return formatTokens(used) + " ctx"
	}
	return fmt.Sprintf("%s/%s ctx", formatTokens(used), formatTokens(limit))
}

func formatTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func formatCost(c float64) string {
	switch {
	case c < 0.01:
		return fmt.Sprintf("$%.4f", c)
	case c < 1.0:
		return fmt.Sprintf("$%.3f", c)
	default:
		return fmt.Sprintf("$%.2f", c)
	}
}

func shortPath(p string) string {
	if p == "" {
		return ""
	}
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func cwdSegment(cwd, branch string) string {
	if branch == "" {
		return cwd
	}
	return cwd + " (" + branch + ")"
}

// gitBranchOf returns the current git branch for the directory, or empty
// string if not a git repo or git is unavailable. Fast-fails when no
// .git is present; only shells out to git when one exists.
func gitBranchOf(dir string) string {
	if dir == "" {
		return ""
	}
	// Walk up looking for .git so subdirectories of a repo report the
	// repo's branch. Bail at filesystem root.
	probe := dir
	for {
		if _, err := os.Stat(probe + "/.git"); err == nil {
			break
		}
		parent := stripTail(probe)
		if parent == probe || parent == "" {
			return "" // not in a git repo
		}
		probe = parent
	}
	cmd := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func stripTail(p string) string {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return ""
	}
	if i == 0 {
		return "/"
	}
	return p[:i]
}

func truncTo(s string, n int) string {
	if displayWidth(s) <= n {
		return s
	}
	if n <= 1 {
		return truncateToWidth(s, n, "")
	}
	return truncateToWidth(s, n, "…")
}

// truncWithEllipsis clamps s to at most n display columns, preserving ANSI
// styling escapes (they don't count toward the budget) and cutting only on rune
// boundaries so wide/CJK content is never split. Appends "…" when it cuts.
func truncWithEllipsis(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if n == 1 {
		return " "
	}
	if visibleLen(s) <= n {
		return s
	}
	budget := n - 1 // reserve a column for the ellipsis
	var b strings.Builder
	w := 0
	inEsc := false
	for _, r := range s {
		if inEsc {
			b.WriteRune(r)
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if r == '\033' {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		rw := runewidth.RuneWidth(r)
		if w+rw > budget {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// visibleLen returns the display-column width of s, ignoring ANSI escapes
// (wide/CJK runes count as 2, combining as 0).
func visibleLen(s string) int {
	return displayWidth(s)
}
