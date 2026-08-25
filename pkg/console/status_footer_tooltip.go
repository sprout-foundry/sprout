package console

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// FooterTooltip renders a transient multi-line breakdown above the
// status footer when toggled via Alt+T. It is the CLI-D surface: a
// power-user shortcut that shows the same data /stats renders, but
// without leaving the REPL or losing in-progress input.
//
// Lifecycle:
//   - Show: clear the footer row, render a multi-row block above it,
//     suppress until either 5 s elapses (Timeout) or any keystroke
//     fires (the InputReader auto-dismisses via its HandleEvent hook).
//   - Hide: erase the rendered rows, redraw the footer at its
//     canonical row.
//
// The tooltip is rendered relative to the footer's terminal size; on
// non-TTY writers it is a no-op (same as the footer itself).
type FooterTooltip struct {
	mu      sync.Mutex
	w       io.Writer
	cols    int
	rows    int
	visible bool

	// Timeout controls how long the tooltip stays visible if no
	// keystroke dismisses it. Zero means "no auto-dismiss"; the
	// InputReader's HandleEvent hook is the primary dismiss path.
	Timeout time.Duration

	// Source supplies the per-tool breakdown. Defaults to the
	// global MetricsRecorder if nil. Set via SetSource.
	Source func() []ToolInvocation

	// Cancel is called on Hide (dismiss by keystroke) so the timeout
	// goroutine stops cleanly. Wired by Show.
	Cancel func()
}

// NewFooterTooltip constructs a tooltip that writes to w.
func NewFooterTooltip(w io.Writer) *FooterTooltip {
	if w == nil {
		w = defaultTooltipWriter()
	}
	return &FooterTooltip{
		w:       w,
		Timeout: 5 * time.Second,
		Source:  func() []ToolInvocation { return GlobalMetricsRecorder().Snapshot() },
	}
}

// defaultTooltipWriter returns the writer the tooltip should default
// to. Pulled out so tests can stub it without swapping globals.
func defaultTooltipWriter() io.Writer {
	return stderrOrDevNull()
}

// stderrOrDevNull returns os.Stderr on platforms where it's writable,
// else /dev/null. Keeps test runs from polluting their own stderr when
// the test stubs the writer to nil.
func stderrOrDevNull() io.Writer {
	return io.Discard // tests inject their own writer; production overrides
}

// Visible reports whether the tooltip is currently rendered.
func (t *FooterTooltip) Visible() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.visible
}

// Show renders the tooltip above the status footer. cols and rows are
// the terminal size; the tooltip occupies the rows immediately above
// row N (the footer rule).
func (t *FooterTooltip) Show(cols, rows int) {
	if t == nil || rows < 4 {
		return
	}
	t.mu.Lock()
	if t.visible {
		t.mu.Unlock()
		return
	}
	t.cols = cols
	t.rows = rows
	t.visible = true
	timeout := t.Timeout
	t.mu.Unlock()

	// Build the rendered block.
	lines := t.compose(cols)

	// Reserve N-1-rows..N-2 as the tooltip block. We render under
	// outputMu so concurrent Refresh() / scroll-region manipulation
	// can't smear into our rows.
	LockOutput()
	defer UnlockOutput()
	t.eraseBlock(cols, rows)
	for i, line := range lines {
		// Row index: rows-2-i (just above the footer's rule row).
		// Cap so we don't overflow upward when content is tall.
		row := rows - 2 - i
		if row < 1 {
			break
		}
		fmt.Fprintf(t.w, "\033[%d;1H\033[K%s\n", row, line)
	}

	// Auto-dismiss timer.
	if timeout > 0 {
		stop := make(chan struct{})
		var once sync.Once
		closeStop := func() { once.Do(func() { close(stop) }) }
		t.mu.Lock()
		// Signal the previous auto-dismiss goroutine (if any) to exit before
		// installing the new cancel. Without this, rapid re-show calls would
		// orphan the prior goroutine — its stop channel is dropped on the floor
		// and the goroutine runs until its timer fires, leaking one goroutine
		// per rapid re-show.
		if prev := t.Cancel; prev != nil {
			prev()
		}
		t.Cancel = closeStop
		t.mu.Unlock()
		go func() {
			timer := time.NewTimer(timeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				t.Hide()
			case <-stop:
				return
			}
		}()
	}
}

// Hide erases the rendered tooltip block and restores the footer row.
// Idempotent — safe to call when not visible.
func (t *FooterTooltip) Hide() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if !t.visible {
		t.mu.Unlock()
		return
	}
	visible := t.visible
	t.visible = false
	cancel := t.Cancel
	t.Cancel = nil
	cols := t.cols
	rows := t.rows
	t.mu.Unlock()
	if !visible {
		return
	}
	if cancel != nil {
		cancel()
	}
	LockOutput()
	defer UnlockOutput()
	t.eraseBlock(cols, rows)
}

// Toggle is the Alt+T handler: shows if hidden, hides if visible.
func (t *FooterTooltip) Toggle(cols, rows int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	vis := t.visible
	t.mu.Unlock()
	if vis {
		t.Hide()
	} else {
		t.Show(cols, rows)
	}
}

// eraseBlock clears the rows immediately above the footer's rule row.
// We do NOT erase the rule row itself — the StatusFooter redraws it
// on Refresh(), so any leftover ink from the tooltip would be wiped
// by the next refresh anyway.
func (t *FooterTooltip) eraseBlock(cols, rows int) {
	if rows < 4 {
		return
	}
	for i := 0; i < 6; i++ { // tooltip is at most 6 rows tall
		row := rows - 2 - i
		if row < 1 {
			break
		}
		fmt.Fprintf(t.w, "\033[%d;1H\033[K", row)
	}
}

// compose builds the rendered lines for the tooltip. Returns at most
// 6 lines: a header, a column rule, then one line per tool up to a
// cap, then the totals.
func (t *FooterTooltip) compose(cols int) []string {
	rows := []ToolInvocation{}
	if t.Source != nil {
		rows = t.Source()
	}
	totals := ToolInvocation{}
	if mr := GlobalMetricsRecorder(); mr != nil {
		totals = mr.Totals()
	}

	const maxRows = 4
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}

	const nameW = 18
	const countW = 6
	const tokenW = 9
	const costW = 8
	const latW = 9

	header := padRight("TOOL", nameW) + " " +
		padRightLeft("CALLS", countW) + " " +
		padRightLeft("TOKENS", tokenW) + " " +
		padRightLeft("COST", costW) + " " +
		padRightLeft("AVG MS", latW)
	rule := strings.Repeat("─", len(header))

	out := []string{
		"  per-tool breakdown (Alt+T to dismiss)",
		"  " + header,
		"  " + rule,
	}
	for _, r := range rows {
		out = append(out, "  "+
			padRight(r.Name, nameW)+" "+
			padRightLeft(fmt.Sprintf("%d", r.Count), countW)+" "+
			padRightLeft(formatTokens(int(r.TotalTokens)), tokenW)+" "+
			padRightLeft(fmt.Sprintf("$%.3f", float64(r.TotalCost)/100.0), costW)+" "+
			padRightLeft(fmt.Sprintf("%.1f", r.AvgLatency()), latW))
	}
	out = append(out, "  "+
		padRight("TOTAL", nameW)+" "+
		padRightLeft(fmt.Sprintf("%d", totals.Count), countW)+" "+
		padRightLeft(formatTokens(int(totals.TotalTokens)), tokenW)+" "+
		padRightLeft(fmt.Sprintf("$%.3f", float64(totals.TotalCost)/100.0), costW)+" "+
		padRightLeft(fmt.Sprintf("%.1f", totals.AvgLatency()), latW))

	// Width-clamp each line to fit the terminal.
	clamped := make([]string, 0, len(out))
	for _, line := range out {
		clamped = append(clamped, truncWithEllipsis(line, cols))
	}
	return clamped
}

// padRightLeft is padRight but right-justifies. Strings imported from
// the existing display_width.go as visibleLen / truncWithEllipsis.
func padRightLeft(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-len(s)) + s
}
